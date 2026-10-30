package torrent

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// A run loop blocked in a data flush longer than the health-check timeout —
// an fdatasync on a slow network share — is slow, not deadlocked: the
// watchdog must keep waiting instead of crashing the daemon.
func TestHealthCheckWaitsOutASlowFlush(t *testing.T) {
	s := &Session{
		config: Config{HealthCheckTimeout: 10 * time.Millisecond},
		closeC: make(chan struct{}),
	}
	tr := &torrent{
		commandC: make(chan func()),
		closeC:   make(chan struct{}),
		log:      logger.New("torrent test"),
		name:     "slow",
	}
	tr.syncingSince.Store(time.Now().UnixNano())

	done := make(chan bool, 1)
	go func() { done <- s.waitRunLoop(tr) }()

	// Several timeouts pass while the flush runs, then the loop answers. The
	// flush marker is never cleared: clearing it opened a window in which the
	// watchdog could read "no flush" in the instant before the command was
	// taken and crash a loop that was alive — the test lost that race on a slow
	// machine. `waitRunLoop` returns on the answer, whatever the marker says.
	time.Sleep(50 * time.Millisecond)
	(<-tr.commandC)()
	if !<-done {
		t.Fatal("waitRunLoop = false, want true once the loop answers")
	}

	// A torrent closing during the wait ends it without a crash.
	tr.syncingSince.Store(time.Now().UnixNano())
	go func() { done <- s.waitRunLoop(tr) }()
	time.Sleep(30 * time.Millisecond)
	close(tr.closeC)
	if <-done {
		t.Fatal("waitRunLoop = true after close, want false")
	}
}
