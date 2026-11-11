package main

import (
	"testing"
	"time"
)

// The probe runs every second while the line is loaded and once every
// autoSpeedRestProbeEvery while it rests; the first probe of a rest is not
// delayed.
func TestAutoSpeedProbeCadenceFollowsTheLine(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	if wait := autoSpeedNextProbe(now, now.Add(-time.Second), false); wait != 0 {
		t.Fatalf("loaded line waited %v", wait)
	}
	if wait := autoSpeedNextProbe(now, time.Time{}, true); wait != 0 {
		t.Fatalf("first resting probe waited %v", wait)
	}
	if wait := autoSpeedNextProbe(now, now.Add(-10*time.Second), true); wait != autoSpeedRestProbeEvery-10*time.Second {
		t.Fatalf("resting probe waits %v, want %v", wait, autoSpeedRestProbeEvery-10*time.Second)
	}
	if wait := autoSpeedNextProbe(now, now.Add(-time.Hour), true); wait != 0 {
		t.Fatalf("an overdue resting probe waited %v", wait)
	}
}

// A line below the idle threshold in both directions is resting; either
// direction above it loads the line.
func TestAutoSpeedLoadingThreshold(t *testing.T) {
	idle := int64(autoSpeedIdleKiB*1024 - 1)
	busy := int64(autoSpeedIdleKiB * 1024)
	if autoSpeedLoading(idle, idle) {
		t.Fatal("keep-alive traffic counted as a loaded line")
	}
	if !autoSpeedLoading(busy, 0) || !autoSpeedLoading(0, busy) {
		t.Fatal("a direction at the threshold did not load the line")
	}
}

// A controller holding a cap is not resting, so the probe keeps its fast
// cadence until the cap is released.
func TestAutoSpeedFreeOnlyWithoutCaps(t *testing.T) {
	var a autoSpeed
	if !a.free() {
		t.Fatal("a fresh controller is not free")
	}
	a.mode = autoSpeedModeControl
	a.downCap = 512
	if a.free() {
		t.Fatal("a controller with a cap counted as free")
	}
}

// The wake never blocks: a second request while one is pending is dropped.
func TestWakeAutoSpeedDoesNotBlock(t *testing.T) {
	d := &Daemon{autoSpeedWake: make(chan struct{}, 1)}
	d.wakeAutoSpeed()
	d.wakeAutoSpeed()
	if len(d.autoSpeedWake) != 1 {
		t.Fatalf("pending wakes = %d, want 1", len(d.autoSpeedWake))
	}
}
