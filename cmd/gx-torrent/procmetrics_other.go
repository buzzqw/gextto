//go:build !linux && !windows

package main

// procmetrics_other.go has no native process counters on this platform: the
// status bar shows no CPU/memory rather than a wrong number.

import "time"

// processMemoryBytes is unknown on this platform.
func processMemoryBytes() int64 { return 0 }

// processCPUTime is unknown on this platform.
func processCPUTime() time.Duration { return 0 }
