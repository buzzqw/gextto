package torrent

import (
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// transientAcceptError is what the kernel returns from Accept when the
// descriptor table is full: net.OpError wrapping the syscall error.
func transientAcceptError() error {
	return &net.OpError{Op: "accept", Err: os.NewSyscallError("accept", syscall.EMFILE)}
}

// timeoutAcceptError reports a timeout, which is transient too.
type timeoutAcceptError struct{}

func (timeoutAcceptError) Error() string { return "accept: i/o timeout" }
func (timeoutAcceptError) Timeout() bool { return true }

// TestAcceptRetryKeepsTheListenerOnTransientErrors locks the rule that a
// transient Accept failure must not stop incoming peers until a restart: the
// loop waits and tries again, while a permanent error still stops it.
func TestAcceptRetryKeepsTheListenerOnTransientErrors(t *testing.T) {
	s := &Session{closeC: make(chan struct{}), log: logger.New("test")}
	delay, warned := acceptRetryInitial, false
	start := time.Now()
	if !s.acceptRetry(transientAcceptError(), &delay, &warned) {
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
	if permanent.acceptRetry(net.ErrClosed, &delay, &warned) {
		t.Fatal("a closed listener must stop the accept loop")
	}
	if !permanent.acceptRetry(&net.OpError{Op: "accept", Err: timeoutAcceptError{}}, &delay, &warned) {
		t.Fatal("an Accept timeout must be retried")
	}
}

// TestAcceptRetryStopsWhenTheSessionCloses: closing the session wins over a
// transient error, so shutdown never sleeps in the backoff.
func TestAcceptRetryStopsWhenTheSessionCloses(t *testing.T) {
	s := &Session{closeC: make(chan struct{}), log: logger.New("test")}
	close(s.closeC)
	delay, warned := acceptRetryInitial, false
	if s.acceptRetry(transientAcceptError(), &delay, &warned) {
		t.Fatal("a closed session must stop the accept loop")
	}
}
