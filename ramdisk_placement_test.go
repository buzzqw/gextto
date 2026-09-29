package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestReleaseFitsRamdisk(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LibtorrentDir = t.TempDir()
	temp := t.TempDir()
	cfg.LibtorrentTempDir = &temp
	cfg.Settings = map[string]string{"libtorrent_ramdisk_dir": t.TempDir()}

	if !ReleaseFitsRamdisk(&models.Release{SizeBytes: 1_000_000_000}, &cfg) {
		t.Fatal("a 1 GB release must fit the default 3.5 GB threshold")
	}
	if ReleaseFitsRamdisk(&models.Release{SizeBytes: 100_000_000_000}, &cfg) {
		t.Fatal("a 100 GB season pack must not fit the RAM disk")
	}
	// Unknown size: treated as fitting, the metadata handler relocates it later.
	if !ReleaseFitsRamdisk(&models.Release{}, &cfg) {
		t.Fatal("an unknown size must be treated as fitting")
	}

	// With the RAM disk disabled there is nothing to skip.
	off := DefaultConfig()
	off.Settings = map[string]string{
		"libtorrent_ramdisk_enabled": "false",
		"libtorrent_ramdisk_dir":     t.TempDir(),
	}
	if !ReleaseFitsRamdisk(&models.Release{SizeBytes: 100_000_000_000}, &off) {
		t.Fatal("a disabled RAM disk must never divert the download")
	}
}

func TestRamdiskOverflowDirPrefersTempDir(t *testing.T) {
	main := t.TempDir()
	temp := t.TempDir()
	cfg := DefaultConfig()
	cfg.LibtorrentDir = main
	cfg.LibtorrentTempDir = &temp
	if dir, ok := ramdiskOverflowDir(&cfg); !ok || dir != temp {
		t.Fatalf("overflow dir = %q (%v), want the temp dir", dir, ok)
	}

	cfg.LibtorrentTempDir = nil
	if dir, ok := ramdiskOverflowDir(&cfg); !ok || dir != main {
		t.Fatalf("overflow dir = %q (%v), want the main dir", dir, ok)
	}
}
