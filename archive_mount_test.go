package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveFileConfirmedMissing(t *testing.T) {
	root := t.TempDir()
	season := filepath.Join(root, "Show", "S01")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(season, "other.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file deleted from a populated folder is really missing.
	if !archiveFileConfirmedMissing(filepath.Join(season, "gone.mkv")) {
		t.Error("deleted file in a populated folder must be confirmed missing")
	}
	// A whole season folder removed while the series folder still has content.
	if !archiveFileConfirmedMissing(filepath.Join(root, "Show", "S02", "gone.mkv")) {
		t.Error("deleted season folder must be confirmed missing")
	}
	// The last file of a folder deleted: the empty folder proves the volume is mounted.
	emptySeason := filepath.Join(root, "Show", "S03")
	if err := os.MkdirAll(emptySeason, 0o755); err != nil {
		t.Fatal(err)
	}
	if !archiveFileConfirmedMissing(filepath.Join(emptySeason, "gone.mkv")) {
		t.Error("deleted last file of an existing folder must be confirmed missing")
	}
	// Unmounted volume: the mount point exists but is empty.
	mount := filepath.Join(t.TempDir(), "nas")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	if archiveFileConfirmedMissing(filepath.Join(mount, "tv", "Show", "S01", "gone.mkv")) {
		t.Error("empty mount point must not count as a deleted file")
	}
	// Nothing exists at all.
	if archiveFileConfirmedMissing("/definitely/not/here/file.mkv") {
		t.Error("an unreachable path must not count as a deleted file")
	}
}
