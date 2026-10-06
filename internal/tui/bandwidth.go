package tui

import (
	"fmt"
	"time"
)

// Traffic indexes for bandwidthMeter.
const (
	trafficTermOut = iota // bytes written to the terminal (SSH downstream)
	trafficTermIn         // bytes read from the keyboard (SSH upstream)
	trafficAPIIn          // bytes received from the daemon API
	trafficAPIOut         // bytes sent to the daemon API
	trafficKinds
)

// bandwidthMeter turns cumulative byte counters into smoothed rates for the
// footer, so the user can see what the TUI itself costs on the link.
type bandwidthMeter struct {
	last  time.Time
	prev  [trafficKinds]int64
	rates [trafficKinds]float64
}

// sample records the counters at now and returns the smoothed rates (bytes
// per second, exponentially weighted so a single burst does not dominate).
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
		if b.rates[index] < 1 {
			b.rates[index] = 0
		}
	}
	b.last, b.prev = now, counters
	return b.rates
}

// formatBandwidth renders the footer meter, e.g. "Term ↓1.2K ↑8B · API ↓3.0K ↑410B /s".
// Two significant digits keep the text stable, so the meter itself rarely
// needs a redraw.
func formatBandwidth(tr *Translator, rates [trafficKinds]float64) string {
	return fmt.Sprintf("%s ↓%s ↑%s · API ↓%s ↑%s /s", tr.T("label.bwterm"),
		compactBytes(rates[trafficTermOut]), compactBytes(rates[trafficTermIn]),
		compactBytes(rates[trafficAPIIn]), compactBytes(rates[trafficAPIOut]))
}

// compactBytes renders a byte count in at most four cells plus the unit.
func compactBytes(value float64) string {
	units := []string{"B", "K", "M", "G"}
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
