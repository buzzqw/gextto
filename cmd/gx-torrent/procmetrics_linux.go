//go:build linux

package main

// procmetrics_linux.go reads this process' own memory and CPU time for the
// status bar. RSS comes from /proc/self/statm, CPU time from getrusage (no
// assumption about the clock-tick rate, unlike /proc/self/stat).

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// processMemoryBytes is the resident set size of this process (0 when unknown).
func processMemoryBytes() int64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || pages < 0 {
		return 0
	}
	return pages * int64(os.Getpagesize())
}

// processCPUTime is the total user+system CPU time used by this process.
func processCPUTime() time.Duration {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	return time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second +
		time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond
}
