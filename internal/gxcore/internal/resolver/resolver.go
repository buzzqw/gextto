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
	// ErrBlocked indicates that the resolved IP is blocked in the blocklist.
	ErrBlocked = errors.New("ip is blocked")
	// ErrNotIPv4Address indicates that the resolved IP address is not IPv4.
	ErrNotIPv4Address = errors.New("not ipv4 address")
	// ErrInvalidPort indicates that the port number in the address is invalid.
	ErrInvalidPort = errors.New("invalid port number")
)

// Resolve `hostport` to an IP address (preferring IPv4, falling back to IPv6).
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
		ip, err = resolveIP(ctx, timeout, host)
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

// resolveIP resolves `host` preferring an IPv4 address and falling back to IPv6.
func resolveIP(ctx context.Context, timeout time.Duration, host string) (net.IP, error) {
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
