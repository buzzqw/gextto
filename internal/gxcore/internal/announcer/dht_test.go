package announcer

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// While the torrent needs more peers the DHT must retry at the (short) minimum
// interval, not wait for the full announce interval: a fresh DHT routing table
// takes tens of seconds to fill, so the cold-start retry is what makes a
// tracker-less magnet converge.
func TestDHTAnnouncerRetriesAtMinInterval(t *testing.T) {
	a := NewDHTAnnouncer()
	var calls atomic.Int32
	go a.Run(func() { calls.Add(1) }, time.Hour, 10*time.Millisecond, logger.New("test"))
	defer a.Close()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) && calls.Load() < 4 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got < 4 {
		t.Fatalf("calls=%d in 300ms at minInterval=10ms, want >=4", got)
	}

	// Once peers are enough, wait for the long interval.
	a.NeedMorePeers(false)
	time.Sleep(30 * time.Millisecond)
	before := calls.Load()
	time.Sleep(80 * time.Millisecond)
	if after := calls.Load(); after != before {
		t.Fatalf("announced %d times after NeedMorePeers(false), want 0", after-before)
	}
}
