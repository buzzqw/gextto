package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestValidateCompletedFile(t *testing.T) {
	dir := t.TempDir()

	matroska := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 200)...)
	good := filepath.Join(dir, "good.mkv")
	writeFile(t, good, matroska)
	if err := validateCompletedFile(good); err != nil {
		t.Fatalf("valid matroska rejected: %v", err)
	}

	mp4 := append([]byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p'}, make([]byte, 100)...)
	goodMP4 := filepath.Join(dir, "good.mp4")
	writeFile(t, goodMP4, mp4)
	if err := validateCompletedFile(goodMP4); err != nil {
		t.Fatalf("valid mp4 rejected: %v", err)
	}

	zeros := filepath.Join(dir, "zeros.mkv")
	writeFile(t, zeros, make([]byte, 4096))
	if err := validateCompletedFile(zeros); err == nil {
		t.Fatal("zero-filled file accepted")
	}

	junk := filepath.Join(dir, "junk.mkv")
	writeFile(t, junk, []byte("this is not a video container at all"))
	if err := validateCompletedFile(junk); err == nil {
		t.Fatal("unrecognised container accepted")
	}

	empty := filepath.Join(dir, "empty.mkv")
	writeFile(t, empty, nil)
	if err := validateCompletedFile(empty); err == nil {
		t.Fatal("empty file accepted")
	}

	// Non-video files are not subject to container validation.
	subtitle := filepath.Join(dir, "subs.srt")
	writeFile(t, subtitle, []byte("1\n00:00:01,000 --> 00:00:02,000\nhello\n"))
	if err := validateCompletedFile(subtitle); err != nil {
		t.Fatalf("subtitle rejected: %v", err)
	}

	if err := validateCompletedFile(filepath.Join(dir, "missing.mkv")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestQuarantineCorruptFile(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "trash")
	bad := filepath.Join(dir, "bad.mkv")
	writeFile(t, bad, make([]byte, 1024))

	cfg := DefaultConfig()
	cfg.TrashPath = &trash
	moved := quarantineCorruptFile(bad, &cfg)
	if moved == "" {
		t.Fatal("file not quarantined")
	}
	if _, err := os.Stat(bad); err == nil {
		t.Fatal("original file still present")
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("quarantined file missing: %v", err)
	}
}

func TestFileSuspiciouslyEmpty(t *testing.T) {
	dir := t.TempDir()
	zeros := filepath.Join(dir, "zeros.bin")
	writeFile(t, zeros, make([]byte, 4096))
	if !fileSuspiciouslyEmpty(zeros) {
		t.Fatal("zero-filled file should be suspicious")
	}
	good := filepath.Join(dir, "good.bin")
	writeFile(t, good, []byte("some real content here"))
	if fileSuspiciouslyEmpty(good) {
		t.Fatal("non-zero file should not be suspicious")
	}
	if !fileSuspiciouslyEmpty(filepath.Join(dir, "missing.bin")) {
		t.Fatal("missing file should be suspicious")
	}
	if !fileSuspiciouslyEmpty(filepath.Join(dir, "empty.bin")) {
		writeFile(t, filepath.Join(dir, "empty.bin"), nil)
		if !fileSuspiciouslyEmpty(filepath.Join(dir, "empty.bin")) {
			t.Fatal("empty file should be suspicious")
		}
	}
}
