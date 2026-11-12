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

// a torrent limiter inherits the session one until an explicit
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

// takeFree counts the bytes a limiter lets through at once, in 16 KiB blocks,
// before the first wait.
func takeFree(l *Limiter) int64 {
	var n int64
	for l.Take(16<<10) == 0 {
		n += 16 << 10
		if n > 1<<30 {
			break
		}
	}
	return n
}

// The burst is a few tens of milliseconds of the rate, not a whole second: a
// second's quota sent at once is a queue in the modem on a slow uplink.
func TestLimiterBurstIsShort(t *testing.T) {
	const rate = 4 << 20 // 4 MiB/s: a second's burst would be 4 MiB
	l := New(rate)
	if got, limit := takeFree(l), int64(rate)/10; got > limit {
		t.Fatalf("%d bytes left at once at %d B/s, want at most %d", got, rate, limit)
	}
}

// A rate change keeps the balance: lowering the rate on a busy line must not
// hand out a fresh burst.
func TestLimiterRateChangeDoesNotRefill(t *testing.T) {
	l := New(8 << 20)
	takeFree(l) // the bucket is now empty or in debt
	l.SetRate(4 << 20)
	if d := l.Take(16 << 10); d == 0 {
		t.Fatal("a rate change refilled the bucket")
	}
}

// Over a stretch of time the limiter lets through the rate, not more: the
// short burst must not change the average.
func TestLimiterKeepsTheRate(t *testing.T) {
	const rate = 2 << 20
	l := New(rate)
	start := time.Now()
	var sent int64
	var due time.Duration // a Take returns the wait until all taken so far is paid
	for due < 500*time.Millisecond {
		due = l.Take(16 << 10)
		sent += 16 << 10
	}
	elapsed := due + time.Since(start)
	got := float64(sent) / elapsed.Seconds()
	if got > rate*1.08 || got < rate*0.92 {
		t.Fatalf("average %.0f B/s, want about %d", got, rate)
	}
}

// A weight divides the wait for the shared debt: twice the weight, half the
// wait; the root rate still bounds the sum (TestLimiterKeepsTheRate).
func TestWeightScalesTheWait(t *testing.T) {
	root := New(1 << 20)
	light, heavy := New(0), New(0)
	light.SetParent(root)
	heavy.SetParent(root)
	heavy.SetWeight(200)
	takeFree(root)
	base := light.Take(64 << 10)
	weighted := heavy.Take(64 << 10)
	if base <= 0 || weighted <= 0 {
		t.Fatalf("no wait in debt: %v %v", base, weighted)
	}
	// The second take saw a larger debt; compare it with what a plain take
	// would have waited for the same debt.
	plain := light.Take(0)
	if weighted >= plain {
		t.Fatalf("weight 200 waited %v, plain take %v", weighted, plain)
	}
	if got := heavy.Weight(); got != 200 {
		t.Fatalf("weight = %d", got)
	}
	heavy.SetWeight(0)
	if got := heavy.Weight(); got != 100 {
		t.Fatalf("reset weight = %d", got)
	}
}

// An unlimited root never waits, whatever the weight.
func TestWeightIgnoredWhenUnlimited(t *testing.T) {
	root := New(0)
	child := New(0)
	child.SetParent(root)
	child.SetWeight(10)
	if d := child.TakeWeighted(1<<20, 0.01); d != 0 {
		t.Fatalf("unlimited waited %v", d)
	}
}

// The trading split gives each class its share whatever the head count, and
// stays out of the way when it is off or only one class is present.
func TestUploadClassesFactor(t *testing.T) {
	var c UploadClasses
	c.Add(ClassTrading, 2)
	c.Add(ClassSeeding, 8)
	if f := c.Factor(ClassTrading); f != 1 {
		t.Fatalf("split off: factor %v", f)
	}
	c.SetTradingShare(80)
	trading, seeding := c.Factor(ClassTrading), c.Factor(ClassSeeding)
	// Shares: 2*trading : 8*seeding must be 80 : 20.
	if got := 2 * trading / (2*trading + 8*seeding); got < 0.79 || got > 0.81 {
		t.Fatalf("trading share = %v (factors %v, %v)", got, trading, seeding)
	}
	c.Add(ClassSeeding, -8)
	if f := c.Factor(ClassTrading); f != 1 {
		t.Fatalf("one class only: factor %v", f)
	}
	c.SetTradingShare(150)
	if c.TradingShare() != 0 {
		t.Fatal("an out-of-range share must turn the split off")
	}
	if f := c.Factor(ClassNone); f != 1 {
		t.Fatalf("no class: factor %v", f)
	}
}
