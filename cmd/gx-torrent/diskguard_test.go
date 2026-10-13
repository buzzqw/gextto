package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/queue"
)

// fakeFree returns a free-space reader reporting free bytes of a 1 TiB disk.
func fakeFree(free int64) func(string) (int64, int64) {
	return func(string) (int64, int64) { return free, 1 << 40 }
}

// TestDiskGuardHysteresis: a folder goes low below the threshold and stays low
// until it has the threshold plus the slack, so it does not flap at the limit.
func TestDiskGuardHysteresis(t *testing.T) {
	const minFree = 1 << 30
	g := &diskGuard{}
	now := time.Now()
	step := func(free int64) bool {
		g.freeFn = fakeFree(free)
		now = now.Add(diskGuardInterval)
		g.refresh(now, []string{"/data"}, minFree, false)
		return g.isLow("/data")
	}
	if step(2 << 30) {
		t.Fatal("2 GiB free is not low")
	}
	if !step(minFree - 1) {
		t.Fatal("below the threshold is low")
	}
	if !step(minFree + diskGuardSlack(minFree) - 1) {
		t.Fatal("resumed inside the hysteresis band")
	}
	if step(minFree + diskGuardSlack(minFree)) {
		t.Fatal("did not resume above threshold + slack")
	}
	// An unmeasurable folder never blocks.
	g.freeFn = func(string) (int64, int64) { return 0, 0 }
	g.refresh(now.Add(diskGuardInterval), []string{"/net"}, minFree, false)
	if g.isLow("/net") {
		t.Fatal("unknown free space blocked the folder")
	}
}

// TestExistingDirWalksUp: a save path not created yet is measured on its
// nearest existing parent.
func TestExistingDirWalksUp(t *testing.T) {
	root := t.TempDir()
	if got := existingDir(filepath.Join(root, "a", "b", "c")); got != root {
		t.Fatalf("existingDir = %q, want %q", got, root)
	}
}

// TestMinFreeDefaults: an older state file (no field) gets the default guard,
// 0 disables it and a negative value is clamped to off.
func TestMinFreeDefaults(t *testing.T) {
	if got := (queue.Config{}).Normalized().MinFree(); got != queue.DefaultMinFreeMB<<20 {
		t.Fatalf("default = %d", got)
	}
	if got := (queue.Config{MinFreeMB: queue.Int64Ptr(0)}).Normalized().MinFree(); got != 0 {
		t.Fatalf("0 = %d, want off", got)
	}
	if got := (queue.Config{MinFreeMB: queue.Int64Ptr(-5)}).Normalized().MinFree(); got != 0 {
		t.Fatalf("negative = %d, want off", got)
	}
}

// TestDiskGuardPausesAndResumesADownload: on a folder below the threshold the
// download is stopped and flagged, and it runs again once space returns.
func TestDiskGuardPausesAndResumesADownload(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "big.bin", 4<<20)
	staging := filepath.Join(t.TempDir(), "staging")
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: staging})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "downloading without peers", func() bool { return stateOf(d, hash) == "downloading" })

	setFree := func(free int64) {
		d.disk.mu.Lock()
		d.disk.freeFn = fakeFree(free)
		d.disk.checkedAt = time.Time{}
		d.disk.mu.Unlock()
		d.poke()
	}
	setFree(10 << 20)
	waitFor(t, "paused by the disk guard", func() bool {
		info, _ := findInfo(d, hash)
		return info.DiskWait && info.State == "paused"
	})
	setFree(100 << 30)
	waitFor(t, "resumed with space", func() bool {
		info, _ := findInfo(d, hash)
		return !info.DiskWait && info.State == "downloading"
	})
}
