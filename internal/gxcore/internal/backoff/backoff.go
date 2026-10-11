// Package backoff implements retry timing for the engine: a BackOff policy, a
// Ticker that emits ticks at the policy's intervals, and a Retry helper.
package backoff

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// Stop tells NextBackOff to stop retrying.
const Stop time.Duration = -1

// BackOff is a retry policy: NextBackOff returns the delay before the next
// attempt, or Stop to give up, and Reset returns the policy to its initial
// state.
type BackOff interface {
	NextBackOff() time.Duration
	Reset()
}

// Default values for ExponentialBackOff.
const (
	DefaultInitialInterval     = 500 * time.Millisecond
	DefaultRandomizationFactor = 0.5
	DefaultMultiplier          = 1.5
	DefaultMaxInterval         = 60 * time.Second
)

// DefaultMaxElapsedTime is the default total retry budget of Retry.
const DefaultMaxElapsedTime = 15 * time.Minute

// ExponentialBackOff grows the delay between attempts by Multiplier, applies a
// random jitter of RandomizationFactor and caps the interval at MaxInterval.
// It is not safe for concurrent use.
type ExponentialBackOff struct {
	InitialInterval     time.Duration
	RandomizationFactor float64
	Multiplier          float64
	MaxInterval         time.Duration

	currentInterval time.Duration
}

// NewExponentialBackOff returns a policy with the default values.
func NewExponentialBackOff() *ExponentialBackOff {
	return &ExponentialBackOff{
		InitialInterval:     DefaultInitialInterval,
		RandomizationFactor: DefaultRandomizationFactor,
		Multiplier:          DefaultMultiplier,
		MaxInterval:         DefaultMaxInterval,
	}
}

// Reset returns the interval to the initial one. It must be called before use.
func (b *ExponentialBackOff) Reset() {
	b.currentInterval = b.InitialInterval
}

// NextBackOff returns the next randomized delay and grows the interval.
func (b *ExponentialBackOff) NextBackOff() time.Duration {
	if b.currentInterval == 0 {
		b.currentInterval = b.InitialInterval
	}
	next := randomizedInterval(b.RandomizationFactor, rand.Float64(), b.currentInterval)
	if float64(b.currentInterval) >= float64(b.MaxInterval)/b.Multiplier {
		b.currentInterval = b.MaxInterval
	} else {
		b.currentInterval = time.Duration(float64(b.currentInterval) * b.Multiplier)
	}
	return next
}

// randomizedInterval returns a value in
// [interval-factor*interval, interval+factor*interval]. With a zero factor it
// returns the interval unchanged.
func randomizedInterval(factor, random float64, interval time.Duration) time.Duration {
	if factor == 0 {
		return interval
	}
	delta := factor * float64(interval)
	return time.Duration(float64(interval) - delta + random*(2*delta+1))
}

// Ticker delivers ticks at the intervals of a BackOff policy on channel C. It is
// guaranteed to tick at least once. C is closed when Stop is called or the
// policy returns Stop.
type Ticker struct {
	C        <-chan time.Time
	c        chan time.Time
	b        BackOff
	stop     chan struct{}
	stopOnce sync.Once
}

// NewTicker returns a Ticker running the given policy.
func NewTicker(b BackOff) *Ticker {
	c := make(chan time.Time)
	t := &Ticker{C: c, c: c, b: b, stop: make(chan struct{})}
	t.b.Reset()
	go t.run()
	return t
}

// Stop turns the ticker off. After Stop, no more ticks are sent.
func (t *Ticker) Stop() {
	t.stopOnce.Do(func() { close(t.stop) })
}

func (t *Ticker) run() {
	defer close(t.c)
	timer := time.NewTimer(0)
	defer timer.Stop()

	// The first tick is immediate.
	select {
	case t.c <- time.Now():
	case <-t.stop:
		return
	}
	for {
		next := t.b.NextBackOff()
		if next == Stop {
			return
		}
		timer.Reset(next)
		select {
		case <-timer.C:
		case <-t.stop:
			return
		}
		select {
		case t.c <- time.Now():
		case <-t.stop:
			return
		}
	}
}

// Operation is the function Retry runs. It is invoked at least once.
type Operation[T any] func() (T, error)

// RetryOption configures Retry.
type RetryOption func(*retryOptions)

type retryOptions struct {
	backOff        BackOff
	maxElapsedTime time.Duration
}

// WithBackOff sets the backoff policy. The default is NewExponentialBackOff.
func WithBackOff(b BackOff) RetryOption {
	return func(o *retryOptions) { o.backOff = b }
}

// WithMaxElapsedTime limits the total wall-clock time spent retrying. Zero
// disables the limit.
func WithMaxElapsedTime(d time.Duration) RetryOption {
	return func(o *retryOptions) { o.maxElapsedTime = d }
}

// Retry runs operation until it succeeds, the context is cancelled, or the
// elapsed-time limit is reached, waiting between attempts according to the
// backoff policy.
func Retry[T any](ctx context.Context, operation Operation[T], opts ...RetryOption) (T, error) {
	args := &retryOptions{backOff: NewExponentialBackOff(), maxElapsedTime: DefaultMaxElapsedTime}
	for _, opt := range opts {
		opt(args)
	}

	startedAt := time.Now()
	args.backOff.Reset()
	for {
		res, err := operation()
		if err == nil {
			return res, nil
		}
		if cerr := ctx.Err(); cerr != nil {
			return res, cerr
		}
		next := args.backOff.NextBackOff()
		if next == Stop {
			return res, err
		}
		if args.maxElapsedTime > 0 && time.Since(startedAt)+next > args.maxElapsedTime {
			return res, err
		}
		timer := time.NewTimer(next)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return res, ctx.Err()
		}
	}
}
