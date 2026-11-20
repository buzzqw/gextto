package safego

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// TestRecoverContainsAPanic pins the reason this package exists: a fault in a
// function that handles a stranger's data ends that function, not the process.
func TestRecoverContainsAPanic(t *testing.T) {
	func() {
		defer Recover("test handler")
		panic("boom")
	}()
}

// TestRecoverReraisesAnInvariant pins the other half: containing a fault must
// never hide a broken session, so the health check's panic goes through.
func TestRecoverReraisesAnInvariant(t *testing.T) {
	defer func() {
		value := recover()
		if value == nil {
			t.Fatal("the invariant panic was swallowed")
		}
		if _, ok := value.(Invariant); !ok {
			t.Fatalf("re-raised %T, want Invariant", value)
		}
	}()
	func() {
		defer RecoverWith(logger.New("test"), "test handler", nil)
		panic(Invariant{Reason: "invalid allocator"})
	}()
}

// TestRecoverWithRunsTheCleanup checks that the fault drops what caused it:
// the handler that panicked must not leave its connection open.
func TestRecoverWithRunsTheCleanup(t *testing.T) {
	closed := false
	func() {
		defer RecoverWith(logger.New("test"), "peer message handler", func() { closed = true })
		panic("boom")
	}()
	if !closed {
		t.Fatal("the cleanup did not run after a contained fault")
	}
}

// TestRecoverWithoutAPanicDoesNothing pins that a normal return is untouched.
func TestRecoverWithoutAPanicDoesNothing(t *testing.T) {
	closed := false
	func() {
		defer RecoverWith(logger.New("test"), "peer message handler", func() { closed = true })
	}()
	if closed {
		t.Fatal("the cleanup ran without a fault")
	}
}

// TestGoRunsAndContains checks the spawn wrapper: fn runs, and a panic inside
// it does not take the process down.
func TestGoRunsAndContains(t *testing.T) {
	done := make(chan struct{})
	Go("test goroutine", func() {
		defer close(done)
		panic("boom")
	})
	<-done
}
