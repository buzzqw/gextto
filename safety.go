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

// safeGoLoop runs fn and restarts it after a panic, sleeping a growing delay
// (starting at workerRestartDelay) between attempts. When stop is non-nil and
// is closed, a pending restart sleep is interrupted and the loop returns, so a
// panicked worker cannot delay shutdown. The caller owns goroutine lifecycle and WaitGroup accounting, so a
// restart never double-reports completion.
func safeGoLoop(name string, stop <-chan struct{}, fn func()) {
	consecutive := 0
	for {
		panicked := false
		started := time.Now()
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
		// A worker that panics again right after a restart (a poisoned torrent,
		// a corrupt row) must not spin in a tight restart loop: the delay doubles
		// for each consecutive quick panic, up to workerRestartMaxDelay. A run
		// that lasted longer than workerStableRun resets the count.
		if time.Since(started) >= workerStableRun {
			consecutive = 0
		}
		delay := workerRestartBackoff(consecutive)
		consecutive++
		if consecutive > 1 {
			logging.Warn("background worker keeps panicking; restart delayed",
				"worker", name, "consecutive_panics", consecutive, "delay", delay.String())
		}
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}
	}
}

// workerRestartMaxDelay caps the restart backoff of a repeatedly panicking
// worker; workerStableRun is the run time after which a worker counts as healthy.
const (
	workerRestartMaxDelay = 5 * time.Minute
	workerStableRun       = 10 * time.Minute
)

// workerRestartBackoff returns the delay before restart number n+1 (n
// consecutive quick panics so far): workerRestartDelay doubled n times, capped.
func workerRestartBackoff(n int) time.Duration {
	delay := workerRestartDelay
	for i := 0; i < n && delay < workerRestartMaxDelay; i++ {
		delay *= 2
	}
	if delay > workerRestartMaxDelay {
		delay = workerRestartMaxDelay
	}
	return delay
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

// recoverGoroutine stops a panic in a fan-out goroutine from terminating the
// whole daemon. In Go an unrecovered panic in any goroutine kills the process,
// and only the top-level workers run under safeGoLoop. Use it as the first
// deferred call of the goroutine, so the goroutine's own deferred cleanup
// (WaitGroup.Done, semaphore release) still runs before it.
func recoverGoroutine(name string) {
	if r := recover(); r != nil {
		logging.Error("goroutine panicked; recovered",
			"goroutine", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
	}
}
