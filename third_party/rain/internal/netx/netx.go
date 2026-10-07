// Package netx centralizes rain's outbound connections (gextto fork) so that
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

// HTTPTransport is an HTTP transport that honors the outbound rules.
func HTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return DialContext(ctx, net.Dialer{}, network, addr)
		},
	}
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
)

// SetUTPDialer enables uTP for peer connections (nil disables it). With only
// set, outgoing peers use uTP alone.
func SetUTPDialer(dial func(ctx context.Context, addr string) (net.Conn, error), only bool) {
	mu.Lock()
	utpDial = dial
	utpOnly = only && dial != nil
	mu.Unlock()
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
		return TrackUTP(&UTPConn{Conn: conn}), nil
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
	go func() {
		conn, err := dialUTP(raceCtx, addr)
		if err == nil {
			conn = TrackUTP(&UTPConn{Conn: conn})
		}
		results <- result{conn, err}
	}()
	go func() {
		conn, err := DialContext(raceCtx, base, network, addr)
		results <- result{conn, err}
	}()
	var firstErr error
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err == nil {
			if IsUTP(r.conn) {
				OutgoingUTP.Add(1)
			} else {
				OutgoingTCP.Add(1)
			}
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

// UTPConn presents a uTP connection with TCP addresses: the rest of rain
// keys peers by *net.TCPAddr.
type UTPConn struct {
	net.Conn
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

// RemoteAddr returns the peer address as *net.TCPAddr.
func (c *UTPConn) RemoteAddr() net.Addr { return tcpAddrOf(c.Conn.RemoteAddr()) }

// LocalAddr returns the local address as *net.TCPAddr.
func (c *UTPConn) LocalAddr() net.Addr { return tcpAddrOf(c.Conn.LocalAddr()) }

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
