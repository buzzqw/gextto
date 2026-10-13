package tracker

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
)

// ErrTierExhausted reports a tier with no contactable tracker left: every
// member asked to never be retried (BEP 31 "retry in: never").
var ErrTierExhausted = errors.New("no usable tracker in tier")

// Tier treats several trackers as one: announces go to the current member,
// failures rotate to the next. A member that answers BEP 31 "retry in:
// never" is dropped for good instead of being cycled back to.
type Tier struct {
	Trackers []Tracker

	mu    sync.Mutex
	index int
	// dead holds the dropped member indexes.
	dead map[int]bool
}

var _ Tracker = (*Tier)(nil)

// NewTier shuffles the members so concurrent torrents spread their first
// announce across the tier instead of hammering the same tracker.
func NewTier(trackers []Tracker) *Tier {
	rand.Shuffle(len(trackers), func(i, j int) { trackers[i], trackers[j] = trackers[j], trackers[i] })
	return &Tier{Trackers: trackers}
}

// Announce contacts the current member, rotating past failures.
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

// URL names the current member for display, or "" for an empty tier. When
// everything is dropped it still names the first member — an address for
// the operator to inspect, not a promise it will be contacted.
func (t *Tier) URL() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(t.Trackers)
	for i := 0; i < n; i++ {
		if index := (t.index + i) % n; !t.dead[index] {
			return t.Trackers[index].URL()
		}
	}
	if n == 0 {
		return ""
	}
	return t.Trackers[0].URL()
}

// Scrape queries the current member for swarm totals, rotating past
// failures like Announce.
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

// current selects the first live member from the rotation point and parks
// the rotation there.
func (t *Tier) current() (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n := len(t.Trackers); n > 0 {
		for i := 0; i < n; i++ {
			if index := (t.index + i) % n; !t.dead[index] {
				t.index = index
				return index, true
			}
		}
	}
	return 0, false
}

// advance rotates past a failed member, dropping it for good on a BEP 31
// never-retry.
func (t *Tier) advance(index int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n := len(t.Trackers); n > 0 {
		var terr *Error
		if errors.As(err, &terr) && terr.NeverRetry {
			if t.dead == nil {
				t.dead = make(map[int]bool)
			}
			t.dead[index] = true
		}
		t.index = (index + 1) % n
	}
}
