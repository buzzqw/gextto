// Package source keeps the per-file eD2k source list: deduplication, failure
// backoff and expiry. It is an isolated building block of the gated transfer
// plane and is not imported by cmd/gx-ed2k yet.
package source

import (
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// Source is one peer that may have a file.
type Source struct {
	IP       net.IP
	TCPPort  uint16
	UDPPort  uint16
	UserHash ed2k.ID
	Type     uint8
	Origin   string
	LastSeen time.Time
	Failures int
	NextTry  time.Time
}

// Key is the identity used for deduplication: a peer is reachable at one
// TCP endpoint even if it was learned from different networks.
func (s Source) Key() string {
	if s.TCPPort != 0 {
		return fmt.Sprintf("%s:%d", s.IP.String(), s.TCPPort)
	}
	return fmt.Sprintf("%s:udp/%d", s.IP.String(), s.UDPPort)
}

// List is a bounded source set for one file.
type List struct {
	max   int
	items map[string]Source
}

// New returns a list capped at max sources (a non-positive value uses 1000).
func New(max int) *List {
	if max <= 0 {
		max = 1000
	}
	return &List{max: max, items: make(map[string]Source, max)}
}

// Add inserts or refreshes a source. It returns false when the address is
// invalid or the source was rejected.
func (l *List) Add(source Source, now time.Time) bool {
	ip := source.IP.To4()
	if ip == nil {
		return false
	}
	lowID := source.TCPPort == 0 && source.UDPPort != 0 && (source.Type == 3 || source.Type == 5 || source.Type == 6)
	if !lowID && (source.TCPPort == 0 || source.TCPPort > 65535) {
		return false
	}
	// A private, link-local or multicast address can never be a public eD2k
	// peer. Loopback stays valid for deterministic fake-peer tests.
	if !ip.IsLoopback() && (!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified()) {
		return false
	}
	// Network and broadcast addresses in a /24 are not hosts; server lists
	// are full of them as padding.
	if ip[3] == 0 || ip[3] == 255 {
		return false
	}
	source.IP = ip
	key := source.Key()
	if existing, ok := l.items[key]; ok {
		// Keep the freshest identity fields and reset the backoff only when
		// the caller says the peer answered (Success handles that).
		existing.LastSeen = now
		if source.UserHash != (ed2k.ID{}) {
			existing.UserHash = source.UserHash
		}
		if source.UDPPort != 0 {
			existing.UDPPort = source.UDPPort
		}
		if source.Origin != "" {
			existing.Origin = source.Origin
		}
		l.items[key] = existing
		return true
	}
	if len(l.items) >= l.max {
		l.evictOldest()
	}
	if source.LastSeen.IsZero() {
		source.LastSeen = now
	}
	l.items[key] = source
	return true
}

func (l *List) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, source := range l.items {
		if oldestKey == "" || source.LastSeen.Before(oldest) {
			oldestKey, oldest = key, source.LastSeen
		}
	}
	if oldestKey != "" {
		delete(l.items, oldestKey)
	}
}

// Success records an answer: the source is usable and the failure count resets.
func (l *List) Success(ip net.IP, port uint16, now time.Time) {
	key := fmt.Sprintf("%s:%d", ip.String(), port)
	source, ok := l.items[key]
	if !ok {
		return
	}
	source.Failures = 0
	source.NextTry = time.Time{}
	source.LastSeen = now
	l.items[key] = source
}

// Fail records a failed attempt and schedules the next one with exponential
// backoff: 30 s, 1 min, 2 min ... capped at one hour.
func (l *List) Fail(ip net.IP, port uint16, now time.Time) {
	key := fmt.Sprintf("%s:%d", ip.String(), port)
	source, ok := l.items[key]
	if !ok {
		return
	}
	source.Failures++
	backoff := 30 * time.Second
	for i := 1; i < source.Failures && backoff < time.Hour; i++ {
		backoff *= 2
	}
	if backoff > time.Hour {
		backoff = time.Hour
	}
	source.NextTry = now.Add(backoff)
	l.items[key] = source
}

// Available returns the sources whose backoff has expired, freshest first.
func (l *List) Available(now time.Time) []Source {
	out := make([]Source, 0, len(l.items))
	for _, source := range l.items {
		if source.NextTry.After(now) {
			continue
		}
		out = append(out, source)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].Key() < out[j].Key()
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

// Expire removes sources not seen for olderThan.
func (l *List) Expire(olderThan time.Duration, now time.Time) int {
	removed := 0
	for key, source := range l.items {
		if source.LastSeen.Add(olderThan).Before(now) {
			delete(l.items, key)
			removed++
		}
	}
	return removed
}

// Remove drops one source.
func (l *List) Remove(ip net.IP, port uint16) {
	delete(l.items, fmt.Sprintf("%s:%d", ip.String(), port))
}

// Len is the number of sources.
func (l *List) Len() int { return len(l.items) }
