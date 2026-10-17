package transport

import (
	"sync"
	"testing"
	"time"
)

// TestWaitEventsWakesUp pins the plain-select path used by every blocking uTP
// Read and Write: it returns when any of the events is set and leaves the
// locker held.
func TestWaitEventsWakesUp(t *testing.T) {
	var events [2]Event
	var mu sync.Mutex
	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		mu.Lock()
		// Event is not safe on its own: the channels are created and the events
		// set under the same mutex waitEvents releases.
		for i := range events {
			_ = events[i].C()
		}
		close(ready)
		waitEvents(&mu, &events[0], &events[1])
		held := events[1].IsSet()
		mu.Unlock()
		if !held {
			t.Error("waitEvents returned without the set event")
		}
		close(done)
	}()
	<-ready
	mu.Lock()
	events[1].Set()
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitEvents did not return")
	}
}

// TestWaitEventsAcceptsMoreThanFive: the five static slots cover every current
// caller; a longer list falls back to reflect.Select instead of panicking.
func TestWaitEventsAcceptsMoreThanFive(t *testing.T) {
	var events [6]Event
	var mu sync.Mutex
	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		mu.Lock()
		for i := range events {
			_ = events[i].C()
		}
		close(ready)
		waitEvents(&mu, &events[0], &events[1], &events[2], &events[3], &events[4], &events[5])
		mu.Unlock()
		close(done)
	}()
	<-ready
	mu.Lock()
	events[5].Set()
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitEvents did not return on the sixth event")
	}
}
