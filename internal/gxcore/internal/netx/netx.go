// Package netx centralizes the engine's outbound connections so that
// an outgoing interface (VPN killswitch) and a proxy apply to peers,
// trackers and web seeds alike.
//
// Settings are process-wide: gx-torrent runs one session at a time.
package netx

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
)

// Settings are the outbound rules.
type Settings struct {
	// Interface binds outgoing connections to an interface name (e.g. wg0)
	// or a local IPv4 address. When it is set and has no IPv4 address, every
	// connection fails: nothing leaks outside the VPN.
	Interface string
	// Proxy is socks5://[user:pass@]host:port or http://[user:pass@]host:port.
	Proxy *url.URL
}

var (
	mu      sync.RWMutex
	current Settings
)

// Configure replaces the outbound rules.
func Configure(s Settings) {
	mu.Lock()
	current = s
	mu.Unlock()
}

// Current returns the outbound rules.
func Current() Settings {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// ErrInterfaceDown is returned while the outgoing interface has no address.
var ErrInterfaceDown = errors.New("outgoing interface has no IPv4 address")

// ErrUDPProxied is returned for UDP traffic when a proxy is set: a TCP proxy
// cannot carry it and sending it directly would bypass the proxy.
var ErrUDPProxied = errors.New("UDP disabled while a proxy is configured")

// InterfaceIP resolves an interface name or literal IP to its IPv4 address.
func InterfaceIP(name string) (net.IP, error) {
	if name == "" {
		return nil, nil
	}
	if ip := net.ParseIP(name); ip != nil {
		return ip, nil
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInterfaceDown, name)
	}
	if iface.Flags&net.FlagUp == 0 {
		return nil, fmt.Errorf("%w: %s is down", ErrInterfaceDown, name)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			return ipnet.IP.To4(), nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrInterfaceDown, name)
}

func localDialer(base net.Dialer, s Settings) (*net.Dialer, error) {
	d := base
	if s.Interface != "" {
		ip, err := InterfaceIP(s.Interface)
		if err != nil {
			return nil, err
		}
		d.LocalAddr = &net.TCPAddr{IP: ip}
	}
	return &d, nil
}

// DialContext opens a TCP connection honoring the interface and the proxy.
func DialContext(ctx context.Context, base net.Dialer, network, addr string) (net.Conn, error) {
	s := Current()
	d, err := localDialer(base, s)
	if err != nil {
		return nil, err
	}
	if s.Proxy == nil {
		return d.DialContext(ctx, network, addr)
	}
	switch s.Proxy.Scheme {
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if s.Proxy.User != nil {
			password, _ := s.Proxy.User.Password()
			auth = &proxy.Auth{User: s.Proxy.User.Username(), Password: password}
		}
		dialer, err := proxy.SOCKS5("tcp", s.Proxy.Host, auth, d)
		if err != nil {
			return nil, err
		}
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, network, addr)
		}
		return dialer.Dial(network, addr)
	case "http", "https":
		return dialConnect(ctx, d, s.Proxy, addr)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", s.Proxy.Scheme)
	}
}

// dialConnect tunnels a TCP connection through an HTTP proxy (CONNECT).
func dialConnect(ctx context.Context, d *net.Dialer, proxyURL *url.URL, addr string) (net.Conn, error) {
	conn, err := d.DialContext(ctx, "tcp", proxyURL.Host)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	request := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n"
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		token := base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username() + ":" + password))
		request += "Proxy-Authorization: Basic " + token + "\r\n"
	}
	request += "\r\n"
	if _, err := conn.Write([]byte(request)); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("proxy CONNECT %s: %s", addr, resp.Status)
	}
	if reader.Buffered() > 0 {
		conn.Close()
		return nil, errors.New("proxy sent data before the tunnel was ready")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// UDPLocalAddr returns the local address for UDP sockets (trackers). It
// refuses UDP while a proxy is configured.
func UDPLocalAddr() (*net.UDPAddr, error) {
	s := Current()
	if s.Proxy != nil {
		return nil, ErrUDPProxied
	}
	if s.Interface == "" {
		return &net.UDPAddr{}, nil
	}
	ip, err := InterfaceIP(s.Interface)
	if err != nil {
		return nil, err
	}
	return &net.UDPAddr{IP: ip}, nil
}

// DialDirectContext opens a TCP connection honoring only the outgoing
// interface, never the proxy. HTTP transports set the proxy themselves
// (http.Transport.Proxy) so a plain HTTP request reaches an HTTP proxy in its
// native form instead of a CONNECT tunnel, which many proxies refuse on port
// 80.
func DialDirectContext(ctx context.Context, base net.Dialer, network, addr string) (net.Conn, error) {
	s := Current()
	d, err := localDialer(base, s)
	if err != nil {
		return nil, err
	}
	return d.DialContext(ctx, network, addr)
}

// HTTPTransport is an HTTP transport that honors the outbound rules. A
// configured proxy is used for public hosts only: localhost, LAN and
// single-label service names are reached directly, because a proxy cannot
// improve a connection to a service on the same machine or network.
func HTTPTransport() *http.Transport {
	s := Current()
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return DialDirectContext(ctx, net.Dialer{}, network, addr)
		},
	}
	if s.Proxy != nil {
		// http, https and socks5/socks5h are handled by net/http: HTTPS uses
		// CONNECT, plain HTTP keeps its absolute form, and a SOCKS5 proxy gets
		// the hostname (the same remote resolution DialContext uses).
		tr.Proxy = func(req *http.Request) (*url.URL, error) {
			if IsLocalHost(req.URL.Hostname()) {
				return nil, nil
			}
			return s.Proxy, nil
		}
	}
	return tr
}

// IsLocalHost reports whether host is a destination a proxy must not see:
// loopback, a private or link-local IP, "localhost", a single-label service
// name (a Docker service, for example) or a .local/.lan/.home/.internal name.
// It never resolves a name: deciding whether to use the proxy must not leak a
// DNS query for a public host.
func IsLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return IsPrivateAddress(ip)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	for _, suffix := range []string{".local", ".lan", ".home", ".internal"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return !strings.Contains(host, ".")
}

// utpDial is the uTP dialer of the session (nil = TCP only); utpOnly skips
// TCP for outgoing peers.
var (
	utpDial func(ctx context.Context, addr string) (net.Conn, error)
	utpOnly bool
)

// Outgoing and incoming peer connections by transport.
var (
	OutgoingUTP atomic.Int64
	OutgoingTCP atomic.Int64
	IncomingUTP atomic.Int64
	IncomingTCP atomic.Int64
)

// SetUTPDialer enables uTP for peer connections (nil disables it). With only
// set, outgoing peers use uTP alone.
func SetUTPDialer(dial func(ctx context.Context, addr string) (net.Conn, error), only bool) {
	mu.Lock()
	utpDial = dial
	utpOnly = only && dial != nil
	mu.Unlock()
}

// Dial preference: per-host memory of which transport won recent dial races,
// so the next race gives the winner a head start instead of opening two
// connections every time. The loser still races (delayed, never skipped),
// so peers speaking a single transport always connect.
const (
	// preferMinSamples is the remembered races before a transport earns a
	// head start.
	preferMinSamples = 4
	// preferMinShare is the winner share, in eighths, required for a head
	// start (6/8 = 75%).
	preferMinShare = 6
	// preferMaxSamples caps the remembered races per host; beyond it both
	// counters halve, so the memory tracks recent conditions instead of
	// going stale.
	preferMaxSamples = 64
	// maxDialPrefs bounds the per-host memory.
	maxDialPrefs = 4096
)

// preferHeadStart is the preferred transport's head start. A var so tests
// can widen it for assertability.
var preferHeadStart = 150 * time.Millisecond

type transportPref struct {
	utpWins, tcpWins int
	// utpEMA/tcpEMA are the winning-latency moving averages, kept for
	// future delay sizing; the head start itself stays fixed.
	utpEMA, tcpEMA time.Duration
}

var (
	prefMu    sync.Mutex
	dialPrefs = map[string]*transportPref{}
)

// DialLatencyEMA reports the recent winning-dial latency moving average
// for the host of addr, preferring the uTP leg. It seeds the uTP initial
// RTO: a handshake spans roughly one RTT plus processing, so it is a safe
// (slightly conservative) RTT estimate.
func DialLatencyEMA(addr string) (time.Duration, bool) {
	prefMu.Lock()
	defer prefMu.Unlock()
	p := dialPrefs[dialHost(addr)]
	if p == nil {
		return 0, false
	}
	if p.utpEMA > 0 {
		return p.utpEMA, true
	}
	if p.tcpEMA > 0 {
		return p.tcpEMA, true
	}
	return 0, false
}

// dialHost keys the dial preference by host: peers on one host share the
// network path whatever their port.
func dialHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// preferredDialTransport reports "utp" or "tcp" when one transport dominates
// recent races to host, else "".
func preferredDialTransport(host string) string {
	prefMu.Lock()
	defer prefMu.Unlock()
	p := dialPrefs[host]
	if p == nil {
		return ""
	}
	total := p.utpWins + p.tcpWins
	if total < preferMinSamples {
		return ""
	}
	if p.utpWins*8 >= total*preferMinShare {
		return "utp"
	}
	if p.tcpWins*8 >= total*preferMinShare {
		return "tcp"
	}
	return ""
}

// noteDialWinner records a race winner with its connect latency.
func noteDialWinner(host, winner string, latency time.Duration) {
	if winner != "utp" && winner != "tcp" {
		return
	}
	prefMu.Lock()
	defer prefMu.Unlock()
	p := dialPrefs[host]
	if p == nil {
		if len(dialPrefs) >= maxDialPrefs {
			for k := range dialPrefs {
				delete(dialPrefs, k)
				break
			}
		}
		p = &transportPref{}
		dialPrefs[host] = p
	}
	if p.utpWins+p.tcpWins >= preferMaxSamples {
		p.utpWins /= 2
		p.tcpWins /= 2
	}
	if winner == "utp" {
		p.utpWins++
		if p.utpEMA == 0 {
			p.utpEMA = latency
		} else {
			p.utpEMA = (p.utpEMA*3 + latency) / 4
		}
		return
	}
	p.tcpWins++
	if p.tcpEMA == 0 {
		p.tcpEMA = latency
	} else {
		p.tcpEMA = (p.tcpEMA*3 + latency) / 4
	}
}

// DialPeer connects to a peer. With uTP enabled (and no proxy) uTP and TCP
// are tried at the same time and the first connection wins, so peers that
// speak only one of the two are reached without waiting for a timeout.
func DialPeer(ctx context.Context, base net.Dialer, network, addr string) (net.Conn, error) {
	mu.RLock()
	dialUTP := utpDial
	only := utpOnly
	proxied := current.Proxy != nil
	mu.RUnlock()
	if dialUTP == nil || proxied {
		conn, err := DialContext(ctx, base, network, addr)
		if err == nil {
			OutgoingTCP.Add(1)
		}
		return conn, err
	}
	if only {
		conn, err := dialUTP(ctx, addr)
		if err != nil {
			return nil, err
		}
		OutgoingUTP.Add(1)
		return TrackUTP(NewUTPConn(conn)), nil
	}
	if base.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, base.Timeout)
		defer cancel()
	}
	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		conn net.Conn
		err  error
	}
	results := make(chan result, 2)
	// runLeg dials after an optional head-start delay. A leg cancelled
	// while waiting reports the cancellation, so the race always collects
	// both results and never leaks the goroutine.
	runLeg := func(delay time.Duration, dial func(context.Context) (net.Conn, error), wrap bool) {
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-raceCtx.Done():
				results <- result{nil, raceCtx.Err()}
				return
			case <-timer.C:
			}
		}
		conn, err := dial(raceCtx)
		if err == nil && wrap {
			conn = TrackUTP(NewUTPConn(conn))
		}
		results <- result{conn, err}
	}
	// The recent winner gets a head start; the loser still races delayed,
	// so single-transport peers connect at worst preferHeadStart later.
	host := dialHost(addr)
	pref := preferredDialTransport(host)
	start := time.Now()
	utpDelay, tcpDelay := time.Duration(0), time.Duration(0)
	switch pref {
	case "utp":
		tcpDelay = preferHeadStart
	case "tcp":
		utpDelay = preferHeadStart
	}
	go runLeg(utpDelay, func(ctx context.Context) (net.Conn, error) {
		return dialUTP(ctx, addr)
	}, true)
	go runLeg(tcpDelay, func(ctx context.Context) (net.Conn, error) {
		return DialContext(ctx, base, network, addr)
	}, false)
	var firstErr error
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err == nil {
			winner := "tcp"
			if IsUTP(r.conn) {
				OutgoingUTP.Add(1)
				winner = "utp"
			} else {
				OutgoingTCP.Add(1)
			}
			noteDialWinner(host, winner, time.Since(start))
			cancel()
			if i == 0 {
				// Close the loser if it connects anyway.
				go func() {
					if other := <-results; other.err == nil {
						other.conn.Close()
					}
				}()
			}
			return r.conn, nil
		}
		if firstErr == nil {
			firstErr = r.err
		}
	}
	return nil, firstErr
}

// UTPConn presents a uTP connection with TCP addresses: the rest of the engine
// keys peers by *net.TCPAddr. The converted addresses are snapshotted once at
// wrap time: RemoteAddr/LocalAddr sit on hot paths (handshake, peer setup,
// logging, stats) and must not convert and allocate per call.
type UTPConn struct {
	net.Conn
	remote net.Addr
	local  net.Addr
}

// NewUTPConn wraps a connected uTP conn, snapshotting its TCP-view
// addresses. The peer address of a connection never changes, so the snapshot
// stays valid for the conn's life.
func NewUTPConn(raw net.Conn) *UTPConn {
	return &UTPConn{Conn: raw, remote: tcpAddrOf(raw.RemoteAddr()), local: tcpAddrOf(raw.LocalAddr())}
}

func tcpAddrOf(addr net.Addr) net.Addr {
	if addr == nil {
		return nil
	}
	if udp, ok := addr.(*net.UDPAddr); ok {
		return &net.TCPAddr{IP: udp.IP, Port: udp.Port, Zone: udp.Zone}
	}
	if resolved, err := net.ResolveTCPAddr("tcp", addr.String()); err == nil {
		return resolved
	}
	return addr
}

// RemoteAddr returns the peer address as *net.TCPAddr (snapshotted).
func (c *UTPConn) RemoteAddr() net.Addr { return c.remote }

// LocalAddr returns the local address as *net.TCPAddr (snapshotted).
func (c *UTPConn) LocalAddr() net.Addr { return c.local }

// IsUTP reports whether a connection runs over uTP.
func IsUTP(conn net.Conn) bool {
	_, ok := conn.(*UTPConn)
	return ok
}

// utpPeers records the remote addresses of open uTP connections, so peer
// listings can tell the transport after encryption wrapped the connection.
var utpPeers sync.Map

func TrackUTP(c *UTPConn) *UTPConn {
	utpPeers.Store(c.RemoteAddr().String(), struct{}{})
	return c
}

// Close closes the connection and forgets its address.
func (c *UTPConn) Close() error {
	utpPeers.Delete(c.RemoteAddr().String())
	return c.Conn.Close()
}

// IsUTPAddr reports whether the peer at addr is connected over uTP.
func IsUTPAddr(addr string) bool {
	_, ok := utpPeers.Load(addr)
	return ok
}

// UTPPeerCount returns how many peer connections are currently open over uTP.
func UTPPeerCount() int {
	count := 0
	utpPeers.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
