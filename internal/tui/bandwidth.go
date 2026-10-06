package tui

import (
	"fmt"
	"time"
)

// Traffic indexes for bandwidthMeter.
const (
	trafficReceived = iota // bytes the TUI receives: daemon responses and keystrokes
	trafficSent            // bytes the TUI sends: screen output and daemon requests
	trafficRequests        // requests made to the daemon API
	trafficKinds
)

// bandwidthMeter turns cumulative counters into smoothed rates for the
// footer, so the user can see what the TUI itself costs.
type bandwidthMeter struct {
	last  time.Time
	prev  [trafficKinds]int64
	rates [trafficKinds]float64
}

// sample records the counters at now and returns the smoothed rates (per
// second, exponentially weighted so a single burst does not dominate).
func (b *bandwidthMeter) sample(now time.Time, counters [trafficKinds]int64) [trafficKinds]float64 {
	if b.last.IsZero() {
		b.last, b.prev = now, counters
		return b.rates
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed <= 0 {
		return b.rates
	}
	for index := range counters {
		rate := float64(counters[index]-b.prev[index]) / elapsed
		b.rates[index] = 0.5*b.rates[index] + 0.5*rate
		if b.rates[index] < 0.05 {
			b.rates[index] = 0
		}
	}
	b.last, b.prev = now, counters
	return b.rates
}

// formatBandwidth renders the footer meter, e.g. "TUI ↓7.2KB/s ↑230B/s · 1.5 req/s".
// Two significant digits keep the text stable, so the meter itself rarely
// needs a redraw.
func formatBandwidth(rates [trafficKinds]float64) string {
	requests := fmt.Sprintf("%.1f", rates[trafficRequests])
	if rates[trafficRequests] >= 10 {
		requests = fmt.Sprintf("%.0f", rates[trafficRequests])
	}
	return fmt.Sprintf("TUI ↓%s/s ↑%s/s · %s req/s", compactBytes(rates[trafficReceived]), compactBytes(rates[trafficSent]), requests)
}

// compactBytes renders a byte count with two significant digits.
func compactBytes(value float64) string {
	units := []string{"B", "KB", "MB", "GB"}
	unit := 0
	for value >= 1000 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 || value >= 10 {
		return fmt.Sprintf("%.0f%s", value, units[unit])
	}
	return fmt.Sprintf("%.1f%s", value, units[unit])
}
