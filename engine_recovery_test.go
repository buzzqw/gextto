package gextto

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGxTorrentPreviousBinary(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "gx-torrent")
	if err := os.WriteFile(binary, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := gxTorrentPreviousBinary(binary); got != "" {
		t.Fatalf("no .prev yet, got %q", got)
	}
	prev := binary + ".prev"
	if err := os.WriteFile(prev, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := gxTorrentPreviousBinary(binary); got != prev {
		t.Fatalf("previous binary = %q, want %q", got, prev)
	}
	// A directory in place of the file is not a rollback candidate.
	if err := os.Remove(prev); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(prev, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := gxTorrentPreviousBinary(binary); got != "" {
		t.Fatalf("a directory must not count as a previous binary, got %q", got)
	}
}

func TestRollbackGxTorrentBinary(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "gx-torrent")
	if err := os.WriteFile(binary, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if rolled, err := rollbackGxTorrentBinary(binary); err != nil || rolled {
		t.Fatalf("rollback without .prev = (%v, %v), want (false, nil)", rolled, err)
	}

	if err := os.WriteFile(binary+".prev", []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	rolled, err := rollbackGxTorrentBinary(binary)
	if err != nil || !rolled {
		t.Fatalf("rollback = (%v, %v), want (true, nil)", rolled, err)
	}
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read restored binary: %v", err)
	}
	if string(content) != "old" {
		t.Fatalf("restored binary = %q, want old", content)
	}
	failed, err := os.ReadFile(binary + ".failed")
	if err != nil {
		t.Fatalf("the failed binary must be kept for inspection: %v", err)
	}
	if string(failed) != "new" {
		t.Fatalf(".failed = %q, want new", failed)
	}
	if _, err := os.Stat(binary + ".prev"); !os.IsNotExist(err) {
		t.Fatal(".prev must be consumed by the rollback")
	}
}

func TestGxUnresponsive(t *testing.T) {
	now := time.Now()
	if gxUnresponsive(now.Add(-time.Minute), now) {
		t.Fatal("one minute is inside the limit")
	}
	if !gxUnresponsive(now.Add(-gxUnresponsiveLimit-time.Second), now) {
		t.Fatal("past the limit must be unresponsive")
	}
}

func TestApplyEngineFallbackUsesExternalQbittorrentWithoutDownload(t *testing.T) {
	cfg := fallbackTestConfig(t)
	cfg.Settings["qbittorrent_url"] = "http://192.168.1.50:8080"
	downloads, restarts := stubEngineFallback(t)

	if !applyEngineFallback(context.Background(), &cfg, BackendGxTorrent, nil) {
		t.Fatal("an external qBittorrent must be used as the fallback")
	}
	if *downloads != 0 {
		t.Fatalf("an external qBittorrent needs no download (downloads=%d)", *downloads)
	}
	if got := readSettingValue(t, cfg.DataDir, "torrent_backend"); got != BackendQbittorrent {
		t.Fatalf("torrent_backend = %q, want %q", got, BackendQbittorrent)
	}
	if len(*restarts) != 1 {
		t.Fatalf("restarts = %v, want one restart", *restarts)
	}
}
