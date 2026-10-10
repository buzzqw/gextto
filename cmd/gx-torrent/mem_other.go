//go:build !linux && !windows

package main

// memoryTotal is unknown on this platform: the cache policy keeps its
// conservative default.
func memoryTotal() int64 { return 0 }

// memoryAvailable is unknown on this platform.
func memoryAvailable() int64 { return 0 }
