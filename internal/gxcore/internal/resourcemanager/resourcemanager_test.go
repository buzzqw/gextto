package resourcemanager

import (
	"testing"
	"time"
)

// TestIsolatedOverLimitRequestIsGranted is the liveness guarantee: a request
// bigger than the entire limit must still be served when nothing is
// outstanding, so a write buffer smaller than one piece cannot deadlock a
// download.
func TestIsolatedOverLimitRequestIsGranted(t *testing.T) {
	m := New[int](10)
	defer m.Close()

	if !m.Request("t", 1, 20, make(chan int, 1), nil) {
		t.Fatal("an isolated over-limit request must be granted")
	}
}

// TestSecondRequestWaitsForBudget keeps the limit meaningful: while one request
// is outstanding, another that does not fit waits, then is served when the
// first is released.
func TestSecondRequestWaitsForBudget(t *testing.T) {
	m := New[int](10)
	defer m.Close()

	if !m.Request("t", 1, 10, make(chan int, 1), nil) {
		t.Fatal("a request that fits must be granted")
	}
	notify := make(chan int, 1)
	if m.Request("t", 2, 6, notify, nil) {
		t.Fatal("a second request must wait while the budget is taken")
	}
	m.Release(10)
	select {
	case got := <-notify:
		if got != 2 {
			t.Fatalf("notified request = %d, want 2", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the queued request")
	}
	m.Release(6)
}

// TestOverLimitThenQueuedIsServed checks that after an overdraft is released,
// the next queued request gets its turn.
func TestOverLimitThenQueuedIsServed(t *testing.T) {
	m := New[int](10)
	defer m.Close()

	if !m.Request("t", 1, 20, make(chan int, 1), nil) {
		t.Fatal("an isolated over-limit request must be granted")
	}
	notify := make(chan int, 1)
	if m.Request("t", 2, 5, notify, nil) {
		t.Fatal("a second request must wait while one is outstanding")
	}
	m.Release(20)
	select {
	case got := <-notify:
		if got != 2 {
			t.Fatalf("notified request = %d, want 2", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the queued request")
	}
	m.Release(5)
}
