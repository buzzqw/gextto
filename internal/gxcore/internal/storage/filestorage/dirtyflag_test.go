package filestorage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyncSkipsCleanFile pins the fix for the resume tick: the engine syncs every
// open file of every torrent before persisting the bitfield, and a torrent with
// thousands of files paid one fdatasync per file even when nothing had been
// written. A clean file must not reach the syscall, and a failed flush must stay
// marked so the next tick retries.
func TestSyncSkipsCleanFile(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "storage"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := s.Open(filepath.Join("folder", "a.bin"), 4)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	of, ok := f.(*osFile)
	if !ok {
		t.Fatalf("unexpected file type %T", f)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("sync of a fresh file: %v", err)
	}
	if of.dirty.Load() {
		t.Fatal("a file nobody wrote was marked dirty")
	}
	if _, err := f.WriteAt([]byte("data"), 0); err != nil {
		t.Fatal(err)
	}
	if !of.dirty.Load() {
		t.Fatal("a write did not mark the file dirty")
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("sync after write: %v", err)
	}
	if of.dirty.Load() {
		t.Fatal("Sync left the file marked dirty")
	}
	// The bytes are on disk after the flush.
	data, err := os.ReadFile(filepath.Join(dir, "storage", "folder", "a.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "data" {
		t.Fatalf("unexpected content %q", data)
	}
}
