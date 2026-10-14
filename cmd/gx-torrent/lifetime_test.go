package main

// lifetime_test.go covers the all-time totals: the delta folding, the engine
// restart reset, the atomic persistence and the empty-data-dir no-op.

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLifetimeTotalsFoldDeltasAndSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	d := &Daemon{opts: Options{DataDir: dir}}
	d.loadLifetime()
	if d.lifetime.Downloaded != 0 || d.lifetime.Uploaded != 0 {
		t.Fatalf("fresh totals not empty: %+v", d.lifetime)
	}
	if d.lifetime.Since.IsZero() {
		t.Fatal("fresh totals must record when they start")
	}

	now := time.Now()
	// The first accumulate asks for a flush; a second one inside the cadence
	// does not.
	if !d.accumulateLifetimeLocked(now, 100, 50) {
		t.Fatal("the first accumulate must ask for a flush")
	}
	if d.accumulateLifetimeLocked(now, 150, 80) {
		t.Fatal("a second accumulate within the cadence must not flush")
	}
	// Deltas, not the raw counters.
	if d.lifetime.Downloaded != 150 || d.lifetime.Uploaded != 80 {
		t.Fatalf("after two samples: %+v", d.lifetime)
	}

	// The engine restarted: its session counters are back near zero and must
	// not subtract from the totals.
	d.accumulateLifetimeLocked(now, 10, 5)
	if d.lifetime.Downloaded != 150 || d.lifetime.Uploaded != 80 {
		t.Fatalf("a counter reset must not change the totals: %+v", d.lifetime)
	}
	// From there the deltas grow again.
	d.accumulateLifetimeLocked(now, 30, 20)
	if d.lifetime.Downloaded != 170 || d.lifetime.Uploaded != 95 {
		t.Fatalf("after the restart: %+v", d.lifetime)
	}

	d.mu.Lock()
	got := d.lifetimeLocked()
	d.mu.Unlock()
	if want := 95.0 / 170.0; math.Abs(got.Ratio-want) > 1e-9 {
		t.Fatalf("ratio = %v, want %v", got.Ratio, want)
	}

	d.saveLifetime()
	if _, err := os.Stat(filepath.Join(dir, "lifetime.json")); err != nil {
		t.Fatalf("totals file not written: %v", err)
	}

	// A new daemon reads them back.
	next := &Daemon{opts: Options{DataDir: dir}}
	next.loadLifetime()
	if next.lifetime.Downloaded != 170 || next.lifetime.Uploaded != 95 {
		t.Fatalf("totals not restored: %+v", next.lifetime)
	}
	if next.lifetime.Since.IsZero() {
		t.Fatal("the start date was not restored")
	}
}

func TestLifetimeCorruptFileStartsFresh(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lifetime.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{opts: Options{DataDir: dir}}
	d.loadLifetime()
	if d.lifetime.Downloaded != 0 || d.lifetime.Since.IsZero() {
		t.Fatalf("a corrupt file must start fresh: %+v", d.lifetime)
	}
}

func TestLifetimeWithoutDataDirPersistsNothing(t *testing.T) {
	d := &Daemon{}
	if d.lifetimePath() != "" {
		t.Fatalf("lifetimePath = %q, want empty", d.lifetimePath())
	}
	d.loadLifetime()
	d.accumulateLifetimeLocked(time.Now(), 10, 20)
	d.saveLifetime() // must be a no-op, never a file in the working directory
	if d.lifetime.Downloaded != 10 || d.lifetime.Uploaded != 20 {
		t.Fatalf("totals not kept in memory: %+v", d.lifetime)
	}
}
