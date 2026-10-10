package bandwidth

import (
	"testing"
	"time"
)

func TestLimiterRateChangesInPlace(t *testing.T) {
	var unset *Limiter
	if d := unset.Take(1 << 20); d != 0 {
		t.Fatalf("nil limiter waited %v", d)
	}
	l := New(0)
	if d := l.Take(1 << 20); d != 0 {
		t.Fatalf("unlimited limiter waited %v", d)
	}
	l.SetRate(1024)
	l.Take(1024) // drain the initial burst
	if d := l.Take(1024); d < 900*time.Millisecond {
		t.Fatalf("1 KiB/s limiter waited only %v for 1 KiB", d)
	}
	l.SetRate(0)
	if d := l.Take(1 << 20); d != 0 || l.Rate() != 0 {
		t.Fatalf("limit removed but waited %v (rate %d)", d, l.Rate())
	}
}

// gextto fork: a torrent limiter inherits the session one until an explicit
// per-torrent limit is set.
func TestLimiterInheritsSessionAndOverrides(t *testing.T) {
	session := New(0)
	child := New(0)
	child.SetParent(session)

	// Inherited: the child follows the session rate.
	session.SetRate(1024)
	if got := child.Rate(); got != 1024 {
		t.Fatalf("inherited rate = %d, want 1024", got)
	}
	session.Take(1024) // drain the session burst
	if d := child.Take(1024); d < 900*time.Millisecond {
		t.Fatalf("inherited 1 KiB/s waited only %v", d)
	}

	// Explicit unlimited overrides the session.
	child.SetLimitKiB(0, session)
	if d := child.Take(1 << 20); d != 0 {
		t.Fatalf("explicit unlimited waited %v", d)
	}
	// Explicit rate overrides the session.
	child.SetLimitKiB(2, session)
	if got := child.Rate(); got != 2048 {
		t.Fatalf("explicit rate = %d, want 2048", got)
	}
	child.Take(2048) // drain the child burst
	if d := child.Take(2048); d < 900*time.Millisecond {
		t.Fatalf("explicit 2 KiB/s waited only %v", d)
	}
	// Back to inherit.
	child.SetLimitKiB(-1, session)
	if got := child.Rate(); got != 1024 {
		t.Fatalf("restored inherit rate = %d, want 1024", got)
	}
}
