package main

// ratehistory_test.go covers the speed history: the three resolutions, the
// rollovers, the peaks and the range fallback.

import (
	"testing"
	"time"
)

func TestRateHistoryKeepsTickRangeAndPeaks(t *testing.T) {
	h := newRateHistory()
	base := time.Now()
	for i := 0; i < 30; i++ {
		h.add(base.Add(time.Duration(i)*3*time.Second), int64(i), int64(i*2))
	}
	got := h.snapshot("1h")
	if got.Range != "1h" || len(got.Samples) != 30 {
		t.Fatalf("1h range: %+v", got)
	}
	if got.DownPeak != 29 || got.UpPeak != 58 {
		t.Fatalf("peaks = %d/%d, want 29/58", got.DownPeak, got.UpPeak)
	}
	if got.Samples[0].Down != 0 || got.Samples[29].Up != 58 {
		t.Fatalf("samples not oldest first: %+v", got.Samples)
	}
	// 87 s of samples already rolled one minute over.
	if n := len(h.snapshot("24h").Samples); n != 1 {
		t.Fatalf("24h samples = %d, want 1", n)
	}
}

func TestRateHistoryUnknownRangeFallsBackToHour(t *testing.T) {
	h := newRateHistory()
	h.add(time.Now(), 5, 6)
	if got := h.snapshot("bogus"); got.Range != "1h" || len(got.Samples) != 1 {
		t.Fatalf("unknown range: %+v", got)
	}
}

func TestRateHistoryFoldsHoursFromMinutes(t *testing.T) {
	h := newRateHistory()
	h.mu.Lock()
	h.minute.push(rateSample{Down: 10, Up: 20})
	h.minute.push(rateSample{Down: 30, Up: 40})
	h.pushHourLocked()
	h.mu.Unlock()

	got := h.snapshot("7d")
	if len(got.Samples) != 1 {
		t.Fatalf("7d samples = %d, want 1", len(got.Samples))
	}
	if got.Samples[0].Down != 20 || got.Samples[0].Up != 30 {
		t.Fatalf("hour average = %+v, want 20/30", got.Samples[0])
	}
	if got.DownPeak != 20 || got.UpPeak != 30 {
		t.Fatalf("7d peaks = %d/%d, want 20/30", got.DownPeak, got.UpPeak)
	}
}

func TestRateHistoryRingIsBounded(t *testing.T) {
	h := newRateHistory()
	base := time.Now()
	// Ten times the capacity: the ring must keep only the newest points and
	// never grow.
	for i := 0; i < rateTickCapacity*10; i++ {
		h.add(base.Add(time.Duration(i)*3*time.Second), int64(i), 0)
	}
	got := h.snapshot("1h")
	if len(got.Samples) != rateTickCapacity {
		t.Fatalf("samples = %d, want %d", len(got.Samples), rateTickCapacity)
	}
	if got.Samples[len(got.Samples)-1].Down != int64(rateTickCapacity*10-1) {
		t.Fatalf("newest sample = %d", got.Samples[len(got.Samples)-1].Down)
	}
}

func TestRateHistoryZeroValueIsSafe(t *testing.T) {
	// A bare Daemon (a test literal) has a zero rateHistory: reading it must
	// not panic on the empty rings.
	var h rateHistory
	if got := h.snapshot("1h"); len(got.Samples) != 0 {
		t.Fatalf("zero history returned %d samples", len(got.Samples))
	}
}
