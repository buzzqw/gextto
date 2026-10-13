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
func (s *Session) checkTorrent(t *torrent) {
	for {
		select {
		case <-time.After(s.config.HealthCheckInterval):
			timeout := time.NewTimer(s.config.HealthCheckTimeout)
			select {
			case t.commandC <- func() {}:
				timeout.Stop()
			case <-t.closeC:
				return
			case <-timeout.C:
				crash(t.id, "Torrent (id="+t.id+") does not respond.")
			}
		case <-t.closeC:
			return
		case <-s.closeC:
			return
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
