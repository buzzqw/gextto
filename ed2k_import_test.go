package gextto

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// movieCopySession stands in for aMule's eD2k session in copy mode: the file is
// imported by link or copy and stays in the incoming folder for sharing.
type movieCopySession struct {
	nullEngine
	removed []string
}

func (s *movieCopySession) Remove(hash string, deleteFiles bool) (bool, error) {
	s.removed = append(s.removed, hash)
	return true, nil
}

func (movieCopySession) ImportCompletedByCopy() bool { return true }

type movieImportFixture struct {
	cfg      Config
	db       *Database
	release  *models.Release
	hash     string
	incoming string
	archive  string
	trash    string
	source   string
	event    models.TorrentEvent
}

// newMovieImportFixture builds a completed eD2k movie: the file is in aMule's
// incoming folder, the library is empty and the release is registered so the
// completion handler can read its metadata.
func newMovieImportFixture(t *testing.T) movieImportFixture {
	t.Helper()
	root := t.TempDir()
	incoming := filepath.Join(root, "incoming")
	archive := filepath.Join(root, "library")
	trash := filepath.Join(root, "trash")
	for _, dir := range []string{incoming, archive, trash} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const name = "Example.Movie.2024.1080p.WEB-DL.mkv"
	source := filepath.Join(incoming, name)
	writeFile(t, source, validMKV(4096))

	const ed2kHash = "f7003a131b7d720c48e9edcd3a11cb62"
	link := "ed2k://|file|" + name + "|4096|" + ed2kHash + "|/"
	release := ParseEd2kRelease(name, link, "ed2k:link", 4096, 4, time.Now().UTC())
	if release == nil || release.Kind != "movie" {
		t.Fatalf("release = %+v, want an eD2k movie", release)
	}

	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(root, "data")
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.DownloadDir = incoming
	cfg.ArchiveRoot = &archive
	trashPath := trash
	cfg.TrashPath = &trashPath
	cfg.CleanupAction = "trash"
	cfg.CleanupUpgrades = true
	cfg.RenameEpisodes = true
	cfg.DryRun = false
	cfg.Movies = []MovieConfig{{Name: "Example Movie", Year: "2024", Enabled: true}}
	// A finite seed (ratio and time): an unlimited one keeps eD2k files in aMule.
	cfg.Transfer.SeedRatio, cfg.Transfer.SeedTimeDays = 1, 7

	db, err := OpenDatabase(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.BeginTorrentAdd(release, 10); err != nil {
		t.Fatalf("BeginTorrentAdd: %v", err)
	}
	if err := db.RegisterTorrentScored(release, 10); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}
	if err := db.EndTorrentAdd(release); err != nil {
		t.Fatalf("EndTorrentAdd: %v", err)
	}
	hash, err := releaseIdentityHash(release)
	if err != nil {
		t.Fatal(err)
	}
	return movieImportFixture{
		cfg:      cfg,
		db:       db,
		release:  release,
		hash:     hash,
		incoming: incoming,
		archive:  archive,
		trash:    trash,
		source:   source,
		event: models.TorrentEvent{
			Kind:     "torrent_finished",
			Hash:     hash,
			Name:     name,
			SavePath: incoming,
		},
	}
}

func countVideos(t *testing.T, dir string) int {
	t.Helper()
	files, err := VideoFiles(dir)
	if err != nil {
		t.Fatalf("VideoFiles(%s): %v", dir, err)
	}
	return len(files)
}

func TestEd2kMovieIsImportedByCopy(t *testing.T) {
	f := newMovieImportFixture(t)
	session := &movieCopySession{}

	processed, err := tev_completeMovieWithArchive(&f.cfg, f.db, session, &f.event, f.release, f.archive, NewTmdbClient(nil), false)
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !processed {
		t.Fatal("movie was not imported")
	}
	if got := countVideos(t, f.archive); got != 1 {
		t.Fatalf("videos in the library = %d, want 1", got)
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatalf("the download must stay in aMule's incoming folder: %v", err)
	}
	if len(session.removed) != 0 {
		t.Fatalf("an accepted movie must not be removed from aMule: %v", session.removed)
	}
	if status, _ := f.db.TorrentStatus(f.hash); status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
}

// TestEd2kInterruptedImportResumesFromTheLibraryCopy pins the retry of an import
// stopped after the copy (a restart while a big file was being copied): the
// library already holds the video under its own name, and the retry must finish
// that import instead of comparing the download with its own copy — which is
// never "better" — and moving the source to the trash.
func TestEd2kInterruptedImportResumesFromTheLibraryCopy(t *testing.T) {
	f := newMovieImportFixture(t)
	copied := filepath.Join(f.archive, filepath.Base(f.source))
	writeFile(t, copied, validMKV(4096)) // same name and size: the copy finished

	session := &movieCopySession{}
	processed, err := tev_completeMovieWithArchive(&f.cfg, f.db, session, &f.event, f.release, f.archive, NewTmdbClient(nil), false)
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !processed {
		t.Fatal("the interrupted import was not completed")
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatalf("the download must stay in aMule's incoming folder: %v", err)
	}
	entries, err := os.ReadDir(f.trash)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the download was discarded instead of resumed: %v", entries)
	}
	if len(session.removed) != 0 {
		t.Fatalf("a resumed import must not remove the download from aMule: %v", session.removed)
	}
	if got := countVideos(t, f.archive); got != 1 {
		t.Fatalf("videos in the library = %d, want 1", got)
	}
	if status, _ := f.db.TorrentStatus(f.hash); status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
}

func TestEd2kMovieInferiorIsDiscardedBeforeCopy(t *testing.T) {
	f := newMovieImportFixture(t)
	// The library already holds the same release: the incoming copy is not an
	// upgrade, so it must be discarded and never linked or copied.
	existing := filepath.Join(f.archive, filepath.Base(f.source))
	writeFile(t, existing, validMKV(8192))
	before, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}

	session := &movieCopySession{}
	processed, err := tev_completeMovieWithArchive(&f.cfg, f.db, session, &f.event, f.release, f.archive, NewTmdbClient(nil), false)
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if processed {
		t.Fatal("an inferior movie must not be imported")
	}
	if got := countVideos(t, f.archive); got != 1 {
		t.Fatalf("videos in the library = %d, want the pre-existing one only", got)
	}
	after, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("the existing library file was overwritten by the inferior download")
	}
	if _, err := os.Stat(f.source); !os.IsNotExist(err) {
		t.Fatalf("the discarded download must leave aMule's incoming folder, stat err = %v", err)
	}
	entries, err := os.ReadDir(f.trash)
	if err != nil || len(entries) == 0 {
		t.Fatalf("the discarded file is not in the trash: entries=%d err=%v", len(entries), err)
	}
	if len(session.removed) != 1 {
		t.Fatalf("the discarded download must be removed from aMule: %v", session.removed)
	}
}

func TestEd2kMovieWithoutArchiveStaysInIncoming(t *testing.T) {
	f := newMovieImportFixture(t)
	// No library folder for movies and a download folder that is not aMule's
	// incoming one: the file stays where aMule saved it and is not moved.
	downloads := filepath.Join(t.TempDir(), "downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	f.cfg.ArchiveRoot = nil
	f.cfg.DownloadDir = downloads
	session := &movieCopySession{}

	processed, err := HandleTorrentEvent(
		&f.cfg,
		session,
		f.db,
		map[string]struct{}{},
		map[string]struct{}{},
		map[string]StorageMoveRetry{},
		f.event,
		NewTmdbClient(nil),
		NewNotifier(),
	)
	if err != nil {
		t.Fatalf("HandleTorrentEvent: %v", err)
	}
	if !processed {
		t.Fatal("the completion handler did not accept the movie")
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatalf("the file must stay in aMule's incoming folder: %v", err)
	}
	if status, _ := f.db.TorrentStatus(f.hash); status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
}

func TestEd2kMovieCompletionTakesTheCopyPath(t *testing.T) {
	f := newMovieImportFixture(t)
	session := &movieCopySession{}

	processed, err := HandleTorrentEvent(
		&f.cfg,
		session,
		f.db,
		map[string]struct{}{},
		map[string]struct{}{},
		map[string]StorageMoveRetry{},
		f.event,
		NewTmdbClient(nil),
		NewNotifier(),
	)
	if err != nil {
		t.Fatalf("HandleTorrentEvent: %v", err)
	}
	if !processed {
		t.Fatal("the completion handler did not import the movie")
	}
	if got := countVideos(t, f.archive); got != 1 {
		t.Fatalf("videos in the library = %d, want 1", got)
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatalf("the download must stay in aMule's incoming folder: %v", err)
	}
}

func TestEd2kMovieMoveModeMovesIntoLibrary(t *testing.T) {
	f := newMovieImportFixture(t)
	f.cfg.Settings["move_episodes"] = "true"
	session := ed2kSession{cfg: &f.cfg}
	if session.ImportCompletedByCopy() {
		t.Fatal("move mode must not force the copy import")
	}
	if !session.MoveCompletedImmediately() {
		t.Fatal("eD2k move mode must move at completion")
	}

	processed, err := HandleTorrentEvent(
		&f.cfg,
		session,
		f.db,
		map[string]struct{}{},
		map[string]struct{}{},
		map[string]StorageMoveRetry{},
		f.event,
		NewTmdbClient(nil),
		NewNotifier(),
	)
	if err != nil {
		t.Fatalf("HandleTorrentEvent: %v", err)
	}
	if !processed {
		t.Fatal("the completion handler did not import the movie")
	}
	if got := countVideos(t, f.archive); got != 1 {
		t.Fatalf("videos in the library = %d, want 1", got)
	}
	if _, err := os.Stat(f.source); !os.IsNotExist(err) {
		t.Fatalf("move mode must remove the source from aMule's incoming folder, stat err = %v", err)
	}
	if status, _ := f.db.TorrentStatus(f.hash); status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
}

func TestEd2kMovieCopyModeKeepsIncoming(t *testing.T) {
	f := newMovieImportFixture(t)
	f.cfg.Settings["move_episodes"] = "false"
	session := ed2kSession{cfg: &f.cfg}
	if !session.ImportCompletedByCopy() {
		t.Fatal("copy mode must use the copy import")
	}

	processed, err := HandleTorrentEvent(
		&f.cfg,
		session,
		f.db,
		map[string]struct{}{},
		map[string]struct{}{},
		map[string]StorageMoveRetry{},
		f.event,
		NewTmdbClient(nil),
		NewNotifier(),
	)
	if err != nil {
		t.Fatalf("HandleTorrentEvent: %v", err)
	}
	if !processed {
		t.Fatal("the completion handler did not import the movie")
	}
	if got := countVideos(t, f.archive); got != 1 {
		t.Fatalf("videos in the library = %d, want 1", got)
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatalf("copy mode must keep the source in aMule's incoming folder: %v", err)
	}
}

// An eD2k import is one log line. It used to be the library line plus a second
// "moved into the library" line about the same file, which read like two
// events; the fate of the source now closes the single line.
func TestEd2kImportIsLoggedOnce(t *testing.T) {
	var captured bytes.Buffer
	logging.SetOutput(&captured)
	defer logging.SetOutput(os.Stdout)

	cfg, db, _, _, event := newEpisodeImportFixture(t, true)
	session := listedEd2kSession{ed2kSession: ed2kSession{cfg: &cfg}, hash: event.Hash}
	if _, err := HandleTorrentEvent(&cfg, session, db, map[string]struct{}{}, map[string]struct{}{},
		map[string]StorageMoveRetry{}, event, NewTmdbClient(nil), NewNotifier()); err != nil {
		t.Fatalf("HandleTorrentEvent: %v", err)
	}
	out := captured.String()
	if count := strings.Count(out, "added to the library"); count != 1 {
		t.Fatalf("want one import line, got %d:\n%s", count, out)
	}
	if strings.Contains(out, "moved into the library") {
		t.Fatalf("the same import is still logged twice:\n%s", out)
	}
	if !strings.Contains(out, "the source leaves aMule's incoming folder") {
		t.Fatalf("the import line does not say where the source went:\n%s", out)
	}
}

func TestCompletedEd2kDownloadReachesTheHistory(t *testing.T) {
	f := newMovieImportFixture(t)
	if _, err := tev_completeMovieWithArchive(&f.cfg, f.db, &movieCopySession{}, &f.event, f.release, f.archive, NewTmdbClient(nil), false); err != nil {
		t.Fatalf("completion: %v", err)
	}
	// The entry point of the event worker: without this the row keeps
	// removed_at NULL and the download history (which lists removed rows)
	// never shows the completed eD2k download.
	recordEd2kCompletion(f.db, f.hash, f.event.Name)

	items, total, err := f.db.CompletedTorrents(0, 10, "")
	if err != nil {
		t.Fatalf("CompletedTorrents: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("history total=%d items=%d, want the completed eD2k download", total, len(items))
	}
	if !strings.EqualFold(items[0].Hash, f.hash) || items[0].Name == "" {
		t.Fatalf("history item = %+v", items[0])
	}
}

// listedEd2kSession is aMule's session with the finished file still in its
// queue: the view carries no seed limits, which the seed policy reads as
// "seed forever".
type listedEd2kSession struct {
	ed2kSession
	hash string
}

func (s listedEd2kSession) List() []models.TorrentView {
	return []models.TorrentView{{Hash: s.hash, Network: networkEd2k, State: "seeding", Progress: 100}}
}

// newEpisodeImportFixture builds a completed eD2k episode in aMule's incoming
// folder, with the series configured and its library folder empty.
func newEpisodeImportFixture(t *testing.T, move bool) (Config, *Database, string, string, models.TorrentEvent) {
	t.Helper()
	root := t.TempDir()
	incoming := filepath.Join(root, "incoming")
	archive := filepath.Join(root, "library", "Example Show")
	for _, dir := range []string{incoming, archive} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const name = "Example.Show.S01E02.1080p.WEB-DL.mkv"
	source := filepath.Join(incoming, name)
	writeFile(t, source, validMKV(4096))
	link := "ed2k://|file|" + name + "|4096|a1b2c3d4e5f60718293a4b5c6d7e8f90|/"
	release := ParseEd2kRelease(name, link, "ed2k:link", 4096, 4, time.Now().UTC())
	if release == nil || release.Kind != "series" {
		t.Fatalf("release = %+v, want an eD2k episode", release)
	}
	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(root, "data")
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.DownloadDir = incoming
	cfg.RenameEpisodes = true
	cfg.DryRun = false
	cfg.Series = []SeriesConfig{{Name: "Example Show", Seasons: "1+", ArchivePath: archive, Enabled: true}}
	cfg.Settings["move_episodes"] = map[bool]string{true: "true", false: "false"}[move]
	cfg.Transfer.SeedRatio, cfg.Transfer.SeedTimeDays = 1, 7
	db, err := OpenDatabase(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RegisterTorrentScored(release, 10); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}
	hash, err := releaseIdentityHash(release)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, db, archive, source, models.TorrentEvent{Kind: "torrent_finished", Hash: hash, Name: name, SavePath: incoming}
}

// TestEd2kEpisodeFollowsMoveOrCopy: an eD2k episode follows the move/copy
// option under a finite seed, even while aMule still lists the file with no
// limits of its own (which once read as "seed forever" and made every eD2k
// episode a copy). An unlimited configured seed keeps it in aMule: see
// TestEd2kUnlimitedSeedKeepsTheFileInAmule.
func TestEd2kEpisodeFollowsMoveOrCopy(t *testing.T) {
	for _, move := range []bool{true, false} {
		cfg, db, archive, source, event := newEpisodeImportFixture(t, move)
		session := listedEd2kSession{ed2kSession: ed2kSession{cfg: &cfg}, hash: event.Hash}
		processed, err := HandleTorrentEvent(&cfg, session, db, map[string]struct{}{}, map[string]struct{}{},
			map[string]StorageMoveRetry{}, event, NewTmdbClient(nil), NewNotifier())
		if err != nil || !processed {
			t.Fatalf("move=%v: processed=%v err=%v", move, processed, err)
		}
		if got := countVideos(t, archive); got != 1 {
			t.Fatalf("move=%v: videos in the library = %d, want 1", move, got)
		}
		_, statErr := os.Stat(source)
		if move && !os.IsNotExist(statErr) {
			t.Fatalf("move mode must take the episode out of aMule's incoming folder, stat err = %v", statErr)
		}
		if !move && statErr != nil {
			t.Fatalf("copy mode must keep the episode in aMule's incoming folder: %v", statErr)
		}
	}
}

// TestEd2kUnlimitedSeedKeepsTheFileInAmule: with the seed configured as
// unlimited (no ratio and no time limit) an eD2k episode or movie is copied
// into the library and stays in aMule's incoming folder, shared, even in move
// mode, like a torrent that seeds forever.
func TestEd2kUnlimitedSeedKeepsTheFileInAmule(t *testing.T) {
	cfg, db, archive, source, event := newEpisodeImportFixture(t, true)
	// Only the ratio is infinite: that alone keeps the file sharing.
	cfg.Transfer.SeedRatio = 0
	session := listedEd2kSession{ed2kSession: ed2kSession{cfg: &cfg}, hash: event.Hash}
	if processed, err := HandleTorrentEvent(&cfg, session, db, map[string]struct{}{}, map[string]struct{}{},
		map[string]StorageMoveRetry{}, event, NewTmdbClient(nil), NewNotifier()); err != nil || !processed {
		t.Fatalf("episode: processed=%v err=%v", processed, err)
	}
	if countVideos(t, archive) != 1 {
		t.Fatal("the episode did not reach the library")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("an unlimited seed must keep the episode in aMule's incoming folder: %v", err)
	}

	f := newMovieImportFixture(t)
	f.cfg.Settings["move_episodes"] = "true"
	f.cfg.Transfer.SeedRatio = 0
	if processed, err := HandleTorrentEvent(&f.cfg, ed2kSession{cfg: &f.cfg}, f.db, map[string]struct{}{}, map[string]struct{}{},
		map[string]StorageMoveRetry{}, f.event, NewTmdbClient(nil), NewNotifier()); err != nil || !processed {
		t.Fatalf("movie: processed=%v err=%v", processed, err)
	}
	if countVideos(t, f.archive) != 1 {
		t.Fatal("the movie did not reach the library")
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatalf("an unlimited seed must keep the movie in aMule's incoming folder: %v", err)
	}
}

type skippedMovieSession struct {
	movieCopySession
	names []string
}

func (s *skippedMovieSession) SkippedVideoFiles(string) ([]string, bool, error) {
	return s.names, true, nil
}

func TestCompletedTorrentReportsSkippedVideoFiles(t *testing.T) {
	f := newMovieImportFixture(t)
	if err := os.Remove(f.source); err != nil {
		t.Fatal(err)
	}
	session := &skippedMovieSession{names: []string{"Example.Movie.2024.1080p.WEB-DL.mkv"}}
	processed, err := tev_completeTorrentOptions(
		&f.cfg, f.db, session, &f.event, f.release, NewTmdbClient(nil), tev_importSource{preserveSource: true},
	)
	if processed {
		t.Fatal("a completion with only skipped files must not be imported")
	}
	if err == nil || !strings.Contains(err.Error(), "qBittorrent skipped") {
		t.Fatalf("error = %v, want the skipped-file explanation", err)
	}
}
