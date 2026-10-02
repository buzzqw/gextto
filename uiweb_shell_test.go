package gextto

import (
	"testing"
	"time"
)

// TestHealthStatusDebouncer verifies the top-bar pill ignores short-lived
// problems and only reports "degraded" once it has lasted the grace period.
func TestHealthStatusDebouncer(t *testing.T) {
	var d healthStatusDebouncer
	base := time.Unix(1_700_000_000, 0)

	if got := d.update("degraded", base); got != "ok" {
		t.Fatalf("first degraded = %q, want ok", got)
	}
	if got := d.update("degraded", base.Add(4*time.Second)); got != "ok" {
		t.Fatalf("degraded after 4s = %q, want ok", got)
	}
	if got := d.update("degraded", base.Add(5*time.Second)); got != "degraded" {
		t.Fatalf("degraded after 5s = %q, want degraded", got)
	}
	if got := d.update("ok", base.Add(6*time.Second)); got != "ok" {
		t.Fatalf("recovered = %q, want ok", got)
	}

	// A blip that clears before the grace period stays hidden.
	if got := d.update("degraded", base.Add(10*time.Second)); got != "ok" {
		t.Fatalf("new blip = %q, want ok", got)
	}
	if got := d.update("ok", base.Add(11*time.Second)); got != "ok" {
		t.Fatalf("blip cleared = %q, want ok", got)
	}

	// A problem that appears after recovery restarts the grace period.
	if got := d.update("degraded", base.Add(20*time.Second)); got != "ok" {
		t.Fatalf("first problem = %q, want ok", got)
	}
	if got := d.update("degraded", base.Add(25*time.Second)); got != "degraded" {
		t.Fatalf("problem after 5s = %q, want degraded", got)
	}
}
