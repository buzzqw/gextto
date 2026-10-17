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
// Raising or re-setting the rate refills the burst, like a fresh bucket.
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
	l.bucket = newBucket(bytesPerSecond)
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
	if l == nil {
		return 0
	}
	for cur := l; cur != nil; {
		if parent := cur.parent.Load(); parent != nil {
			cur = parent
			continue
		}
		if cur.rate.Load() == 0 {
			return 0
		}
		cur.mu.RLock()
		bucket := cur.bucket
		cur.mu.RUnlock()
		if bucket == nil {
			return 0
		}
		return bucket.take(n)
	}
	return 0
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

// newBucket builds a full bucket: rate tokens per second, burst of one
// second's worth. A fresh bucket therefore absorbs an immediate burst.
func newBucket(bytesPerSecond int64) *bucket {
	return &bucket{
		rate:   float64(bytesPerSecond),
		max:    float64(bytesPerSecond),
		tokens: float64(bytesPerSecond),
		last:   time.Now(),
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
