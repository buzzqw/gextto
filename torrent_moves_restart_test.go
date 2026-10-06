package gextto

import (
	"testing"
	"time"
)

// A storage move in progress must survive a restart: the retry schedule and the
// post-seed protection are saved by the running worker and restored by the next.
func TestStorageMovesSurviveRestart(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()

	// Worker before the restart.
	retries := map[string]StorageMoveRetry{
		"aaaa": {destination: "/archive/Show/Season 01", postSeed: true, attempts: 2, nextAttempt: now.Add(time.Minute), inFlight: true},
		"bbbb": {destination: "/archive/Movies", attempts: 0, nextAttempt: now},
	}
	postSeed := map[string]struct{}{"aaaa": {}, "cccc": {}}
	persisted := restoreTorrentMoves(db, map[string]StorageMoveRetry{}, map[string]struct{}{}, now)
	syncTorrentMoves(db, persisted, retries, postSeed)

	// Worker after the restart.
	restoredRetries := map[string]StorageMoveRetry{}
	restoredPostSeed := map[string]struct{}{}
	restoreTorrentMoves(db, restoredRetries, restoredPostSeed, now)

	if len(restoredRetries) != 2 {
		t.Fatalf("restored retries = %+v, want 2", restoredRetries)
	}
	got := restoredRetries["aaaa"]
	if got.destination != "/archive/Show/Season 01" || !got.postSeed || got.attempts != 2 {
		t.Fatalf("restored retry = %+v", got)
	}
	// Nothing runs after a restart: the move is checked again after the grace.
	if !got.inFlight || got.nextAttempt.Before(now.Add(torrentMoveRestoreGrace-time.Second)) {
		t.Fatalf("restored retry must wait the grace period: %+v", got)
	}
	if _, ok := restoredPostSeed["aaaa"]; !ok {
		t.Fatal("post-seed protection of a pending move was lost")
	}
	// A protection without its retry has nothing left to protect.
	if _, ok := restoredPostSeed["cccc"]; ok {
		t.Fatal("stale post-seed protection was restored")
	}
}

// Finished moves must leave the database, and unchanged state must not be
// rewritten at every tick.
func TestSyncTorrentMovesRemovesFinishedMoves(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	retries := map[string]StorageMoveRetry{"aaaa": {destination: "/archive/A", nextAttempt: now}}
	postSeed := map[string]struct{}{}
	persisted := restoreTorrentMoves(db, map[string]StorageMoveRetry{}, map[string]struct{}{}, now)
	syncTorrentMoves(db, persisted, retries, postSeed)

	var updatedAt string
	if err := db.db.QueryRow("SELECT updated_at FROM torrent_moves WHERE hash='aaaa'").Scan(&updatedAt); err != nil {
		t.Fatalf("move not saved: %v", err)
	}
	if _, err := db.db.Exec("UPDATE torrent_moves SET updated_at='marker' WHERE hash='aaaa'"); err != nil {
		t.Fatal(err)
	}
	// Only the volatile fields change: no write.
	entry := retries["aaaa"]
	entry.nextAttempt = now.Add(time.Hour)
	entry.inFlight = true
	retries["aaaa"] = entry
	syncTorrentMoves(db, persisted, retries, postSeed)
	if err := db.db.QueryRow("SELECT updated_at FROM torrent_moves WHERE hash='aaaa'").Scan(&updatedAt); err != nil || updatedAt != "marker" {
		t.Fatalf("unchanged move was rewritten: updated_at=%q err=%v", updatedAt, err)
	}

	delete(retries, "aaaa")
	syncTorrentMoves(db, persisted, retries, postSeed)
	var count int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM torrent_moves").Scan(&count); err != nil || count != 0 {
		t.Fatalf("finished move still saved: count=%d err=%v", count, err)
	}
	if len(persisted) != 0 {
		t.Fatalf("persisted snapshot not cleared: %+v", persisted)
	}
}
