package trackermanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resolver"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker/httptracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker/udptracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker/wstracker"
)

// TrackerManager shares transports across trackers: one HTTP stack (with the
// session's proxy/blocklist-aware dialer) and one UDP engine for the whole
// session, instead of a socket per tracker.
type TrackerManager struct {
	httpTransport *http.Transport
	udpTransport  *udptracker.Transport
	// dial is the egress the HTTP transport and the WebSocket tracker share:
	// resolver (with the blocklist) plus proxy.
	dial          func(ctx context.Context, network, addr string) (net.Conn, error)
	tlsSkipVerify bool
	// mu guards wsTrackers, the WebSocket trackers whose sockets must be closed
	// when the session goes away.
	mu         sync.Mutex
	wsTrackers []*wstracker.Tracker
}

// New builds the shared transports. TLS verification follows tlsSkipVerify;
// the UDP engine starts its run loop immediately.
func New(bl *blocklist.Blocklist, dnsTimeout time.Duration, tlsSkipVerify bool) *TrackerManager {
	m := &TrackerManager{
		httpTransport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: tlsSkipVerify}, // nolint: gosec
		},
		udpTransport:  udptracker.NewTransport(bl, dnsTimeout),
		tlsSkipVerify: tlsSkipVerify,
	}
	go m.udpTransport.Run()
	m.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if netx.Current().Proxy != nil {
			// Proxied: let the proxy resolve the name, no DNS leak locally.
			return netx.DialContext(ctx, net.Dialer{}, network, addr)
		}
		ip, port, err := resolver.Resolve(ctx, addr, dnsTimeout, bl)
		if err != nil {
			return nil, err
		}
		return netx.DialContext(ctx, net.Dialer{}, network, (&net.TCPAddr{IP: ip, Port: port}).String())
	}
	m.httpTransport.DialContext = m.dial
	return m
}

// Close drains both transports.
func (m *TrackerManager) Close() {
	m.mu.Lock()
	for _, t := range m.wsTrackers {
		t.Close()
	}
	m.wsTrackers = nil
	m.mu.Unlock()
	m.httpTransport.CloseIdleConnections()
	m.udpTransport.Close()
}

// Get builds the tracker client for a URL: HTTP(S) or UDP by scheme.
func (m *TrackerManager) Get(s string, httpTimeout time.Duration, httpUserAgent string, httpMaxResponseLength int64) (tracker.Tracker, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		return httptracker.New(s, u, httpTimeout, m.httpTransport, httpUserAgent, httpMaxResponseLength), nil
	case "udp":
		return udptracker.New(s, u, m.udpTransport), nil
	case "ws", "wss":
		wt := wstracker.New(s, u, httpTimeout, m.dial, m.tlsSkipVerify, httpUserAgent)
		m.mu.Lock()
		m.wsTrackers = append(m.wsTrackers, wt)
		m.mu.Unlock()
		return wt, nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}
