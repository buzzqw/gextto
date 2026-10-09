//go:build cgo

package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestLibtorrentVersion(t *testing.T) {
	if LibtorrentVersion() == "" {
		t.Fatal("empty libtorrent version")
	}
}

func TestAddOptionsFlags(t *testing.T) {
	cases := []struct {
		options AddOptions
		want    int32
	}{
		{AddOptions{}, 0},
		{AddOptions{Paused: true}, 1},
		{AddOptions{Sequential: true}, 1 << 1},
		{AddOptions{SeedMode: true}, 1 << 2},
		{AddOptions{QueueTop: true}, 1 << 3},
		{AddOptions{Paused: true, QueueTop: true}, 1 | (1 << 3)},
	}
	for _, tc := range cases {
		if got := tc.options.Flags(); got != tc.want {
			t.Errorf("Flags(%+v) = %d, want %d", tc.options, got, tc.want)
		}
	}
}

func TestFreeSpaceAndRamdiskHelpers(t *testing.T) {
	free := FreeSpaceBytes(t.TempDir())
	if free == nil || *free == 0 {
		t.Fatalf("free space = %v", free)
	}
	if FreeSpaceBytes(filepath.Join(t.TempDir(), "does-not-exist")) != nil {
		t.Log("statfs resolved a non-existent path on this filesystem; tolerated")
	}

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if !PathOnRamdisk(nested, root) {
		t.Fatal("nested path should be on the ramdisk")
	}
	if PathOnRamdisk(t.TempDir(), root) {
		t.Fatal("unrelated path should not be on the ramdisk")
	}
}

func TestRamdiskFits(t *testing.T) {
	gib := uint64(1024 * 1024 * 1024)
	if err := RamdiskFits(0, 0, 10*gib, 0, 4*gib); err != nil {
		t.Fatalf("should fit: %v", err)
	}
	if err := RamdiskFits(2*gib, 0, 10*gib, 0, 4*gib); err == nil {
		t.Fatal("threshold exceeded should fail")
	}
	if err := RamdiskFits(0, gib, 6*gib, 2*gib, 4*gib); err == nil {
		t.Fatal("insufficient effective free space should fail")
	}
}

func TestLibtorrentClientDryRun(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	cfg.StateDir = filepath.Join(dir, "state")
	cfg.DryRun = true
	cfg.LibtorrentEnabled = false

	client, err := NewLibtorrentClient(&cfg)
	if err != nil {
		t.Fatalf("dry-run client: %v", err)
	}
	if !client.DryRun {
		t.Fatal("client should be in dry-run")
	}
	if views := client.List(); len(views) != 0 {
		t.Fatalf("fresh client has %d torrents", len(views))
	}
	if _, err := client.Add("magnet:?xt=urn:btih:"+repeat("a", 40), &cfg); err != nil {
		t.Fatalf("dry-run add: %v", err)
	}
	if err := client.Shutdown(&cfg); err != nil {
		t.Fatalf("dry-run shutdown: %v", err)
	}
}

func repeat(value string, count int) string {
	out := ""
	for i := 0; i < count; i++ {
		out += value
	}
	return out
}

func TestTrimMemoryIsSafe(t *testing.T) {
	// Releasing glibc arenas must be idempotent and never panic.
	TrimMemory()
	TrimMemory()
}

func TestProcessMetricsExposeGoRuntime(t *testing.T) {
	metrics := ProcessMetrics()
	if metrics.GoGoroutines < 1 {
		t.Fatalf("goroutines = %d", metrics.GoGoroutines)
	}
	if metrics.GoHeapBytes == 0 {
		t.Fatal("Go heap bytes should be non-zero")
	}
}

func TestRamdiskFitsUsesRemainingBytes(t *testing.T) {
	const gib = uint64(1024 * 1024 * 1024)
	// A 2 GiB torrent with only 1.5 GiB left to write fits in 2 GiB free
	// (1.5 + 0.5 margin). With the old full-size rule it would have been
	// relocated mid-transfer.
	if err := RamdiskFitsRemaining(5*gib, gib/2, 2*gib, 0, 2*gib, 1536*1024*1024); err != nil {
		t.Fatalf("nearly complete torrent should stay on the RAM disk: %v", err)
	}
	// Nothing downloaded yet: the full size still requires relocation.
	if err := RamdiskFitsRemaining(5*gib, gib/2, 2*gib, 0, 2*gib, 2*gib); err == nil {
		t.Fatal("a torrent with everything left to write must not fit")
	}
	// The size threshold rejects an oversized torrent even if almost done.
	if err := RamdiskFitsRemaining(1*gib, gib/2, 10*gib, 0, 2*gib, 1); err == nil {
		t.Fatal("torrent above the threshold must be rejected")
	}
	// RamdiskFits keeps the original (full-size) semantics for callers/tests.
	if err := RamdiskFits(5*gib, gib/2, 2*gib, 0, 2*gib); err == nil {
		t.Fatal("RamdiskFits should use the full size")
	}
	if err := RamdiskFits(5*gib, gib/2, 10*gib, 0, 2*gib); err != nil {
		t.Fatalf("torrent that fits entirely should pass: %v", err)
	}
}

func TestRecheckGuardPersists(t *testing.T) {
	dir := t.TempDir()
	client := &LibtorrentClient{stateDir: dir}
	if client.recentlyRechecked("ABC123", time.Hour) {
		t.Fatal("unexpected recheck state on a fresh client")
	}
	client.markRechecked("ABC123")
	if !client.recentlyRechecked("abc123", time.Hour) {
		t.Fatal("recheck not recorded (case-insensitive)")
	}
	// The guard survives a restart.
	reloaded := &LibtorrentClient{stateDir: dir}
	if !reloaded.recentlyRechecked("ABC123", time.Hour) {
		t.Fatal("recheck guard not persisted")
	}
	if reloaded.recentlyRechecked("ABC123", 0) {
		t.Fatal("zero window should never match")
	}
}

func TestLibtorrentRemoveInvalidatesCacheAndExcludesTorrent(t *testing.T) {
	dir := t.TempDir()
	client := &LibtorrentClient{
		torrents: map[string]models.TorrentView{
			"hash1": {Hash: "hash1", Name: "Torrent 1"},
			"hash2": {Hash: "hash2", Name: "Torrent 2"},
		},
		removed:  make(map[string]time.Time),
		stateDir: dir,
		DryRun:   true,
	}

	// 1. Initial list warms cache
	list := client.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(list))
	}
	client.listMu.Lock()
	if !client.listValid {
		client.listMu.Unlock()
		t.Fatal("expected list cache to be valid after List()")
	}
	client.listMu.Unlock()

	// 2. Remove hash1
	ok, err := client.Remove("hash1", false)
	if err != nil || !ok {
		t.Fatalf("expected successful remove, got ok=%v, err=%v", ok, err)
	}

	if !client.isRecentlyRemoved("hash1") {
		t.Fatal("expected hash1 to be marked as recently removed")
	}

	// 3. List should immediately NOT contain hash1, even within the 500ms window
	listAfter := client.List()
	if len(listAfter) != 1 || listAfter[0].Hash != "hash2" {
		t.Fatalf("expected only hash2 in list after remove, got %#v", listAfter)
	}

	// 4. unmarkRemoved allows it again
	client.unmarkRemoved("hash1")
	if client.isRecentlyRemoved("hash1") {
		t.Fatal("expected hash1 to not be recently removed after unmark")
	}
}

func TestLibtorrentStateFileRejectsNonHashes(t *testing.T) {
	dir := t.TempDir()
	client := &LibtorrentClient{stateDir: dir}
	v1 := "0123456789abcdef0123456789abcdef01234567"
	if path, ok := client.stateFile(strings.ToUpper(v1), ".torrent"); !ok || path != filepath.Join(dir, v1+".torrent") {
		t.Fatalf("v1 hash: got %q, %v", path, ok)
	}
	v2 := strings.Repeat("ab", 32)
	if _, ok := client.stateFile(v2, ".fastresume"); !ok {
		t.Fatal("v2 hash refused")
	}
	for _, hash := range []string{"", "../escape", "../../" + v1, v1 + "/x", "zz" + v1[2:], v1[:39]} {
		if path, ok := client.stateFile(hash, ".torrent"); ok {
			t.Errorf("stateFile(%q) accepted: %q", hash, path)
		}
	}
}
