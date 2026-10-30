package gextto

// ed2k_rates_test.go covers the windowed eD2k rates the top bar shows: aMule's
// live speed is a one-second window, so an upload in bursts used to read a few
// hundred KB/s for an instant and nothing in between.

import (
	"testing"
	"time"
)

// shortEd2kRateSpan lowers the minimum span so a test can feed readings a few
// milliseconds apart.
func shortEd2kRateSpan(t *testing.T) {
	t.Helper()
	previous := ed2kRateMinSpan
	ed2kRateMinSpan = time.Second
	t.Cleanup(func() { ed2kRateMinSpan = previous })
}

// TestEd2kRateTrackerUsesLiveFiguresAtFirst: the first reading has no span to
// average over, so aMule's own speeds are shown.
func TestEd2kRateTrackerUsesLiveFiguresAtFirst(t *testing.T) {
	var tracker ed2kRateTracker
	down, up := tracker.observe(time.Now(), 0, 0, 4096, 2048)
	if down != 4096 || up != 2048 {
		t.Fatalf("first reading = %d/%d, want aMule's 4096/2048", down, up)
	}
}

// TestEd2kRateTrackerAveragesABurst: a client that uploads 553 KiB every 15 s
// reads zero in aMule's one-second window between the bursts; over the window
// the badge shows the bytes that really left.
func TestEd2kRateTrackerAveragesABurst(t *testing.T) {
	shortEd2kRateSpan(t)
	var tracker ed2kRateTracker
	start := time.Now()
	tracker.observe(start, 0, 0, 0, 0)
	if _, up := tracker.observe(start.Add(10*time.Second), 0, 0, 0, 0); up != 0 {
		t.Fatalf("no traffic yet = %d B/s, want 0", up)
	}
	// The burst lands on this reading: the 20 s from the first one carried it,
	// so the average is the burst spread over that span. aMule's live figure
	// would have said 553 KB/s for one second and then nothing.
	if _, up := tracker.observe(start.Add(20*time.Second), 553*1024, 0, 0, 0); up != 553*1024/20 {
		t.Fatalf("the reading of the burst itself = %d B/s, want %d B/s", up, 553*1024/20)
	}
	// The window now covers 30 s, the oldest reading included.
	if _, up := tracker.observe(start.Add(30*time.Second), 553*1024, 0, 0, 0); up != 553*1024/30 {
		t.Fatalf("30 s after the start = %d B/s, want %d B/s", up, 553*1024/30)
	}
	// An idle reading does not blank the figure while the burst is still in
	// the window: the oldest reading drops and the span stays at 30 s.
	if _, up := tracker.observe(start.Add(40*time.Second), 553*1024, 0, 0, 0); up != 553*1024/30 {
		t.Fatalf("one idle reading later = %d B/s, want %d B/s", up, 553*1024/30)
	}
}

// TestEd2kRateTrackerStartsAgainAfterARestart: amuled restarts its counters
// from zero; the negative difference must not read as traffic nor blank the
// badge for a whole window.
func TestEd2kRateTrackerStartsAgainAfterARestart(t *testing.T) {
	shortEd2kRateSpan(t)
	var tracker ed2kRateTracker
	start := time.Now()
	tracker.observe(start, 10<<20, 20<<20, 0, 0)
	if _, up := tracker.observe(start.Add(10*time.Second), 11<<20, 20<<20, 0, 0); up != (1<<20)/10 {
		t.Fatalf("steady upload = %d B/s, want %d B/s", up, (1<<20)/10)
	}
	down, up := tracker.observe(start.Add(20*time.Second), 4096, 8192, 3000, 2000)
	if down != 3000 || up != 2000 {
		t.Fatalf("after a restart = %d/%d, want aMule's 3000/2000", down, up)
	}
}

// TestEd2kRateTrackerDropsAStaleWindow: readings minutes apart say nothing
// about the current speed, so aMule's live figures win and the window starts
// again from that reading.
func TestEd2kRateTrackerDropsAStaleWindow(t *testing.T) {
	shortEd2kRateSpan(t)
	var tracker ed2kRateTracker
	start := time.Now()
	tracker.observe(start, 0, 0, 0, 0)
	if down, up := tracker.observe(start.Add(5*time.Minute), 100<<20, 0, 3000, 2000); down != 3000 || up != 2000 {
		t.Fatalf("after a long silence = %d/%d, want aMule's 3000/2000", down, up)
	}
	if _, up := tracker.observe(start.Add(5*time.Minute+10*time.Second), 110<<20, 0, 0, 0); up != (10<<20)/10 {
		t.Fatalf("the window did not start again: %d B/s, want %d B/s", up, (10<<20)/10)
	}
}

// TestEd2kRateTrackerFallsBackWithoutCounters: a core with no statistics
// history answers the status but sends no session totals, and averaging zeros
// would show no traffic at all.
func TestEd2kRateTrackerFallsBackWithoutCounters(t *testing.T) {
	shortEd2kRateSpan(t)
	var tracker ed2kRateTracker
	start := time.Now()
	tracker.observe(start, 10<<20, 20<<20, 0, 0)
	if down, up := tracker.live(3000, 2000); down != 3000 || up != 2000 {
		t.Fatalf("without counters = %d/%d, want aMule's 3000/2000", down, up)
	}
	// The window starts again from the first reading with counters back: that
	// reading has no span yet, the next one averages again.
	if down, up := tracker.observe(start.Add(10*time.Second), 11<<20, 20<<20, 0, 0); down != 0 || up != 0 {
		t.Fatalf("first reading with counters back = %d/%d, want aMule's 0/0", down, up)
	}
	if down, up := tracker.observe(start.Add(20*time.Second), 12<<20, 20<<20, 0, 0); down != 0 || up != (1<<20)/10 {
		t.Fatalf("window restarted = %d/%d, want 0 and %d B/s", down, up, (1<<20)/10)
	}
}

// TestV2Ed2kRatesShowTheWindowedCounters: the top bar reads the average the
// tracker keeps, not aMule's one-second speed.
func TestV2Ed2kRatesShowTheWindowedCounters(t *testing.T) {
	core, cfg := ed2kTestCore(t, nil)
	state := newTestAppState(t)
	for key, value := range cfg.Settings {
		if err := SaveSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	core.SetSessionTotals(0, 0)
	// The first call caches aMule's status and feeds the tracker.
	v2Ed2kRates(state)

	ed2kState.mu.Lock()
	ed2kState.rates.down, ed2kState.rates.up = 300*1024, 40*1024
	ed2kState.mu.Unlock()
	down, up := v2Ed2kRates(state)
	if down != 300*1024 || up != 40*1024 {
		t.Fatalf("top bar eD2k rates = %d/%d, want the windowed 300 KiB/s / 40 KiB/s", down, up)
	}

	// The floor still applies to the averaged figures.
	ed2kState.mu.Lock()
	ed2kState.rates.down, ed2kState.rates.up = 400, 500
	ed2kState.mu.Unlock()
	if down, up := v2Ed2kRates(state); down != 0 || up != 0 {
		t.Fatalf("sub-kilobyte rates = %d/%d, want 0/0", down, up)
	}
}
