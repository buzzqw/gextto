package gextto

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
