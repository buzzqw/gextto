//go:build windows

package main

import "os/exec"

// openBrowser opens url in the default browser. rundll32 with the
// FileProtocolHandler is the shell-independent way; a failure (no browser, a
// service session) is left to the caller to ignore.
func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
