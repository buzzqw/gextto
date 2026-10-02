package gextto

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

type mockTorrentEngine struct {
	TorrentEngine
	torrents []models.TorrentView
	removed  map[string]bool
}

func (m *mockTorrentEngine) List() []models.TorrentView {
	return m.torrents
}

func (m *mockTorrentEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	m.removed[hash] = deleteFiles
	return true, nil
}

func TestSafeRemoveTorrentMovesToTrash(t *testing.T) {
	root := t.TempDir()
	downloadDir := filepath.Join(root, "downloads")
	trashDir := filepath.Join(root, "trash")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatal(err)
	}

	testFile := filepath.Join(downloadDir, "TestShow.S01E01.mkv")
	if err := os.WriteFile(testFile, []byte("video payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash := "1111222233334444555566667777888899990000"
	engine := &mockTorrentEngine{
		torrents: []models.TorrentView{
			{
				Hash:      hash,
				Name:      "TestShow.S01E01.mkv",
				SavePath:  downloadDir,
				State:     "seeding",
				TotalSize: 13,
			},
		},
		removed: map[string]bool{},
	}

	s := &AppState{
		torrent_engine: engine,
	}

	cfg := DefaultConfig()
	cfg.TrashPath = &trashDir
	cfg.CleanupAction = "move"

	// Removal with deleteFiles = true should move to trash, and pass deleteFiles = false to engine
	ok, err := SafeRemoveTorrent(s, &cfg, hash, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected torrent to be removed")
	}

	// Verify original file is gone from downloads
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Errorf("file %s should have been moved out of download dir", testFile)
	}

	// Verify engine was told false (so it doesn't double-unlink)
	if engine.removed[hash] {
		t.Errorf("engine should have received deleteFiles=false, got true")
	}

	// Verify file is in trash
	entries, _ := os.ReadDir(trashDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in trash, got %d", len(entries))
	}
}

func TestSingleEpisodeBlockedWhenSeasonPackActive(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDatabase(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	seriesName := "ActiveSeries"
	packHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	s1 := int64(1)
	packRelease := models.Release{
		Kind:      "series",
		Series:    &seriesName,
		Season:    &s1,
		IsPack:    true,
		Title:     "ActiveSeries.S01.1080p",
		Quality:   ParseQuality("ActiveSeries.S01.1080p"),
		SizeBytes: 10_000_000,
		Magnet:    "magnet:?xt=urn:btih:" + packHash,
	}
	if err := db.RegisterTorrentScored(&packRelease, 1000); err != nil {
		t.Fatal(err)
	}

	// Verify single episode of the same season is rejected because the pack is active
	e1 := int64(1)
	singleRelease := models.Release{
		Kind:      "series",
		Series:    &seriesName,
		Season:    &s1,
		Episode:   &e1,
		Title:     "ActiveSeries.S01E01.1080p",
		Quality:   ParseQuality("ActiveSeries.S01E01.1080p"),
		SizeBytes: 1_000_000,
		Magnet:    "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}

	approved, reason, err := db.CheckSeriesScored(&singleRelease, 1000, DefaultUpgradeMinScoreDiff, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Errorf("single episode should have been rejected while pack is active")
	}
	if reason != "active_episode" {
		t.Errorf("expected reason 'active_episode', got %q", reason)
	}

	// Verify another season pack of the same season is also rejected because a pack is active
	anotherPack := models.Release{
		Kind:      "series",
		Series:    &seriesName,
		Season:    &s1,
		IsPack:    true,
		Title:     "ActiveSeries.S01.720p",
		Quality:   ParseQuality("ActiveSeries.S01.720p"),
		SizeBytes: 8_000_000,
		Magnet:    "magnet:?xt=urn:btih:cccccccccccccccccccccccccccccccccccccccc",
	}
	packApproved, packReason, packErr := db.CheckSeriesScored(&anotherPack, 800, DefaultUpgradeMinScoreDiff, nil)
	if packErr != nil {
		t.Fatalf("unexpected error checking pack: %v", packErr)
	}
	if packApproved {
		t.Errorf("second pack should have been rejected while first pack is active")
	}
	if packReason != "active_pack" {
		t.Errorf("expected reason 'active_pack', got %q", packReason)
	}
}
