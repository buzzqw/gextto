//go:build linux

package main

import "golang.org/x/sys/unix"

// disableTransparentHugePages keeps the kernel from backing the Go heap with
// 2 MB pages. When a host runs with THP set to "always", a huge page that keeps
// a single live object cannot be given back after a burst: the collector
// frees whole 4 KB spans, but the rest of the huge page stays resident, so a
// search can leave tens of MB behind. The daemon is I/O-bound, not CPU-bound,
// so it loses nothing measurable and keeps its resting footprint honest.
func disableTransparentHugePages() {
	_ = unix.Prctl(unix.PR_SET_THP_DISABLE, 1, 0, 0, 0)
}
