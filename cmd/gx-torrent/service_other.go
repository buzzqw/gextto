//go:build !windows

package main

import "errors"

// isWindowsService is always false off Windows: the interactive path is used.
func isWindowsService() bool { return false }

// runWindowsService is never reached off Windows.
func runWindowsService(opts Options) error {
	return errors.New("the Windows service mode is only available on Windows")
}
