package trackermanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/buzzqw/gextto/internal/engine/internal/netx"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/buzzqw/gextto/internal/engine/internal/blocklist"
	"github.com/buzzqw/gextto/internal/engine/internal/resolver"
	"github.com/buzzqw/gextto/internal/engine/internal/tracker"
	"github.com/buzzqw/gextto/internal/engine/internal/tracker/httptracker"
	"github.com/buzzqw/gextto/internal/engine/internal/tracker/udptracker"
)

// TrackerManager is a manager for using the same transport for same domains/IPs.
// Manages both HTTP and UDP trackers.
type TrackerManager struct {
	httpTransport *http.Transport
	udpTransport  *udptracker.Transport
}

// New returns a new TrackerManager.
func New(bl *blocklist.Blocklist, dnsTimeout time.Duration, tlsSkipVerify bool) *TrackerManager {
	m := &TrackerManager{
		httpTransport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: tlsSkipVerify}, // nolint: gosec
		},
		udpTransport: udptracker.NewTransport(bl, dnsTimeout),
	}
	go m.udpTransport.Run()
	m.httpTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if netx.Current().Proxy != nil {
			// Let the proxy resolve the name: no DNS leak.
			return netx.DialContext(ctx, net.Dialer{}, network, addr)
		}
		ip, port, err := resolver.Resolve(ctx, addr, dnsTimeout, bl)
		if err != nil {
			return nil, err
		}
		taddr := &net.TCPAddr{IP: ip, Port: port}
		return netx.DialContext(ctx, net.Dialer{}, network, taddr.String())
	}
	return m
}

func (m *TrackerManager) Close() {
	m.httpTransport.CloseIdleConnections()
	m.udpTransport.Close()
}

// Get a new Tracker implementation from the manager.
func (m *TrackerManager) Get(s string, httpTimeout time.Duration, httpUserAgent string, httpMaxResponseLength int64) (tracker.Tracker, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		tr := httptracker.New(s, u, httpTimeout, m.httpTransport, httpUserAgent, httpMaxResponseLength)
		return tr, nil
	case "udp":
		tr := udptracker.New(s, u, m.udpTransport)
		return tr, nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}
