//go:build !windows

package main

import (
	"os"
	"os/exec"
)

// openBrowser opens url with the desktop opener, but only when a graphical
// session is present: a headless server is silently skipped and the wizard
// stays reachable at the address printed in the log. xdg-open is the portable
// entry point; gio and sensible-browser cover the few desktops that ship only
// one of them.
func openBrowser(url string) error {
	if !graphicalSession() {
		return nil
	}
	for _, opener := range []string{"xdg-open", "gio", "sensible-browser"} {
		path, err := exec.LookPath(opener)
		if err != nil {
			continue
		}
		args := []string{url}
		if opener == "gio" {
			args = []string{"open", url}
		}
		if err := exec.Command(path, args...).Start(); err == nil {
			return nil
		}
	}
	return nil
}

// graphicalSession reports whether a desktop session is available, so a
// headless server never tries to launch a browser.
func graphicalSession() bool {
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return true
	}
	switch os.Getenv("XDG_SESSION_TYPE") {
	case "x11", "wayland":
		return true
	}
	return false
}
