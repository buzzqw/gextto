// Package safego is the one place where the engine turns a panic into a logged
// error instead of the death of the process.
//
// A peer chooses every byte that reaches a connection's read loop, so a bug in
// a parser or in a handler is reachable from outside. Without a recover the
// panic runs up the goroutine that serves that peer and the whole process
// exits: every torrent stops, not just the one whose connection was attacked.
// Other engines learned this the hard way (several remote-crash advisories in
// 2026); here the invariant is that a fault costs one connection, or one
// handler's work on one message, and leaves a line in the log that says what
// happened.
//
// The recover must be called by the deferred function itself — Go's rule, not
// ours — so the helpers take the cleanup to run as a callback instead of
// returning a value a caller would wrap in a closure:
//
//	defer safego.RecoverWith(t.log, "peer message handler", func() { t.closePeer(pe) })
package safego

import (
	"fmt"
	"runtime/debug"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// Invariant marks a panic that is deliberate: the health check found state it
// can no longer trust and the dump plus the exit are the point. Recover lets it
// through, so containing a fault never hides a broken session.
type Invariant struct {
	Reason string
}

// Error makes an Invariant usable as an error where one is expected.
func (i Invariant) Error() string { return i.Reason }

// Recover must be deferred as the first statement of a goroutine or of a
// function that handles data from outside (a peer, a tracker, a torrent's own
// loop). A panic is logged and swallowed: the caller returns, its own defers
// have already closed what it owned, and the session goes on.
func Recover(component string) bool {
	value := recover()
	return contain(logger.New(component), component, value, nil)
}

// RecoverWith is Recover for a caller that already has a logger — a torrent
// handler, whose logger names the torrent the fault happened in — and that
// must also drop whatever caused the fault (onFault may be nil).
func RecoverWith(log logger.Logger, component string, onFault func()) bool {
	value := recover()
	return contain(log, component, value, onFault)
}

// contain logs the fault and reports whether there was one. An Invariant is
// deliberately re-raised.
func contain(log logger.Logger, component string, value any, onFault func()) bool {
	if value == nil {
		return false
	}
	if invariant, ok := value.(Invariant); ok {
		panic(invariant)
	}
	log.Errorln(fmt.Sprintf("The %s stopped after an unexpected error: %v", component, value))
	// The stack is for whoever fixes the bug, not for the person running the
	// daemon: at debug level, next to the sentence above.
	log.Debugln(string(debug.Stack()))
	if onFault != nil {
		onFault()
	}
	return true
}

// Go runs fn in a new goroutine with Recover in front of it.
func Go(component string, fn func()) {
	go func() {
		defer Recover(component)
		fn()
	}()
}
