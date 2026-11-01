package main

// feed_pace.go spaces the requests to one site so importing a list of feeds
// does not get the client rate-limited: 13 uploader listings of TorrentGalaxy,
// or 15 of ext.to, would otherwise be fetched back to back in one cycle and the
// site answers HTTP 429 for the rest (which then look like broken feeds). The
// pacer keeps a minimum gap between two requests to the same host and remembers
// the hosts that asked to slow down, so the remaining feeds of that host are
// left for the next check instead of failing one after the other.
//
// The judgement grows with the site's answer. A host that answers 429 once gets
// the base cooldown; if it answers again after that, the cooldown doubles each
// time up to feedHostMaxCooldown, and the gap between its requests grows too.
// Answers clear the record again, so a site that was merely busy returns to the
// normal pace instead of being punished for ever.

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"
)

// feedHostGap is the minimum gap between two requests to the same host; the
// tests shorten it.
var feedHostGap = 2 * time.Second

const (
	// feedHostCooldown is how long a host is left alone after it answers 429
	// for the first time: long enough for the site's window to pass, short
	// enough that a feed is not left stale for a whole polling interval.
	feedHostCooldown = 10 * time.Minute
	// feedHostMaxCooldown caps the growing cooldown of a site that keeps
	// answering 429: after a few rounds it is asked a handful of times a day,
	// not every ten minutes.
	feedHostMaxCooldown = 6 * time.Hour
	// feedHostMaxGap caps the gap between two requests to a site that answered
	// 429, so a long feed still finishes.
	feedHostMaxGap = 30 * time.Second
	// feedHostRecoverAfter is how many good answers step the judgement back
	// down: a site that stops complaining gets its normal pace back.
	feedHostRecoverAfter = 20
)

// feedHostState is what the pacer knows about one site.
type feedHostState struct {
	// last is when the previous request started.
	last time.Time
	// until is the end of the 429 cooldown; zero means "not cooling".
	until time.Time
	// gap is the spacing in force for this host.
	gap time.Duration
	// strikes counts the consecutive rate-limit answers; it grows the cooldown.
	strikes int
	// good counts the answers since the last rate limit; enough of them lower
	// the strikes and shrink the gap.
	good int
}

// feedPacer spaces the feed requests per host.
type feedPacer struct {
	gap      time.Duration
	cooldown time.Duration

	mu    sync.Mutex
	hosts map[string]*feedHostState
}

func newFeedPacer(gap, cooldown time.Duration) *feedPacer {
	return &feedPacer{
		gap:      gap,
		cooldown: cooldown,
		hosts:    map[string]*feedHostState{},
	}
}

// wait blocks until the host may be asked again and returns the remaining
// cooldown when the host asked to slow down (0 = go ahead). A nil pacer or an
// empty host (a Torznab search, a DHT feed) never waits.
func (p *feedPacer) wait(ctx context.Context, host string) time.Duration {
	if p == nil || host == "" {
		return 0
	}
	p.mu.Lock()
	state := p.host(host)
	now := time.Now()
	if !state.until.IsZero() && !now.Before(state.until) {
		state.until = time.Time{}
	}
	if !state.until.IsZero() {
		remaining := time.Until(state.until)
		p.mu.Unlock()
		return remaining
	}
	last := state.last
	gap := state.gap
	p.mu.Unlock()
	if last.IsZero() {
		return 0
	}
	if wait := gap - time.Since(last); wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return 0
}

// done records one request to the host. A rate-limit answer puts the host on a
// cooldown that doubles while the site keeps answering 429; a good answer steps
// the judgement back down.
func (p *feedPacer) done(host string, err error) {
	if p == nil || host == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.host(host)
	now := time.Now()
	state.last = now
	if rateLimited(err) {
		state.good = 0
		state.strikes++
		state.gap = p.nextGap(state.gap)
		wait := p.cooldown
		for i := 1; i < state.strikes && wait < feedHostMaxCooldown; i++ {
			wait *= 2
		}
		if wait > feedHostMaxCooldown {
			wait = feedHostMaxCooldown
		}
		state.until = now.Add(wait)
		return
	}
	if err != nil || state.strikes == 0 {
		return
	}
	state.good++
	if state.good < feedHostRecoverAfter {
		return
	}
	state.good = 0
	state.strikes--
	state.gap /= 2
	if state.gap < p.gap {
		state.gap = p.gap
	}
}

// nextGap doubles the spacing of one host up to the cap.
func (p *feedPacer) nextGap(current time.Duration) time.Duration {
	if current <= 0 {
		current = p.gap
	}
	current *= 2
	if current > feedHostMaxGap {
		current = feedHostMaxGap
	}
	if current < p.gap {
		current = p.gap
	}
	return current
}

// host returns the state of one site, created with the normal pace the first
// time. It must be called with p.mu held.
func (p *feedPacer) host(name string) *feedHostState {
	state := p.hosts[name]
	if state == nil {
		state = &feedHostState{gap: p.gap}
		p.hosts[name] = state
	}
	return state
}

// rateLimited reports a site asking to slow down (HTTP 429).
func rateLimited(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "429") || strings.Contains(text, "too many requests")
}

// feedHost is the host a feed is fetched from, the key of the pacer: empty for a
// Torznab search (the indexers do the fetching) and for a DHT feed.
func feedHost(rawURL, search string) string {
	if strings.TrimSpace(search) != "" {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "dht" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
