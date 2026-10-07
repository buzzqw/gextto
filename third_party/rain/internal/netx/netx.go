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
