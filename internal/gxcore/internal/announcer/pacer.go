package announcer

// pacer.go spaces the announces to one tracker host. Every torrent has its own
// announcer, so without a session-wide gate a client with hundreds of torrents
// on the same private tracker sends them all at once when the intervals expire
// or after a restart. The gate is per host, not per URL: HTTP, UDP and
// WebSocket announces to the same host share it, while two trackers on
// different hosts do not wait for each other. The torrent's own interval is
// untouched; only a burst is spread.

import (
	"context"
	"math"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The defaults: four announces a second per host with a burst of eight. At a
// 30-minute interval a thousand torrents average well under one announce a
// second, so the gate only smooths the start and the interval expiries; a
// private tracker does not receive a thousand requests in the same instant.
const (
	DefaultAnnouncesPerSecond = 4
	DefaultAnnounceBurst      = 8
)

// Pacer is a token bucket per tracker host, safe for concurrent use.
type Pacer struct {
	mu    sync.Mutex
	gates map[string]*pacerGate
	rate  float64 // tokens per second
	burst float64
}

type pacerGate struct {
	tokens float64
	last   time.Time
}

// NewPacer builds a pacer; a non-positive rate or burst means no pacing
// (nil), which keeps tests and callers that do not want a queue cheap.
func NewPacer(perSecond float64, burst int) *Pacer {
	if perSecond <= 0 || burst < 1 {
		return nil
	}
	return &Pacer{gates: make(map[string]*pacerGate), rate: perSecond, burst: float64(burst)}
}

// Wait blocks until the host has a token or ctx ends. The lock is never held
// during the sleep, so a host waiting for its next token does not block another
// host.
func (p *Pacer) Wait(ctx context.Context, host string) error {
	if p == nil {
		return nil
	}
	for {
		p.mu.Lock()
		gate := p.gates[host]
		if gate == nil {
			gate = &pacerGate{tokens: p.burst, last: time.Now()}
			p.gates[host] = gate
		}
		now := time.Now()
		gate.tokens = math.Min(p.burst, gate.tokens+now.Sub(gate.last).Seconds()*p.rate)
		gate.last = now
		if gate.tokens >= 1 {
			gate.tokens--
			p.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - gate.tokens) / p.rate * float64(time.Second))
		p.mu.Unlock()
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

// AnnounceHost is the pacing key of an announce URL: the host and port,
// lowercased, with the scheme's default port when the URL omits it, so HTTP,
// UDP and WebSocket trackers of the same server share a gate. A URL without a
// host (a test's placeholder) keys on itself, so it still gets a gate instead
// of a panic.
func AnnounceHost(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return strings.ToLower(raw)
	}
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http", "ws", "udp":
			port = "80"
		case "https", "wss":
			port = "443"
		}
	}
	if port == "" {
		return strings.ToLower(u.Hostname())
	}
	return strings.ToLower(net.JoinHostPort(u.Hostname(), port))
}
