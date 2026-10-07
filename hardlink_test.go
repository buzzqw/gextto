package gextto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func sameInode(t *testing.T, left, right string) bool {
	t.Helper()
	a, err := os.Stat(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(right)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(a, b)
}

func TestLinkOrCopyFileLinksAndSurvivesDeletingTheDownload(t *testing.T) {
	cfg := DefaultConfig()
	root := t.TempDir()
	source := filepath.Join(root, "downloads", "Show.S01E01.mkv")
	target := filepath.Join(root, "library", "Show", "Show.S01E01.mkv")
	content := []byte("\x1a\x45\xdf\xa3 episode payload")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	linked, err := linkOrCopyFile(&cfg, source, target)
	if err != nil {
		t.Fatal(err)
	}
	if !linked || !sameInode(t, source, target) {
		t.Fatalf("expected a hardlink on the same filesystem, linked=%v", linked)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".*gextto-link*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary link names left behind: %v", leftovers)
	}
	// Someone deletes the download "duplicate" by hand: the library keeps it.
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("library file lost after deleting the download name: %q, %v", got, err)
	}
}

func TestLinkOrCopyFileCopiesWhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings[hardlinkSetting] = "false"
	root := t.TempDir()
	source := filepath.Join(root, "a.mkv")
	target := filepath.Join(root, "lib", "a.mkv")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked, err := linkOrCopyFile(&cfg, source, target)
	if err != nil {
		t.Fatal(err)
	}
	if linked || sameInode(t, source, target) {
		t.Fatal("hardlink_seeding=false must copy")
	}
}

func TestLinkOrCopyFileReplacesSameFileWithoutLeftovers(t *testing.T) {
	cfg := DefaultConfig()
	root := t.TempDir()
	source := filepath.Join(root, "a.mkv")
	target := filepath.Join(root, "lib", "a.mkv")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := linkOrCopyFile(&cfg, source, target); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the target, found %d entries", len(entries))
	}
}

func TestLinkOrCopyFileFallsBackToCopy(t *testing.T) {
	cfg := DefaultConfig()
	root := t.TempDir()
	source := filepath.Join(root, "file.mkv")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	// /dev/shm is normally a different filesystem from the test temp dir.
	shm, err := os.MkdirTemp("/dev/shm", "gextto-link-")
	if err != nil {
		t.Skip("no /dev/shm")
	}
	defer os.RemoveAll(shm)
	if devA, okA := deviceOf(root); okA {
		if devB, okB := deviceOf(shm); okB && devA == devB {
			t.Skip("/dev/shm is on the same filesystem")
		}
	}
	target := filepath.Join(shm, "file.mkv")
	linked, err := linkOrCopyFile(&cfg, source, target)
	if err != nil {
		t.Fatal(err)
	}
	if linked {
		t.Fatal("a cross-filesystem link cannot succeed")
	}
	if got, _ := os.ReadFile(target); string(got) != "payload" {
		t.Fatalf("fallback copy content = %q", got)
	}
}

func TestStagePackFileLinksEpisode(t *testing.T) {
	cfg := DefaultConfig()
	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "Show", "Season 01")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	packFile := filepath.Join(source, "Show.S01E02.1080p.WEB-DL.mkv")
	if err := os.WriteFile(packFile, []byte("\x1a\x45\xdf\xa3 matroska payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := PackSourceFile{Path: packFile, Season: 1, Episode: 2}
	placed, ok, err := StagePackFile(&file, source, destination, nil, &cfg, 400)
	if err != nil || !ok {
		t.Fatalf("staging failed: placed=%q ok=%v err=%v", placed, ok, err)
	}
	if !sameInode(t, packFile, placed) {
		t.Fatal("pack episode should be hardlinked into the library")
	}
}
