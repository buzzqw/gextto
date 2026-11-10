package gextto

// torrent_event_behaviour_test.go pins what HandleTorrentEvent does over the
// life of one download — completion, the move into the library, a failed
// move and its retry, the import once the files arrived, and a completion
// recovered after a restart — so splitting the handler by phase cannot change
// it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

const behaviourHash = "fedcba9876543210fedcba9876543210fedcba98"

// countingSession counts the storage moves on top of the stub session.
type countingSession struct {
	stubTorrentSession
	moves int
}

func (s *countingSession) MoveStorage(hash, destination string) (bool, error) {
	s.moves++
	return s.stubTorrentSession.MoveStorage(hash, destination)
}

type behaviourEnv struct {
	db        *Database
	cfg       *Config
	session   *countingSession
	downloads string
	library   string
	name      string
	moves     map[string]struct{}
	postSeed  map[string]struct{}
	retries   map[string]StorageMoveRetry
}

func newBehaviourEnv(t *testing.T) *behaviourEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDatabase(filepath.Join(dir, "gextto.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	downloads, library, trash := filepath.Join(dir, "downloads"), filepath.Join(dir, "library"), filepath.Join(dir, "trash")
	for _, d := range []string{downloads, library} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	year := int64(1999)
	release := &models.Release{
		Title:  "Example.Movie.1999.1080p.WEB.H264-GROUP",
		Magnet: "magnet:?xt=urn:btih:" + behaviourHash,
		Kind:   "movie",
		Year:   &year,
	}
	if err := db.RegisterTorrentScored(release, 1000); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}
	name := "Example.Movie.1999.1080p.WEB.H264-GROUP.mkv"
	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = byte(i*7 + 1) // written data, not a preallocated file of zeros
	}
	copy(payload, []byte{0x1A, 0x45, 0xDF, 0xA3}) // a Matroska header
	if err := os.WriteFile(filepath.Join(downloads, name), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{}
	cfg.DownloadDir = downloads
	cfg.ArchiveRoot = &library
	cfg.TrashPath = &trash
	session := &countingSession{stubTorrentSession: stubTorrentSession{list: []models.TorrentView{{
		Hash: behaviourHash, Name: name, SavePath: downloads, State: "seeding",
		Progress: 100, TotalSize: 64 * 1024, TotalDone: 64 * 1024,
	}}}}
	return &behaviourEnv{db: db, cfg: &cfg, session: session, downloads: downloads, library: library, name: name,
		moves: map[string]struct{}{}, postSeed: map[string]struct{}{}, retries: map[string]StorageMoveRetry{}}
}

func (e *behaviourEnv) handle(t *testing.T, kind, savePath, message string) bool {
	t.Helper()
	event := models.TorrentEvent{Kind: kind, Hash: behaviourHash, Name: e.name, SavePath: savePath, Message: message}
	processed, err := HandleTorrentEvent(e.cfg, e.session, e.db, e.moves, e.postSeed, e.retries, event,
		NewTmdbClientWithLanguage(nil, "it-IT"), FromConfig(e.cfg))
	if err != nil {
		t.Fatalf("%s: %v", kind, err)
	}
	return processed
}

// The life of a film: finished → moved into the library (once, however many
// times the engine repeats the alert) → a failed move scheduled again → the
// files arrive and the film is imported → the completion recovered after a
// restart changes nothing.
func TestTorrentEventLifeOfACompletedFilm(t *testing.T) {
	e := newBehaviourEnv(t)

	if e.handle(t, "torrent_finished", e.downloads, "") {
		t.Fatal("finishing must ask for the move, not import yet")
	}
	if got := e.session.moved[behaviourHash]; got != e.library {
		t.Fatalf("moved to %q, want the library %q", got, e.library)
	}
	if _, ok := e.moves[behaviourHash]; !ok {
		t.Fatal("the move request was not recorded")
	}
	if r, ok := e.retries[behaviourHash]; !ok || !r.inFlight || r.destination != e.library {
		t.Fatalf("in-flight move = %+v ok=%v", r, ok)
	}

	// The engine repeats the alert while the move runs: no second move.
	e.handle(t, "torrent_finished", e.downloads, "")
	if e.session.moves != 1 {
		t.Fatalf("%d moves for one completion", e.session.moves)
	}

	// The move fails: it is scheduled again for the same destination.
	e.handle(t, "storage_move_failed", e.downloads, "disk busy")
	r, ok := e.retries[behaviourHash]
	if !ok || r.inFlight || r.attempts != 1 || r.destination != e.library || !r.nextAttempt.After(time.Now()) {
		t.Fatalf("retry after a failed move = %+v ok=%v", r, ok)
	}
	if _, ok := e.moves[behaviourHash]; ok {
		t.Fatal("a failed move left its request open")
	}

	// The retry succeeds: the files are in the library and the film is imported.
	if err := os.Rename(filepath.Join(e.downloads, e.name), filepath.Join(e.library, e.name)); err != nil {
		t.Fatal(err)
	}
	e.session.list[0].SavePath = e.library
	if !e.handle(t, "storage_moved", e.library, "") {
		t.Fatal("the arrival in the library was not processed as the completion")
	}
	if _, ok := e.retries[behaviourHash]; ok {
		t.Fatal("the retry is still scheduled after the move succeeded")
	}
	status, _ := e.db.TorrentStatus(behaviourHash)
	if status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
	archived, _ := e.db.TorrentProcessed(behaviourHash)
	if archived == nil || !strings.HasPrefix(*archived, e.library) {
		t.Fatalf("archived path = %v, want inside %q", archived, e.library)
	}
	if _, err := os.Stat(*archived); err != nil {
		t.Fatalf("the archived file is missing: %v", err)
	}

	// After a restart the engine reports the completion again, with fresh
	// in-memory state: nothing is moved or imported a second time.
	e.moves, e.postSeed, e.retries = map[string]struct{}{}, map[string]struct{}{}, map[string]StorageMoveRetry{}
	moves := e.session.moves
	if e.handle(t, "torrent_finished", e.library, "") {
		t.Fatal("a completion recovered after a restart was imported again")
	}
	if e.handle(t, "storage_moved", e.library, "") {
		t.Fatal("a storage move recovered after a restart was imported again")
	}
	if e.session.moves != moves || len(e.retries) != 0 {
		t.Fatalf("a recovered completion moved again: %d moves, retries %+v", e.session.moves-moves, e.retries)
	}
}

// A move that fails because the files already reached the destination is
// done, not retried.
func TestTorrentEventMoveFailureAlreadySatisfied(t *testing.T) {
	e := newBehaviourEnv(t)
	e.handle(t, "torrent_finished", e.downloads, "")
	if err := os.Rename(filepath.Join(e.downloads, e.name), filepath.Join(e.library, e.name)); err != nil {
		t.Fatal(err)
	}
	e.session.list[0].SavePath = e.library
	e.handle(t, "storage_move_failed", e.downloads, "file exists")
	if r, ok := e.retries[behaviourHash]; ok && !r.inFlight {
		t.Fatalf("a satisfied move was scheduled again: %+v", r)
	}
}

// Engine alerts never touch the database and never fail the handler.
func TestTorrentEventAlertsAreHandledWithoutTheDatabase(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{}
	for _, kind := range []string{"tracker_error", "hash_failed", "resume_save_failed", "torrent_error", "portmap_error", "session_error"} {
		event := models.TorrentEvent{Kind: kind, Hash: behaviourHash, Name: "x", Message: "m"}
		processed, err := HandleTorrentEvent(&cfg, &stubTorrentSession{}, nil, nil, nil, nil, event, nil, FromConfig(&cfg))
		if processed || err != nil {
			t.Fatalf("%s: processed=%v err=%v", kind, processed, err)
		}
	}
}

// A torrent Gextto did not choose is marked completed where it is, once.
func TestTorrentEventForeignTorrentCompletes(t *testing.T) {
	e := newBehaviourEnv(t)
	const foreign = "1111111111111111111111111111111111111111"
	event := models.TorrentEvent{Kind: "torrent_finished", Hash: foreign, Name: "something", SavePath: e.downloads}
	processed, err := HandleTorrentEvent(e.cfg, e.session, e.db, e.moves, e.postSeed, e.retries, event, nil, FromConfig(e.cfg))
	if processed || err != nil {
		t.Fatalf("foreign completion: processed=%v err=%v", processed, err)
	}
	if e.session.moves != 0 {
		t.Fatal("a foreign torrent was moved")
	}
}

// An episode with no library folder is completed where it was downloaded.
func TestTorrentEventEpisodeWithoutLibraryStaysInPlace(t *testing.T) {
	e := newBehaviourEnv(t)
	e.cfg.ArchiveRoot = nil
	const hash = "2222222222222222222222222222222222222222"
	show, season, episode := "Series A", int64(1), int64(2)
	release := &models.Release{Title: "Series.A.S01E02.1080p.WEB.H264-GROUP", Magnet: "magnet:?xt=urn:btih:" + hash,
		Kind: "series", Series: &show, Season: &season, Episode: &episode}
	if err := e.db.RegisterTorrentScored(release, 100); err != nil {
		t.Fatal(err)
	}
	name := "Series.A.S01E02.1080p.WEB.H264-GROUP.mkv"
	if err := os.WriteFile(filepath.Join(e.downloads, name), []byte("\x1a\x45\xdf\xa3 episode payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	event := models.TorrentEvent{Kind: "torrent_finished", Hash: hash, Name: name, SavePath: e.downloads}
	processed, err := HandleTorrentEvent(e.cfg, e.session, e.db, e.moves, e.postSeed, e.retries, event, nil, FromConfig(e.cfg))
	if err != nil || !processed {
		t.Fatalf("episode without library: processed=%v err=%v", processed, err)
	}
	if e.session.moves != 0 {
		t.Fatal("an episode without a library folder was moved")
	}
	archived, _ := e.db.TorrentProcessed(hash)
	if archived == nil || *archived != filepath.Join(e.downloads, name) {
		t.Fatalf("recorded path = %v, want the download itself", archived)
	}
}
