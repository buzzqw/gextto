package gextto

// line_usage.go tells whether someone else in the house is using the line.
// The latency guard (latency_guard.go) must only yield to other users: when
// nobody else is on the line, the queue Gextto's own download builds hurts no
// one and the whole line is Gextto's. The router's UPnP IGD counters give the
// total WAN traffic; the traffic of the others is that total minus Gextto's,
// once the protocol overhead (TCP/IP headers, DHT, trackers) is accounted for.

import (
	"sync"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
)

const (
	// lineUsageReadEvery is how often the router counters are read.
	lineUsageReadEvery = 4 * time.Second
	// lineUsageRediscover is the wait before looking for the router again
	// after it was not found or stopped answering.
	lineUsageRediscover = 5 * time.Minute
	// lineUsageOverheadWindow is how long the lowest overhead ratio is kept:
	// the lowest ratio seen is a moment when only Gextto was on the line.
	lineUsageOverheadWindow = time.Hour
	// lineUsageOverheadMinBytes: only rates above this teach the overhead.
	lineUsageOverheadMinBytes = 1 << 20
	// lineUsageDefaultOverhead is assumed until a ratio has been learned;
	// lineUsageMaxOverhead bounds what can be learned.
	lineUsageDefaultOverhead = 0.05
	lineUsageMaxOverhead     = 0.15
	// lineUsageOthersMinBytes and lineUsageOthersShare decide that someone
	// else is on the line: their traffic is above both.
	lineUsageOthersMinBytes = 48 << 10
	lineUsageOthersShare    = 0.02
	// lineUsageVotes: the others count as present in 2 of the last 3 reads.
	lineUsageVotes = 3
)

// wanCounters reads the router's total WAN bytes.
type wanCounters interface {
	GetTotalBytesReceived() (uint64, error)
	GetTotalBytesSent() (uint64, error)
}

// discoverWANCounters finds the router's WANCommonInterfaceConfig service.
var discoverWANCounters = func() wanCounters {
	clients, _, err := internetgateway2.NewWANCommonInterfaceConfig1Clients()
	if err != nil {
		return nil
	}
	for _, client := range clients {
		if _, err := client.GetTotalBytesReceived(); err == nil {
			return client
		}
	}
	return nil
}

// overheadTracker keeps the lowest WAN/own ratio of the last window.
type overheadTracker struct {
	samples []overheadSample
}

type overheadSample struct {
	at    time.Time
	ratio float64
}

func (o *overheadTracker) add(now time.Time, wan, own float64) {
	if own >= lineUsageOverheadMinBytes && wan >= own {
		ratio := wan/own - 1
		if ratio > lineUsageMaxOverhead {
			ratio = lineUsageMaxOverhead
		}
		o.samples = append(o.samples, overheadSample{at: now, ratio: ratio})
	}
	cut := now.Add(-lineUsageOverheadWindow)
	i := 0
	for i < len(o.samples) && o.samples[i].at.Before(cut) {
		i++
	}
	o.samples = o.samples[i:]
}

func (o *overheadTracker) ratio() float64 {
	if len(o.samples) == 0 {
		return lineUsageDefaultOverhead
	}
	low := o.samples[0].ratio
	for _, s := range o.samples[1:] {
		if s.ratio < low {
			low = s.ratio
		}
	}
	return low
}

// othersOn reports whether the others' traffic is significant next to own.
func othersOn(wan, own, overhead float64) bool {
	others := wan - own*(1+overhead)
	return others >= lineUsageOthersMinBytes && others >= own*lineUsageOthersShare
}

// lineUsage is the "someone else is on the line" detector. The zero value is
// ready to use.
type lineUsage struct {
	mu          sync.Mutex
	counters    wanCounters
	discoverAt  time.Time
	lastRx      uint64
	lastTx      uint64
	lastAt      time.Time
	ownDown     float64 // own bytes accumulated since lastAt (rate × seconds)
	ownUp       float64
	ownLastAt   time.Time
	downHead    overheadTracker
	upHead      overheadTracker
	votes       []bool
	known       bool
	othersDown  float64
	othersUp    float64
	lastPresent bool
}

// counterDelta is the growth of a 32- or 64-bit counter that may have
// wrapped since the previous read.
func counterDelta(previous, current uint64) uint64 {
	if current >= previous {
		return current - previous
	}
	if previous <= 1<<32 {
		return (1<<32 - previous) + current
	}
	return 0 // a reset of a 64-bit counter: no measurement
}

// observe accumulates Gextto's own rates (bytes/s) between two reads.
func (l *lineUsage) observe(now time.Time, ownDown, ownUp uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.ownLastAt.IsZero() {
		seconds := now.Sub(l.ownLastAt).Seconds()
		l.ownDown += float64(ownDown) * seconds
		l.ownUp += float64(ownUp) * seconds
	}
	l.ownLastAt = now
}

// read samples the router when due and returns whether the others are on the
// line and whether that is known (a router that answers).
func (l *lineUsage) read(now time.Time) (present, known bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counters == nil {
		if now.Before(l.discoverAt) {
			return false, false
		}
		l.discoverAt = now.Add(lineUsageRediscover)
		l.mu.Unlock()
		counters := discoverWANCounters()
		l.mu.Lock()
		if counters == nil {
			l.known = false
			return false, false
		}
		l.counters = counters
		l.lastAt = time.Time{}
	}
	if !l.lastAt.IsZero() && now.Sub(l.lastAt) < lineUsageReadEvery {
		return l.lastPresent, l.known
	}
	rx, errRx := l.counters.GetTotalBytesReceived()
	tx, errTx := l.counters.GetTotalBytesSent()
	if errRx != nil || errTx != nil {
		l.counters, l.known = nil, false
		l.discoverAt = now.Add(lineUsageRediscover)
		return false, false
	}
	if l.lastAt.IsZero() {
		l.lastRx, l.lastTx, l.lastAt = rx, tx, now
		l.ownDown, l.ownUp = 0, 0
		return false, l.known
	}
	seconds := now.Sub(l.lastAt).Seconds()
	wanDown := float64(counterDelta(l.lastRx, rx)) / seconds
	wanUp := float64(counterDelta(l.lastTx, tx)) / seconds
	ownDown, ownUp := l.ownDown/seconds, l.ownUp/seconds
	l.lastRx, l.lastTx, l.lastAt = rx, tx, now
	l.ownDown, l.ownUp = 0, 0
	l.downHead.add(now, wanDown, ownDown)
	l.upHead.add(now, wanUp, ownUp)
	l.othersDown = max(wanDown-ownDown*(1+l.downHead.ratio()), 0)
	l.othersUp = max(wanUp-ownUp*(1+l.upHead.ratio()), 0)
	vote := othersOn(wanDown, ownDown, l.downHead.ratio()) || othersOn(wanUp, ownUp, l.upHead.ratio())
	l.votes = append(l.votes, vote)
	if len(l.votes) > lineUsageVotes {
		l.votes = l.votes[len(l.votes)-lineUsageVotes:]
	}
	yes := 0
	for _, v := range l.votes {
		if v {
			yes++
		}
	}
	l.known = true
	l.lastPresent = yes*2 > len(l.votes)
	return l.lastPresent, true
}

// others reports the last estimate of the others' traffic (bytes/s).
func (l *lineUsage) others() (down, up float64, known bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.othersDown, l.othersUp, l.known
}

// reset forgets the counters' baseline and the votes (Auto ended).
func (l *lineUsage) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastAt, l.ownLastAt = time.Time{}, time.Time{}
	l.ownDown, l.ownUp = 0, 0
	l.votes = nil
	l.lastPresent = false
}
