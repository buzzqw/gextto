package externalip

import (
	"net"
	"slices"

	"github.com/cenkalti/log"
)

var ips []net.IP

func init() {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Warningln("cannot get interface addresses:", err)
		return
	}
	for _, addr := range addrs {
		in, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if i4 := in.IP.To4(); i4 != nil {
			if isPublicIP(i4) {
				ips = append(ips, i4)
			}
			continue
		}
		if isPublicIPv6(in.IP) {
			ips = append(ips, in.IP)
		}
	}
}

// isPublicIPv6 reports whether ip is a global IPv6 address that belongs to this
// host (so we do not dial ourselves). ULA (fc00::/7), link-local and loopback
// are excluded.
func isPublicIPv6(ip net.IP) bool {
	if ip == nil || ip.To4() != nil || ip.To16() == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate()
}

func isPublicIP(ip4 net.IP) bool {
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

// IsExternal returns true if the given IP matches one of the IP address of the external network interfaces on the server.
func IsExternal(ip net.IP) bool {
	return slices.ContainsFunc(ips, func(i net.IP) bool {
		return ip.Equal(i)
	})
}

// FirstExternalIP returns the first external IP of the network interfaces on the server.
func FirstExternalIP() net.IP {
	if len(ips) == 0 {
		return nil
	}
	return ips[0]
}
