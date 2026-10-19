//go:build !windows

package main

import (
	"log"
	"os"
	"syscall"
	"time"
)

// restartProcess replaces the current image with the newly installed binary,
// keeping the command line and the environment. The listeners are close-on-exec
// and are recreated by the new process.
func restartProcess(exe string) {
	time.Sleep(500 * time.Millisecond)
	args := append([]string{exe}, os.Args[1:]...)
	if err := syscall.Exec(exe, args, os.Environ()); err != nil {
		log.Printf("gx-ed2k: restart after update: %v", err)
	}
}
