package gextto

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// packFileCheckSetup is a pack the cycle added stopped at its metadata, with
// its deferred announcement recorded, and a webhook that collects events.
func packFileCheckSetup(t *testing.T, files []models.FileView) (*Config, *Database, *packInspectEngine, string, chan string) {
	t.Helper()
	events := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			Event string `json:"event"`
		}
		_ = json.Unmarshal(body, &envelope)
		events <- envelope.Event
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	cfg, db, engine, hash := packInspectFixture(t, files)
	cfg.NotifyWebhookURL = &server.URL
	cfg.NotifyEventDownloads = true
	if err := db.SetPackFileCheck(hash, "📥 Downloading Show season 1 — fills a missing episode (1080p)",
		map[string]any{"title": "Show - Stagione 1", "kind": "series", "source": "test"}); err != nil {
		t.Fatalf("set pack file check: %v", err)
	}
	engine.torrents[0].State = "paused"
	engine.torrents[0].TotalDone = 0
	return cfg, db, engine, hash, events
}

func drainEvents(events chan string) []string {
	var got []string
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case event := <-events:
			got = append(got, event)
		case <-deadline:
			return got
		}
	}
}

// TestAPackWithAUsefulFileStartsAndIsAnnouncedThen: the download line and the
// notification wait for the file list; a useful episode inside starts the
// pack and sends them.
func TestAPackWithAUsefulFileStartsAndIsAnnouncedThen(t *testing.T) {
	cfg, db, engine, hash, events := packFileCheckSetup(t, []models.FileView{
		{Path: "Show.S01E01.mkv"},
		{Path: "Show.S01E03.mkv"}, // not in the library: useful
	})
	InspectActivePacks(cfg, engine, db, FromConfig(cfg), map[string]packInspectionCheck{})
	if !engine.resumed[hash] {
		t.Fatal("a useful pack stopped at its metadata was not started")
	}
	if got := drainEvents(events); len(got) != 1 || got[0] != "download_started" {
		t.Fatalf("events = %v, want one download_started", got)
	}
	if pending, _ := db.PackFileCheck(hash); pending != nil {
		t.Fatal("the deferred announcement was kept after it was sent")
	}
}

// TestAPackWithNothingUsefulIsDroppedWithoutAnAnnouncement: never announced,
// so nothing is notified, and the pack goes.
func TestAPackWithNothingUsefulIsDroppedWithoutAnAnnouncement(t *testing.T) {
	cfg, db, engine, hash, events := packFileCheckSetup(t, []models.FileView{{Path: "Show.S01E01.mkv"}})
	InspectActivePacks(cfg, engine, db, FromConfig(cfg), map[string]packInspectionCheck{})
	if _, removed := engine.removed[hash]; !removed {
		t.Fatal("a useless pack was kept")
	}
	if engine.resumed[hash] {
		t.Fatal("a useless pack was started")
	}
	if got := drainEvents(events); len(got) != 0 {
		t.Fatalf("events = %v, want none for a pack never announced", got)
	}
	if pending, _ := db.PackFileCheck(hash); pending != nil {
		t.Fatal("the deferred announcement outlived the discarded pack")
	}
}

// TestAPackIsLeftToTheEngineUntilItStops: resuming before the engine applied
// its stop would be undone by the stop.
func TestAPackIsLeftToTheEngineUntilItStops(t *testing.T) {
	cfg, db, engine, hash, _ := packFileCheckSetup(t, []models.FileView{
		{Path: "Show.S01E03.mkv"},
	})
	engine.torrents[0].State = "downloading"
	InspectActivePacks(cfg, engine, db, nil, map[string]packInspectionCheck{})
	if engine.resumed[hash] {
		t.Fatal("the pack was resumed before the engine stopped it")
	}
	if pending, _ := db.PackFileCheck(hash); pending == nil {
		t.Fatal("the deferred announcement was lost while waiting")
	}
}

// TestOnlyAMagnetPackHasItsFileListReadFirst: a .torrent already gave the
// file list, a single episode has none to read, and a dry run adds nothing.
func TestOnlyAMagnetPackHasItsFileListReadFirst(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DryRun = false
	cfg.Settings = map[string]string{packInspectionSetting: "true"}
	season := int64(3)
	pack := &models.Release{Kind: "series", IsPack: true, Season: &season, EpisodeRange: []int64{0}}
	if !packFileListUnknown(&cfg, pack, nil) {
		t.Fatal("a magnet pack is added without reading its files first")
	}
	file := "/tmp/x.torrent"
	if packFileListUnknown(&cfg, pack, &file) {
		t.Fatal("a pack whose .torrent was read is held for its file list")
	}
	single := &models.Release{Kind: "series", Season: &season, EpisodeRange: []int64{4}}
	if packFileListUnknown(&cfg, single, nil) {
		t.Fatal("a single episode is held for its file list")
	}
	cfg.Settings[packInspectionSetting] = "false"
	if packFileListUnknown(&cfg, pack, nil) {
		t.Fatal("held without the pack inspection")
	}
}
