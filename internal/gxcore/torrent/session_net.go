package torrent

// session_net.go (gextto fork, F4 IPv6): the listening sockets prefer
// dual-stack, so one port serves both IPv4 and IPv6 peers like qBittorrent.
//
// An unspecified host ("", "0.0.0.0" or "::") opens a dual-stack socket and
// falls back to IPv4 when the machine has no IPv6. A specific address binds its
// own family. IPv4-mapped peers keep their dotted-quad form in net.IP.String(),
// so the existing IP-keyed maps (connectedPeerIPs, bans) stay consistent.

import (
	"net"
	"strconv"
)

// listenTCPOn binds a TCP listener for the given host and port.
func listenTCPOn(host string, port int) (*net.TCPListener, error) {
	ip := net.ParseIP(host)
	switch {
	case ip != nil && ip.To4() != nil && !ip.IsUnspecified():
		return net.ListenTCP("tcp4", &net.TCPAddr{IP: ip.To4(), Port: port})
	case ip != nil && ip.To4() == nil && !ip.IsUnspecified():
		return net.ListenTCP("tcp6", &net.TCPAddr{IP: ip, Port: port})
	default:
		// Unspecified: prefer a dual-stack socket, fall back to IPv4-only.
		l, err := net.ListenTCP("tcp", &net.TCPAddr{Port: port})
		if err != nil {
			return net.ListenTCP("tcp4", &net.TCPAddr{Port: port})
		}
		return l, nil
	}
}

// listenUDPOn binds a UDP socket for the given host and port. The socket is
// shared by uTP and the DHT.
func listenUDPOn(host string, port int) (net.PacketConn, error) {
	ip := net.ParseIP(host)
	switch {
	case ip != nil && ip.To4() != nil && !ip.IsUnspecified():
		return net.ListenPacket("udp4", net.JoinHostPort(ip.To4().String(), strconv.Itoa(port)))
	case ip != nil && ip.To4() == nil && !ip.IsUnspecified():
		return net.ListenPacket("udp6", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
	default:
		pc, err := net.ListenPacket("udp", net.JoinHostPort("", strconv.Itoa(port)))
		if err != nil {
			return net.ListenPacket("udp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
		}
		return pc, nil
	}
}
