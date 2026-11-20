package announcer

// latency_test.go locks the announce latency figures: the last duration, the
// median over the window (so one slow reply does not look like a slow tracker)
// and the window's size.

import (
	"testing"
	"time"
)

func TestStatsReportTheAnnounceLatency(t *testing.T) {
	a := &PeriodicalAnnouncer{status: Contacting}
	a.announceStarted = time.Now().Add(-300 * time.Millisecond)
	a.recordAnnounceLatency()
	a.announceStarted = time.Now().Add(-100 * time.Millisecond)
	a.recordAnnounceLatency()
	a.status = Working

	stats := a.stats()
	if stats.LatencySamples != 2 {
		t.Fatalf("samples = %d, want 2", stats.LatencySamples)
	}
	if stats.MedianLatency < 150*time.Millisecond || stats.MedianLatency > 250*time.Millisecond {
		t.Fatalf("median = %v, want about 200ms", stats.MedianLatency)
	}
	if stats.LastLatency < 50*time.Millisecond || stats.LastLatency > 150*time.Millisecond {
		t.Fatalf("last = %v, want about 100ms", stats.LastLatency)
	}
	if stats.InFlight {
		t.Fatal("no announce is in flight after the reply closed it")
	}
}

func TestLatencyWindowKeepsTheNewestAnnounces(t *testing.T) {
	a := &PeriodicalAnnouncer{status: Contacting}
	for i := 0; i < announceLatencyWindow+5; i++ {
		a.announceStarted = time.Now().Add(-time.Duration(i+1) * time.Millisecond)
		a.recordAnnounceLatency()
	}
	stats := a.stats()
	if stats.LatencySamples != announceLatencyWindow {
		t.Fatalf("samples = %d, want %d", stats.LatencySamples, announceLatencyWindow)
	}
	if stats.LastLatency < 19*time.Millisecond || stats.LastLatency > 30*time.Millisecond {
		t.Fatalf("last = %v, want the newest sample (about 25ms)", stats.LastLatency)
	}
	if stats.MedianLatency <= 0 {
		t.Fatalf("median = %v, want the window's median", stats.MedianLatency)
	}
}

func TestMedianDuration(t *testing.T) {
	if got := medianDuration(nil); got != 0 {
		t.Fatalf("empty median = %v, want 0", got)
	}
	odd := []time.Duration{3 * time.Millisecond, 1 * time.Millisecond, 2 * time.Millisecond}
	if got := medianDuration(odd); got != 2*time.Millisecond {
		t.Fatalf("odd median = %v, want 2ms", got)
	}
	even := []time.Duration{4 * time.Millisecond, 1 * time.Millisecond, 3 * time.Millisecond, 2 * time.Millisecond}
	if got := medianDuration(even); got != 2500*time.Microsecond {
		t.Fatalf("even median = %v, want 2.5ms", got)
	}
}

func TestStatsMarkTheAnnounceInFlight(t *testing.T) {
	a := &PeriodicalAnnouncer{status: Contacting}
	if !a.stats().InFlight {
		t.Fatal("an announce waiting for the reply must be reported in flight")
	}
	a.status = Working
	if a.stats().InFlight {
		t.Fatal("a settled announce is not in flight")
	}
}
