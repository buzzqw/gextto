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
// The background context means a waiter is never cancelled: slots are always
// eventually released by the piece pipeline completing or failing.
func (s *Semaphore) Wait() {
	s.waiting.Add(1)
	_ = s.sem.Acquire(context.Background(), 1)
	s.waiting.Add(-1)
	s.active.Add(1)
}

// Signal frees one held slot, waking a random waiter.
func (s *Semaphore) Signal() {
	s.sem.Release(1)
	s.active.Add(-1)
}
