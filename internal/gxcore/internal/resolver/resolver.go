package resolver

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
)

var (
	// ErrBlocked rejects an address whose IP is blocklisted.
	ErrBlocked = errors.New("ip is blocked")
	// ErrNotIPv4Address reports a resolution with no usable address.
	ErrNotIPv4Address = errors.New("not ipv4 address")
	// ErrInvalidPort rejects out-of-range ports.
	ErrInvalidPort = errors.New("invalid port number")
)

// Resolve parses hostport, resolves the host (literals skip DNS), and
// validates the result: IPv4 preferred with IPv6 fallback, blocklist
// enforced. It returns the dialable IP and port.
func Resolve(ctx context.Context, hostport string, timeout time.Duration, bl *blocklist.Blocklist) (net.IP, int, error) {
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, 0, err
	}
	if port <= 0 || port > 65535 {
		return nil, 0, ErrInvalidPort
	}
	ip := net.ParseIP(host)
	if ip == nil {
		ip, err = lookupIP(ctx, timeout, host)
		if err != nil {
			return nil, 0, err
		}
	}
	if i4 := ip.To4(); i4 != nil {
		ip = i4
	}
	if bl != nil && bl.Blocked(ip) {
		return nil, 0, ErrBlocked
	}
	return ip, port, nil
}

// lookupIP resolves host with a deadline, preferring IPv4 records and
// accepting IPv6 only when no IPv4 exists.
func lookupIP(ctx context.Context, timeout time.Duration, host string) (net.IP, error) {
	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ia := range addrs {
		if i4 := ia.IP.To4(); i4 != nil {
			return i4, nil
		}
	}
	for _, ia := range addrs {
		if ia.IP.To4() == nil && ia.IP.To16() != nil {
			return ia.IP, nil
		}
	}
	return nil, ErrNotIPv4Address
}
