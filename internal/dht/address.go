package dht

import (
	"fmt"
	"net"
	"strconv"
)

// encodeCompactPeerAddr encodes hostPort ("host:port") in the BitTorrent
// compact peer format: 4 address bytes + 2 port bytes for IPv4, 16 + 2 for
// IPv6. It returns an empty string when hostPort is not a valid IP and port.
func encodeCompactPeerAddr(hostPort string) string {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return ""
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		b := make([]byte, net.IPv4len+2)
		copy(b, v4)
		b[4] = byte(p >> 8)
		b[5] = byte(p)
		return string(b)
	}
	b := make([]byte, net.IPv6len+2)
	copy(b, ip.To16())
	b[16] = byte(p >> 8)
	b[17] = byte(p)
	return string(b)
}

// decodeCompactPeerAddr is the inverse of encodeCompactPeerAddr. It returns an
// empty string when the input is neither 6 nor 18 bytes long.
func decodeCompactPeerAddr(bin string) string {
	switch len(bin) {
	case net.IPv4len + 2:
		return fmt.Sprintf("%s:%d", net.IP(bin[:net.IPv4len]).String(),
			uint16(bin[4])<<8|uint16(bin[5]))
	case net.IPv6len + 2:
		return fmt.Sprintf("[%s]:%d", net.IP(bin[:net.IPv6len]).String(),
			uint16(bin[16])<<8|uint16(bin[17]))
	default:
		return ""
	}
}
