package dht

import (
	"testing"
	"time"
)

// TestPingSlowlyStopsWithoutReader locks the shutdown fix: when the run loop is
// gone (stop closed) and nobody reads pingRequest, pingSlowly must return
// instead of blocking on the send (which made DHT.Stop hang forever).
func TestPingSlowlyStopsWithoutReader(t *testing.T) {
	pingRequest := make(chan *remoteNode) // never read
	stop := make(chan bool)
	close(stop)

	done := make(chan struct{})
	go func() {
		pingSlowly(pingRequest, []*remoteNode{{}, {}}, time.Minute, stop)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pingSlowly did not return after stop")
	}
}
