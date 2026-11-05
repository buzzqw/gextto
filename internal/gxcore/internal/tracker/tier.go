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
//
// With announceAll set (qBittorrent's "announce to all trackers in a tier")
// every live member is contacted in parallel and the responses are merged, so
// a tier with a dead or partial first tracker does not hide the peers the
// others know about.
type Tier struct {
	Trackers []Tracker

	mu          sync.Mutex
	index       int
	announceAll bool
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

// Announce contacts the current member, rotating past failures. In
// announce-all mode it contacts every live member in parallel and merges their
// peers and swarm counts.
func (t *Tier) Announce(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error) {
	t.mu.Lock()
	all := t.announceAll && len(t.Trackers) > 1
	t.mu.Unlock()
	if all {
		return t.announceEveryMember(ctx, req)
	}
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

// SetAnnounceAll turns the parallel policy on or off. It is set when the tier
// is built (torrent.Config.AnnounceAllInTier).
func (t *Tier) SetAnnounceAll(on bool) {
	t.mu.Lock()
	t.announceAll = on
	t.mu.Unlock()
}

// announceEveryMember asks all live members at once and merges what they
// return. A failed member is advanced past (and dropped on BEP 31 "never"),
// exactly as in the one-at-a-time path; the others still answer.
func (t *Tier) announceEveryMember(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error) {
	t.mu.Lock()
	indexes := make([]int, 0, len(t.Trackers))
	for i := range t.Trackers {
		if !t.dead[i] {
			indexes = append(indexes, i)
		}
	}
	t.mu.Unlock()
	if len(indexes) == 0 {
		return nil, ErrTierExhausted
	}

	type result struct {
		index int
		resp  *AnnounceResponse
		err   error
	}
	results := make(chan result, len(indexes))
	for _, index := range indexes {
		go func(index int) {
			resp, err := t.Trackers[index].Announce(ctx, req)
			results <- result{index: index, resp: resp, err: err}
		}(index)
	}

	var responses []*AnnounceResponse
	var firstErr error
	for range indexes {
		res := <-results
		if res.err != nil {
			t.advance(res.index, res.err)
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		responses = append(responses, res.resp)
	}
	if len(responses) == 0 {
		if firstErr == nil {
			firstErr = ErrTierExhausted
		}
		return nil, firstErr
	}
	return mergeAnnounceResponses(responses), nil
}

// mergeAnnounceResponses combines parallel answers: every peer once, the
// shortest interval (the strictest tracker wins), the largest swarm counts (a
// tracker that sees fewer peers must not shrink the one that sees more) and
// the first warning.
func mergeAnnounceResponses(responses []*AnnounceResponse) *AnnounceResponse {
	out := &AnnounceResponse{}
	seen := make(map[string]bool)
	for _, resp := range responses {
		if resp == nil {
			continue
		}
		if resp.Interval > 0 && (out.Interval == 0 || resp.Interval < out.Interval) {
			out.Interval = resp.Interval
		}
		if resp.MinInterval > out.MinInterval {
			out.MinInterval = resp.MinInterval
		}
		if resp.Seeders > out.Seeders {
			out.Seeders = resp.Seeders
		}
		if resp.Leechers > out.Leechers {
			out.Leechers = resp.Leechers
		}
		if out.WarningMessage == "" {
			out.WarningMessage = resp.WarningMessage
		}
		for _, peer := range resp.Peers {
			if peer == nil {
				continue
			}
			key := peer.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out.Peers = append(out.Peers, peer)
		}
	}
	return out
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
