package main

// procmetrics.go samples this process' own CPU and memory for the status bar.
// The platform readers live in procmetrics_linux.go / _windows.go / _other.go.

import "time"

// processUsage returns the CPU load (percent, measured since the previous
// sample) and the current resident memory of the gx-torrent process. The first
// call has no previous sample, so the CPU percent is 0.
func (d *Daemon) processUsage(now time.Time) (cpuPercent float64, rssBytes int64) {
	rssBytes = processMemoryBytes()
	cpu := processCPUTime()

	d.procSampleMu.Lock()
	defer d.procSampleMu.Unlock()
	if !d.procLastAt.IsZero() {
		if elapsed := now.Sub(d.procLastAt).Seconds(); elapsed > 0 {
			cpuPercent = float64(cpu-d.procLastCPU) / float64(time.Second) / elapsed * 100
		}
	}
	d.procLastCPU = cpu
	d.procLastAt = now
	if cpuPercent < 0 {
		cpuPercent = 0
	}
	return cpuPercent, rssBytes
}
