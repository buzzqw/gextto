//go:build !linux

package main

// disableTransparentHugePages exists only on Linux: the other platforms do not
// offer the process-scoped switch, and the daemon runs there without it.
func disableTransparentHugePages() {}
