package trackermanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
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
// session, instead of a socket per tracker. Trackers taken from torrent data
// get a second, public-only HTTP transport and dialer; operator-supplied
// trackers keep the unrestricted egress.
type TrackerManager struct {
	httpTransport       *http.Transport
	publicHTTPTransport *http.Transport
	udpTransport        *udptracker.Transport
	// dial is the egress the HTTP transport and the WebSocket tracker share:
	// resolver (with the blocklist) plus proxy. publicDial additionally refuses
	// private, loopback and link-local destinations.
	dial          func(ctx context.Context, network, addr string) (net.Conn, error)
	publicDial    func(ctx context.Context, network, addr string) (net.Conn, error)
	dialContext   func(ctx context.Context, network, addr string) (net.Conn, error)
	resolve       func(ctx context.Context, hostport string, timeout time.Duration, bl *blocklist.Blocklist) (net.IP, int, error)
	blocklist     *blocklist.Blocklist
	dnsTimeout    time.Duration
	allowPrivate  bool
	tlsSkipVerify bool
	// mu guards wsTrackers, the WebSocket trackers whose sockets must be closed
	// when the session goes away.
	mu         sync.Mutex
	wsTrackers []*wstracker.Tracker
}

// New builds the shared transports with the safe default for torrent-supplied
// trackers. TLS verification follows tlsSkipVerify; the UDP engine starts its
// run loop immediately.
func New(bl *blocklist.Blocklist, dnsTimeout time.Duration, tlsSkipVerify bool) *TrackerManager {
	return NewWithPolicy(bl, dnsTimeout, tlsSkipVerify, false)
}

// NewWithPolicy is New with the operator's private-source opt-in. When
// allowPrivate is true, torrent-supplied trackers use the same egress as
// operator-supplied ones.
func NewWithPolicy(bl *blocklist.Blocklist, dnsTimeout time.Duration, tlsSkipVerify, allowPrivate bool) *TrackerManager {
	m := &TrackerManager{
		udpTransport:  udptracker.NewTransport(bl, dnsTimeout),
		blocklist:     bl,
		dnsTimeout:    dnsTimeout,
		allowPrivate:  allowPrivate,
		tlsSkipVerify: tlsSkipVerify,
		dialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return netx.DialContext(ctx, net.Dialer{}, network, addr)
		},
		resolve: resolver.Resolve,
	}
	m.dial = m.makeDial(false)
	m.publicDial = m.makeDial(true)
	m.httpTransport = m.newHTTPTransport(m.dial)
	m.publicHTTPTransport = m.newHTTPTransport(m.publicDial)
	go m.udpTransport.Run()
	return m
}

func (m *TrackerManager) newHTTPTransport(dial func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	return &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: m.tlsSkipVerify}, // nolint: gosec
		// A tracker answer is a stranger's: cap the headers instead of
		// leaving net/http's 10 MiB per request to whoever answers.
		MaxResponseHeaderBytes: 256 << 10,
		DialContext:            dial,
	}
}

// makeDial resolves once and connects to the checked address, so a DNS answer
// cannot switch to a private host after validation. The public variant checks
// even when a proxy is configured: otherwise the proxy would be the only party
// able to resolve the destination and could reach services on its own LAN.
func (m *TrackerManager) makeDial(public bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !public && netx.Current().Proxy != nil {
			// Trusted, proxied egress keeps the existing no-local-DNS behavior.
			return m.dialContext(ctx, network, addr)
		}
		ip, port, err := m.resolve(ctx, addr, m.dnsTimeout, m.blocklist)
		if err != nil {
			return nil, err
		}
		if public && !m.allowPrivate {
			if err := checkPublicTrackerAddr(ip, port); err != nil {
				return nil, err
			}
		}
		return m.dialContext(ctx, network, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
	}
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
	m.publicHTTPTransport.CloseIdleConnections()
	m.udpTransport.Close()
}

// Release closes a WebSocket tracker the session no longer uses. A ws tracker
// owns its socket and its registration, and nothing else would close them
// before the whole session ends: a hot-swapped or removed tracker would keep
// both open for the daemon's lifetime. Other schemes share a transport and
// need no release.
func (m *TrackerManager) Release(t tracker.Tracker) {
	wt, ok := t.(*wstracker.Tracker)
	if !ok || wt == nil {
		return
	}
	wt.Close()
	m.mu.Lock()
	for i, known := range m.wsTrackers {
		if known == wt {
			m.wsTrackers = append(m.wsTrackers[:i], m.wsTrackers[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
}

// Get builds an operator-supplied tracker. Calling Get is an explicit trust
// decision: private, loopback and link-local destinations are allowed. URLs
// from metainfo, magnets or resume data must use GetUntrusted instead.
func (m *TrackerManager) Get(s string, httpTimeout time.Duration, httpUserAgent string, httpMaxResponseLength int64) (tracker.Tracker, error) {
	return m.get(s, httpTimeout, httpUserAgent, httpMaxResponseLength, false)
}

// GetUntrusted builds a tracker whose URL came from torrent data. Unless the
// operator opted in, HTTP(S) and WebSocket dials (including redirects) are
// restricted to public addresses and UDP destinations are resolved and pinned
// to a checked public IP before the shared transport ever sees them.
func (m *TrackerManager) GetUntrusted(s string, httpTimeout time.Duration, httpUserAgent string, httpMaxResponseLength int64) (tracker.Tracker, error) {
	return m.get(s, httpTimeout, httpUserAgent, httpMaxResponseLength, true)
}

func (m *TrackerManager) get(s string, httpTimeout time.Duration, httpUserAgent string, httpMaxResponseLength int64, untrusted bool) (tracker.Tracker, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("tracker URL has no host")
	}
	public := untrusted && !m.allowPrivate
	if public {
		if ip := net.ParseIP(u.Hostname()); ip != nil {
			if err := checkPublicTrackerAddr(ip, 0); err != nil {
				return nil, err
			}
		}
	}
	switch u.Scheme {
	case "http", "https":
		transport := m.httpTransport
		if public {
			transport = m.publicHTTPTransport
		}
		return httptracker.New(s, u, httpTimeout, transport, httpUserAgent, httpMaxResponseLength), nil
	case "udp":
		if public {
			return &publicUDPTracker{rawURL: s, parsed: *u, manager: m}, nil
		}
		return udptracker.New(s, u, m.udpTransport), nil
	case "ws", "wss":
		dial := m.dial
		if public {
			dial = m.publicDial
		}
		wt := wstracker.New(s, u, httpTimeout, dial, m.tlsSkipVerify, httpUserAgent)
		m.mu.Lock()
		m.wsTrackers = append(m.wsTrackers, wt)
		m.mu.Unlock()
		return wt, nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

// publicUDPTracker validates and pins the destination for each exchange. Doing
// this at announce/scrape time instead of Get keeps a temporary DNS failure from
// permanently dropping an otherwise operational tracker when a torrent is added.
type publicUDPTracker struct {
	rawURL  string
	parsed  url.URL
	manager *TrackerManager
}

var _ tracker.Tracker = (*publicUDPTracker)(nil)

func (t *publicUDPTracker) URL() string { return t.rawURL }

func (t *publicUDPTracker) client(ctx context.Context) (tracker.Tracker, error) {
	ip, port, err := t.manager.resolve(ctx, t.parsed.Host, t.manager.dnsTimeout, t.manager.blocklist)
	if err != nil {
		return nil, err
	}
	if err := checkPublicTrackerAddr(ip, port); err != nil {
		return nil, err
	}
	// The shared UDP transport resolves again. Pin this exchange to the IP
	// checked here so a second DNS answer cannot redirect the datagrams.
	pinned := t.parsed
	pinned.Host = net.JoinHostPort(ip.String(), strconv.Itoa(port))
	return udptracker.New(t.rawURL, &pinned, t.manager.udpTransport), nil
}

func (t *publicUDPTracker) Announce(ctx context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	client, err := t.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.Announce(ctx, req)
}

func (t *publicUDPTracker) Scrape(ctx context.Context, req tracker.ScrapeRequest) (*tracker.ScrapeResponse, error) {
	client, err := t.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.Scrape(ctx, req)
}

// checkPublicTrackerAddr extends the shared web-source guard with deprecated
// IPv6 site-local addresses (fec0::/10), which are still LAN destinations but
// are not reported by net.IP.IsPrivate.
func checkPublicTrackerAddr(ip net.IP, port int) error {
	if err := netx.CheckPublicTCPAddr(ip, port); err != nil {
		return err
	}
	if ip.To4() == nil {
		if v6 := ip.To16(); v6 != nil && v6[0] == 0xfe && v6[1]&0xc0 == 0xc0 {
			return fmt.Errorf("%w: %s", netx.ErrPrivateAddress, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		}
	}
	return nil
}
