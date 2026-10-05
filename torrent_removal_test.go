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
	paused   map[string]bool
}

func (m *mockTorrentEngine) List() []models.TorrentView {
	return m.torrents
}

func (m *mockTorrentEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	if m.removed == nil {
		m.removed = map[string]bool{}
	}
	m.removed[hash] = deleteFiles
	return true, nil
}

func (m *mockTorrentEngine) Pause(hash string) (bool, error) {
	if m.paused == nil {
		m.paused = map[string]bool{}
	}
	m.paused[hash] = true
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

func TestArchiveAndRemoveTorrentEpisodeMovesAndRenames(t *testing.T) {
	root := t.TempDir()
	downloadDir := filepath.Join(root, "downloads")
	archiveDir := filepath.Join(root, "archive", "TestShow")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Matroska container magic header + padding
	mkvHeader := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42, 0x86, 0x81, 0x01, 0x42, 0xF7, 0x81, 0x01, 0x42, 0xF2, 0x81}
	testFile := filepath.Join(downloadDir, "TestShow.S01E01.1080p.mkv")
	if err := os.WriteFile(testFile, mkvHeader, 0o644); err != nil {
		t.Fatal(err)
	}

	dbFile := filepath.Join(root, "test.db")
	db, err := OpenDatabase(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sName := "TestShow"
	s1 := int64(1)
	e1 := int64(1)
	hash := "1111222233334444555566667777888899990000"
	rel := models.Release{
		Kind:      "series",
		Series:    &sName,
		Season:    &s1,
		Episode:   &e1,
		Title:     "TestShow.S01E01.1080p",
		Quality:   ParseQuality("TestShow.S01E01.1080p"),
		SizeBytes: int64(len(mkvHeader)),
		Magnet:    "magnet:?xt=urn:btih:" + hash,
	}
	if err := db.RegisterTorrentScored(&rel, 1000); err != nil {
		t.Fatal(err)
	}

	engine := &mockTorrentEngine{
		torrents: []models.TorrentView{
			{
				Hash:      hash,
				Name:      "TestShow.S01E01.1080p.mkv",
				SavePath:  downloadDir,
				State:     "seeding",
				Progress:  100.0,
				TotalSize: int64(len(mkvHeader)),
				TotalDone: int64(len(mkvHeader)),
			},
		},
		removed: map[string]bool{},
		paused:  map[string]bool{},
	}

	s := &AppState{
		torrent_engine: engine,
		db:             db,
	}

	cfg := DefaultConfig()
	cfg.LibtorrentDir = downloadDir
	cfg.Series = append(cfg.Series, SeriesConfig{
		Name:        "TestShow",
		Enabled:     true,
		ArchivePath: archiveDir,
	})

	ok, err := ArchiveAndRemoveTorrent(s, &cfg, hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ArchiveAndRemoveTorrent to succeed")
	}

	// Verify original file is gone from downloadDir
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Errorf("original file %s should have been moved from downloads", testFile)
	}

	// Verify file now exists in archiveDir
	entries, err := os.ReadDir(archiveDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 file in archiveDir %s, got %v (err: %v)", archiveDir, entries, err)
	}

	// Verify engine was instructed to pause and remove (deleteFiles = false)
	if !engine.paused[hash] {
		t.Errorf("torrent should have been paused before removal")
	}
	if val, removed := engine.removed[hash]; !removed || val {
		t.Errorf("expected engine.Remove(hash, false), got removed=%v, deleteFiles=%v", removed, val)
	}
}

// TestArchiveAndRemoveTorrentFinalizesUnrenamedArchivedCopy reproduces a
// post-seed relocation whose import step never ran: the file already sits in
// the archive folder under its original torrent name and the DB records that
// path as processed. ArchiveAndRemoveTorrent must not treat it as fully
// archived: it has to run the rename (and quality checks) on the file in place.
func TestArchiveAndRemoveTorrentFinalizesUnrenamedArchivedCopy(t *testing.T) {
	root := t.TempDir()
	archiveDir := filepath.Join(root, "archive", "TestShow")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mkvHeader := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42, 0x86, 0x81, 0x01, 0x42, 0xF7, 0x81, 0x01, 0x42, 0xF2, 0x81}
	torrentName := "TestShow.S01E01.1080p.mkv"
	archived := filepath.Join(archiveDir, torrentName)
	if err := os.WriteFile(archived, mkvHeader, 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := OpenDatabase(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sName := "TestShow"
	s1 := int64(1)
	e1 := int64(1)
	hash := "aaaabbbbccccddddeeeeffff0000111122223333"
	rel := models.Release{
		Kind:      "series",
		Series:    &sName,
		Season:    &s1,
		Episode:   &e1,
		Title:     "TestShow.S01E01.1080p",
		Quality:   ParseQuality("TestShow.S01E01.1080p"),
		SizeBytes: int64(len(mkvHeader)),
		Magnet:    "magnet:?xt=urn:btih:" + hash,
	}
	if err := db.RegisterTorrentScored(&rel, 1000); err != nil {
		t.Fatal(err)
	}
	// The relocation recorded the un-renamed file as the processed copy.
	if _, err := db.db.Exec("UPDATE torrent_meta SET processed_path=?1 WHERE hash=?2", archived, hash); err != nil {
		t.Fatal(err)
	}

	engine := &mockTorrentEngine{
		torrents: []models.TorrentView{
			{
				Hash:      hash,
				Name:      torrentName,
				SavePath:  archiveDir,
				State:     "paused",
				Progress:  100.0,
				TotalSize: int64(len(mkvHeader)),
				TotalDone: int64(len(mkvHeader)),
			},
		},
		removed: map[string]bool{},
		paused:  map[string]bool{},
	}
	s := &AppState{torrent_engine: engine, db: db}

	cfg := DefaultConfig()
	cfg.LibtorrentDir = archiveDir
	cfg.RenameEpisodes = true
	cfg.Series = append(cfg.Series, SeriesConfig{Name: "TestShow", Enabled: true, ArchivePath: archiveDir})

	if finalized := gh6_archivedCopyFinalized(s.db, torrentName, hash); finalized {
		t.Fatal("an un-renamed archived copy must not be considered finalized")
	}

	ok, err := ArchiveAndRemoveTorrent(s, &cfg, hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ArchiveAndRemoveTorrent to succeed")
	}

	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in archiveDir, got %v", entries)
	}
	if entries[0].Name() == torrentName {
		t.Errorf("file was not renamed: still %q", entries[0].Name())
	}
	processed, err := db.TorrentProcessed(hash)
	if err != nil || processed == nil || *processed == archived {
		t.Errorf("processed path not updated to the renamed file: %v (err %v)", processed, err)
	}
}

func TestArchiveAndRemoveTorrentIncompleteFails(t *testing.T) {
	root := t.TempDir()
	downloadDir := filepath.Join(root, "downloads")

	hash := "2222333344445555666677778888999900001111"
	engine := &mockTorrentEngine{
		torrents: []models.TorrentView{
			{
				Hash:      hash,
				Name:      "IncompleteShow.mkv",
				SavePath:  downloadDir,
				State:     "downloading",
				Progress:  45.0,
				TotalSize: 10000,
				TotalDone: 4500,
			},
		},
		removed: map[string]bool{},
		paused:  map[string]bool{},
	}

	s := &AppState{
		torrent_engine: engine,
	}
	cfg := DefaultConfig()

	ok, err := ArchiveAndRemoveTorrent(s, &cfg, hash)
	if ok || err == nil {
		t.Fatalf("expected failure archiving incomplete torrent, got ok=%v, err=%v", ok, err)
	}
}
