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
// slow share cannot delay the others) and costs about 4 KB each; the round is
// one timer reset in place, so it allocates nothing. A stopped torrent does no
// I/O and is probed every stoppedProbeFactor intervals: a long queue at rest
// wakes a few times a minute instead of ten times a second.
func (s *Session) checkTorrent(t *torrent) {
	interval := s.config.HealthCheckInterval
	if interval <= 0 {
		return // disabled: no timer, no probe
	}
	timer := time.NewTimer(probeDelay(interval, t.loopStopped.Load()))
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			if !answeredWithin(t.answeredAt.Load(), time.Now(), interval) {
				if !s.waitRunLoop(t) {
					return
				}
			}
			// A poller that reached the loop a moment ago already proved it alive.
			timer.Reset(probeDelay(interval, t.loopStopped.Load()))
		case <-t.closeC:
			return
		case <-s.closeC:
			return
		}
	}
}

// stoppedProbeFactor stretches the probe interval of a stopped torrent.
const stoppedProbeFactor = 6

// probeDelay is the wait before the next liveness probe.
func probeDelay(interval time.Duration, stopped bool) time.Duration {
	if stopped {
		return interval * stoppedProbeFactor
	}
	return interval
}

// answeredWithin reports whether a loop that last took a command at (UnixNano,
// 0 for never) did so less than window before now.
func answeredWithin(at int64, now time.Time, window time.Duration) bool {
	return at != 0 && now.Sub(time.Unix(0, at)) < window
}

// livenessProbe is the command the health check posts: it only proves the loop
// answers, so it does not count as a change (see Torrent.Changes).
func (t *torrent) livenessProbe() { t.quietCommand = true }

// waitRunLoop posts the liveness probe and reports false when the torrent or
// the session closes first. A loop still inside a data flush is slow, not
// stuck — on a network share one fdatasync can outlast the timeout — so the
// wait goes on while the flush lasts, with one warning instead of a crash.
func (s *Session) waitRunLoop(t *torrent) bool {
	warned := false
	for {
		timeout := time.NewTimer(s.config.HealthCheckTimeout)
		select {
		case t.commandC <- t.livenessProbe:
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
				case t.commandC <- t.livenessProbe:
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
