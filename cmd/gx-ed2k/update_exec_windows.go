//go:build windows

package main

// restartProcess is unreachable on Windows: handleUpdateApply refuses to
// replace a running .exe and the page keeps the zip download link.
func restartProcess(string) {}
