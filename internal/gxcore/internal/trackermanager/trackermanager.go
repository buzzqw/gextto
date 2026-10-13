package trackermanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resolver"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker/httptracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker/udptracker"
)

// TrackerManager shares transports across trackers: one HTTP stack (with the
// session's proxy/blocklist-aware dialer) and one UDP engine for the whole
// session, instead of a socket per tracker.
type TrackerManager struct {
	httpTransport *http.Transport
	udpTransport  *udptracker.Transport
}

// New builds the shared transports. TLS verification follows tlsSkipVerify;
// the UDP engine starts its run loop immediately.
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
			// Proxied: let the proxy resolve the name, no DNS leak locally.
			return netx.DialContext(ctx, net.Dialer{}, network, addr)
		}
		ip, port, err := resolver.Resolve(ctx, addr, dnsTimeout, bl)
		if err != nil {
			return nil, err
		}
		return netx.DialContext(ctx, net.Dialer{}, network, (&net.TCPAddr{IP: ip, Port: port}).String())
	}
	return m
}

// Close drains both transports.
func (m *TrackerManager) Close() {
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
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}
