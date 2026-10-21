package main

import (
	"os"
	"runtime/debug"
	"time"
)

// gcPercent is the GOGC the daemon runs with unless the environment sets one.
// The heap is dominated by bounded buffers (the write cache's piece buffers,
// the read cache), and the collector costs about 1 % of the CPU under a full
// download. With Go's default of 100 the heap may grow to twice the live data
// before a collection. 50 trims that headroom from the resident memory at a
// small CPU cost (docs/measurements.md).
const gcPercent = 50

// applyGCDefault sets gcPercent when GOGC is not in the environment, and
// reports whether it did.
func applyGCDefault() bool {
	if _, set := os.LookupEnv("GOGC"); set {
		return false
	}
	debug.SetGCPercent(gcPercent)
	return true
}

const (
	// idleBeforeRelease is how long the daemon must have no active download
	// before returning free heap to the OS.
	idleBeforeRelease = 5 * time.Minute
	// memoryReleaseEvery bounds how often the idle release runs.
	memoryReleaseEvery = 10 * time.Minute
)

// memoryReleaseDue reports whether an idle daemon should return free heap to
// the OS. lastActive is when a download was last active (the daemon start when
// none has run yet).
func memoryReleaseDue(now, lastActive, lastRelease time.Time) bool {
	if lastActive.IsZero() || now.Sub(lastActive) < idleBeforeRelease {
		return false
	}
	return lastRelease.IsZero() || now.Sub(lastRelease) >= memoryReleaseEvery
}

// maybeReleaseMemory returns the heap retained by a finished transfer to the
// OS. Piece buffers live in per-torrent sync.Pools until a collection: after a
// burst the heap can stay at hundreds of MB while the daemon is idle, because
// nothing allocates enough to trigger a GC. debug.FreeOSMemory collects and
// hands the free spans back; it is STW, so only after a quiet period and at
// most every memoryReleaseEvery. Call without d.mu. moving is true while the
// total transfer rate (download plus upload) is at least the queue's slow-rate
// threshold: a quiet daemon needs no big cache and does not keep the heap
// alive.
func (d *Daemon) maybeReleaseMemory(now time.Time, moving bool) {
	if moving {
		d.lastDownloadAt = now
		return
	}
	lastActive := d.lastDownloadAt
	if lastActive.IsZero() {
		lastActive = d.startedAt
	}
	if !memoryReleaseDue(now, lastActive, d.lastMemoryReleaseAt) {
		return
	}
	d.lastMemoryReleaseAt = now
	debug.FreeOSMemory()
	logf("memory released: no active downloads for %s", now.Sub(lastActive).Round(time.Minute))
}
