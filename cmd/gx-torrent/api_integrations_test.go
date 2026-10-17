package main

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/settings"
)

// TestMetricsArePrometheusText pins the metrics endpoint: the Prometheus text
// content type, a HELP and a TYPE line for every family, well-formed samples,
// and the families a dashboard relies on.
func TestMetricsArePrometheusText(t *testing.T) {
	d := newTestDaemon(t)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp := mustGet(t, srv.URL+"/api/v1/metrics")
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("content type %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	sample := regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*(\{[^}]*\})? -?[0-9.eE+-]+$`)
	help, typ := map[string]bool{}, map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		switch {
		case strings.HasPrefix(line, "# HELP "):
			help[strings.Fields(line)[2]] = true
		case strings.HasPrefix(line, "# TYPE "):
			fields := strings.Fields(line)
			typ[fields[2]] = fields[3]
		case !sample.MatchString(line):
			t.Errorf("malformed sample line %q", line)
		}
	}
	// Prometheus naming: a counter ends in _total and nothing else does, so
	// rate() and the dashboards interpret every family the right way.
	for family, kind := range typ {
		switch {
		case kind != "gauge" && kind != "counter":
			t.Errorf("family %s has type %q, want gauge or counter", family, kind)
		case kind == "counter" && !strings.HasSuffix(family, "_total"):
			t.Errorf("counter family %s does not end in _total", family)
		case kind == "gauge" && strings.HasSuffix(family, "_total"):
			t.Errorf("gauge family %s ends in _total (reserved for counters)", family)
		}
	}
	for _, family := range []string{
		"gxtorrent_info", "gxtorrent_download_rate_bytes_per_second", "gxtorrent_upload_rate_bytes_per_second",
		"gxtorrent_torrents", "gxtorrent_torrents_by_state", "gxtorrent_peers", "gxtorrent_lifetime_downloaded_bytes_total",
		"gxtorrent_disk_free_bytes", "process_resident_memory_bytes", "process_cpu_seconds_total", "process_start_time_seconds",
	} {
		if !help[family] || typ[family] == "" {
			t.Errorf("family %s missing its HELP/TYPE", family)
		}
	}
	if !strings.Contains(text, `gxtorrent_torrents_by_state{state="seeding"}`) {
		t.Error("torrents by state not labelled")
	}
}

// TestEventsStreamOpens pins the token API event stream: Server-Sent Events
// with the opening comment, ready for a subscriber.
func TestEventsStreamOpens(t *testing.T) {
	d := newTestDaemon(t)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, ": gx-torrent events") {
		t.Fatalf("first line %q, %v", line, err)
	}
}

// TestEventsStreamDeliversAndFollowsSession pins the two properties the stream
// must have: an engine event reaches the client, and when the session is
// replaced the stream ends (the client reconnects and subscribes to the new
// one) instead of staying attached to a session that will never emit again.
func TestEventsStreamDeliversAndFollowsSession(t *testing.T) {
	d := newTestDaemon(t)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, ": gx-torrent events") {
		t.Fatalf("opening line %q, %v", line, err)
	}

	src := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 20_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	if line := readEventDataLine(t, reader); !strings.Contains(line, "torrent_added") {
		t.Fatalf("first event %q, want torrent_added", line)
	}

	// A setting that only the engine reads reopens the session: the stream
	// must end rather than keep the subscription of the closed session.
	d.mu.Lock()
	d.restartSessionLocked()
	d.mu.Unlock()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("stream did not end after the session restart: %v", err)
	}
}

// readEventDataLine returns the next "data:" line, skipping comments and blank
// lines (the keep-alive).
func readEventDataLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the event stream: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			return line
		}
	}
}

// TestQbitCountAndSpeedLimitsMode pins torrents/count and the explicit
// setSpeedLimitsMode, read back through speedLimitsMode.
func TestQbitCountAndSpeedLimitsMode(t *testing.T) {
	store, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := newTestDaemonFull(t, NetworkOptions{PortBegin: 42000, PortEnd: 42100, Encryption: 1, PEX: true}, store)
	src := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 20_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()
	resp := mustGet(t, srv.URL+"/api/v2/torrents/count")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(body)) != "1" {
		t.Fatalf("count = %q, want 1", body)
	}
	bad, err := http.PostForm(srv.URL+"/api/v2/transfer/setSpeedLimitsMode", url.Values{"mode": {"2"}})
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("mode=2 -> %d, want 400", bad.StatusCode)
	}
	for _, mode := range []string{"1", "0"} {
		resp, err := http.PostForm(srv.URL+"/api/v2/transfer/setSpeedLimitsMode", url.Values{"mode": {mode}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		got := mustGet(t, srv.URL+"/api/v2/transfer/speedLimitsMode")
		value, _ := io.ReadAll(got.Body)
		got.Body.Close()
		if strings.TrimSpace(string(value)) != mode {
			t.Fatalf("after setSpeedLimitsMode %s the mode is %q", mode, value)
		}
	}
}

// TestQbitWebSeedEditing pins addWebSeeds, editWebSeed and removeWebSeeds, and
// their errors: an unknown torrent (404), an invalid URL (400), an edit of a
// web seed the torrent does not have (409).
func TestQbitWebSeedEditing(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 20_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()
	post := func(path string, form url.Values) int {
		resp, err := http.PostForm(srv.URL+path, form)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	seeds := func() []string {
		var out []qbitWebseed
		getJSONInto(t, srv.URL+"/api/v2/torrents/webseeds?hash="+hash, &out)
		urls := make([]string, 0, len(out))
		for _, seed := range out {
			urls = append(urls, seed.URL)
		}
		return urls
	}
	waitSeeds := func(want string) {
		deadline := time.Now().Add(3 * time.Second)
		for {
			got := strings.Join(seeds(), " ")
			if got == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("web seeds = %q, want %q", got, want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if code := post("/api/v2/torrents/addWebSeeds", url.Values{"hash": {"0000000000000000000000000000000000000000"}, "urls": {"http://a.example/x"}}); code != http.StatusNotFound {
		t.Fatalf("unknown torrent -> %d, want 404", code)
	}
	if code := post("/api/v2/torrents/addWebSeeds", url.Values{"hash": {hash}, "urls": {"ftp://a.example/x"}}); code != http.StatusBadRequest {
		t.Fatalf("invalid URL -> %d, want 400", code)
	}
	if code := post("/api/v2/torrents/addWebSeeds", url.Values{"hash": {hash}, "urls": {"http://a.example/movie.bin|https://b.example/movie.bin"}}); code != http.StatusOK {
		t.Fatalf("addWebSeeds -> %d", code)
	}
	waitSeeds("http://a.example/movie.bin https://b.example/movie.bin")
	if code := post("/api/v2/torrents/editWebSeed", url.Values{"hash": {hash}, "origUrl": {"http://nope.example/x"}, "newUrl": {"http://c.example/movie.bin"}}); code != http.StatusConflict {
		t.Fatalf("edit of an unknown web seed -> %d, want 409", code)
	}
	if code := post("/api/v2/torrents/editWebSeed", url.Values{"hash": {hash}, "origUrl": {"http://a.example/movie.bin"}, "newUrl": {"http://c.example/movie.bin"}}); code != http.StatusOK {
		t.Fatalf("editWebSeed -> %d", code)
	}
	waitSeeds("https://b.example/movie.bin http://c.example/movie.bin")
	if code := post("/api/v2/torrents/removeWebSeeds", url.Values{"hash": {hash}, "urls": {"https://b.example/movie.bin|http://c.example/movie.bin"}}); code != http.StatusOK {
		t.Fatalf("removeWebSeeds -> %d", code)
	}
	waitSeeds("")
}

// BenchmarkMetricsScrape measures one Prometheus scrape of a daemon with one
// torrent: the cost is paid only when a scraper asks.
func BenchmarkMetricsScrape(b *testing.B) {
	d := newTestDaemonBench(b)
	handler := d.routes()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func newTestDaemonBench(b *testing.B) *Daemon {
	b.Helper()
	return newTestDaemonFull(b, NetworkOptions{PortBegin: 42500, PortEnd: 42600, Encryption: 1, PEX: true}, nil)
}
