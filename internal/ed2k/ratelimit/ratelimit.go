// Package ratelimit implements a token bucket used to cap eD2k transfer
// rates. A nil bucket means "no limit", so callers do not need conditionals.
package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"
)

// Bucket allows a configured number of bytes per second with a one-second
// burst. The zero value is not usable; use New.
type Bucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

// New returns a bucket for bytesPerSecond, or nil for "unlimited".
func New(bytesPerSecond int64) *Bucket {
	if bytesPerSecond <= 0 {
		return nil
	}
	rate := float64(bytesPerSecond)
	return &Bucket{rate: rate, burst: rate, tokens: rate, last: time.Now()}
}

// Wait blocks until n bytes fit in the bucket or the context is cancelled.
// It is safe to call on a nil bucket.
func (b *Bucket) Wait(ctx context.Context, n int) error {
	if b == nil || n <= 0 {
		return nil
	}
	remaining := float64(n)
	for remaining > 0 {
		chunk := math.Min(remaining, b.burst)
		if err := b.take(ctx, chunk); err != nil {
			return err
		}
		remaining -= chunk
	}
	return nil
}

func (b *Bucket) take(ctx context.Context, n float64) error {
	for {
		b.mu.Lock()
		now := time.Now()
		if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
			b.tokens = math.Min(b.burst, b.tokens+elapsed*b.rate)
			b.last = now
		}
		if b.tokens >= n {
			b.tokens -= n
			b.mu.Unlock()
			return nil
		}
		missing := n - b.tokens
		b.mu.Unlock()

		wait := time.Duration(missing / b.rate * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
