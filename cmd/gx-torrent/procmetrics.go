package main

// procmetrics.go samples this process' own CPU and memory for the status bar.
// The platform readers live in procmetrics_linux.go / _windows.go / _other.go.

import "time"

// processStartedAt is when this binary began running: the package is
// initialised before main, so it is the start time within milliseconds. It
// feeds the standard process_start_time_seconds gauge, which dashboards use to
// tell a restart apart from a stable process.
var processStartedAt = time.Now()

// processUsage returns the CPU load (percent of one core) and the current
// resident memory of the gx-torrent process.
//
// The CPU figure is a short average, not an instant: a render closer than a
// second to the previous one has too little CPU time to be meaningful and would
// spike on any burst, so it reuses the previous value, and each accepted sample
// is smoothed into a moving average. A multi-core machine can legitimately read
// above 100% (two busy cores = ~200%).
func (d *Daemon) processUsage(now time.Time) (cpuPercent float64, rssBytes int64) {
	rssBytes = processMemoryBytes()
	cpu := processCPUTime()

	d.procSampleMu.Lock()
	defer d.procSampleMu.Unlock()
	// The first sample only sets the baseline.
	if d.procLastAt.IsZero() {
		d.procLastCPU, d.procLastAt = cpu, now
		return 0, rssBytes
	}
	elapsed := now.Sub(d.procLastAt).Seconds()
	if elapsed < 1 {
		return d.procLastPct, rssBytes
	}
	instant := float64(cpu-d.procLastCPU) / float64(time.Second) / elapsed * 100
	if instant < 0 {
		instant = 0
	}
	if d.procHavePct {
		instant = d.procLastPct*0.6 + instant*0.4
	}
	d.procLastPct = instant
	d.procHavePct = true
	d.procLastCPU, d.procLastAt = cpu, now
	return instant, rssBytes
}
