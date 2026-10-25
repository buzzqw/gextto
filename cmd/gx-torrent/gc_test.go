package main

import (
	"os"
	"runtime/debug"
	"testing"
	"time"
)

func TestApplyGCDefaultRespectsGOGC(t *testing.T) {
	prev := debug.SetGCPercent(100)
	t.Cleanup(func() { debug.SetGCPercent(prev) })

	t.Setenv("GOGC", "200")
	if applyGCDefault() {
		t.Fatal("GOGC in the environment must win")
	}
	if got := debug.SetGCPercent(100); got != 100 {
		t.Fatalf("GC percent changed to %d with GOGC set", got)
	}
}

func TestApplyGCDefaultSetsPercent(t *testing.T) {
	prev := debug.SetGCPercent(100)
	t.Cleanup(func() { debug.SetGCPercent(prev) })

	t.Setenv("GOGC", "") // registers the restore of the original value
	os.Unsetenv("GOGC")
	if !applyGCDefault() {
		t.Fatal("GOGC unset: the default must apply")
	}
	if got := debug.SetGCPercent(100); got != gcPercent {
		t.Fatalf("GC percent = %d, want %d", got, gcPercent)
	}
}

func TestMemoryReleaseDue(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		active  time.Time
		release time.Time
		want    bool
	}{
		{name: "no download yet", active: time.Time{}, want: false},
		{name: "just active", active: now.Add(-time.Minute), want: false},
		{name: "idle long enough", active: now.Add(-6 * time.Minute), want: true},
		{name: "released recently", active: now.Add(-time.Hour), release: now.Add(-time.Minute), want: false},
		{name: "release due again", active: now.Add(-time.Hour), release: now.Add(-11 * time.Minute), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := memoryReleaseDue(now, test.active, test.release); got != test.want {
				t.Fatalf("memoryReleaseDue = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMemoryLogDueAggregatesTheIdlePeriod(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if !memoryLogDue(now, time.Time{}) {
		t.Fatal("the first release after a download must be logged")
	}
	if memoryLogDue(now, now.Add(-10*time.Minute)) {
		t.Fatal("the ten-minute release must not repeat the sentence")
	}
	if !memoryLogDue(now, now.Add(-memoryLogEvery)) {
		t.Fatal("the idle release must be logged again after memoryLogEvery")
	}
}
