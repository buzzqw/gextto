package externalip

import (
	"net"
	"slices"

	"github.com/buzzqw/gextto/internal/gxcore/logging"
)

// ips caches the host's public interface addresses at startup, so the
// session can recognize (and refuse to dial) itself.
var ips []net.IP

func init() {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		logging.Warningln("cannot get interface addresses:", err)
		return
	}
	for _, addr := range addrs {
		in, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if i4 := in.IP.To4(); i4 != nil {
			if isPublicIPv4(i4) {
				ips = append(ips, i4)
			}
			continue
		}
		if isPublicIPv6(in.IP) {
			ips = append(ips, in.IP)
		}
	}
}

// isPublicIPv6 reports global unicast IPv6 addresses: no loopback,
// link-local, unspecified, ULA or private ranges.
func isPublicIPv6(ip net.IP) bool {
	if ip == nil || ip.To4() != nil || ip.To16() == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate()
}

// isPublicIPv4 reports routable IPv4 addresses: no loopback, link-local or
// RFC 1918 ranges.
func isPublicIPv4(ip4 net.IP) bool {
	if ip4.IsLoopback() || ip4.IsLinkLocalMulticast() || ip4.IsLinkLocalUnicast() {
		return false
	}
	switch {
	case ip4[0] == 10:
		return false
	case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
		return false
	case ip4[0] == 192 && ip4[1] == 168:
		return false
	default:
		return true
	}
}

// IsExternal reports whether ip is one of this host's own public interface
// addresses.
func IsExternal(ip net.IP) bool {
	return slices.ContainsFunc(ips, func(i net.IP) bool {
		return ip.Equal(i)
	})
}

// FirstExternalIP returns an own public address, or nil on hosts without one.
func FirstExternalIP() net.IP {
	if len(ips) == 0 {
		return nil
	}
	return ips[0]
}
