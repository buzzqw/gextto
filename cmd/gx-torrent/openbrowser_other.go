//go:build !windows

package main

import (
	"os"
	"os/exec"
)

// openBrowser opens url with xdg-open, but only when a graphical session is
// present: a headless server is silently skipped and the wizard stays reachable
// at the printed address.
func openBrowser(url string) error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return nil
	}
	return exec.Command("xdg-open", url).Start()
}
