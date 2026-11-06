package main

import (
	"os"
	"runtime/debug"
)

// gcPercent is the GOGC the daemon runs with unless the environment sets one.
// A search allocates tens of times the live heap: the feed and indexer bodies,
// the releases and the dedup indexes. With Go's default of 100 the heap may
// double before a collection, and the peak is what a small home server feels.
// 50 lowers it at about 2.5 % more CPU, and the daemon is I/O-bound
// (docs/measurements.md).
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
