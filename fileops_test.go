package gextto

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func init() {
	// Several tests stop the background workers and keep copying files in the
	// same process; the daemon's "block after close" would hang them.
	mediaFileOps.blockWhenClosed = false
}

func TestFileOperationRegistryTracksAndCloses(t *testing.T) {
	registry := &fileOperationRegistry{active: map[uint64]fileOperation{}, blockWhenClosed: true}
	done := registry.begin("Episode.S01E01.mkv")
	if names := registry.runningNames(); len(names) != 1 || names[0] != "Episode.S01E01.mkv" {
		t.Fatalf("running = %v", names)
	}
	if registry.closeIfIdle() {
		t.Fatal("the registry must not close while a copy is running")
	}
	done()
	done() // idempotent
	if !registry.closeIfIdle() {
		t.Fatal("an idle registry should close")
	}
	started := make(chan struct{})
	go func() {
		registry.begin("late.mkv")
		close(started)
	}()
	select {
	case <-started:
		t.Fatal("a copy must not start after shutdown closed the registry")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCopyFileAtomicallyRegistersOperation(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	if err := os.WriteFile(source, []byte(strings.Repeat("x", 1024)), 0o644); err != nil {
		t.Fatal(err)
	}
	before := len(mediaFileOps.running())
	if err := copyFileAtomically(source, filepath.Join(dir, "lib", "target.mkv")); err != nil {
		t.Fatal(err)
	}
	if after := len(mediaFileOps.running()); after != before {
		t.Fatalf("operation not released: before=%d after=%d", before, after)
	}
}

func TestStopBackgroundWorkersWaitsForFileCopies(t *testing.T) {
	previousTimeout, previousNotice := workerStopTimeout, shutdownNoticeInterval
	workerStopTimeout, shutdownNoticeInterval = 50*time.Millisecond, time.Hour
	defer func() { workerStopTimeout, shutdownNoticeInterval = previousTimeout, previousNotice }()
	previous := mediaFileOps
	mediaFileOps = &fileOperationRegistry{active: map[uint64]fileOperation{}}
	defer func() { mediaFileOps = previous }()

	state := &AppState{bgStop: make(chan struct{})}
	// A worker stuck on something that is not a file copy...
	state.bgWG.Add(1)
	defer state.bgWG.Done()
	// ...while a copy runs for longer than workerStopTimeout.
	finish := mediaFileOps.begin("Big.Season.Pack.mkv")
	go func() {
		time.Sleep(400 * time.Millisecond)
		finish()
	}()
	started := time.Now()
	stopBackgroundWorkers(state)
	if elapsed := time.Since(started); elapsed < 400*time.Millisecond {
		t.Fatalf("shutdown returned after %v, while the copy was still running", elapsed)
	}
	if !mediaFileOps.closed {
		t.Fatal("the registry should be closed when shutdown proceeds")
	}
}

func TestRetryStorageMovesHonoursBackoffAfterFailure(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "downloads")
	destination := filepath.Join(dir, "library")
	for _, path := range []string{source, destination} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	session := &stubTorrentSession{list: []models.TorrentView{{Hash: seedTestHash, Name: "Show.S01", SavePath: source}}}
	retries := map[string]StorageMoveRetry{}
	moveRequests := map[string]struct{}{}
	postSeed := map[string]struct{}{}
	// A move just failed: it is scheduled with a backoff.
	tev_scheduleStorageMoveRetry(retries, seedTestHash, destination, true, time.Now())
	for tick := 0; tick < 20; tick++ {
		RetryStorageMoves(session, moveRequests, postSeed, retries)
	}
	if len(session.moved) != 0 {
		t.Fatalf("a failed move was re-issued before its backoff expired: %v", session.moved)
	}
	entry := retries[seedTestHash]
	entry.nextAttempt = time.Now().Add(-time.Second)
	retries[seedTestHash] = entry
	RetryStorageMoves(session, moveRequests, postSeed, retries)
	if session.moved[seedTestHash] != destination {
		t.Fatalf("the move should be retried once the backoff expired: %v", session.moved)
	}
}

type movingStubSession struct {
	stubTorrentSession
	moving map[string]string
}

func (s *movingStubSession) MovingStorage() (map[string]string, bool) { return s.moving, true }

func TestRetryStorageMovesWaitsWhileLibtorrentIsStillCopying(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "downloads")
	destination := filepath.Join(dir, "library")
	for _, path := range []string{source, destination} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	session := &movingStubSession{
		stubTorrentSession: stubTorrentSession{list: []models.TorrentView{{Hash: seedTestHash, Name: "Show.S01", SavePath: source}}},
		moving:             map[string]string{seedTestHash: "Show.S01"},
	}
	retries := map[string]StorageMoveRetry{
		seedTestHash: {destination: destination, postSeed: true, inFlight: true, nextAttempt: time.Now().Add(-time.Second)},
	}
	RetryStorageMoves(session, map[string]struct{}{}, map[string]struct{}{}, retries)
	if len(session.moved) != 0 {
		t.Fatalf("a move libtorrent is still copying must not be re-issued: %v", session.moved)
	}
	entry := retries[seedTestHash]
	if !entry.inFlight || !entry.nextAttempt.After(time.Now()) || entry.attempts != 0 {
		t.Fatalf("the in-flight move should just be checked again later: %+v", entry)
	}
	// Once libtorrent is done and the torrent still is not there, it is retried.
	session.moving = map[string]string{}
	entry.nextAttempt = time.Now().Add(-time.Second)
	retries[seedTestHash] = entry
	RetryStorageMoves(session, map[string]struct{}{}, map[string]struct{}{}, retries)
	if session.moved[seedTestHash] != destination {
		t.Fatalf("a finished-but-not-applied move should be retried: %v", session.moved)
	}
}

func TestManualMoveAllowsConfiguredSeriesArchive(t *testing.T) {
	cfg := &Config{Series: []SeriesConfig{{Name: "Wolf Like Me", ArchivePath: "/nas/SerieTV/Wolf.Like.Me"}}}
	if !gh7_isConfiguredArchive(cfg, nil, seedTestHash, "/nas/SerieTV/Wolf.Like.Me") {
		t.Fatal("the archive of a configured series should be an allowed destination")
	}
	if gh7_isConfiguredArchive(cfg, nil, seedTestHash, "/etc") {
		t.Fatal("an unrelated folder must not be allowed")
	}
}

func TestTorrentTransferringAndIdle(t *testing.T) {
	cases := []struct {
		view         models.TorrentView
		transferring bool
		idle         bool
	}{
		{models.TorrentView{State: "downloading", Progress: 40, DownloadRate: 1}, true, false},
		{models.TorrentView{State: "seeding", Progress: 100, UploadRate: 1}, true, false},
		{models.TorrentView{State: "downloading", Progress: 74}, false, true},
		{models.TorrentView{State: "checking_files", Progress: 27}, false, true},
		{models.TorrentView{State: "paused", Progress: 10}, false, false},
		{models.TorrentView{State: "seeding", Progress: 100}, false, false},
	}
	for _, tc := range cases {
		if got := TorrentTransferring(tc.view); got != tc.transferring {
			t.Errorf("TorrentTransferring(%+v) = %v", tc.view, got)
		}
		if got := TorrentIdle(tc.view); got != tc.idle {
			t.Errorf("TorrentIdle(%+v) = %v", tc.view, got)
		}
	}
}

type busyMoveSession struct {
	stubTorrentSession
	calls int
}

func (s *busyMoveSession) MoveStorage(hash, destination string) (bool, error) {
	s.calls++
	return false, errors.New("gx-torrent: the torrent is already being moved")
}

func TestRetryStorageMovesWaitsForAMoveTheEngineIsAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "downloads")
	destination := filepath.Join(dir, "library")
	for _, path := range []string{source, destination} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	session := &busyMoveSession{stubTorrentSession: stubTorrentSession{list: []models.TorrentView{{Hash: seedTestHash, Name: "Show.S01", SavePath: source}}}}
	// Restored after a Gextto restart: not in flight for this run yet.
	retries := map[string]StorageMoveRetry{
		seedTestHash: {destination: destination, postSeed: true, nextAttempt: time.Now().Add(-time.Second)},
	}
	moveRequests := map[string]struct{}{}
	RetryStorageMoves(session, moveRequests, map[string]struct{}{}, retries)
	entry := retries[seedTestHash]
	if !entry.inFlight || entry.attempts != 0 || !entry.nextAttempt.After(time.Now()) {
		t.Fatalf("a move already running must be waited for, not counted as a failure: %+v", entry)
	}
	for tick := 0; tick < 10; tick++ {
		RetryStorageMoves(session, moveRequests, map[string]struct{}{}, retries)
	}
	if session.calls != 1 {
		t.Fatalf("the running move was re-issued %d times", session.calls)
	}
}
