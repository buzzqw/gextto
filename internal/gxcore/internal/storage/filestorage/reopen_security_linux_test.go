//go:build linux

package filestorage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReopenRefusesEscapingIntermediateSymlink covers the transient-I/O retry
// path: after a file is open, replacing one of its parents with a link must not
// make the fresh handle escape the original storage root.
func TestReopenRefusesEscapingIntermediateSymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "downloads")
	dir := filepath.Join(root, "folder")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := New(root, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := s.Open(filepath.Join("folder", "a.bin"), 4)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	of := f.(*osFile)
	if _, err := f.WriteAt([]byte("data"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, filepath.Join(root, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	if err := of.reopen(); err == nil {
		t.Fatal("reopen followed an intermediate symlink outside the storage root")
	}
	if _, err := os.Stat(filepath.Join(outside, "a.bin")); !os.IsNotExist(err) {
		t.Fatalf("reopen created a file outside the storage root: %v", err)
	}
}
