package main

// feed_pace.go spaces the requests to one site so importing a list of feeds
// does not get the client rate-limited: 13 uploader listings of TorrentGalaxy,
// or 15 of ext.to, would otherwise be fetched back to back in one cycle and the
// site answers HTTP 429 for the rest (which then look like broken feeds). The
// pacer keeps a minimum gap between two requests to the same host and remembers
// the hosts that asked to slow down, so the remaining feeds of that host are
// left for the next check instead of failing one after the other.

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
	// feedHostCooldown is how long a host is left alone after it answers 429:
	// long enough for the site's window to pass, short enough that a feed is not
	// left stale for a whole polling interval.
	feedHostCooldown = 10 * time.Minute
)

// feedPacer spaces the feed requests per host.
type feedPacer struct {
	gap      time.Duration
	cooldown time.Duration

	mu    sync.Mutex
	last  map[string]time.Time
	until map[string]time.Time
}

func newFeedPacer(gap, cooldown time.Duration) *feedPacer {
	return &feedPacer{
		gap:      gap,
		cooldown: cooldown,
		last:     map[string]time.Time{},
		until:    map[string]time.Time{},
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
	until, cooling := p.until[host]
	last, seen := p.last[host]
	if cooling && !time.Now().Before(until) {
		delete(p.until, host)
		cooling = false
	}
	p.mu.Unlock()
	if cooling {
		return time.Until(until)
	}
	if seen {
		if gap := p.gap - time.Since(last); gap > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(gap):
			}
		}
	}
	return 0
}

// done records one request to the host; a rate-limit answer puts the host on
// cooldown, so its remaining feeds are skipped for a while.
func (p *feedPacer) done(host string, err error) {
	if p == nil || host == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last[host] = time.Now()
	if err != nil && rateLimited(err) {
		p.until[host] = time.Now().Add(p.cooldown)
	}
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
