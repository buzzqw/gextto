package gextto

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/buzzqw/gextto/internal/utils"
)

func TestFetchTorrentRedirectsToMagnet(t *testing.T) {
	magnetExpected := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Minions+%26+Monsters+2026&tr=udp%3A%2F%2Ftracker.example.com%3A1337%2Fannounce"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dl/minions":
			http.Redirect(w, r, magnetExpected, http.StatusFound)
		case "/hop1":
			http.Redirect(w, r, "/dl/minions", http.StatusMovedPermanently)
		case "/plain-magnet":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(magnetExpected))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	engine := NewEngine()
	ctx := context.Background()

	// 1. Direct redirect to magnet
	payload, magnet, err := engine.FetchTorrent(ctx, server.URL+"/dl/minions")
	if err != nil {
		t.Fatalf("FetchTorrent direct redirect failed: %v", err)
	}
	if len(payload) > 0 {
		t.Fatalf("expected nil payload, got %d bytes", len(payload))
	}
	if magnet != magnetExpected {
		t.Fatalf("got magnet %q, want %q", magnet, magnetExpected)
	}

	// 2. Multi-hop redirect to magnet
	payload, magnet, err = engine.FetchTorrent(ctx, server.URL+"/hop1")
	if err != nil {
		t.Fatalf("FetchTorrent multi-hop redirect failed: %v", err)
	}
	if len(payload) > 0 {
		t.Fatalf("expected nil payload, got %d bytes", len(payload))
	}
	if magnet != magnetExpected {
		t.Fatalf("got magnet %q, want %q", magnet, magnetExpected)
	}

	// 3. Plain text magnet body
	payload, magnet, err = engine.FetchTorrent(ctx, server.URL+"/plain-magnet")
	if err != nil {
		t.Fatalf("FetchTorrent plain magnet body failed: %v", err)
	}
	if len(payload) > 0 {
		t.Fatalf("expected nil payload, got %d bytes", len(payload))
	}
	if magnet != magnetExpected {
		t.Fatalf("got magnet %q, want %q", magnet, magnetExpected)
	}

	// 4. Direct magnet URL passed to FetchTorrent
	payload, magnet, err = engine.FetchTorrent(ctx, magnetExpected)
	if err != nil {
		t.Fatalf("FetchTorrent direct magnet failed: %v", err)
	}
	if magnet != magnetExpected {
		t.Fatalf("got magnet %q, want %q", magnet, magnetExpected)
	}
}

func TestResolveTorrentURLRedirectToMagnet(t *testing.T) {
	magnetTarget := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Minions+%26+Monsters+2026&tr=udp%3A%2F%2Ftracker.example.com%3A1337%2Fannounce"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, magnetTarget, http.StatusFound)
	}))
	defer server.Close()

	engine := NewEngine()
	cfg := &Config{StateDir: t.TempDir()}
	title := "Minions & Monsters"

	// Resolve the redirect URL ending in .torrent
	resolvedMagnet, path, err := resolveTorrentURL(context.Background(), engine, cfg, server.URL+"/dl/download.torrent", &title)
	if err != nil {
		t.Fatalf("resolveTorrentURL failed: %v", err)
	}
	if path != "" {
		t.Fatalf("expected empty path for magnet resolution, got %q", path)
	}
	hash, ok := utils.MagnetHash(resolvedMagnet)
	if !ok || !strings.EqualFold(hash, "0123456789abcdef0123456789abcdef01234567") {
		t.Fatalf("unexpected hash from resolved magnet: %q", resolvedMagnet)
	}
}

func TestRunCycleDomainResolvesTorrentURLRedirectToMagnet(t *testing.T) {
	magnetTarget := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Monitored.Show.S01E01.1080p.WEB-DL&tr=udp%3A%2F%2Ftracker.example.com%3A1337%2Fannounce"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.xml":
			xml := fmt.Sprintf(`<rss><channel>
<item>
<title>Monitored.Show.S01E01.1080p.WEB-DL</title>
<link>%s/download/item.torrent</link>
</item>
</channel></rss>`, server.URL)
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(xml))
		case "/download/item.torrent":
			http.Redirect(w, r, magnetTarget, http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	state := newCycleState(t)
	cfg := *state.cfg
	cfg.Series = []SeriesConfig{{Name: "Monitored Show", Enabled: true}}
	cfg.FeedURLs = []string{server.URL + "/feed.xml"}
	cfg.Indexers = nil
	cfg.WebsearchEngines = nil
	cfg.DryRun = true

	domain := "series"
	stats, err := RunCycleDomain(
		context.Background(),
		&cfg,
		state.engine,
		state.db,
		state.archive,
		state.comics,
		state.notifier,
		state.activeEngine(),
		&domain,
	)
	if err != nil {
		t.Fatalf("RunCycleDomain: %v", err)
	}
	if stats == nil {
		t.Fatal("RunCycleDomain returned nil stats")
	}
	if stats.Scraped < 1 {
		t.Fatalf("Scraped = %d, want at least 1", stats.Scraped)
	}
	if stats.DownloadsStarted != 1 {
		t.Fatalf("DownloadsStarted = %d, want 1", stats.DownloadsStarted)
	}
}

// TestFetchTorrentErrorNamesTheHostAndPort: all the indexers can live on one
// machine (Jackett on 9117, Prowlarr on 9696, a Torznab service on 9118), so a
// 502 must say which one answered. The path and query carry the API key and
// must stay out of the message.
func TestFetchTorrentErrorNamesTheHostAndPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	engine := NewEngine()
	_, _, err := engine.FetchTorrent(context.Background(), server.URL+"/api/v2.0/indexers/tracker/results/torrents?apikey=SECRET")
	if err == nil {
		t.Fatal("a 502 must be an error")
	}
	host := strings.TrimPrefix(server.URL, "http://")
	if !strings.Contains(err.Error(), host) {
		t.Fatalf("error %q does not name %s", err, host)
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/api/") {
		t.Fatalf("error %q leaks the URL path or the API key", err)
	}
}

// TestFetchTorrentErrorCarriesTheProxyReason: a non-2xx answer from an indexer
// proxy carries its own explanation (mircrew: "magnet not available: MirCrew
// login failed: …"). The log and the indexer-failure notification must show
// that sentence, not only the status code; the text is one line and never
// echoes an API key.
func TestFetchTorrentErrorCarriesTheProxyReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("magnet not available: MirCrew login failed: credenziali rifiutate (apikey=SECRET)\nretry later"))
	}))
	defer server.Close()

	engine := NewEngine()
	_, _, err := engine.FetchTorrent(context.Background(), server.URL+"/dl/topic")
	if err == nil {
		t.Fatal("a 502 must be an error")
	}
	if !strings.Contains(err.Error(), "MirCrew login failed") {
		t.Fatalf("error %q lost the proxy reason", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error %q leaks the API key", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("error %q is not one line", err)
	}
}

// A "no magnet for this request" answer is not a dead link: mircrew says it for
// a topic whose magnet is not stored yet (the first post must be thanked), and
// the link becomes usable as soon as that changes.
func TestFetchTorrentNoMagnetIsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no magnet for this request"))
	}))
	defer server.Close()

	engine := NewEngine()
	_, _, err := engine.FetchTorrent(context.Background(), server.URL+"/dl/235774.torrent")
	if !errors.Is(err, errTorrentNotAvailable) {
		t.Fatalf("404 no-magnet should be transient, got %v", err)
	}
}

// A login that needs renewing is a source-wide transient too, not a dead link.
func TestFetchTorrentLoginFailureIsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("magnet not available: MirCrew login failed: credenziali rifiutate"))
	}))
	defer server.Close()

	engine := NewEngine()
	_, _, err := engine.FetchTorrent(context.Background(), server.URL+"/dl/topic")
	if !errors.Is(err, errTorrentNotAvailable) {
		t.Fatalf("a login failure should be transient, got %v", err)
	}
}

// A plain 404 is still a dead link: the quarantine must keep working.
func TestFetchTorrentPlainNotFoundIsNotTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	engine := NewEngine()
	_, _, err := engine.FetchTorrent(context.Background(), server.URL+"/dl/gone")
	if err == nil || errors.Is(err, errTorrentNotAvailable) {
		t.Fatalf("a plain 404 should stay a dead link, got %v", err)
	}
}

// TestRunCycleOpensDeclaredHashLinksOnlyForTheDownload pins the deferred link:
// a candidate whose infohash the indexer declared is decided on that hash, and
// its link is opened only when it is downloaded. A link without a declared
// hash is judged on its title first and opened only when it would be. An aggregator that counts
// downloads (indexarr) must not count the candidates already in the library.
func TestRunCycleOpensDeclaredHashLinksOnlyForTheDownload(t *testing.T) {
	const libraryHash = "1111111111111111111111111111111111111111"
	const newHash = "2222222222222222222222222222222222222222"
	opened := map[string]int{}
	var mu sync.Mutex
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/feed.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>Show.S01E01.1080p.WEB-DL.H.265</title><link>%[1]s/dl/1.torrent</link><size>900000000</size><torznab:attr name="infohash" value="%[2]s"/></item>
<item><title>Show.S01E02.1080p.WEB-DL.H.265</title><link>%[1]s/dl/2.torrent</link><size>900000000</size><torznab:attr name="infohash" value="%[3]s"/></item>
<item><title>Show.S01E01.720p.WEB-DL.H.264</title><link>%[1]s/dl/3.torrent</link><size>700000000</size></item>
<item><title>Show.S01E03.1080p.WEB-DL.H.265</title><link>%[1]s/dl/4.torrent</link><size>900000000</size></item>
</channel></rss>`, server.URL, libraryHash, newHash)
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			mu.Lock()
			opened[r.URL.Path]++
			mu.Unlock()
			hash := map[string]string{"/dl/1.torrent": libraryHash, "/dl/2.torrent": newHash,
				"/dl/3.torrent": "3333333333333333333333333333333333333333", "/dl/4.torrent": "4444444444444444444444444444444444444444"}[r.URL.Path]
			http.Redirect(w, r, "magnet:?xt=urn:btih:"+hash+"&dn=Show&tr=udp%3A%2F%2Ftracker.example.com%3A1337%2Fannounce", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	state := newCycleState(t)
	if _, err := state.db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Show')"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(id,series_id,season,episode,title,quality_score,downloaded_at) VALUES (1,1,1,1,'Show.S01E01.1080p.WEB-DL.H.265',1900,datetime('now'))"); err != nil {
		t.Fatal(err)
	}
	cfg := *state.cfg
	cfg.Series = []SeriesConfig{{Name: "Show", Enabled: true}}
	cfg.FeedURLs = []string{server.URL + "/feed.xml"}
	cfg.Indexers = nil
	cfg.WebsearchEngines = nil
	cfg.DryRun = true

	domain := "series"
	stats, err := RunCycleDomain(context.Background(), &cfg, state.engine, state.db, state.archive, state.comics, state.notifier, state.activeEngine(), &domain)
	if err != nil {
		t.Fatalf("RunCycleDomain: %v", err)
	}
	if stats.DownloadsStarted != 2 {
		t.Fatalf("DownloadsStarted = %d, want 2", stats.DownloadsStarted)
	}
	mu.Lock()
	defer mu.Unlock()
	// 1 and 3 are S01E01, already in the library; 2 declares its hash and 4
	// does not, and both are downloaded.
	if opened["/dl/1.torrent"] != 0 || opened["/dl/3.torrent"] != 0 || opened["/dl/2.torrent"] != 1 || opened["/dl/4.torrent"] != 1 {
		t.Fatalf("links opened = %v, want only the downloaded ones, once each", opened)
	}
}
