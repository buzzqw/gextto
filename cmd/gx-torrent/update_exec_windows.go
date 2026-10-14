//go:build windows

package main

import "errors"

// restartInto is not available on Windows: the service is updated by hand
// (the update section offers the download only).
func restartInto(exe string) error {
	return errors.New("in-place restart is not available on Windows")
}
