package upstream

// GEXTTO: own support primitives (Event, waitEvents, jitterDuration,
// stoppedFuncTimer), so no third-party transport helper is linked (the
// anacrolix/missinggo and anacrolix/sync modules are gone). Semantics match
// the reference; the monotonic clock is the one Go already keeps in time.Time.

import (
	"math"
	"math/rand"
	"reflect"
	"sync"
	"time"
)

// Event is a boolean flag backed by a channel that is closed when set. Like
// missinggo.Event it is not safe for concurrent use on its own: callers hold
// the socket mutex.
type Event struct {
	ch     chan struct{}
	closed bool
}

// C returns a channel that is closed when the event is set. The channel is
// created lazily, so an event nobody waits on (the common case: the per-send
// acked flag) allocates nothing when it is Set.
func (e *Event) C() <-chan struct{} {
	if e.ch == nil {
		e.ch = make(chan struct{})
		if e.closed {
			close(e.ch)
		}
	}
	return e.ch
}

// Set turns the event on and reports whether this was the first Set. It only
// allocates when a waiter already holds the channel.
func (e *Event) Set() (first bool) {
	if e.closed {
		return false
	}
	e.closed = true
	if e.ch != nil {
		close(e.ch)
	}
	return true
}

// Clear turns the event off.
func (e *Event) Clear() {
	if e.closed {
		e.ch = nil
		e.closed = false
	}
}

// IsSet reports whether the event is on.
func (e *Event) IsSet() bool { return e.closed }

// Wait blocks until the event is set.
func (e *Event) Wait() { <-e.C() }

// LockedChan returns the event's channel while holding lock, so a caller can
// release the lock and then select on the channel. Replaces
// missinggo.Event.LockedChan.
func (e *Event) LockedChan(lock sync.Locker) <-chan struct{} {
	lock.Lock()
	ch := e.C()
	lock.Unlock()
	return ch
}

// SetBool sets the event to b.
func (e *Event) SetBool(b bool) {
	if b {
		e.Set()
	} else {
		e.Clear()
	}
}

// waitEvents blocks until any of evs is set: it releases l (already held) while
// it waits and re-acquires it before returning. Replaces missinggo.WaitEvents.
func waitEvents(l sync.Locker, evs ...*Event) {
	cases := make([]reflect.SelectCase, 0, len(evs))
	for _, ev := range evs {
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ev.C()),
		})
	}
	l.Unlock()
	reflect.Select(cases)
	l.Lock()
}

// jitterDuration returns a random duration in
// [average-plusMinus, average+plusMinus]. Replaces missinggo.JitterDuration.
func jitterDuration(average, plusMinus time.Duration) time.Duration {
	return average - plusMinus + time.Duration(rand.Int63n(2*int64(plusMinus)+1))
}

// stoppedFuncTimer returns a stopped timer that runs f once when reset.
// Replaces missinggo.StoppedFuncTimer.
func stoppedFuncTimer(f func()) *time.Timer {
	t := time.AfterFunc(math.MaxInt64, f)
	if !t.Stop() {
		panic("timer already fired")
	}
	return t
}
