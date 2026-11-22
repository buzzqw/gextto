package gextto

import (
	"path/filepath"
	"testing"
)


// TestTrashJournalRoundTrip: a move records where the entry came from and the
// restore path can be read back; deleting a stale line is harmless.
func TestTrashJournalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	recordTrashMove(dir, "/media/series/episode.mkv", filepath.Join(dir, "episode.mkv"))
	if got := trashOriginalPath(dir, "episode.mkv"); got != "/media/series/episode.mkv" {
		t.Fatalf("original = %q, want the recorded path", got)
	}
	recordTrashMove(dir, "/media/other.mkv", filepath.Join(dir, "other.mkv"))
	forgetTrashMove(dir, "episode.mkv")
	if got := trashOriginalPath(dir, "episode.mkv"); got != "" {
		t.Fatalf("forgotten entry still has an original: %q", got)
	}
	if got := trashOriginalPath(dir, "other.mkv"); got != "/media/other.mkv" {
		t.Fatalf("the other entry was lost: %q", got)
	}
	forgetTrashMove(dir, "..") // traversal attempts must not touch the journal
}
