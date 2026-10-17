package main

import (
	"os"
	"runtime/debug"
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
