package gextto

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The mircrew-indexer is a direct Torznab service on the LAN. These tests lock
// the integration points found while checking it against the live service.

func TestTorznabHostIsLocal(t *testing.T) {
	for rawURL, want := range map[string]bool{
		"http://127.0.0.1:9118/api?t=caps": true,
		"http://localhost:9118/api":        true,
		"http://192.168.1.161:9118/api":    true,
		"http://10.0.0.5/api":              true,
		"http://[::1]:9118/api":            true,
		"https://indexer.example.org/api":  false,
		"http://203.0.113.7/api":           false,
		"::not a url::":                    false,
	} {
		if got := torznabHostIsLocal(rawURL); got != want {
			t.Errorf("torznabHostIsLocal(%q) = %v, want %v", rawURL, got, want)
		}
	}
}

// TestLocalTorznabTransportErrorSkipsFlareSolverr: a LAN service that drops
// the connection (the indexer's handler crashed: EOF) must not be retried
// through FlareSolverr, which cannot fix a network error and doubled the
// request on the indexer.
func TestLocalTorznabTransportErrorSkipsFlareSolverr(t *testing.T) {
	var flareCalls atomic.Int32
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flareCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer flare.Close()
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer indexer.Close()

	config := IndexerConfig{Name: "mircrew indexerr", URL: indexer.URL + "/api", APIKey: "k", Enabled: true}
	if _, err := FetchTorznabFlareSolverr(context.Background(), config, "Show S01E01", nil, &flare.URL); err == nil {
		t.Fatal("expected the transport error to be returned")
	}
	if calls := flareCalls.Load(); calls != 0 {
		t.Fatalf("FlareSolverr called %d times for a local Torznab service", calls)
	}
}

// TestAutomaticSearchBudgetCoversIndexerTimeout: the per-indexer limit only
// matters if the whole search waits longer than it (MirCrew may search its
// forum and thank a topic before answering).
func TestAutomaticSearchBudgetCoversIndexerTimeout(t *testing.T) {
	if automaticSearchTimeout <= indexerRequestTimeout {
		t.Fatalf("automaticSearchTimeout %s must exceed indexerRequestTimeout %s", automaticSearchTimeout, indexerRequestTimeout)
	}
}

// TestGh5MirCrewServiceStatusReportsForumLogin: the Torznab API keeps
// answering from the local index when the forum login fails, so the health row
// must read the service's /status as well.
func TestGh5MirCrewServiceStatusReportsForumLogin(t *testing.T) {
	var loggedIn atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server title="mircrew-indexer"/></caps>`))
		case "/status":
			if loggedIn.Load() {
				_, _ = w.Write([]byte(`{"ok": true, "logged_in": true, "login_error": "", "last_error": "old"}`))
			} else {
				_, _ = w.Write([]byte(`{"ok": true, "logged_in": false, "login_error": "wrong password"}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := DefaultConfig()
	cfg.Indexers = []IndexerConfig{{Name: "mircrew indexerr", URL: server.URL + "/api", APIKey: "k", Enabled: true}}

	reset := func() {
		gh5_mirCrewProbe.Lock()
		gh5_mirCrewProbe.entry = nil
		gh5_mirCrewProbe.Unlock()
	}
	reset()
	status := gh5_mirCrewServiceStatus(&cfg)
	if status == nil || !strings.Contains(status.UserMessage, "login al forum MirCrew non è riuscito: wrong password") || !strings.Contains(status.SuggestedAction, "Rifai login") {
		t.Fatalf("failed forum login not reported: %#v", status)
	}

	loggedIn.Store(true)
	reset()
	if status = gh5_mirCrewServiceStatus(&cfg); status == nil || status.UserMessage != "Servizio mircrew-indexer attivo e raggiungibile." {
		t.Fatalf("healthy service misreported: %#v", status)
	}

	// Right after the service starts it is still logging in: not a failure.
	starting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok": true, "logged_in": false, "login_error": "", "last_run": ""}`))
	}))
	defer starting.Close()
	if _, failed := gh5_mirCrewLoginState(starting.URL); failed {
		t.Fatal("a service still logging in must not be reported as failed")
	}
	lost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok": true, "logged_in": false, "login_error": "", "last_run": "2026-10-08T11:17:51Z"}`))
	}))
	defer lost.Close()
	if _, failed := gh5_mirCrewLoginState(lost.URL); !failed {
		t.Fatal("a login lost after a completed pass must be reported")
	}

	// An older service without /status is simply reachable.
	if _, failed := gh5_mirCrewLoginState(server.URL + "/missing"); failed {
		t.Fatal("a service without /status must not be reported as failed")
	}
}

// TestMirCrewTorznabItemIsReadAsIs uses the exact XML the fixed indexer emits
// (generated by mircrew_indexer.core.torznab_search): no seeders attribute
// must mean "unknown" (-1), not a dead torrent, and the topic date must become
// the release date used by the maximum-age filter.
func TestMirCrewTorznabItemIsReadAsIs(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><title>mircrew-indexer</title><response offset="0" total="1"/><item><title>FBI.Most.Wanted-1x02-Conseguenze.DLMux-1080p-x264-AC3.ITA-ENG.mkv</title><pubDate>Mon, 05 Oct 2026 10:17:44 +0000</pubDate><guid isPermaLink="false">0123456789abcdef0123456789abcdef01234567</guid><link>magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=FBI.Most.Wanted-1x02</link><category>5000</category><enclosure url="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=FBI.Most.Wanted-1x02" length="0" type="application/x-bittorrent"/><torznab:attr name="category" value="5000"/><torznab:attr name="infohash" value="0123456789abcdef0123456789abcdef01234567"/><torznab:attr name="season" value="1"/><torznab:attr name="episode" value="2"/></item></channel></rss>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(feed))
	}))
	defer server.Close()
	indexer := IndexerConfig{Name: "mircrew indexerr", URL: server.URL + "/api", APIKey: "k", Enabled: true}
	releases, err := FetchTorznab(context.Background(), indexer, "FBI Most Wanted S01E02")
	if err != nil || len(releases) != 1 {
		t.Fatalf("releases = %d, err = %v", len(releases), err)
	}
	release := releases[0]
	if release.Seeders != -1 {
		t.Fatalf("seeders = %d, want -1 (unknown)", release.Seeders)
	}
	if release.Season == nil || *release.Season != 1 || release.Episode == nil || *release.Episode != 2 {
		t.Fatalf("episode = %v/%v, want S01E02", release.Season, release.Episode)
	}
	if release.Quality.Source != "webdl" || !release.Quality.IsIta {
		t.Fatalf("quality = %+v, want Italian WEB-DL (DLMux)", release.Quality)
	}
	if got := release.DiscoveredAt.UTC().Format("2006-01-02 15:04"); got != "2026-10-05 10:17" {
		t.Fatalf("release date = %s, want the topic date 2026-10-05 10:17", got)
	}
}
