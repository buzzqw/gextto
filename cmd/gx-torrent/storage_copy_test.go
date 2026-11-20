package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestCopyFileRefusesShortCopy: a source that shrinks mid-copy must not become
// the copied file. The partial target is removed, so the caller cannot rename
// a truncated file into place.
func TestCopyFileRefusesShortCopy(t *testing.T) {
	original := copyFileData
	t.Cleanup(func() { copyFileData = original })
	copyFileData = func(dst io.Writer, src io.Reader, size int64) (int64, error) {
		n, err := io.CopyN(dst, src, size-1)
		return n, err
	}

	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.bin")
	err := copyFile(source, target, 0o644)
	if err == nil {
		t.Fatal("copyFile accepted a short copy")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("partial target left behind: %v", statErr)
	}
}

// TestCopyFileCopiesWholeSource: the normal path still copies every byte.
func TestCopyFileCopiesWholeSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.bin")
	if err := copyFile(source, target, 0o644); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Fatalf("target = %q, want %q", got, "data")
	}
}
