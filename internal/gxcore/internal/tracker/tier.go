package tracker

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
)

// ErrTierExhausted is returned when every tracker in a tier asked to never be
// retried (BEP 31 "retry in: never") and none is left to announce to.
var ErrTierExhausted = errors.New("no usable tracker in tier")

// Tier implements the Tracker interface and contains multiple Trackers which tries to announce to the working Tracker.
type Tier struct {
	Trackers []Tracker

	mu    sync.Mutex
	index int
	// dead marks the trackers dropped for good after a BEP 31 "retry in: never".
	dead map[int]bool
}

var _ Tracker = (*Tier)(nil)

// NewTier returns a new Tier.
func NewTier(trackers []Tracker) *Tier {
	rand.Shuffle(len(trackers), func(i, j int) { trackers[i], trackers[j] = trackers[j], trackers[i] })
	return &Tier{
		Trackers: trackers,
	}
}

// Announce a torrent to the tracker.
// If announce fails, the next announce will be made to the next Tracker in the
// tier. A failure carrying BEP 31 "retry in: never" drops that tracker for good
// instead of cycling back to it.
func (t *Tier) Announce(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error) {
	index, ok := t.current()
	if !ok {
		return nil, ErrTierExhausted
	}
	resp, err := t.Trackers[index].Announce(ctx, req)
	if err != nil {
		t.advance(index, err)
	}
	return resp, err
}

// URL returns the current Tracker in the Tier.
func (t *Tier) URL() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(t.Trackers)
	for i := 0; i < n; i++ {
		index := (t.index + i) % n
		if !t.dead[index] {
			return t.Trackers[index].URL()
		}
	}
	if n == 0 {
		return ""
	}
	// Every tracker is dropped: report the first one for display.
	return t.Trackers[0].URL()
}

// Scrape queries the current Tracker in the Tier for swarm statistics. On
// error the next Scrape goes to the next Tracker, as Announce does.
func (t *Tier) Scrape(ctx context.Context, req ScrapeRequest) (*ScrapeResponse, error) {
	index, ok := t.current()
	if !ok {
		return nil, ErrTierExhausted
	}
	resp, err := t.Trackers[index].Scrape(ctx, req)
	if err != nil {
		t.advance(index, err)
	}
	return resp, err
}

// current returns the index of the tracker to contact and whether one is
// available: the first one from the current position that was not dropped.
func (t *Tier) current() (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(t.Trackers)
	if n == 0 {
		return 0, false
	}
	for i := 0; i < n; i++ {
		index := (t.index + i) % n
		if !t.dead[index] {
			t.index = index
			return index, true
		}
	}
	return 0, false
}

// advance moves the tier to the next tracker after a failure. When the error is
// a BEP 31 "retry in: never" the failed tracker is dropped for good.
func (t *Tier) advance(index int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(t.Trackers)
	if n == 0 {
		return
	}
	var terr *Error
	if errors.As(err, &terr) && terr.NeverRetry {
		if t.dead == nil {
			t.dead = make(map[int]bool)
		}
		t.dead[index] = true
	}
	t.index = (index + 1) % n
}
