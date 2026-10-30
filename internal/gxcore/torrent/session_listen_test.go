package torrent

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// temporaryAcceptError mimics a net.Error that reports itself as temporary,
// like the EMFILE the kernel returns when the descriptor table is full.
type temporaryAcceptError struct{}

func (temporaryAcceptError) Error() string   { return "too many open files" }
func (temporaryAcceptError) Timeout() bool   { return false }
func (temporaryAcceptError) Temporary() bool { return true }

// TestAcceptRetryKeepsTheListenerOnTransientErrors locks the rule that a
// transient Accept failure must not stop incoming peers until a restart: the
// loop waits and tries again, while a permanent error still stops it.
func TestAcceptRetryKeepsTheListenerOnTransientErrors(t *testing.T) {
	s := &Session{closeC: make(chan struct{}), log: logger.New("test")}
	delay, warned := acceptRetryInitial, false
	start := time.Now()
	if !s.acceptRetry(&net.OpError{Op: "accept", Err: temporaryAcceptError{}}, &delay, &warned) {
		t.Fatal("a temporary Accept error must be retried")
	}
	if !warned {
		t.Fatal("the first transient failure must be reported")
	}
	if elapsed := time.Since(start); elapsed < acceptRetryInitial {
		t.Fatalf("retry waited %v, want at least %v", elapsed, acceptRetryInitial)
	}
	if delay <= acceptRetryInitial {
		t.Fatalf("retry delay = %v, want it to grow", delay)
	}

	// A successful accept resets the backoff; the loop does that, so check the
	// permanent path on a fresh session.
	permanent := &Session{closeC: make(chan struct{}), log: logger.New("test")}
	delay, warned = acceptRetryInitial, false
	if permanent.acceptRetry(errors.New("listener is gone"), &delay, &warned) {
		t.Fatal("a permanent Accept error must stop the listener")
	}
}

// TestAcceptRetryStopsWhenTheSessionCloses: closing the session wins over a
// transient error, so shutdown never sleeps in the backoff.
func TestAcceptRetryStopsWhenTheSessionCloses(t *testing.T) {
	s := &Session{closeC: make(chan struct{}), log: logger.New("test")}
	close(s.closeC)
	delay, warned := acceptRetryInitial, false
	if s.acceptRetry(&net.OpError{Op: "accept", Err: temporaryAcceptError{}}, &delay, &warned) {
		t.Fatal("a closed session must stop the accept loop")
	}
}
