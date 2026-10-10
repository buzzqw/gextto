//go:build windows

package main

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// openBrowser opens url in the default browser. rundll32 with the
// FileProtocolHandler is the shell-independent way. A process in session 0 (a
// Windows service, or a task running without a logged-on user) has no desktop,
// so it is skipped like a headless Unix server; the wizard stays reachable at
// the address printed in the log.
func openBrowser(url string) error {
	var session uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &session); err == nil && session == 0 {
		return nil
	}
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
