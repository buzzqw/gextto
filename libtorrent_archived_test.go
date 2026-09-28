package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTevArchivedCopyPresent covers the completion guard used after a storage
// move. A season pack is archived into a directory, so a directory that still
// holds files must count as an existing archived copy; otherwise the same pack
// is re-processed and re-archived on every move (the duplicate-report bug).
func TestTevArchivedCopyPresent(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "series.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.db.Close() })

	root := t.TempDir()

	seed := func(hash string) {
		t.Helper()
		if _, err := db.db.Exec("INSERT INTO torrent_meta(hash,name,updated_at) VALUES(?1,?2,datetime('now'))", hash, hash); err != nil {
			t.Fatalf("seed torrent_meta %s: %v", hash, err)
		}
	}
	complete := func(hash, path string) {
		t.Helper()
		seed(hash)
		if err := db.MarkTorrentCompleted(hash, path, 4); err != nil {
			t.Fatalf("MarkTorrentCompleted %s: %v", hash, err)
		}
	}

	// 1. Regular file (single release).
	filePath := filepath.Join(root, "release.mkv")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	complete("hash-file", filePath)
	if !tevArchivedCopyPresent(db, "hash-file") {
		t.Fatal("regular file archive should be present")
	}

	// 2. Directory with a file (season pack copied into the series folder).
	dirPath := filepath.Join(root, "I delitti del BarLume")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "S07E01.mkv"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write pack file: %v", err)
	}
	complete("hash-dir", dirPath)
	if !tevArchivedCopyPresent(db, "hash-dir") {
		t.Fatal("non-empty directory archive should be present")
	}

	// 3. Nested file inside a season subfolder.
	nested := filepath.Join(root, "Series")
	if err := os.MkdirAll(filepath.Join(nested, "Season 01"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "Season 01", "E01.mkv"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	complete("hash-nested", nested)
	if !tevArchivedCopyPresent(db, "hash-nested") {
		t.Fatal("directory with a nested file should be present")
	}

	// 4. Empty directory (files were deleted): must be re-processed.
	emptyDir := filepath.Join(root, "Empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}
	complete("hash-empty", emptyDir)
	if tevArchivedCopyPresent(db, "hash-empty") {
		t.Fatal("empty directory archive should be treated as missing")
	}

	// 5. Missing path.
	complete("hash-gone", filepath.Join(root, "gone.mkv"))
	if tevArchivedCopyPresent(db, "hash-gone") {
		t.Fatal("missing archive should be treated as absent")
	}

	// 6. No torrent_meta row at all.
	if tevArchivedCopyPresent(db, "hash-unknown") {
		t.Fatal("unknown hash should be treated as absent")
	}
}
