package main

import (
	"math"
	"strings"
	"testing"
)

func TestSetLimitsRejectsNonFiniteSeedRatio(t *testing.T) {
	d := &Daemon{}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		err := d.setLimits("missing", nil, nil, &value, nil)
		if err == nil || !strings.Contains(err.Error(), "finite") {
			t.Fatalf("setLimits(%v) error = %v, want a finite-number validation error", value, err)
		}
	}
}

func TestEncodeSnapshotSurvivesANonFiniteRatio(t *testing.T) {
	snapshot := &persistedState{Torrents: map[string]*torrentMeta{
		"a": {SeedRatio: math.NaN()},
		"b": {SeedRatio: math.Inf(1)},
		"c": {SeedRatio: 1.5},
	}}
	if _, err := encodeSnapshot(snapshot); err != nil {
		t.Fatalf("encodeSnapshot with a non-finite ratio: %v", err)
	}
	for key, want := range map[string]float64{"a": -1, "b": -1, "c": 1.5} {
		if got := snapshot.Torrents[key].SeedRatio; got != want {
			t.Fatalf("ratio %s = %v, want %v", key, got, want)
		}
	}
}

func TestParseFiniteFloatRefusesNaNAndInfinities(t *testing.T) {
	for _, raw := range []string{"NaN", "nan", "+Inf", "-Inf", "inf", "Infinity", "", "x"} {
		if _, ok := parseFiniteFloat(raw); ok {
			t.Fatalf("parseFiniteFloat(%q) accepted a non-finite or invalid value", raw)
		}
	}
	if v, ok := parseFiniteFloat(" 2.5 "); !ok || v != 2.5 {
		t.Fatalf("parseFiniteFloat(2.5) = %v, %v", v, ok)
	}
}

func TestStatsCountsMovesFromTheLiveSet(t *testing.T) {
	d := newTestDaemon(t)
	d.mu.Lock()
	d.moving["started-this-tick"] = true
	d.mu.Unlock()
	if got := d.stats().Moving; got != 1 {
		t.Fatalf("moving = %d, want 1: a move must count before the views are republished", got)
	}
}
