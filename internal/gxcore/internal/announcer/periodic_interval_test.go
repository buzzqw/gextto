package announcer

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// TestSteadyAnnounceInterval locks the pace chosen from a tracker's answer: a
// zero or negative interval keeps a working default instead of re-announcing
// immediately, the minimum interval is a floor and a week is the ceiling.
func TestSteadyAnnounceInterval(t *testing.T) {
	cases := []struct {
		name     string
		interval time.Duration
		min      time.Duration
		want     time.Duration
	}{
		{"missing interval", 0, time.Minute, tracker.DefaultAnnounceInterval},
		{"negative interval", -time.Hour, time.Minute, tracker.DefaultAnnounceInterval},
		{"normal", 30 * time.Minute, time.Minute, 30 * time.Minute},
		{"below the floor", 10 * time.Second, 5 * time.Minute, 5 * time.Minute},
		{"above a week", 30 * 24 * time.Hour, time.Minute, tracker.MaxAnnounceInterval},
		{"zero floor too", 0, 0, tracker.DefaultAnnounceInterval},
	}
	for _, c := range cases {
		if got := steadyAnnounceInterval(c.interval, c.min); got != c.want {
			t.Errorf("%s: steadyAnnounceInterval(%v, %v) = %v, want %v",
				c.name, c.interval, c.min, got, c.want)
		}
	}
}
