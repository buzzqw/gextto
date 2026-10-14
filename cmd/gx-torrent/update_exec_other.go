//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restartInto replaces the process with exe, same arguments and environment:
// the PID stays, so systemd (or whatever started the daemon) keeps tracking
// it. Go opens every descriptor close-on-exec, so nothing leaks into the new
// image.
func restartInto(exe string) error {
	return syscall.Exec(exe, os.Args, os.Environ())
}
