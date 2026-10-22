package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
