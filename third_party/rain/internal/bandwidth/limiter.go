// Package bandwidth provides a rate limiter whose rate can change while peers
// hold it (gextto fork). rain used *ratelimit.Bucket directly, fixed when the
// session was created, so a new speed limit needed a session restart.
package bandwidth

import (
	"sync"
	"time"

	"github.com/juju/ratelimit"
)

// Limiter is a token bucket shared by all peers of a session. A nil Limiter or
// a zero rate means unlimited.
type Limiter struct {
	mu     sync.RWMutex
	bucket *ratelimit.Bucket
	rate   int64
}

// New returns a limiter of bytesPerSecond (0 = unlimited).
func New(bytesPerSecond int64) *Limiter {
	l := &Limiter{}
	l.SetRate(bytesPerSecond)
	return l
}

// SetRate changes the rate (0 = unlimited). Peers pick it up on their next
// Take; a wait already computed with the old rate is not shortened.
func (l *Limiter) SetRate(bytesPerSecond int64) {
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if bytesPerSecond == l.rate && (bytesPerSecond == 0) == (l.bucket == nil) {
		return
	}
	l.rate = bytesPerSecond
	if bytesPerSecond == 0 {
		l.bucket = nil
		return
	}
	l.bucket = ratelimit.NewBucketWithRate(float64(bytesPerSecond), bytesPerSecond)
}

// Rate returns the current rate in bytes per second (0 = unlimited).
func (l *Limiter) Rate() int64 {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.rate
}

// Take takes n bytes from the bucket and returns how long the caller must
// wait before using them.
func (l *Limiter) Take(n int64) time.Duration {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	bucket := l.bucket
	l.mu.RUnlock()
	if bucket == nil {
		return 0
	}
	return bucket.Take(n)
}
