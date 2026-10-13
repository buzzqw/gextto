package resourcemanager

import "math/rand/v2"

// ResourceManager rations a finite budget (piece-buffer bytes) across
// requesters keyed by name. Unservable requests queue and are notified on
// room; an isolated over-limit request is always granted, so a limit smaller
// than one request can never wedge every requester forever.
type ResourceManager[T any] struct {
	limit     int64
	available int64
	objects   int
	requests  map[string][]request[T]
	requestC  chan request[T]
	releaseC  chan int64
	limitC    chan int64 // SetLimit
	statsC    chan chan Stats
	closeC    chan struct{}
	doneC     chan struct{}
}

// request is one queued or notified grant.
type request[T any] struct {
	key     string
	data    T
	n       int64
	notifyC chan T
	cancelC chan struct{}
	doneC   chan bool
}

// Stats snapshots the budget: allocated size/objects and queued keys.
type Stats struct {
	AllocatedSize    int64
	AllocatedObjects int
	PendingKeys      int
}

// New starts a manager over limit units.
func New[T any](limit int64) *ResourceManager[T] {
	m := &ResourceManager[T]{
		limit:     limit,
		available: limit,
		requests:  make(map[string][]request[T]),
		requestC:  make(chan request[T]),
		releaseC:  make(chan int64),
		limitC:    make(chan int64),
		statsC:    make(chan chan Stats),
		closeC:    make(chan struct{}),
		doneC:     make(chan struct{}),
	}
	go m.run()
	return m
}

// Close stops the manager and waits for the loop to exit.
func (m *ResourceManager[T]) Close() {
	close(m.closeC)
	<-m.doneC
}

// Stats queries the current budget. Zero after Close.
func (m *ResourceManager[T]) Stats() Stats {
	var stats Stats
	ch := make(chan Stats)
	select {
	case m.statsC <- ch:
		select {
		case stats = <-ch:
		case <-m.closeC:
		}
	case <-m.closeC:
	}
	return stats
}

// Request asks n units for key, delivering data on notifyC when granted.
// Release must follow every grant. Negative asks are refused outright;
// cancellation while queued reports ungranted.
func (m *ResourceManager[T]) Request(key string, data T, n int64, notifyC chan T, cancelC chan struct{}) (acquired bool) {
	if n < 0 {
		return
	}
	r := request[T]{
		key:     key,
		data:    data,
		n:       n,
		notifyC: notifyC,
		cancelC: cancelC,
		doneC:   make(chan bool),
	}
	select {
	case m.requestC <- r:
		select {
		case acquired = <-r.doneC:
		case <-m.closeC:
		}
	case <-m.closeC:
	}
	return
}

// SetLimit retunes the budget live. Shrinking below the allocated amount
// only pauses new grants until releases catch up. Negative limits are
// ignored.
func (m *ResourceManager[T]) SetLimit(limit int64) {
	if limit < 0 {
		return
	}
	select {
	case m.limitC <- limit:
	case <-m.closeC:
	}
}

// Release returns n units to the budget.
func (m *ResourceManager[T]) Release(n int64) {
	select {
	case m.releaseC <- n:
	case <-m.closeC:
	}
}

func (m *ResourceManager[T]) run() {
	for {
		req, i := m.grantable()
		select {
		case r := <-m.requestC:
			m.handleRequest(r)
		case limit := <-m.limitC:
			m.available += limit - m.limit
			m.limit = limit
		case n := <-m.releaseC:
			m.available += n
			m.objects--
			if m.available > m.limit {
				panic("invalid release call")
			}
		case req.notifyC <- req.data:
			m.available -= req.n
			m.objects++
			m.deleteRequest(req.key, i)
		case <-req.cancelC:
			m.deleteRequest(req.key, i)
		case ch := <-m.statsC:
			stats := Stats{
				AllocatedObjects: m.objects,
				AllocatedSize:    m.limit - m.available,
				PendingKeys:      len(m.requests),
			}
			select {
			case ch <- stats:
			case <-m.closeC:
			}
		case <-m.closeC:
			close(m.doneC)
			return
		}
	}
}

// deleteRequest drops the i-th queued request of key, swap-with-last.
func (m *ResourceManager[T]) deleteRequest(key string, i int) {
	rs := m.requests[key]
	rs[i] = rs[len(rs)-1]
	rs = rs[:len(rs)-1]
	if len(rs) > 0 {
		m.requests[key] = rs
	} else {
		delete(m.requests, key)
	}
}

// grantable picks a queued request the free budget covers. With nothing
// outstanding it also returns an over-limit one: the single isolated grant
// may drive available negative, which is exactly the liveness guarantee.
// While anything is outstanding, only fitting requests are granted.
func (m *ResourceManager[T]) grantable() (request[T], int) {
	for _, rs := range m.requests {
		i := rand.IntN(len(rs)) // nolint: gosec
		r := rs[i]
		if r.n > m.available && m.objects != 0 {
			break
		}
		return r, i
	}
	return request[T]{}, -1
}

// handleRequest grants immediately when affordable (or nothing is out,
// preserving the liveness guarantee), else queues. A raced cancellation
// drops the request instead.
func (m *ResourceManager[T]) handleRequest(r request[T]) {
	acquired := m.available >= r.n || m.objects == 0
	select {
	case r.doneC <- acquired:
		if acquired {
			m.available -= r.n
			m.objects++
		} else {
			m.requests[r.key] = append(m.requests[r.key], r)
		}
	case <-r.cancelC:
	}
}
