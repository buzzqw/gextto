package semaphore

import (
	"context"
	"sync/atomic"

	"golang.org/x/sync/semaphore"
)

// Semaphore caps concurrent access to a shared resource (piece write slots,
// read loads). It wraps x/sync's weighted semaphore and additionally tracks
// how many goroutines are queued versus holding a slot, for diagnostics.
type Semaphore struct {
	sem     *semaphore.Weighted
	waiting atomic.Int32
	active  atomic.Int32
}

// New builds a counting semaphore with n slots.
func New(n int) *Semaphore {
	return &Semaphore{sem: semaphore.NewWeighted(int64(n))}
}

// Waiting reports how many goroutines are currently blocked in Wait.
func (s *Semaphore) Waiting() int {
	return int(s.waiting.Load())
}

// Len reports how many slots are currently held.
func (s *Semaphore) Len() int {
	return int(s.active.Load())
}

// Wait blocks until a slot is free, then holds it.
func (s *Semaphore) Wait() {
	_ = s.WaitContext(context.Background())
}

// WaitContext blocks until a slot is free or ctx is cancelled. A cancelled
// waiter holds no slot and must not be paired with Signal.
func (s *Semaphore) WaitContext(ctx context.Context) error {
	s.waiting.Add(1)
	err := s.sem.Acquire(ctx, 1)
	s.waiting.Add(-1)
	if err != nil {
		return err
	}
	s.active.Add(1)
	return nil
}

// Signal frees one held slot, waking a random waiter.
func (s *Semaphore) Signal() {
	s.sem.Release(1)
	s.active.Add(-1)
}
