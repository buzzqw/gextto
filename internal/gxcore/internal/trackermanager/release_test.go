package trackermanager

import (
	"testing"
	"time"
)

// TestReleaseForgetsAWebSocketTracker pins that a hot-swapped tracker does not
// keep a socket and its registration for the daemon's lifetime: the manager
// must forget it, and releasing it twice (or releasing a scheme that shares a
// transport) must stay harmless.
func TestReleaseForgetsAWebSocketTracker(t *testing.T) {
	m := New(nil, time.Second, false)
	defer m.Close()

	ws, err := m.Get("wss://tracker.example/announce", time.Second, "test", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(m.wsTrackers); got != 1 {
		t.Fatalf("the manager tracks %d WebSocket trackers, want 1", got)
	}
	m.Release(ws)
	if got := len(m.wsTrackers); got != 0 {
		t.Fatalf("the manager still tracks %d WebSocket trackers", got)
	}
	m.Release(ws) // idempotent
	m.Release(nil)

	http, err := m.Get("http://tracker.example/announce", time.Second, "test", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	m.Release(http)
	if got := len(m.wsTrackers); got != 0 {
		t.Fatalf("releasing an HTTP tracker changed the WebSocket list to %d", got)
	}
}
