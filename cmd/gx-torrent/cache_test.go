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

func TestAdaptCacheFirstEvaluationApplies(t *testing.T) {
	d := &Daemon{opts: Options{DownloadDir: t.TempDir()}}
	d.adaptCacheLocked(time.Now(), 3, 1, 1<<20)
	if d.cacheRead <= 0 || d.cacheWrite <= 0 {
		t.Fatalf("no target computed: read=%d write=%d", d.cacheRead, d.cacheWrite)
	}
	if !d.restartPending {
		t.Fatal("first evaluation must schedule a session reopen")
	}
	if d.cacheClass == "" {
		t.Fatal("storage class not recorded")
	}

	// A second call within the interval must not schedule anything.
	d.restartPending = false
	d.adaptCacheLocked(time.Now(), 3, 1, 1<<20)
	if d.restartPending {
		t.Fatal("must not recompute within the check interval")
	}
}
