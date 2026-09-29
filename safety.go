package gextto

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// workerRestartDelay is the pause before a panicked worker is restarted. It is a
// variable so tests can shorten it.
var workerRestartDelay = 5 * time.Second

// safeGo runs fn on its own goroutine and restarts it if it panics. A long-lived
// worker (torrent events, cycles, backups, housekeeping) must never be able to
// kill the process: a programming error there is logged and retried, and the
// daemon keeps serving. Production workers should prefer safeGoLoop so the
// restart delay is also interruptible on shutdown.
func safeGo(name string, fn func()) {
	go safeGoLoop(name, nil, fn)
}

// safeGoLoop runs fn and restarts it after a panic, sleeping workerRestartDelay
// between attempts. When stop is non-nil and is closed, a pending restart sleep
// is interrupted and the loop returns, so a panicked worker cannot delay
// shutdown. The caller owns goroutine lifecycle and WaitGroup accounting, so a
// restart never double-reports completion.
func safeGoLoop(name string, stop <-chan struct{}, fn func()) {
	for {
		panicked := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					logging.Error("background worker panicked; restarting",
						"worker", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				}
			}()
			fn()
		}()
		if !panicked {
			return
		}
		select {
		case <-stop:
			return
		case <-time.After(workerRestartDelay):
		}
	}
}

// guardHandler wraps an HTTP handler so a panic returns a clean 500 response and
// is logged, instead of aborting the connection with a bare stack trace.
func guardHandler(name string, fn HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, s *AppState) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logging.Error("http handler panicked",
					"handler", name, "path", r.URL.Path,
					"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
				// Write a JSON error only when nothing has been committed yet;
				// otherwise the header is already flushed and we just log.
				defer func() { _ = recover() }()
				jsonError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		fn(w, r, s)
	}
}
