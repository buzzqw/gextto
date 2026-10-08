package main

import (
	"testing"
	"time"
)

func TestAdaptiveCacheManualOverride(t *testing.T) {
	read, write, reason := adaptiveCache(QueueConfig{CacheMB: 2048}, cacheInputs{ram: 16 << 30, available: 8 << 30})
	if read != 2048*mib || write != 2048*mib {
		t.Fatalf("manual override ignored: read=%d write=%d", read, write)
	}
	if reason != "manual" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestAdaptiveCacheScalesWithWorkload(t *testing.T) {
	ram := int64(16 << 30)
	avail := int64(12 << 30)
	idleWrite := int64(0)
	for i, downloads := range []int{0, 1, 4, 8} {
		_, write, _ := adaptiveCache(QueueConfig{}, cacheInputs{ram: ram, available: avail, activeDownloads: downloads, class: "ssd"})
		if i == 0 {
			idleWrite = write
		}
		if write < idleWrite {
			t.Fatalf("write cache shrank with more downloads: %d < %d", write, idleWrite)
		}
	}
	_, writeMany, _ := adaptiveCache(QueueConfig{}, cacheInputs{ram: ram, available: avail, activeDownloads: 8, class: "ssd"})
	_, writeIdle, _ := adaptiveCache(QueueConfig{}, cacheInputs{ram: ram, available: avail, activeDownloads: 0, class: "ssd"})
	if writeMany <= writeIdle {
		t.Fatalf("more downloads must raise the write cache: %d vs %d", writeMany, writeIdle)
	}

	readIdle, _, _ := adaptiveCache(QueueConfig{}, cacheInputs{ram: ram, available: avail, class: "ssd"})
	readSeeds, _, _ := adaptiveCache(QueueConfig{}, cacheInputs{ram: ram, available: avail, activeSeeds: 6, class: "ssd"})
	if readSeeds <= readIdle {
		t.Fatalf("more seeds must raise the read cache: %d vs %d", readSeeds, readIdle)
	}
}

func TestAdaptiveCacheSlowStorageLeansLarger(t *testing.T) {
	in := cacheInputs{ram: 16 << 30, available: 12 << 30, activeDownloads: 2, class: "ssd"}
	_, ssd, _ := adaptiveCache(QueueConfig{}, in)
	in.class = "network"
	_, network, _ := adaptiveCache(QueueConfig{}, in)
	if network <= ssd {
		t.Fatalf("network storage should buffer more: network=%d ssd=%d", network, ssd)
	}
}

func TestAdaptiveCacheClamps(t *testing.T) {
	// Almost no free memory: floors, never zero.
	read, write, _ := adaptiveCache(QueueConfig{}, cacheInputs{ram: 1 << 30, available: 16 << 20, class: "ssd"})
	if read < cacheReadMin || write < cacheWriteMin {
		t.Fatalf("below floors: read=%d write=%d", read, write)
	}
	// Huge machine: hard ceilings.
	read, write, _ = adaptiveCache(QueueConfig{}, cacheInputs{ram: 512 << 30, available: 256 << 30, activeDownloads: 50, activeSeeds: 50, class: "network"})
	if read > cacheReadMax || write > cacheWriteMax {
		t.Fatalf("above ceilings: read=%d write=%d", read, write)
	}
}

func TestAdaptiveCacheMissingSignalsFallsBack(t *testing.T) {
	read, write, reason := adaptiveCache(QueueConfig{}, cacheInputs{})
	if read <= 0 || write <= 0 || reason != "fallback" {
		t.Fatalf("fallback missing: read=%d write=%d reason=%q", read, write, reason)
	}
}

func TestCacheMovedThreshold(t *testing.T) {
	if cacheMoved(100, 120) {
		t.Fatal("20% must not move")
	}
	if !cacheMoved(100, 130) {
		t.Fatal("30% must move")
	}
	if !cacheMoved(0, 50) {
		t.Fatal("from zero must move")
	}
}

func TestStorageClassLocalIsNotNetwork(t *testing.T) {
	class := storageClass(t.TempDir())
	if class == "network" {
		t.Fatalf("a local temp dir classified as network")
	}
	if class == "" {
		t.Fatal("empty storage class")
	}
}

func TestAdaptCacheAppliesInPlaceWithoutSessionReopen(t *testing.T) {
	d := &Daemon{opts: Options{DownloadDir: t.TempDir()}}
	start := time.Now()
	d.adaptCacheLocked(start, 3, 1, 1<<20)
	if d.cacheRead <= 0 || d.cacheWrite <= 0 {
		t.Fatalf("no target computed: read=%d write=%d", d.cacheRead, d.cacheWrite)
	}
	if d.restartPending {
		t.Fatal("the cache is resized in place: no session reopen")
	}
	if d.cacheClass == "" {
		t.Fatal("storage class not recorded")
	}

	// A second call within the interval must not recompute.
	applied := d.cacheAppliedAt
	d.adaptCacheLocked(start.Add(time.Second), 9, 9, 1<<30)
	if d.cacheAppliedAt != applied || d.restartPending {
		t.Fatal("must not recompute within the check interval")
	}
}

func TestAdaptiveCacheDisabledUsesStatic(t *testing.T) {
	off := false
	read, write, reason := adaptiveCache(
		QueueConfig{Auto: &off},
		cacheInputs{ram: 16 << 30, available: 12 << 30, activeDownloads: 8, activeSeeds: 4, class: "network"},
	)
	if reason != "static" {
		t.Fatalf("reason = %q, want static", reason)
	}
	wantRead, wantWrite, _ := cacheSizes(QueueConfig{}, 16<<30)
	if read != wantRead || write != wantWrite {
		t.Fatalf("static sizing mismatch: got %d/%d want %d/%d", read, write, wantRead, wantWrite)
	}
}

func TestRuntimeVersionUsesGxTorrentBuild(t *testing.T) {
	v := runtimeVersion()
	if len(v) < 5 || v[:4] != "1.1." {
		t.Fatalf("runtime version = %q, want 1.1.<gx-torrent build>", v)
	}
}

// A manual size is an explicit choice: even a small change must be applied at
// once, not swallowed by the adaptive policy's 25% hysteresis.
func TestAdaptCacheAppliesSmallManualChange(t *testing.T) {
	d := &Daemon{opts: Options{DownloadDir: t.TempDir()}}
	start := time.Now()
	d.adaptCacheLocked(start, 1, 0, 0) // first evaluation, adaptive
	applied := d.cacheRead
	if applied <= 0 {
		t.Fatalf("no cache target computed: %d", applied)
	}
	d.state.Config.CacheMB = applied/mib + 8 // manual, a few MiB away (<25%)
	d.cacheCheckedAt = time.Time{}           // as setConfig does on the change
	d.cacheAppliedAt = time.Time{}
	d.adaptCacheLocked(start.Add(time.Second), 1, 0, 0)
	if want := (applied/mib + 8) * mib; d.cacheRead != want || d.cacheWrite != want {
		t.Fatalf("small manual cache change ignored: got read=%d write=%d want %d", d.cacheRead, d.cacheWrite, want)
	}
	if d.restartPending {
		t.Fatal("the cache is resized in place: no session reopen")
	}
}
