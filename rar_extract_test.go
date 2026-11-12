package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

// A folder that already holds its video is left alone, and a folder with no
// archive yields nothing: the extraction never runs where it is not needed.
func TestReleaseArchivesOnlyWhenNoVideo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Serie.S01E01.mkv"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.rar"), []byte("not really"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := extractReleaseArchives(dir, "test")
	if err != nil || len(written) != 0 {
		t.Fatalf("folder with a video: %v, %v", written, err)
	}
	empty := t.TempDir()
	written, err = extractReleaseArchives(empty, "test")
	if err != nil || len(written) != 0 {
		t.Fatalf("folder without archives: %v, %v", written, err)
	}
	// A damaged archive is reported, not extracted.
	broken := t.TempDir()
	_ = os.WriteFile(filepath.Join(broken, "set.rar"), []byte("garbage"), 0o644)
	if _, err := extractReleaseArchives(broken, "test"); err == nil {
		t.Fatal("a damaged archive was accepted")
	}
	if entries, _ := os.ReadDir(broken); len(entries) != 1 {
		t.Fatalf("files left behind: %v", entries)
	}
}
