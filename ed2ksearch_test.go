package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func ed2kTestRelease() models.Release {
	link := "ed2k://|file|Rare%20Italian%20Movie%201974%20(1080p).avi|700000000|0123456789abcdef0123456789abcdef|/"
	release := ParseEd2kRelease("Rare Italian Movie 1974 (1080p).avi", link, "ed2k:kad", 700000000, 9, time.Now().UTC())
	if release == nil {
		panic("ParseEd2kRelease returned nil")
	}
	return *release
}

func TestParseEd2kRelease(t *testing.T) {
	release := ed2kTestRelease()
	if release.Network != "ed2k" {
		t.Fatalf("network = %q", release.Network)
	}
	if release.Magnet != "" {
		t.Fatalf("magnet must be empty, got %q", release.Magnet)
	}
	if !strings.HasPrefix(release.Ed2kLink, "ed2k://|file|") {
		t.Fatalf("link = %q", release.Ed2kLink)
	}
	if release.SizeBytes != 700000000 || release.Sources != 9 {
		t.Fatalf("size/sources = %d/%d", release.SizeBytes, release.Sources)
	}
	if release.Quality.Resolution != "1080p" {
		t.Fatalf("resolution = %q", release.Quality.Resolution)
	}
	if release.Seeders != -1 {
		t.Fatalf("eD2k sources must not be mapped to Seeders: %d", release.Seeders)
	}
}

func TestReleaseDedupKeyForEd2k(t *testing.T) {
	release := ed2kTestRelease()
	key, ok := releaseDedupKey(&release)
	if !ok || key != "ed2k:0123456789abcdef0123456789abcdef" {
		t.Fatalf("dedup key = %q, %v", key, ok)
	}
}

func TestSearchED2KAgainstFakeDaemon(t *testing.T) {
	link := "ed2k://|file|The%20Matrix%201999%201080p.mkv|7000000000|dddddddddddddddddddddddddddddddd|/"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": "1.1.1", "download": false})
		case "/api/v1/search":
			if r.URL.Query().Get("q") != "The Matrix 1999" {
				t.Errorf("query = %q", r.URL.Query().Get("q"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"results": []map[string]any{{
					"name": "The Matrix 1999 1080p.mkv", "size_bytes": 7000000000,
					"hash": "dddddddddddddddddddddddddddddddd", "link": link,
					"sources": 12, "origin": "kad",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ed2kInvalidateHealth()
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.Settings = map[string]string{
		"ed2k_enabled": "true", "ed2k_url": server.URL,
	}
	releases := SearchED2K(context.Background(), cfg, "The Matrix 1999")
	if len(releases) != 1 {
		t.Fatalf("got %d releases", len(releases))
	}
	if releases[0].Network != "ed2k" || releases[0].Ed2kLink != link || releases[0].Sources != 12 {
		t.Fatalf("release = %+v", releases[0])
	}
	if releases[0].Quality.Resolution != "1080p" {
		t.Fatalf("quality = %+v", releases[0].Quality)
	}
}

func TestSearchED2KDisabledByDefault(t *testing.T) {
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.Settings = map[string]string{}
	if got := SearchED2K(context.Background(), cfg, "anything"); len(got) != 0 {
		t.Fatalf("disabled provider returned %d releases", len(got))
	}
}

func TestEd2kFallbackRequiresDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "download": true})
	}))
	defer server.Close()
	ed2kInvalidateHealth()
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.Settings = map[string]string{
		"ed2k_enabled": "true", "ed2k_fallback": "true", "ed2k_url": server.URL,
	}
	if search, download := ed2kAvailability(cfg); !search || !download {
		t.Fatalf("availability = %v/%v", search, download)
	}
	if !ed2kFallbackEligible(cfg) {
		t.Fatal("fallback should be eligible")
	}
	cfg.Settings["ed2k_fallback"] = "false"
	if ed2kFallbackEligible(cfg) {
		t.Fatal("fallback must be off when the switch is off")
	}
}

func TestSearchSeriesED2KFiltersAutomaticCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"results": []map[string]any{
				{
					"name": "Rare Show S02E03 1080p.mkv", "size_bytes": 5000000000,
					"hash": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "sources": 1, "origin": "kad",
				},
				{
					"name": "Rare Show S02E03 720p.mkv", "size_bytes": 900000000,
					"hash": "ffffffffffffffffffffffffffffffff", "sources": 8, "origin": "kad",
				},
				{
					"name": "Rare Show S02E04 1080p.mkv", "size_bytes": 5000000000,
					"hash": "11111111111111111111111111111111", "sources": 9, "origin": "kad",
				},
			},
		})
	}))
	defer server.Close()
	ed2kInvalidateHealth()
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.Settings = map[string]string{
		"ed2k_enabled": "true", "ed2k_url": server.URL, "ed2k_min_sources": "3",
	}
	series := &SeriesConfig{Name: "Rare Show"}
	engine := &Engine{}
	got := engine.SearchSeriesED2K(context.Background(), cfg, series, 2, 3, false)
	if len(got) != 1 {
		t.Fatalf("accepted %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].Sources != 8 || got[0].Quality.Resolution != "720p" {
		t.Fatalf("candidate = %+v", got[0])
	}
	if *got[0].Series != "Rare Show" {
		t.Fatalf("series = %q", *got[0].Series)
	}
}

func TestDatabaseStoresEd2kIdentity(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := ed2kTestRelease()
	if err := db.RegisterTorrentScored(&release, 10); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}
	const hash = "0123456789abcdef0123456789abcdef"
	if got := db.TorrentNetwork(hash); got != "ed2k" {
		t.Fatalf("network = %q", got)
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		t.Fatalf("TorrentMeta: %v, %+v", err, meta)
	}
	if meta.Release.Ed2kLink == "" || meta.Release.Network != "ed2k" {
		t.Fatalf("metadata = %+v", meta.Release)
	}
}

func TestSearchSeriesED2KQueriesAliasesAndDedupes(t *testing.T) {
	var mu sync.Mutex
	queries := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query().Get("q"))
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"results": []map[string]any{
				{
					"name": "Murdoch Mysteries S13E12 1080p.mkv", "size_bytes": 2000000000,
					"hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "sources": 4, "origin": "kad",
				},
			},
		})
	}))
	defer server.Close()
	ed2kInvalidateHealth()
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.Settings = map[string]string{"ed2k_enabled": "true", "ed2k_url": server.URL}
	series := &SeriesConfig{Name: "I misteri di Murdoch", Aliases: []string{"Murdoch Mysteries"}}
	engine := &Engine{}
	got := engine.SearchSeriesED2K(context.Background(), cfg, series, 13, 12, true)
	if len(got) != 1 {
		t.Fatalf("aliases returned %d candidates, want 1 after dedup: %+v", len(got), got)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(queries, "\n")
	for _, want := range []string{"I misteri di Murdoch S13E12", "Murdoch Mysteries S13E12"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("queries missing %q: %v", want, queries)
		}
	}
}

func TestSearchED2KManualBypassesDisabledProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "search": true, "download": false})
		case "/api/v1/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"results": []map[string]any{{
					"name": "Rare Movie 1974 1080p.avi", "size_bytes": 700000000,
					"hash": "0123456789abcdef0123456789abcdef", "sources": 4, "origin": "kad",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ed2kInvalidateHealth()
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.Settings = map[string]string{"ed2k_enabled": "false", "ed2k_url": server.URL}
	if got := SearchED2K(context.Background(), cfg, "rare movie"); len(got) != 0 {
		t.Fatalf("disabled provider returned %d releases", len(got))
	}
	got := SearchED2KManual(context.Background(), cfg, "rare movie")
	if len(got) != 1 || got[0].Title == "" {
		t.Fatalf("manual search = %+v", got)
	}
	if ed2kEnabled(cfg) {
		t.Fatal("the per-search opt-in must not persist the setting")
	}
}
