package gextto

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func crossSeedTestTarget(t *testing.T) (*Database, string) {
	t.Helper()
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hash := strings.Repeat("a", 40)
	series := "Example Show"
	season, episode := int64(1), int64(2)
	release := models.Release{
		Title: "Example.Show.S01E02.1080p", Magnet: "magnet:?xt=urn:btih:" + hash,
		Source: "original", Kind: "series", Series: &series, Season: &season, Episode: &episode,
		EpisodeRange: []int64{episode}, Seeders: -1, Peers: -1,
	}
	if err := db.RegisterTorrentScored(&release, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkTorrentCompleted(hash, filepath.Join(t.TempDir(), "library.mkv"), 1234); err != nil {
		t.Fatal(err)
	}
	return db, hash
}

func TestDiscoverCrossSeedKeepsEveryExactHashSourceAndRedactsTrackers(t *testing.T) {
	db, hash := crossSeedTestTarget(t)
	var torrentFetches atomic.Int32
	linkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		torrentFetches.Add(1)
		_, _ = io.WriteString(w, "not a torrent")
	}))
	defer linkServer.Close()

	magnet := "magnet:?xt=urn:btih:" + hash + "&tr=https%3A%2F%2Fuser%3Apass%40tracker.example%2Fannounce%3Fsecret%3Dhidden"
	body := func(tracker, extra string) string {
		xmlMagnet := strings.ReplaceAll(magnet, "&", "&amp;")
		return `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><item>
			<title>Example.Show.S01E02.1080p</title>
			<torznab:attr name="jackettindexer" value="` + tracker + `"/>
			<torznab:attr name="magneturl" value="` + xmlMagnet + `"/>
		</item>` + extra + `</channel></rss>`
	}
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body("Tracker One", `<item><title>Different.Show.S01E02.1080p</title><torznab:attr name="infohash" value="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"/></item><item><title>Unknown.Show.S01E03.1080p</title><link>`+linkServer.URL+`/download.torrent</link></item>`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body("Tracker Two", ""))
	}))
	defer second.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer broken.Close()

	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}
	cfg.Indexers = []IndexerConfig{
		{Name: "first", URL: first.URL, Enabled: true, Manager: ManagerJackett},
		{Name: "second", URL: second.URL, Enabled: true, Manager: ManagerJackett},
		{Name: "broken", URL: broken.URL, Enabled: true, Manager: ManagerJackett},
		{Name: "disabled", URL: linkServer.URL, Enabled: false, Manager: ManagerJackett},
	}
	result, err := discoverCrossSeed(context.Background(), db, &cfg, hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("matches = %+v, want one exact match per source", result.Matches)
	}
	if !result.Partial || len(result.Errors) != 1 || result.Errors[0].Source != "broken" {
		t.Fatalf("source failure not visible: partial=%v errors=%+v", result.Partial, result.Errors)
	}
	sources := map[string]bool{}
	for _, match := range result.Matches {
		sources[match.Source] = true
		if len(match.TrackerHosts) != 1 || match.TrackerHosts[0] != "tracker.example" {
			t.Fatalf("tracker hosts = %v, want only tracker.example", match.TrackerHosts)
		}
	}
	if !sources["first:Tracker One"] || !sources["second:Tracker Two"] {
		t.Fatalf("sources = %v, want both trackers", sources)
	}
	if torrentFetches.Load() != 0 {
		t.Fatalf("link-only result fetched %d times; discovery must stay read-only", torrentFetches.Load())
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "hidden") || strings.Contains(string(encoded), "user:pass") {
		t.Fatalf("response leaked tracker credentials: %s", encoded)
	}
	var count int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM torrent_meta").Scan(&count); err != nil || count != 1 {
		t.Fatalf("torrent rows after discovery = %d, err = %v", count, err)
	}
}

func TestDiscoverCrossSeedRejectsUnknownTarget(t *testing.T) {
	db, _ := crossSeedTestTarget(t)
	cfg := DefaultConfig()
	_, err := discoverCrossSeed(context.Background(), db, &cfg, strings.Repeat("c", 40))
	if err != errCrossSeedTargetNotFound {
		t.Fatalf("err = %v, want target not found", err)
	}
}

func TestCrossSeedDiscoverAPIValidatesHash(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/library/cross-seed/discover", strings.NewReader(`{"hash":"nope"}`))
	CrossSeedDiscover(recorder, request, nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", recorder.Code)
	}
}
