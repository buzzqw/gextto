package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestEngineCooldownBlocksAfterAFailure(t *testing.T) {
	// The cooldown map is process-global: clear this test's entry before and
	// after the assertion so the test stays idempotent under `go test -count=N`.
	clearCooldown := func() {
		engineCooldownMu.Lock()
		delete(engineCooldown, "test-cooldown-engine")
		engineCooldownMu.Unlock()
	}
	clearCooldown()
	t.Cleanup(clearCooldown)

	if engineInCooldown("test-cooldown-engine") {
		t.Fatal("engine should not start in cooldown")
	}
	setEngineCooldown("test-cooldown-engine", "HTTP 429 Too Many Requests")
	if !engineInCooldown("test-cooldown-engine") {
		t.Fatal("engine should be in cooldown after a 429")
	}
	// Un motore diverso non è toccato.
	if engineInCooldown("test-cooldown-other") {
		t.Fatal("unrelated engine must not be in cooldown")
	}
}

func TestBuildsSanitizedMagnetWithTrackers(t *testing.T) {
	magnet := buildMagnet("0123456789012345678901234567890123456789", "Example Show")
	if magnet == "" {
		t.Fatal("build_magnet returned no magnet")
	}
	if !strings.Contains(magnet, "xt=urn:btih:0123456789012345678901234567890123456789") {
		t.Fatalf("magnet = %q", magnet)
	}
	if !strings.Contains(magnet, "&dn=Example%20Show") && !strings.Contains(magnet, "&dn=Example+Show") {
		t.Fatalf("magnet missing display name: %q", magnet)
	}
	if !strings.Contains(magnet, "&tr=") {
		t.Fatalf("magnet missing trackers: %q", magnet)
	}
}

func TestExtractsAndSanitizesHTMLMagnet(t *testing.T) {
	body := `<a href="magnet:?xt=urn:btih:0123456789012345678901234567890123456789&dn=Example%20Show&x=bad space">download</a>`
	magnet := firstMagnet(body)
	if !strings.HasPrefix(magnet, "magnet:?xt=urn:btih:") {
		t.Fatalf("magnet = %q", magnet)
	}
	if strings.Contains(magnet, "bad space") {
		t.Fatalf("magnet should be sanitized: %q", magnet)
	}
}

func resetCloudflareMemoryForTest(t *testing.T) {
	t.Helper()
	cfSessionsMu.Lock()
	cfSessions = map[string]cfSession{}
	cfSessionsMu.Unlock()
	fsSessionMu.Lock()
	fsSessionByDomain = map[string]fsSessionEntry{}
	fsSessionDisabled = map[string]bool{}
	fsSessionMu.Unlock()
	ConfigureCloudflareState(t.TempDir())
}

func TestWebHTMLReusesFlareSolverrSession(t *testing.T) {
	resetCloudflareMemoryForTest(t)
	var targetCalls atomic.Int32
	var flareCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		if r.Header.Get("Cookie") == "cf_clearance=ok" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>solved target page</body></html>"))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><title>Just a moment...</title></html>"))
	}))
	defer target.Close()
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flareCalls.Add(1)
		if r.URL.Path != "/v1" {
			http.NotFound(w, r)
			return
		}
		var command struct {
			Cmd string `json:"cmd"`
		}
		_ = json.NewDecoder(r.Body).Decode(&command)
		if command.Cmd == "sessions.create" {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "session": "sess-1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"solution": map[string]any{
				"userAgent": "TestBrowser/1.0",
				"cookies":   []map[string]string{{"name": "cf_clearance", "value": "ok"}},
				"response":  "<html><body>solved by browser</body></html>",
			},
		})
	}))
	defer flare.Close()

	body, err := fetchHtml(context.Background(), target.URL, &flare.URL)
	if err != nil || !strings.Contains(body, "solved by browser") {
		t.Fatalf("first fetch = %q, %v", body, err)
	}
	body, err = fetchHtml(context.Background(), target.URL, &flare.URL)
	if err != nil || !strings.Contains(body, "solved target page") {
		t.Fatalf("second fetch = %q, %v", body, err)
	}
	// One session handshake plus one solve; the second fetch reuses the solved
	// cookies and never calls FlareSolverr again.
	if got := flareCalls.Load(); got != 2 {
		t.Fatalf("FlareSolverr calls = %d, want 2", got)
	}
	if got := targetCalls.Load(); got != 2 {
		t.Fatalf("target calls = %d, want 2", got)
	}
}

func TestFlareSolverrReusesPersistentBrowserSession(t *testing.T) {
	resetCloudflareMemoryForTest(t)
	var createCalls atomic.Int32
	var getCalls atomic.Int32
	var sessionsSeen []string
	var mu sync.Mutex
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always challenge, so every fetch must go through FlareSolverr.
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><title>Just a moment...</title></html>"))
	}))
	defer target.Close()
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var command struct {
			Cmd     string `json:"cmd"`
			Session string `json:"session"`
		}
		_ = json.NewDecoder(r.Body).Decode(&command)
		switch command.Cmd {
		case "sessions.create":
			createCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "session": "sess-42"})
		default:
			getCalls.Add(1)
			mu.Lock()
			sessionsSeen = append(sessionsSeen, command.Session)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":   "ok",
				"solution": map[string]any{"response": "<html><body>solved</body></html>"},
			})
		}
	}))
	defer flare.Close()

	for i := 0; i < 2; i++ {
		if _, err := fetchHtml(context.Background(), target.URL, &flare.URL); err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
	}
	if got := createCalls.Load(); got != 1 {
		t.Fatalf("sessions.create calls = %d, want 1", got)
	}
	if got := getCalls.Load(); got != 2 {
		t.Fatalf("request.get calls = %d, want 2", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, session := range sessionsSeen {
		if session != "sess-42" {
			t.Fatalf("request.get %d used session %q, want sess-42", i, session)
		}
	}
}

func TestWebHTMLDoesNotUseFlareSolverrForNonCloudflareStatuses(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			resetCloudflareMemoryForTest(t)
			var flareCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer target.Close()
			flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				flareCalls.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer flare.Close()

			_, err := fetchHtml(context.Background(), target.URL, &flare.URL)
			if err == nil || !strings.Contains(err.Error(), http.StatusText(status)) && !strings.Contains(err.Error(), "HTTP ") {
				t.Fatalf("fetch error = %v", err)
			}
			if got := flareCalls.Load(); got != 0 {
				t.Fatalf("FlareSolverr calls for HTTP %d = %d", status, got)
			}
		})
	}
}

// TestLimetorrentsSearchUsesTheFormEndpoint pins the URL of the engine: the
// old path form (/search/all/<query>/seeds/1/) now answers 302 to http and
// 301 back to https, so the engine reported itself unreachable at every cycle.
func TestLimetorrentsSearchUsesTheFormEndpoint(t *testing.T) {
	got := limetorrentsSearchURL("https://limetorrent.store/", false, "matrix resurrections")
	base, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse %q: %v", got, err)
	}
	if base.Host != "limetorrent.store" || base.Path != "/search.php" {
		t.Fatalf("search URL = %q, want https://limetorrent.store/search.php", got)
	}
	values := base.Query()
	if values.Get("q") != "matrix resurrections" {
		t.Fatalf("q = %q, want the query unchanged", values.Get("q"))
	}
	if values.Get("order") != "seeders" || values.Get("orderby") != "DESC" {
		t.Fatalf("sort = %q/%q, want DESC by seeders", values.Get("orderby"), values.Get("order"))
	}
	if strings.Contains(got, "/search/all/") {
		t.Fatalf("search URL still uses the redirecting path form: %q", got)
	}
}

// TestLimetorrentsDetailsDropsDuplicateRows checks the parser against the
// shape of the real page: every row links the title and the magnet to the same
// detail URL, and eight rows mean eight torrents, not four.
func TestLimetorrentsDetailsDropsDuplicateRows(t *testing.T) {
	var page strings.Builder
	page.WriteString(`<html><body><table class="table2"><tbody>`)
	for i := 1; i <= 12; i++ {
		href := fmt.Sprintf("https://limetorrent.store/example-release-%d.html", i)
		page.WriteString(`<tr><td><a href="` + href + `" class="tt-name">Example release ` + strconv.Itoa(i) + `</a></td>`)
		page.WriteString(`<td class="tdleft"><a href="` + href + `"><img src="/dl.png" alt=""></a></td></tr>`)
	}
	page.WriteString(`</tbody></table></body></html>`)

	base, err := url.Parse(limetorrentsSearchURL("https://limetorrent.store", false, "example"))
	if err != nil {
		t.Fatal(err)
	}
	details, err := limetorrentsDetails(page.String(), base)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(details) != 8 {
		t.Fatalf("details = %d, want the cap of 8 distinct torrents", len(details))
	}
	if details[0].title != "Example release 1" || !strings.HasSuffix(details[0].url, "/example-release-1.html") {
		t.Fatalf("first detail = %+v", details[0])
	}
	for i, detail := range details {
		if detail.url != "https://limetorrent.store/example-release-"+strconv.Itoa(i+1)+".html" {
			t.Fatalf("detail %d = %q: rows are duplicated or out of order", i, detail.url)
		}
	}
}

// limetorrentsTestServer serves a search page with one row and a detail page
// with (or without) a magnet, like the real site does.
func limetorrentsTestServer(t *testing.T, withMagnet bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	const hash = "0123456789abcdef0123456789abcdef01234567"
	searchPage := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><table class="table2"><tbody><tr>
			<td><div class="tt-name"><a href="/example-release-1.html">Example release</a></div></td>
			</tr></tbody></table></body></html>`)
	}
	mux.HandleFunc("/search.php", searchPage)
	mux.HandleFunc("/search/all/", searchPage)
	mux.HandleFunc("/example-release-1.html", func(w http.ResponseWriter, r *http.Request) {
		if withMagnet {
			fmt.Fprintf(w, `<html><body><a href="magnet:?xt=urn:btih:%s">Get this torrent</a></body></html>`, hash)
			return
		}
		// The limetorrent.net shape: the detail page hands the magnet to another
		// host and carries none of its own.
		fmt.Fprint(w, `<html><body><a href="https://limetorrent.store/example-release-1.html">Get this torrent</a></body></html>`)
	})
	return server
}

// TestLimetorrentsFallsBackToTheNextMirror checks the two ways a mirror is
// useless, both of which must move on to the next one instead of ending the
// search: a host that errors, and a host whose search page has no magnet in
// its detail pages (what limetorrent.net does today).
func TestLimetorrentsFallsBackToTheNextMirror(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)
	empty := limetorrentsTestServer(t, false)
	working := limetorrentsTestServer(t, true)

	for _, tc := range []struct {
		name    string
		mirrors []limetorrentsMirror
	}{
		{"host that errors", []limetorrentsMirror{{host: broken.URL}, {host: working.URL}}},
		{"host without magnets", []limetorrentsMirror{{host: empty.URL}, {host: working.URL}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := limetorrentsMirrors
			limetorrentsMirrors = tc.mirrors
			t.Cleanup(func() { limetorrentsMirrors = previous })

			results, err := searchLimetorrents(context.Background(), "example", nil)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("results = %d, want 1 from the second mirror", len(results))
			}
			if results[0].source != "LimeTorrents" || !strings.HasPrefix(results[0].magnet, "magnet:?xt=urn:btih:") {
				t.Fatalf("result = %+v", results[0])
			}
		})
	}
}

// TestLimetorrentsReportsFailureWhenEveryMirrorFails keeps the warning honest:
// with nothing from any mirror, the error reaches the cycle and the engine is
// reported unreachable instead of silently finding nothing.
func TestLimetorrentsReportsFailureWhenEveryMirrorFails(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)

	previous := limetorrentsMirrors
	limetorrentsMirrors = []limetorrentsMirror{{host: broken.URL}, {host: broken.URL}}
	t.Cleanup(func() { limetorrentsMirrors = previous })

	results, err := searchLimetorrents(context.Background(), "example", nil)
	if err == nil {
		t.Fatalf("no error and %d results, want the failure reported", len(results))
	}
	if results != nil {
		t.Fatalf("results = %+v, want none", results)
	}
}

// TestLimetorrentsMirrorForms pins each configured host and the search URL it
// speaks: the store and net hosts answer /search.php, www.limetorrents.fun only
// the older path form (it 404s on /search.php). A mirror whose form is wrong
// costs a failed request in every cycle.
func TestLimetorrentsMirrorForms(t *testing.T) {
	hosts := map[string]limetorrentsMirror{}
	for _, mirror := range limetorrentsMirrors {
		hosts[mirror.host] = mirror
	}
	for _, host := range []string{"https://limetorrent.store", "https://www.limetorrents.fun", "https://limetorrent.net"} {
		if _, ok := hosts[host]; !ok {
			t.Fatalf("mirror %q is missing from the list", host)
		}
	}
	if hosts["https://www.limetorrents.fun"].pathForm != true {
		t.Fatal("www.limetorrents.fun must use the path form: /search.php is a 404 there")
	}
	if hosts["https://limetorrent.store"].pathForm != false {
		t.Fatal("limetorrent.store must use /search.php")
	}

	got := limetorrentsSearchURL("https://www.limetorrents.fun", true, "matrix resurrections")
	if got != "https://www.limetorrents.fun/search/all/matrix-resurrections/seeds/1/" {
		t.Fatalf("path-form search URL = %q", got)
	}
}

// TestWebEngineOptionsMatchTheKnownOnes keeps the checkboxes of the settings
// page and the filter that runs a search in step. Removing a dead engine from
// one list and not the other either offers a choice that does nothing or leaves
// a value nobody can tick; Torrentz2 was retired on 2026-11-10 because every
// address of the service was parked or gone.
func TestWebEngineOptionsMatchTheKnownOnes(t *testing.T) {
	offered := make(map[string]bool, len(uiWebsearchEngines))
	for _, option := range uiWebsearchEngines {
		offered[option.Value] = true
		if !knownWebEngine(option.Value) {
			t.Errorf("%q is offered in the settings but the search does not know it", option.Value)
		}
	}
	for _, engine := range []string{"bitsearch", "tpb", "1337x", "bt4g", "knaben",
		"nyaa", "eztv", "btdig", "limetorrents", "torrentscsv"} {
		if !offered[engine] {
			t.Errorf("%q runs a search but cannot be ticked", engine)
		}
	}
	if offered["torrentz2"] || knownWebEngine("torrentz2") {
		t.Error("torrentz2 is back: every Torrentz2 address is parked or gone")
	}
}
