package gextto

import (
	"testing"
	"time"
)

// fakeWAN is a router whose counters the test advances; it wraps at 32 bits.
type fakeWAN struct{ rx, tx uint64 }

func (f *fakeWAN) GetTotalBytesReceived() (uint64, error) { return f.rx % (1 << 32), nil }
func (f *fakeWAN) GetTotalBytesSent() (uint64, error)     { return f.tx % (1 << 32), nil }

// lineStep moves the clock by one read interval with the given WAN and own
// download rates (bytes/s) and returns the detector's verdict.
func lineStep(l *lineUsage, wan *fakeWAN, now *time.Time, wanDown, ownDown uint64) (bool, bool) {
	step := lineUsageReadEvery
	l.observe(*now, ownDown, 0)
	*now = now.Add(step)
	wan.rx += wanDown * uint64(step/time.Second)
	l.observe(*now, ownDown, 0)
	return l.read(*now)
}

// TestLineUsageSeesTheOthersAndNotTheOverhead pins the detector: Gextto alone
// (WAN = own traffic plus 4 % of overhead) is "nobody else", across a 32-bit
// counter wrap; another device adding 600 KiB/s is seen once a majority of the
// last five reads agree, and its traffic is estimated.
func TestLineUsageSeesTheOthersAndNotTheOverhead(t *testing.T) {
	wan := &fakeWAN{rx: 1<<32 - 10<<20}
	saved := discoverWANCounters
	discoverWANCounters = func() wanCounters { return wan }
	t.Cleanup(func() { discoverWANCounters = saved })
	l := &lineUsage{}
	now := time.Unix(1_800_000_000, 0)
	const own = 9 << 20
	l.read(now) // first read: the baseline of the counters
	for i := 0; i < 10; i++ {
		present, known := lineStep(l, wan, &now, own*104/100, own)
		if !known || present {
			t.Fatalf("read %d alone: present=%v known=%v", i, present, known)
		}
	}
	if ratio := l.downHead.ratio(); ratio < 0.035 || ratio > 0.045 {
		t.Fatalf("learned overhead %.3f, want ~0.04", ratio)
	}
	// Five reads, majority: three are needed, so the first read alone (or a
	// single burst) cannot move the caps.
	var present bool
	for i := 0; i < 3; i++ {
		present, _ = lineStep(l, wan, &now, own*104/100+600<<10, own)
	}
	if !present {
		t.Fatal("another device at 600 KiB/s was not seen")
	}
	if down, _, _ := l.others(); down < 550<<10 || down > 650<<10 {
		t.Fatalf("others' download %.0f B/s, want ~600 KiB/s", down)
	}
}

// TestLineUsageIgnoresBackgroundTraffic: a phone sync or a notification at
// 120 KiB/s is not someone using the line. It must not make the guard yield.
func TestLineUsageIgnoresBackgroundTraffic(t *testing.T) {
	wan := &fakeWAN{}
	saved := discoverWANCounters
	discoverWANCounters = func() wanCounters { return wan }
	t.Cleanup(func() { discoverWANCounters = saved })
	l := &lineUsage{}
	now := time.Unix(1_800_000_000, 0)
	const own = 9 << 20
	l.read(now) // baseline
	for i := 0; i < 10; i++ {
		if present, known := lineStep(l, wan, &now, own*104/100+120<<10, own); !known || present {
			t.Fatalf("read %d at 120 KiB/s of background traffic: present=%v known=%v", i, present, known)
		}
	}
}

// TestOthersOnUsesAnAbsoluteFloor pins the threshold itself: background noise
// is below it, a real second user is above it.
func TestOthersOnUsesAnAbsoluteFloor(t *testing.T) {
	const own = 100 << 10
	if othersOn(float64(124<<10), float64(own), 0.05) {
		t.Fatal("124 KiB/s of background traffic counted as someone using the line")
	}
	if !othersOn(float64(600<<10), float64(own), 0.05) {
		t.Fatal("600 KiB/s of another user was not counted")
	}
}

// TestCounterDeltaWraps pins the 32-bit wrap and a 64-bit reset.
func TestCounterDeltaWraps(t *testing.T) {
	if got := counterDelta(1<<32-100, 50); got != 150 {
		t.Fatalf("32-bit wrap delta = %d, want 150", got)
	}
	if got := counterDelta(1<<40, 5); got != 0 {
		t.Fatalf("64-bit reset delta = %d, want 0", got)
	}
}
