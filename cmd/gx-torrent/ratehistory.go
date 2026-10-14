package main

// ratehistory.go keeps the transfer speed over time for the Statistics page.
// It lives in memory only: like qBittorrent's graph it starts empty when the
// daemon restarts. Three resolutions are kept so every range stays cheap to
// draw — one point per queue tick for the last hour, one per minute for the
// last day, one per hour for the last week — and the peaks are read from the
// same buffers. Each ring is fixed-size, so sampling allocates nothing.

import (
	"sync"
	"time"
)

const (
	// rateTickCapacity is about an hour at the default 3 s tick.
	rateTickCapacity = 1200
	// rateMinuteCapacity is a day of one-minute averages.
	rateMinuteCapacity = 1440
	// rateHourCapacity is a week of one-hour averages.
	rateHourCapacity = 168
	// rateHourFromMinutes is how many minute averages an hour average folds.
	rateHourFromMinutes = 60
)

// rateSample is one point of the graph (bytes/s).
type rateSample struct {
	Down int64 `json:"d"`
	Up   int64 `json:"u"`
}

// rateRange is one range of the graph with its peaks.
type rateRange struct {
	Range    string       `json:"range"`
	Samples  []rateSample `json:"samples"`
	DownPeak int64        `json:"down_peak"`
	UpPeak   int64        `json:"up_peak"`
}

// rateRing is a fixed-size ring: pushing never allocates and never grows.
type rateRing struct {
	buf  []rateSample
	head int
	n    int
}

func newRateRing(capacity int) rateRing {
	return rateRing{buf: make([]rateSample, capacity)}
}

func (r *rateRing) push(s rateSample) {
	r.buf[r.head] = s
	r.head = (r.head + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

// snapshot returns the points oldest first.
func (r *rateRing) snapshot() []rateSample {
	if r.n == 0 || len(r.buf) == 0 {
		return nil
	}
	out := make([]rateSample, r.n)
	start := (r.head - r.n + len(r.buf)) % len(r.buf)
	for i := range out {
		out[i] = r.buf[(start+i)%len(r.buf)]
	}
	return out
}

// rateHistory is the three-ring speed history. It is written by the serial
// queue tick and read by the HTTP handlers, so it has its own lock.
type rateHistory struct {
	mu     sync.Mutex
	tick   rateRing
	minute rateRing
	hour   rateRing

	lastMinute time.Time
	lastHour   time.Time
	// Accumulators for the minute average being built.
	minSumDown int64
	minSumUp   int64
	minCount   int
}

func newRateHistory() rateHistory {
	return rateHistory{
		tick:   newRateRing(rateTickCapacity),
		minute: newRateRing(rateMinuteCapacity),
		hour:   newRateRing(rateHourCapacity),
	}
}

// add records one sample. now is the tick time; down and up are bytes/s.
func (h *rateHistory) add(now time.Time, down, up int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tick.push(rateSample{Down: down, Up: up})
	h.minSumDown += down
	h.minSumUp += up
	h.minCount++
	if h.lastMinute.IsZero() {
		h.lastMinute = now
		return
	}
	if now.Sub(h.lastMinute) >= time.Minute {
		h.pushMinuteLocked()
		h.lastMinute = now
	}
	if h.lastHour.IsZero() {
		h.lastHour = now
	} else if now.Sub(h.lastHour) >= time.Hour {
		h.pushHourLocked()
		h.lastHour = now
	}
}

// pushMinuteLocked folds the samples of the last minute into the minute ring.
func (h *rateHistory) pushMinuteLocked() {
	if h.minCount == 0 {
		return
	}
	h.minute.push(rateSample{
		Down: h.minSumDown / int64(h.minCount),
		Up:   h.minSumUp / int64(h.minCount),
	})
	h.minSumDown, h.minSumUp, h.minCount = 0, 0, 0
}

// pushHourLocked folds the last hour of minute averages into the hour ring.
func (h *rateHistory) pushHourLocked() {
	minutes := h.minute.snapshot()
	n := rateHourFromMinutes
	if len(minutes) < n {
		n = len(minutes)
	}
	if n == 0 {
		return
	}
	var down, up int64
	for _, s := range minutes[len(minutes)-n:] {
		down += s.Down
		up += s.Up
	}
	h.hour.push(rateSample{Down: down / int64(n), Up: up / int64(n)})
}

// ranges lists the available range names, the short one first.
func (h *rateHistory) ranges() []string { return []string{"1h", "24h", "7d"} }

// snapshot returns one range with its peaks.
func (h *rateHistory) snapshot(name string) rateRange {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := rateRange{Range: name}
	switch name {
	case "24h":
		out.Samples = h.minute.snapshot()
	case "7d":
		out.Samples = h.hour.snapshot()
	default:
		out.Range = "1h"
		out.Samples = h.tick.snapshot()
	}
	for _, s := range out.Samples {
		if s.Down > out.DownPeak {
			out.DownPeak = s.Down
		}
		if s.Up > out.UpPeak {
			out.UpPeak = s.Up
		}
	}
	return out
}
