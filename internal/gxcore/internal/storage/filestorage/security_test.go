package filestorage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestOpenRefusesEscapingIntermediateSymlink pins the root boundary on parent
// creation and file open: a pre-existing link inside the download directory
// must not redirect a torrent file into a sibling directory.
func TestOpenRefusesEscapingIntermediateSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows junction case is covered separately")
	}
	base := t.TempDir()
	root := filepath.Join(base, "downloads")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	s, err := New(root, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Open(filepath.Join("link", "escaped.bin"), 16); err == nil {
		t.Fatal("an intermediate symlink escaping the storage root was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.bin")); !os.IsNotExist(err) {
		t.Fatalf("a file appeared outside the storage root: %v", err)
	}
}

// TestOpenRefusesEscapingFinalSymlink checks the terminal component too. The
// outside file is deliberately shorter than the torrent expects, so following
// the link would both escape the root and truncate someone else's data.
func TestOpenRefusesEscapingFinalSymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "downloads")
	outside := filepath.Join(base, "outside.bin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "file.bin")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	s, err := New(root, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Open("file.bin", 16); err == nil {
		t.Fatal("a final symlink escaping the storage root was accepted")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "keep" {
		t.Fatalf("outside file changed: %q, %v", data, err)
	}
}
