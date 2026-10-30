package filestorage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenRefusesPathOutsideRoot is the last line of defence on the write path:
// storage.Open is called with torrent-relative names, and a ".." component (a
// crafted torrent name, a rename, a resume override) must never create or
// truncate a file above the download folder.
func TestOpenRefusesPathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "downloads")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := New(dest, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escaped.bin", filepath.Join("..", "escaped.bin"), "/etc/passwd", "."} {
		if _, _, err := s.Open(name, 10); err == nil {
			t.Errorf("Open(%q) was accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "escaped.bin")); err == nil {
		t.Fatal("a file was created outside the storage root")
	}
	if _, _, err := s.Open(filepath.Join("folder", "ok.bin"), 10); err != nil {
		t.Fatalf("a normal path was refused: %v", err)
	}
}
