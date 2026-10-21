//go:build !linux

package main

import "time"

// processCPU is not measured outside Linux: the real-swarm measurement then
// reports bytes and peers only.
func processCPU() time.Duration { return 0 }
