package filestorage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteSyncPersists guards the O_SYNC removal: files are opened buffered and
// durability now comes from storage.File.Sync(), so a write followed by Sync
// must be on disk after the file is reopened.
func TestWriteSyncPersists(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f, exists, err := s.Open("a.bin", 4)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("a fresh file must not exist")
	}
	if _, err := f.WriteAt([]byte("data"), 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 4 || string(got[:4]) != "data" {
		t.Fatalf("read back %q, want data", got)
	}
}
