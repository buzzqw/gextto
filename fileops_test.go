package gextto

import (
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
