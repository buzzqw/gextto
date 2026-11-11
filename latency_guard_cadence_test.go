package gextto

import (
	"testing"
	"time"
)

// While the line rests the guard probes once every latencyGuardRestProbeEvery;
// a transfer, or a cap still held, brings back a probe at every round.
func TestLatencyGuardProbesSlowlyWhileTheLineRests(t *testing.T) {
	g := newLatencyGuard()
	now := time.Unix(1_000_000, 0)
	if !g.probeDue(now, 0, 0) {
		t.Fatal("the first probe of a resting line was skipped")
	}
	if g.probeDue(now.Add(latencyGuardProbeEvery), 0, 0) {
		t.Fatal("a resting line was probed at the loaded cadence")
	}
	if !g.probeDue(now.Add(latencyGuardRestProbeEvery), 0, 0) {
		t.Fatal("a resting line went unprobed past the rest cadence")
	}
	busy := uint64(latencyGuardIdleKiB * 1024)
	at := now.Add(latencyGuardRestProbeEvery + latencyGuardProbeEvery)
	if !g.probeDue(at, busy, 0) {
		t.Fatal("a download did not bring the probe back at once")
	}
	if !g.probeDue(at.Add(latencyGuardProbeEvery), 0, busy) {
		t.Fatal("an upload did not keep the probe at the loaded cadence")
	}
	g.mu.Lock()
	g.mode, g.upCap = guardControl, 256
	g.mu.Unlock()
	if !g.probeDue(at.Add(2*latencyGuardProbeEvery), 0, 0) {
		t.Fatal("a held cap did not keep the probe at the loaded cadence")
	}
}
