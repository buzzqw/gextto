package torrent

import (
	"os"
	"runtime"
	"time"
)

// checkTorrent watches one torrent's run loop: each interval it posts a
// no-op command the loop must receive to prove liveness. A loop that stays
// silent past the timeout dumps every goroutine stack to a temp file and
// panics — loud by design, so deadlocks surface in testing instead of
// masquerading as stalls in production.
//
// One goroutine per torrent keeps the probes independent (a torrent busy on a
// slow share cannot delay the others) and costs about 4 KB each; the interval
// is a ticker, so a running session allocates no timer per round.
func (s *Session) checkTorrent(t *torrent) {
	if s.config.HealthCheckInterval <= 0 {
		return // disabled: no ticker, no probe
	}
	ticker := time.NewTicker(s.config.HealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !s.waitRunLoop(t) {
				return
			}
		case <-t.closeC:
			return
		case <-s.closeC:
			return
		}
	}
}

// waitRunLoop posts the liveness probe and reports false when the torrent or
// the session closes first. A loop still inside a data flush is slow, not
// stuck — on a network share one fdatasync can outlast the timeout — so the
// wait goes on while the flush lasts, with one warning instead of a crash.
func (s *Session) waitRunLoop(t *torrent) bool {
	warned := false
	for {
		timeout := time.NewTimer(s.config.HealthCheckTimeout)
		select {
		case t.commandC <- func() {}:
			timeout.Stop()
			if warned {
				t.log.Infof("The disk answered again: %q is running normally", t.Name())
			}
			return true
		case <-t.closeC:
			timeout.Stop()
			return false
		case <-s.closeC:
			timeout.Stop()
			return false
		case <-timeout.C:
			since := t.syncingSince.Load()
			if since == 0 {
				// The probe and the timer can become ready in the same
				// instant: the loop may have woken up to take the command just
				// as the timer fired. One non-blocking attempt before
				// declaring it dead, so the select's coin flip can never crash
				// a loop that was answering.
				select {
				case t.commandC <- func() {}:
					return true
				default:
				}
				crash(t.id, "Torrent (id="+t.id+") does not respond.")
			}
			if !warned {
				t.log.Warningf("Writing %q to disk has taken %s: the storage is slow to answer (a busy network share?), waiting for it",
					t.Name(), time.Since(time.Unix(0, since)).Round(time.Second))
				warned = true
			}
		}
	}
}

// crash persists the goroutine stacks for post-mortem and panics.
func crash(torrentID string, msg string) {
	f, err := os.CreateTemp("", "gx-core-crash-dump-"+torrentID+"-*")
	if err == nil {
		msg += " Saving goroutine stacks to: " + f.Name()
		b := make([]byte, 100<<20)
		b = b[:runtime.Stack(b, true)]
		_, _ = f.Write(b)
		_ = f.Close()
	}
	panic(msg)
}

func (t *torrent) crash(msg string) {
	crash(t.id, msg)
}
