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
