package gextto

import (
	"testing"
	"time"
)

// TestHealthLogStateTransitions verifies the monitor logs a degraded status on
// transition (and on reason changes / hourly heartbeat) without flooding the log.
func TestHealthLogStateTransitions(t *testing.T) {
	var state healthLogState
	base := time.Unix(1_700_000_000, 0)

	if got := state.observe("ok", "", base); got != "" {
		t.Fatalf("healthy start = %q, want empty", got)
	}
	if got := state.observe("degraded", "cartella dati non scrivibile", base.Add(30*time.Second)); got != "degraded" {
		t.Fatalf("first degraded = %q, want degraded", got)
	}
	if got := state.observe("degraded", "cartella dati non scrivibile", base.Add(time.Minute)); got != "" {
		t.Fatalf("unchanged degraded = %q, want empty", got)
	}
	if got := state.observe("degraded", "Download assente", base.Add(2*time.Minute)); got != "degraded" {
		t.Fatalf("new reason = %q, want degraded", got)
	}
	if got := state.observe("degraded", "Download assente", base.Add(2*time.Minute+time.Hour)); got != "degraded" {
		t.Fatalf("heartbeat = %q, want degraded", got)
	}
	if got := state.observe("ok", "", base.Add(2*time.Minute+time.Hour+time.Minute)); got != "recovered" {
		t.Fatalf("recovery = %q, want recovered", got)
	}
	if got := state.observe("ok", "", base.Add(2*time.Minute+time.Hour+2*time.Minute)); got != "" {
		t.Fatalf("steady healthy = %q, want empty", got)
	}
}
