package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// stubTorrentSession is the minimal TorrentSession used by the seed/removal
// tests. It only records removals; every other operation is a no-op.
type stubTorrentSession struct {
	list        []models.TorrentView
	removed     []string
	deleteFlags []bool
	moved       map[string]string
	associated  map[string]string
}

func (s *stubTorrentSession) List() []models.TorrentView        { return s.list }
func (s *stubTorrentSession) PollEvents() []models.TorrentEvent { return nil }
func (s *stubTorrentSession) Pause(string) (bool, error)        { return true, nil }
func (s *stubTorrentSession) Resume(string) (bool, error)       { return true, nil }
func (s *stubTorrentSession) Restart(string) (bool, error)      { return true, nil }
func (s *stubTorrentSession) Remove(hash string, deleteFiles bool) (bool, error) {
	s.removed = append(s.removed, hash)
	s.deleteFlags = append(s.deleteFlags, deleteFiles)
	return true, nil
}
func (s *stubTorrentSession) ForceRecheck(string) (bool, error)             { return true, nil }
func (s *stubTorrentSession) Reannounce(string) (bool, error)               { return true, nil }
func (s *stubTorrentSession) MarkStalled(string) (bool, error)              { return true, nil }
func (s *stubTorrentSession) ClearStalled(string)                           {}
func (s *stubTorrentSession) RamdiskUncommittedBytes(string, string) uint64 { return 0 }
func (s *stubTorrentSession) MoveStorage(hash, destination string) (bool, error) {
	if s.moved == nil {
		s.moved = map[string]string{}
	}
	s.moved[hash] = destination
	return true, nil
}
func (s *stubTorrentSession) AssociateStorage(hash, destination string) (bool, error) {
	if s.associated == nil {
		s.associated = map[string]string{}
	}
	s.associated[hash] = destination
	return true, nil
}

const seedTestHash = "0123456789abcdef0123456789abcdef01234567"

func seedTestSetup(t *testing.T) (*Database, *Config, models.TorrentView, string, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDatabase(filepath.Join(dir, "gextto.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	downloads := filepath.Join(dir, "downloads")
	library := filepath.Join(dir, "library")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}

	title := "Show"
	season := int64(1)
	episode := int64(1)
	release := &models.Release{
		Title:   "Show.S01E01.1080p",
		Magnet:  "magnet:?xt=urn:btih:" + seedTestHash,
		Kind:    "series",
		Series:  &title,
		Season:  &season,
		Episode: &episode,
	}
	if err := db.RegisterTorrentScored(release, 100); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}

	source := filepath.Join(downloads, "Show.S01E01.1080p.mkv")
	if err := os.WriteFile(source, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	processed := filepath.Join(library, "Show - S01E01.mkv")
	if err := os.WriteFile(processed, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkTorrentCompleted(seedTestHash, processed, 5); err != nil {
		t.Fatalf("MarkTorrentCompleted: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Libtorrent.SeedRatio = 0.5
	cfg.Settings = map[string]string{}

	view := models.TorrentView{
		Hash:            seedTestHash,
		Name:            "Show.S01E01.1080p.mkv",
		SavePath:        downloads,
		State:           "paused",
		Progress:        100,
		TotalSize:       5,
		TotalDone:       5,
		SeedRatio:       -1,
		SeedDays:        -1,
		AllTimeDownload: 100,
		AllTimeUpload:   1000,
		SeedingSeconds:  60,
	}
	return db, &cfg, view, source, processed
}

// TestRemoveSeededCompletedHonorsAutoRemove covers the reported bug: with
// "Elimina i completati dopo il seed" off, a completed torrent that kept its
// source for seeding must stay in the session after the seed limit.
func TestRemoveSeededCompletedHonorsAutoRemove(t *testing.T) {
	db, cfg, view, source, processed := seedTestSetup(t)

	session := &stubTorrentSession{list: []models.TorrentView{view}}
	cfg.Libtorrent.AutoRemoveCompleted = false
	RemoveSeededCompleted(cfg, session, db, map[string]struct{}{})
	if len(session.removed) != 0 {
		t.Fatalf("auto_remove_completed=false removed torrents: %v", session.removed)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source must be kept when move_episodes is off: %v", err)
	}

	cfg.Libtorrent.AutoRemoveCompleted = true
	session = &stubTorrentSession{list: []models.TorrentView{view}}
	RemoveSeededCompleted(cfg, session, db, map[string]struct{}{})
	if len(session.removed) != 1 || session.removed[0] != seedTestHash {
		t.Fatalf("auto_remove_completed=true removed=%v, want [%s]", session.removed, seedTestHash)
	}
	if _, err := os.Stat(processed); err != nil {
		t.Fatalf("library copy must stay: %v", err)
	}
}

// TestRemoveSeededCompletedKeepsSourceBeforeArchive makes sure the seed
// cleanup does not delete the download source on its own: the end-of-seed move
// (EnforceSeedPolicy) is what archives it, and with automatic removal off the
// torrent stays listed.
func TestRemoveSeededCompletedKeepsSourceBeforeArchive(t *testing.T) {
	db, cfg, view, source, _ := seedTestSetup(t)
	cfg.Libtorrent.AutoRemoveCompleted = false
	cfg.Settings["move_episodes"] = "true"

	session := &stubTorrentSession{list: []models.TorrentView{view}}
	RemoveSeededCompleted(cfg, session, db, map[string]struct{}{})
	if len(session.removed) != 0 {
		t.Fatalf("torrent must stay with auto_remove_completed=false: %v", session.removed)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("RemoveSeededCompleted must not delete the source on its own: %v", err)
	}
}

func TestSeedingCompletionAlreadyRecorded(t *testing.T) {
	db, _, _, _, _ := seedTestSetup(t)
	if tevSeedingCompletionAlreadyRecorded(db, seedTestHash) {
		t.Fatal("an archived completion must not be treated as an unarchived seeding completion")
	}
	if _, err := db.db.Exec("UPDATE torrent_meta SET processed_path='' WHERE hash=?", seedTestHash); err != nil {
		t.Fatal(err)
	}
	if !tevSeedingCompletionAlreadyRecorded(db, seedTestHash) {
		t.Fatal("expected completed torrent without an archive path to suppress duplicate completion")
	}
}

// TestBgTorrentNeedsArchiveImport pins the Marshals regression: after the
// end-of-seed relocation the row is "completed" with an empty processed path, so
// the archive import (rename, MediaInfo, completion notification) must still
// run instead of being treated as already archived.
func TestBgTorrentNeedsArchiveImport(t *testing.T) {
	db, cfg, view, source, processed := seedTestSetup(t)
	library := filepath.Dir(processed)
	cfg.ArchiveRoot = &library

	// Deferred state after the post-seed move: the completion marker is set but
	// no processed path was recorded, and the payload now lives in the archive.
	if _, err := db.db.Exec("UPDATE torrent_meta SET processed_path='' WHERE hash=?", seedTestHash); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(library, filepath.Base(source))
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moved, data, 0o644); err != nil {
		t.Fatal(err)
	}
	view.HasMetadata = true
	view.SavePath = library
	view.TotalSize = int64(len(data))
	view.TotalDone = int64(len(data))

	if !bg_torrentNeedsArchiveImport(cfg, db, &view) {
		t.Fatal("a completed single moved to the archive must still need its import")
	}
	// Once a processed path is recorded the import has run and must not repeat.
	if err := db.MarkTorrentCompleted(seedTestHash, moved, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if bg_torrentNeedsArchiveImport(cfg, db, &view) {
		t.Fatal("a recorded archive copy must not be re-imported")
	}
}

// TestDetachCompletedArchivedSinglesHonorsAutoRemove makes sure the recovery
// pass does not remove kept completed singles behind the user's back.
func TestDetachCompletedArchivedSinglesHonorsAutoRemove(t *testing.T) {
	db, cfg, view, source, _ := seedTestSetup(t)
	// The download source is gone: without the setting this recovery pass would
	// otherwise detach the torrent.
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	cfg.Libtorrent.AutoRemoveCompleted = false
	session := &stubTorrentSession{list: []models.TorrentView{view}}
	DetachCompletedArchivedSingles(cfg, session, db)
	if len(session.removed) != 0 {
		t.Fatalf("auto_remove_completed=false detached: %v", session.removed)
	}
}

// TestEnforceSeedPolicyArchivesAtSeedEnd pins the deferred archive: a torrent
// that seeded from the download folder is moved into its library destination at
// the end of the seed (not at completion).
func TestEnforceSeedPolicyArchivesAtSeedEnd(t *testing.T) {
	db, cfg, view, source, _ := seedTestSetup(t)
	// Deferred state: the release is marked completed at completion time but the
	// processed path is still the download source (inside the download folder).
	if err := db.MarkTorrentCompleted(seedTestHash, source, 5); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(filepath.Dir(filepath.Dir(source)), "library")
	cfg.ArchiveRoot = &library

	session := &stubTorrentSession{list: []models.TorrentView{view}}
	EnforceSeedPolicy(cfg, session, db, map[string]struct{}{}, map[string]StorageMoveRetry{}, map[string]time.Time{})
	if got := session.moved[seedTestHash]; got != library {
		t.Fatalf("MoveStorage destination = %q, want %q", got, library)
	}
}

// TestEnforceSeedPolicyMovesRamdiskSourceDirectlyToArchive makes sure the
// RAM-disk post-seed relocation does not first move a completed torrent back to
// the generic download directory. That directory may contain unrelated files
// and is not the release's configured archive destination.
func TestEnforceSeedPolicyMovesRamdiskSourceDirectlyToArchive(t *testing.T) {
	db, cfg, view, source, processed := seedTestSetup(t)
	cfg.Settings["libtorrent_ramdisk_enabled"] = "yes"
	cfg.Settings["libtorrent_ramdisk_dir"] = filepath.Dir(source)
	cfg.LibtorrentDir = filepath.Join(t.TempDir(), "downloads")
	library := filepath.Dir(processed)
	cfg.ArchiveRoot = &library
	if err := db.MarkTorrentCompleted(seedTestHash, source, 5); err != nil {
		t.Fatal(err)
	}

	session := &stubTorrentSession{list: []models.TorrentView{view}}
	EnforceSeedPolicy(cfg, session, db, map[string]struct{}{}, map[string]StorageMoveRetry{}, map[string]time.Time{})
	if got := session.moved[seedTestHash]; got != library {
		t.Fatalf("RAM-disk post-seed destination = %q, want %q", got, library)
	}
}

// TestEnforceSeedPolicyAssociatesExistingArchive recovers a destination that
// was populated before a restart interrupted the storage_moved event.
func TestEnforceSeedPolicyAssociatesExistingArchive(t *testing.T) {
	db, cfg, view, source, processed := seedTestSetup(t)
	cfg.Settings["libtorrent_ramdisk_enabled"] = "yes"
	cfg.Settings["libtorrent_ramdisk_dir"] = filepath.Dir(source)
	cfg.LibtorrentDir = filepath.Join(t.TempDir(), "downloads")
	library := filepath.Dir(processed)
	cfg.ArchiveRoot = &library
	target := filepath.Join(library, filepath.Base(source))
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkTorrentCompleted(seedTestHash, source, int64(len(data))); err != nil {
		t.Fatal(err)
	}

	session := &stubTorrentSession{list: []models.TorrentView{view}}
	EnforceSeedPolicy(cfg, session, db, map[string]struct{}{}, map[string]StorageMoveRetry{}, map[string]time.Time{})
	if got := session.associated[seedTestHash]; got != library {
		t.Fatalf("existing archive association = %q, want %q", got, library)
	}
	if got, err := db.TorrentProcessed(seedTestHash); err != nil || got == nil || *got != target {
		t.Fatalf("existing archive processed path = %v, %v; want %q", got, err, target)
	}
	if _, moved := session.moved[seedTestHash]; moved {
		t.Fatalf("existing archive should be associated, not moved: %v", session.moved)
	}
}

// TestEnforceSeedPolicySkipsAlreadyArchivedCopy makes sure the end-of-seed
// archive does not move a source whose library copy is already in place.
func TestEnforceSeedPolicySkipsAlreadyArchivedCopy(t *testing.T) {
	db, cfg, view, _, processed := seedTestSetup(t)
	library := filepath.Dir(processed)
	cfg.ArchiveRoot = &library

	session := &stubTorrentSession{list: []models.TorrentView{view}}
	EnforceSeedPolicy(cfg, session, db, map[string]struct{}{}, map[string]StorageMoveRetry{}, map[string]time.Time{})
	if len(session.moved) != 0 {
		t.Fatalf("already archived torrent was moved: %v", session.moved)
	}
}

// TestHandleTorrentEventDefersSingleFileArchive pins the completion side of the
// deferred archive: a single episode in move mode is marked downloaded (so the
// gap filler does not refetch it) but its file stays in the download folder and
// no storage move happens before the seed limit.
func TestHandleTorrentEventDefersSingleFileArchive(t *testing.T) {
	db, cfg, _, source, _ := seedTestSetup(t)
	downloads := filepath.Dir(source)
	library := filepath.Join(filepath.Dir(downloads), "library")
	cfg.ArchiveRoot = &library
	cfg.Settings["move_episodes"] = "true"

	const hash2 = "fedcba9876543210fedcba9876543210fedcba98"
	title := "Show"
	season := int64(2)
	episode := int64(1)
	release := &models.Release{
		Title:   "Show.S02E01.1080p",
		Magnet:  "magnet:?xt=urn:btih:" + hash2,
		Kind:    "series",
		Series:  &title,
		Season:  &season,
		Episode: &episode,
	}
	if err := db.RegisterTorrentScored(release, 100); err != nil {
		t.Fatal(err)
	}
	source2 := filepath.Join(downloads, "Show.S02E01.1080p.mkv")
	if err := os.WriteFile(source2, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	notifier := FromConfig(cfg)
	tmdb := NewTmdbClientWithLanguage(nil, "it-IT")
	event := models.TorrentEvent{
		Kind:     "torrent_finished",
		Hash:     hash2,
		Name:     "Show.S02E01.1080p.mkv",
		SavePath: downloads,
	}
	session := &stubTorrentSession{}
	processed, err := HandleTorrentEvent(cfg, session, db, map[string]struct{}{}, map[string]struct{}{}, map[string]StorageMoveRetry{}, event, tmdb, notifier)
	if err != nil {
		t.Fatalf("HandleTorrentEvent: %v", err)
	}
	if processed {
		t.Fatalf("deferred completion must report not-processed (no move done yet)")
	}
	if len(session.moved) != 0 {
		t.Fatalf("deferred completion must not move storage: %v", session.moved)
	}
	status, _ := db.TorrentStatus(hash2)
	if status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
	processedPath, _ := db.TorrentProcessed(hash2)
	if processedPath != nil && strings.TrimSpace(*processedPath) != "" {
		t.Fatalf("processed path must stay empty until the end of the seed, got %q", *processedPath)
	}
	if _, err := os.Stat(source2); err != nil {
		t.Fatalf("source must stay in downloads for seeding: %v", err)
	}
}

func TestRemoveSeededCompletedRecognizesArchivedNasCopy(t *testing.T) {
	db, cfg, view, _, processed := seedTestSetup(t)
	// Simulate the file having been moved to NAS, so libtorrent reports 0 progress and paused state
	view.Progress = 0.0
	view.TotalDone = 0
	view.State = "paused"

	session := &stubTorrentSession{list: []models.TorrentView{view}}
	cfg.Libtorrent.AutoRemoveCompleted = true
	RemoveSeededCompleted(cfg, session, db, map[string]struct{}{})
	if len(session.removed) != 1 || session.removed[0] != seedTestHash {
		t.Fatalf("archived torrent with 0 local progress must be recognized as completed and removed, got: %v", session.removed)
	}
	if _, err := os.Stat(processed); err != nil {
		t.Fatalf("library copy must stay: %v", err)
	}
}

func TestTeVeSeedsForever(t *testing.T) {
	session := &stubTorrentSession{list: []models.TorrentView{{Hash: seedTestHash, SeedRatio: 0.0, SeedDays: -1}}}
	cfg := DefaultConfig()
	if !tev_seedsForever(&cfg, session, seedTestHash) {
		t.Fatal("per-torrent ratio 0 must mean infinite seed")
	}
	session.list[0].SeedRatio = 2.0
	if tev_seedsForever(&cfg, session, seedTestHash) {
		t.Fatal("finite per-torrent limit must not be infinite")
	}
	session.list[0].SeedRatio = -1.0
	session.list[0].SeedDays = -1
	if !tev_seedsForever(&cfg, session, seedTestHash) {
		t.Fatal("no per-torrent and no global limit must mean infinite seed")
	}
	cfg.Libtorrent.SeedRatio = 1.5
	if tev_seedsForever(&cfg, session, seedTestHash) {
		t.Fatal("global ratio limit must make the policy finite")
	}
}
