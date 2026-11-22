package torrent

// Which IPv6 address the announce publishes (BEP 7 "ipv6").
//
// A machine has several: link-local ones always, usually a global, often a
// temporary (privacy) one that rotates, sometimes a deprecated one during a
// prefix change. Publishing the wrong one is worse than publishing none: the
// tracker lists an endpoint nobody can reach, and a v6 peer wastes a dial on
// it. The rule is the one BiglyBT adopted in 4.1 — global unicast only, never a
// temporary or deprecated address — and when there is no global address the
// announce carries no ipv6 at all (a ULA is reachable only inside the LAN).

import (
	"bytes"
	"encoding/hex"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// announceIPv6TTL is how long the chosen address is reused: interfaces come
// and go and a privacy address rotates, but an announce happens every half hour
// at the earliest, so a fresh look per announce would be wasted work across
// hundreds of torrents.
const announceIPv6TTL = 5 * time.Minute

// announceIPv6 returns the address to publish, or nil. skip, when not nil,
// reports an address the system itself marks as unusable (temporary,
// deprecated, tentative).
func announceIPv6(candidates []net.IP, skip func(net.IP) bool) net.IP {
	for _, ip := range candidates {
		if ip == nil || ip.To4() != nil || !ip.IsGlobalUnicast() {
			continue
		}
		// ULA (fc00::/7): unique inside a site, unroutable outside it.
		if ip.IsPrivate() {
			continue
		}
		if skip != nil && skip(ip) {
			continue
		}
		return ip
	}
	return nil
}

// interfaceIPv6Addrs lists the IPv6 addresses of the up, non-loopback
// interfaces, in the order the system reports them.
func interfaceIPv6Addrs() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			out = append(out, ipnet.IP)
		}
	}
	return out
}

// Linux address flags from <linux/if_addr.h>, as /proc/net/if_inet6 reports
// them.
const (
	ifaFTemporary  = 0x01
	ifaFDeprecated = 0x20
	ifaFTentative  = 0x40
)

// systemIPv6Unusable returns the checker for the flags the system exposes, or
// nil where they cannot be read (every other platform): the caller then trusts
// the address class alone.
func systemIPv6Unusable() func(net.IP) bool {
	data, err := os.ReadFile("/proc/net/if_inet6")
	if err != nil {
		return nil
	}
	flags := parseIfInet6(data)
	if len(flags) == 0 {
		return nil
	}
	return func(ip net.IP) bool {
		f, ok := flags[ipKey(ip)]
		if !ok {
			return false
		}
		return f&(ifaFTemporary|ifaFDeprecated|ifaFTentative) != 0
	}
}

// ipKey renders an IPv6 address as the 16-byte key /proc/net/if_inet6 uses.
func ipKey(ip net.IP) [16]byte {
	var key [16]byte
	copy(key[:], ip.To16())
	return key
}

// parseIfInet6 reads the flags column of /proc/net/if_inet6:
// "fe80…01 02 40 20 80 eth0" — address, index, prefix length, scope, flags,
// device. Malformed lines are skipped.
func parseIfInet6(data []byte) map[[16]byte]uint8 {
	out := make(map[[16]byte]uint8)
	for _, line := range bytes.Split(data, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) < 5 {
			continue
		}
		var addr [16]byte
		raw, err := hex.DecodeString(fields[0])
		if err != nil || len(raw) != 16 {
			continue
		}
		copy(addr[:], raw)
		flags, err := strconv.ParseUint(fields[4], 16, 8)
		if err != nil {
			continue
		}
		out[addr] = uint8(flags)
	}
	return out
}

// listensIPv6 reports whether an incoming IPv6 connection can reach us: an
// empty or unspecified host is bound dual-stack, a literal is what it says.
func (s *Session) listensIPv6() bool {
	host := strings.TrimSpace(s.config.Host)
	if host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.To4() == nil
	}
	// A scoped literal (fe80::1%eth0): IPv6.
	return strings.Contains(host, ":")
}

// announceIPv6Address is the address an announce publishes, cached for
// announceIPv6TTL. It is empty when inbound IPv6 cannot work: publishing an
// address nobody can connect to only wastes a peer's dial.
func (s *Session) announceIPv6Address() string {
	if s.config.ListenPort <= 0 || !s.listensIPv6() {
		return ""
	}
	s.mAnnounce6.Lock()
	defer s.mAnnounce6.Unlock()
	if !s.announce6At.IsZero() && time.Since(s.announce6At) < announceIPv6TTL {
		return s.announce6
	}
	s.announce6 = ""
	if ip := announceIPv6(interfaceIPv6Addrs(), systemIPv6Unusable()); ip != nil {
		s.announce6 = ip.String()
	}
	s.announce6At = time.Now()
	return s.announce6
}
