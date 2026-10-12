package netx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
)

// ErrPrivateAddress is returned when a URL taken from torrent or magnet data
// (an xs=/as= metadata source, a web seed) points to a private, loopback or
// link-local address.
var ErrPrivateAddress = errors.New("refusing to connect to a private or local address")

// privateNets are the destinations a URL from untrusted torrent data must not
// reach: a magnet from an RSS feed would otherwise make the client request
// pages of the router, the NAS or other services on the LAN (SSRF).
var privateNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8",      // "this" network
		"10.0.0.0/8",     // RFC 1918
		"100.64.0.0/10",  // carrier-grade NAT
		"127.0.0.0/8",    // loopback
		"169.254.0.0/16", // link-local
		"172.16.0.0/12",  // RFC 1918
		"192.0.0.0/24",   // IETF protocol assignments
		"192.168.0.0/16", // RFC 1918
		"198.18.0.0/15",  // benchmarking
		"224.0.0.0/4",    // multicast
		"240.0.0.0/4",    // reserved, broadcast
		"::/128",         // unspecified
		"::1/128",        // loopback
		"fc00::/7",       // unique local
		"fe80::/10",      // link-local
		"ff00::/8",       // multicast
		"64:ff9b:1::/48", // local-use NAT64
		"2001:db8::/32",  // documentation
	} {
		_, n, err := net.ParseCIDR(cidr)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// IsPrivateAddress reports whether ip is private, loopback, link-local,
// multicast or otherwise not a public internet address. An IPv4-mapped IPv6
// address is checked as IPv4.
func IsPrivateAddress(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range privateNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// resolvePublic resolves host and returns its first public address, or
// ErrPrivateAddress when every address is private.
func resolvePublic(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if IsPrivateAddress(ip) {
			return nil, fmt.Errorf("%w: %s", ErrPrivateAddress, host)
		}
		return ip, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if !IsPrivateAddress(a.IP) {
			return a.IP, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrPrivateAddress, host)
}

// DialPublic connects like DialContext (outgoing interface, proxy) but only to
// a public address: the host is resolved here and the connection goes to the
// checked IP, so a DNS answer cannot switch to a private address afterwards.
func DialPublic(ctx context.Context, base net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip, err := resolvePublic(ctx, host)
	if err != nil {
		return nil, err
	}
	return DialContext(ctx, base, network, net.JoinHostPort(ip.String(), port))
}

// CheckPublicTCPAddr returns ErrPrivateAddress for a private destination.
func CheckPublicTCPAddr(ip net.IP, port int) error {
	if IsPrivateAddress(ip) {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
	}
	return nil
}

// PublicHTTPTransport is HTTPTransport restricted to public addresses, for
// URLs that come from torrent or magnet data rather than from the operator.
func PublicHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return DialPublic(ctx, net.Dialer{}, network, addr)
		},
	}
}
