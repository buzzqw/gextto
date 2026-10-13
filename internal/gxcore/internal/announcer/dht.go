package announcer

import (
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// DHTAnnouncer re-announces the torrent to the DHT on a schedule: eagerly
// (every minInterval) while the swarm is hungry for peers, lazily (every
// interval) otherwise. NeedMorePeers flips between the two paces.
type DHTAnnouncer struct {
	lastAnnounce   time.Time
	needMorePeers  bool
	needMorePeersC chan bool
	closeC         chan struct{}
	doneC          chan struct{}
}

// NewDHTAnnouncer builds an announcer starting in hungry mode, so the first
// peers arrive as fast as the minimum interval allows.
func NewDHTAnnouncer() *DHTAnnouncer {
	return &DHTAnnouncer{
		needMorePeersC: make(chan bool),
		closeC:         make(chan struct{}),
		doneC:          make(chan struct{}),
		needMorePeers:  true,
	}
}

// Close stops the announcer and waits for the Run loop to exit.
func (a *DHTAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

// NeedMorePeers switches the announce pace. It is dropped silently when the
// announcer is already gone.
func (a *DHTAnnouncer) NeedMorePeers(val bool) {
	select {
	case a.needMorePeersC <- val:
	case <-a.doneC:
	}
}

// Run executes the announce loop; call it as a goroutine. The first announce
// fires immediately, then the timer follows the current pace.
func (a *DHTAnnouncer) Run(announceFunc func(), interval, minInterval time.Duration, l logger.Logger) {
	defer close(a.doneC)

	timer := time.NewTimer(minInterval)
	defer timer.Stop()

	a.announceAndReschedule(timer, announceFunc, interval, minInterval)
	for {
		select {
		case <-timer.C:
			a.announceAndReschedule(timer, announceFunc, interval, minInterval)
		case a.needMorePeers = <-a.needMorePeersC:
			a.reschedule(timer, interval, minInterval)
		case <-a.closeC:
			return
		}
	}
}

// announceAndReschedule runs one announce and rearms the timer for the next.
func (a *DHTAnnouncer) announceAndReschedule(timer *time.Timer, announceFunc func(), interval, minInterval time.Duration) {
	announceFunc()
	a.lastAnnounce = time.Now()
	a.reschedule(timer, interval, minInterval)
}

// reschedule arms the timer for one pace step after the last announce. A
// non-positive duration fires immediately, which is the correct catch-up
// when an announce is overdue.
func (a *DHTAnnouncer) reschedule(timer *time.Timer, interval, minInterval time.Duration) {
	pace := interval
	if a.needMorePeers {
		pace = minInterval
	}
	timer.Reset(time.Until(a.lastAnnounce.Add(pace)))
}
