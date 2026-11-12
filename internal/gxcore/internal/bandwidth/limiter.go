// Package bandwidth provides a rate limiter whose rate can change while peers
// hold it. The engine used a fixed bucket created with the session, so a new
// speed limit needed a session restart.
package bandwidth

import (
	"sync"
	"sync/atomic"
	"time"
)

// Limiter is a token bucket shared by the peers that hold it. A nil Limiter or
// a zero rate means unlimited. A limiter may also inherit another one: a torrent
// without an explicit limit forwards to the session limiter.
type Limiter struct {
	mu     sync.RWMutex
	bucket *bucket
	// rate and parent are atomics so Take can answer "unlimited" without taking
	// a lock: with tens of peers asking per block, the shared limiter was a
	// contention point even when the line was not throttled at all.
	rate   atomic.Int64
	parent atomic.Pointer[Limiter]
	// weight is the share in percent (0 = 100), see SetWeight.
	weight atomic.Int32
}

// New returns a limiter of bytesPerSecond (0 = unlimited).
func New(bytesPerSecond int64) *Limiter {
	l := &Limiter{}
	l.SetRate(bytesPerSecond)
	return l
}

// SetParent makes this limiter inherit another one: Take and Rate
// forward to the parent until a new parent is set. A nil parent clears the
// inheritance.
func (l *Limiter) SetParent(parent *Limiter) {
	l.parent.Store(parent)
}

// SetLimitKiB sets an explicit limit in KiB/s. A negative value
// inherits the session limiter, zero means unlimited.
func (l *Limiter) SetLimitKiB(kib int64, session *Limiter) {
	switch {
	case kib < 0:
		l.SetParent(session)
	case kib == 0:
		l.SetParent(nil)
		l.SetRate(0)
	default:
		l.SetParent(nil)
		l.SetRate(kib * 1024)
	}
}

// SetRate changes the rate (0 = unlimited). Peers pick it up on their next
// Take; a wait already computed with the old rate is not shortened.
// A change keeps the bucket's balance (clamped to the new burst): the
// automatic speed control moves the rate every few seconds, and refilling the
// bucket each time handed out a fresh burst right when the line was already
// queueing. Only a limit set where there was none starts with a full bucket.
func (l *Limiter) SetRate(bytesPerSecond int64) {
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if bytesPerSecond == l.rate.Load() && (bytesPerSecond == 0) == (l.bucket == nil) {
		return
	}
	if bytesPerSecond == 0 {
		l.bucket = nil
		l.rate.Store(0)
		return
	}
	if l.bucket != nil {
		l.bucket.setRate(bytesPerSecond)
	} else {
		l.bucket = newBucket(bytesPerSecond)
	}
	l.rate.Store(bytesPerSecond)
}

// Rate returns the current rate in bytes per second (0 = unlimited).
func (l *Limiter) Rate() int64 {
	if l == nil {
		return 0
	}
	for cur := l; cur != nil; {
		if parent := cur.parent.Load(); parent != nil {
			cur = parent
			continue
		}
		return cur.rate.Load()
	}
	return 0
}

// Take takes n bytes from the bucket and returns how long the caller must
// wait before using them. It never blocks: the caller sleeps for the
// returned duration. An unlimited limiter (or one inheriting an unlimited
// parent) answers with atomics only, without taking the shared lock.
func (l *Limiter) Take(n int64) time.Duration {
	return l.TakeWeighted(n, 1)
}

// bucket is a classic token bucket: tokens accrue at rate per second up to a
// burst of max, and Takes may borrow into debt, waiting proportionally
// longer. All arithmetic is float64 seconds; rates here stay far below 2^53,
// so token accounting is exact.
type bucket struct {
	mu     sync.Mutex
	rate   float64
	max    float64
	tokens float64
	last   time.Time
}

// burstWindow is how much of a second's quota may leave at once. A burst of a
// whole second filled the modem's queue on a slow capped uplink by itself, and
// the ping of everything else on the line rose with it; 25 ms keeps that queue
// short. At a steady rate the bucket is near empty and every block waits its
// turn anyway, so a short burst changes only what leaves the moment traffic
// resumes, not the rate.
const burstWindow = 25 * time.Millisecond

// minBurst lets one 16 KiB block through without a wait, so a moderate limit
// does not make every block sleep a few milliseconds.
const minBurst = 16 << 10

// burstFor is the bucket size for a rate: burstWindow of it, at least one
// block, and never more than one second's worth.
func burstFor(rate float64) float64 {
	b := rate * burstWindow.Seconds()
	if b < minBurst {
		b = minBurst
	}
	if b > rate {
		b = rate
	}
	return b
}

// newBucket builds a full bucket: rate tokens per second, burst of
// burstFor(rate). A fresh bucket therefore absorbs one short burst.
func newBucket(bytesPerSecond int64) *bucket {
	rate := float64(bytesPerSecond)
	burst := burstFor(rate)
	return &bucket{
		rate:   rate,
		max:    burst,
		tokens: burst,
		last:   time.Now(),
	}
}

// setRate moves the bucket to a new rate, keeping the tokens (or the debt)
// accrued so far and clamping them to the new burst.
func (b *bucket) setRate(bytesPerSecond int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	b.last = now
	b.rate = float64(bytesPerSecond)
	b.max = burstFor(b.rate)
	if b.tokens > b.max {
		b.tokens = b.max
	}
}

// take removes n tokens and reports the wait before they may be spent.
// A take beyond the current balance returns a proportional wait instead of
// blocking; the deficit is repaid by future accrual.
func (b *bucket) take(n int64) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.max {
		b.tokens = b.max
	}
	b.last = now
	b.tokens -= float64(n)
	if b.tokens >= 0 {
		return 0
	}
	return time.Duration(-b.tokens / b.rate * float64(time.Second))
}

// weightScale bounds a weight factor, so a misconfigured weight cannot make a
// consumer wait for minutes or never wait at all.
const (
	minWeightFactor = 0.01
	maxWeightFactor = 100
)

// SetWeight sets this limiter's share relative to the other consumers of the
// same root bucket, in percent (100 = normal, 200 = twice the share, 50 =
// half). A value <= 0 restores 100. The weight only matters when the root has
// a rate and is in debt: an unlimited limiter never waits.
func (l *Limiter) SetWeight(percent int) {
	if l == nil {
		return
	}
	if percent <= 0 {
		percent = 100
	}
	l.weight.Store(int32(percent))
}

// Weight returns the weight in percent (100 when never set).
func (l *Limiter) Weight() int {
	if l == nil {
		return 100
	}
	if w := l.weight.Load(); w > 0 {
		return int(w)
	}
	return 100
}

// TakeWeighted is Take with a share factor: the wait for the bucket's debt
// is divided by the product of factor and the weights of every limiter on the
// way to the root. The debt is shared, so the root rate still caps the sum of
// all consumers and each one gets a share proportional to its weight; with
// every weight equal it is exactly Take.
func (l *Limiter) TakeWeighted(n int64, factor float64) time.Duration {
	if l == nil {
		return 0
	}
	// Find the root first: an unlimited line answers with atomics only, as
	// fast as before weights existed; the weights are read only under a limit.
	root := l
	for parent := root.parent.Load(); parent != nil; parent = root.parent.Load() {
		root = parent
	}
	if root.rate.Load() == 0 {
		return 0
	}
	root.mu.RLock()
	bucket := root.bucket
	root.mu.RUnlock()
	if bucket == nil {
		return 0
	}
	d := bucket.take(n)
	if d <= 0 {
		return d
	}
	if factor <= 0 {
		factor = 1
	}
	for cur := l; cur != nil; cur = cur.parent.Load() {
		if w := cur.weight.Load(); w > 0 && w != 100 {
			factor *= float64(w) / 100
		}
	}
	if factor == 1 {
		return d
	}
	factor = min(max(factor, minWeightFactor), maxWeightFactor)
	return time.Duration(float64(d) / factor)
}

// Upload classes: a peer of a torrent still downloading that sends data back
// is trading; every other peer receiving data is seeding.
const (
	ClassNone int32 = iota
	ClassTrading
	ClassSeeding
)

// UploadClasses splits a limited upload between trading and seeding peers.
// TradingShare is the percent of the upload trading peers get when both
// classes want more than the limit allows; 0 (or 100) turns the split off.
// The counts are the peers in each class across the session, kept by the
// torrents at every unchoke round.
type UploadClasses struct {
	share  atomic.Int32
	counts [3]atomic.Int64
}

// SetTradingShare sets the trading share in percent; values outside 1..99
// turn the split off.
func (c *UploadClasses) SetTradingShare(percent int) {
	if percent < 1 || percent > 99 {
		percent = 0
	}
	c.share.Store(int32(percent))
}

// TradingShare returns the configured share (0 = off).
func (c *UploadClasses) TradingShare() int { return int(c.share.Load()) }

// Add moves the count of a class by delta.
func (c *UploadClasses) Add(class int32, delta int64) {
	if class <= ClassNone || class > ClassSeeding || delta == 0 {
		return
	}
	c.counts[class].Add(delta)
}

// Count returns the peers counted in a class.
func (c *UploadClasses) Count(class int32) int64 {
	if class <= ClassNone || class > ClassSeeding {
		return 0
	}
	return c.counts[class].Load()
}

// Factor is the share factor of one peer of a class: the class share divided
// by the peers in the class, normalised so that a split equal to the head
// count gives 1. With the split off, or only one class present, it is 1.
func (c *UploadClasses) Factor(class int32) float64 {
	if c == nil || class == ClassNone {
		return 1
	}
	share := float64(c.share.Load())
	if share <= 0 {
		return 1
	}
	trading, seeding := float64(c.counts[ClassTrading].Load()), float64(c.counts[ClassSeeding].Load())
	if trading <= 0 || seeding <= 0 {
		return 1
	}
	total := trading + seeding
	if class == ClassTrading {
		return share / 100 * total / trading
	}
	return (100 - share) / 100 * total / seeding
}
