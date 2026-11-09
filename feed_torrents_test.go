package gextto

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSweepStagedFeedTorrentsRemovesOnlyOldCopies pins the safety net: only
// staged .torrent files past the retention bound go away; a file the current
// cycle just downloaded, or a non-torrent file, stays.
func TestSweepStagedFeedTorrentsRemovesOnlyOldCopies(t *testing.T) {
	state := t.TempDir()
	dir := filepath.Join(state, "feed_torrents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	oldPath := filepath.Join(dir, "old.torrent")
	freshPath := filepath.Join(dir, "fresh.torrent")
	keepPath := filepath.Join(dir, "notes.txt")
	for _, path := range []string{oldPath, freshPath, keepPath} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-feedTorrentRetention - time.Hour)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}

	if got := sweepStagedFeedTorrents(state, now); got != 1 {
		t.Fatalf("sweep removed %d files, want 1", got)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("the old staged .torrent must be removed, stat = %v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("the fresh staged .torrent must stay: %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("a non-torrent file must stay: %v", err)
	}
	if got := sweepStagedFeedTorrents(filepath.Join(state, "missing"), now); got != 0 {
		t.Fatalf("a missing folder removed %d files, want 0", got)
	}
}

// TestRemoveTorrentFileIgnoresMissing pins that the cleanup after an add never
// turns a missing or empty path into an error.
func TestRemoveTorrentFileIgnoresMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.torrent")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeTorrentFile(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the .torrent must be removed, stat = %v", err)
	}
	removeTorrentFile(path)
	removeTorrentFile("")
}
