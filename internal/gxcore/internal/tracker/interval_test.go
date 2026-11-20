package tracker

import (
	"testing"
	"time"
)

// TestAnnounceIntervalFromSeconds: a tracker's interval is stranger data. Zero
// and negative values must not turn into an immediate re-announce, and a huge
// value must not overflow the duration or silence the torrent for years.
func TestAnnounceIntervalFromSeconds(t *testing.T) {
	cases := []struct {
		seconds int64
		want    time.Duration
	}{
		{-3600, 0},
		{0, 0},
		{60, time.Minute},
		{int64(MaxAnnounceInterval/time.Second) - 1, MaxAnnounceInterval - time.Second},
		{int64(MaxAnnounceInterval / time.Second), MaxAnnounceInterval},
		{1 << 31, MaxAnnounceInterval},
		{1<<62 + 1, MaxAnnounceInterval},
	}
	for _, c := range cases {
		if got := AnnounceIntervalFromSeconds(c.seconds); got != c.want {
			t.Errorf("AnnounceIntervalFromSeconds(%d) = %v, want %v", c.seconds, got, c.want)
		}
	}
}

// TestClampAnnounceInterval: same rules for a duration a tracker implementation
// already built.
func TestClampAnnounceInterval(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want time.Duration
	}{
		{-time.Minute, 0},
		{0, 0},
		{90 * time.Minute, 90 * time.Minute},
		{MaxAnnounceInterval, MaxAnnounceInterval},
		{MaxAnnounceInterval + time.Hour, MaxAnnounceInterval},
	}
	for _, c := range cases {
		if got := ClampAnnounceInterval(c.in); got != c.want {
			t.Errorf("ClampAnnounceInterval(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
