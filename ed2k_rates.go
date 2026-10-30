package gextto

// ed2k_rates.go turns aMule's session counters into the eD2k figures the top
// bar shows.
//
// aMule reports its live speeds over a one-second window. A client that takes
// a file in bursts therefore reads hundreds of KB/s for one instant and nothing
// in between, and adding that to the torrents made the top bar drop to zero
// while data was still moving. The monotonic counters of the session — the
// bytes aMule sent and received since amuled started — give the rate over the
// last half minute, which is what a person reading the badge wants.

import "time"

const (
	// ed2kRateWindow is the span the badge averages over: long enough to cover
	// a client's request cycle instead of a single burst.
	ed2kRateWindow = 30 * time.Second
	// ed2kRateMaxGap is how far apart two readings may be before the counters
	// stop meaning anything (aMule was down, or nobody asked for a while).
	ed2kRateMaxGap = 2 * ed2kRateWindow
)

// ed2kRateMinSpan is the shortest span worth dividing by: below it the two
// readings are too close to give a rate, and aMule's own live figures are used
// instead. Tests shorten it.
var ed2kRateMinSpan = 2 * time.Second

// ed2kRateReading is one reading of aMule's session counters.
type ed2kRateReading struct {
	At   time.Time
	Sent int64
	Recv int64
}

// ed2kRateTracker averages aMule's counters over ed2kRateWindow.
type ed2kRateTracker struct {
	readings []ed2kRateReading
	down     uint64
	up       uint64
}

// observe feeds one status reading and returns the rates to show. liveDown and
// liveUp are aMule's own speeds, used until the window has a span to divide by
// (the first reading after a start, a restart or a long silence).
func (t *ed2kRateTracker) observe(at time.Time, sent, recv, liveDown, liveUp int64) (uint64, uint64) {
	if n := len(t.readings); n > 0 {
		last := t.readings[n-1]
		// amuled restarted: its counters begin again from zero, and the
		// negative difference would read as no traffic for the whole window.
		if sent < last.Sent || recv < last.Recv || at.Before(last.At) {
			t.readings = t.readings[:0]
		}
	}
	t.readings = append(t.readings, ed2kRateReading{At: at, Sent: sent, Recv: recv})
	// Only the window is kept, never fewer than two readings: the oldest one
	// may sit just before the edge and still cover the full span.
	edge := at.Add(-ed2kRateWindow)
	for len(t.readings) > 2 && t.readings[0].At.Before(edge) {
		t.readings = t.readings[1:]
	}
	first, last := t.readings[0], t.readings[len(t.readings)-1]
	span := last.At.Sub(first.At)
	if span < ed2kRateMinSpan || span > ed2kRateMaxGap {
		// Nothing to average over: start again from this reading.
		t.readings = t.readings[len(t.readings)-1:]
		t.down, t.up = ed2kRateValue(liveDown), ed2kRateValue(liveUp)
		return t.down, t.up
	}
	t.down = ed2kRateValue(int64(float64(last.Recv-first.Recv) / span.Seconds()))
	t.up = ed2kRateValue(int64(float64(last.Sent-first.Sent) / span.Seconds()))
	return t.down, t.up
}

// live records aMule's own speeds for the times its counters cannot be read:
// a core with no statistics history answers the status but sends no session
// totals, and averaging zeros would show no traffic at all.
func (t *ed2kRateTracker) live(down, up int64) (uint64, uint64) {
	t.readings = t.readings[:0]
	t.down, t.up = ed2kRateValue(down), ed2kRateValue(up)
	return t.down, t.up
}

// ed2kRateValue keeps a rate unsigned; a counter that went backwards is no
// traffic.
func ed2kRateValue(rate int64) uint64 {
	if rate <= 0 {
		return 0
	}
	return uint64(rate)
}
