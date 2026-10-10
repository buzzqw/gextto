//go:build !linux && !windows

package main

// diskFreeBytes is unknown on this platform: the daemon reports no free space.
func diskFreeBytes(path string) (free, total int64) { return 0, 0 }

// networkFilesystem is unknown on this platform: the storage class stays
// "unknown" and the cache policy keeps its conservative default.
func networkFilesystem(path string) bool { return false }
