//go:build !linux && !windows

package main

// localStorageClass is unknown on this platform: the cache policy stays with
// its conservative default.
func localStorageClass(path string) string { return "unknown" }
