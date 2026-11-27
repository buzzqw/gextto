package gextto

import "testing"

// The registry is keyed by torrent hash, survives several overlapping begins
// for the same torrent and is idempotent when the copy ends.
func TestLibraryCopyTracker(t *testing.T) {
	hash := "ABCDEF0123456789ABCDEF0123456789ABCDEF01"
	if libraryCopyInProgress(hash) {
		t.Fatal("a copy is already marked for a torrent that never copied anything")
	}
	first := beginLibraryCopy(hash, "/library/file.mkv")
	second := beginLibraryCopy(hash, "/library/other.mkv")
	if !libraryCopyInProgress(hash) || !libraryCopyInProgress("abcdef0123456789abcdef0123456789abcdef01") {
		t.Fatal("the copy is not marked by hash, case-insensitively")
	}
	first()
	if !libraryCopyInProgress(hash) {
		t.Fatal("the badge disappeared while a second copy is still running")
	}
	second()
	second()
	if libraryCopyInProgress(hash) {
		t.Fatal("the copy is still marked after it ended")
	}
}
