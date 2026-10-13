package announcer

import (
	"context"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// StopAnnouncer delivers the final stopped announce to every tracker when a
// torrent leaves the session. Run it, then wait on Done (or Close it to give
// up waiting); the announce itself is bounded by timeout either way.
type StopAnnouncer struct {
	log      logger.Logger
	timeout  time.Duration
	trackers []tracker.Tracker
	torrent  tracker.Torrent
	resultC  chan struct{}
	closeC   chan struct{}
	doneC    chan struct{}
}

// NewStopAnnouncer builds the stopped-event announcer. resultC receives one
// value once every tracker has been contacted (or the wait is abandoned).
func NewStopAnnouncer(trackers []tracker.Tracker, tra tracker.Torrent, timeout time.Duration, resultC chan struct{}, l logger.Logger) *StopAnnouncer {
	return &StopAnnouncer{
		log:      l,
		timeout:  timeout,
		trackers: trackers,
		torrent:  tra,
		resultC:  resultC,
		closeC:   make(chan struct{}),
		doneC:    make(chan struct{}),
	}
}

// Close abandons the wait for the trackers' replies.
func (a *StopAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

// Done closes when the announcer has finished, one way or the other.
func (a *StopAnnouncer) Done() <-chan struct{} {
	return a.doneC
}

// Run contacts every tracker in parallel and then signals resultC, unless
// Close wins the race first.
func (a *StopAnnouncer) Run() {
	defer close(a.doneC)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(a.timeout))
	defer cancel()
	// If Run returns early (Close), release the context so the in-flight
	// announces do not linger until the deadline.
	go func() {
		select {
		case <-ctx.Done():
		case <-a.closeC:
			cancel()
		}
	}()

	var pending sync.WaitGroup
	for _, trk := range a.trackers {
		pending.Add(1)
		go func(trk tracker.Tracker) {
			defer pending.Done()
			req := tracker.AnnounceRequest{
				Torrent: a.torrent,
				Event:   tracker.EventStopped,
			}
			_, _ = trk.Announce(ctx, req)
		}(trk)
	}
	pending.Wait()

	select {
	case a.resultC <- struct{}{}:
	case <-a.closeC:
	}
}
