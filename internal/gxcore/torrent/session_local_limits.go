package torrent

import (
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
)

// SetLimitLocalPeers applies (true) or lifts (false) the speed limits for the
// peers on the local network. The change reaches the peers already connected:
// they hold the torrent's local limiters, whose inheritance is switched here.
func (s *Session) SetLimitLocalPeers(on bool) {
	s.limitLocalPeers.Store(on)
	for _, t := range s.ListTorrents() {
		t.torrent.applyLocalPeerLimits(on)
	}
}

// LimitLocalPeers reports whether the speed limits apply to LAN peers.
func (s *Session) LimitLocalPeers() bool {
	return s.limitLocalPeers.Load()
}

// applyLocalPeerLimits points the local limiters at the torrent limiters (on)
// or leaves them unlimited (off). SetParent is atomic, so it is safe from any
// goroutine while peers keep taking from them.
func (t *torrent) applyLocalPeerLimits(on bool) {
	if t.bucketLocalDownload == nil || t.bucketLocalUpload == nil {
		return
	}
	if on {
		t.bucketLocalDownload.SetParent(t.bucketDownload)
		t.bucketLocalUpload.SetParent(t.bucketUpload)
		return
	}
	t.bucketLocalDownload.SetParent(nil)
	t.bucketLocalUpload.SetParent(nil)
}

// peerLimiters picks the limiters a new peer holds: the local ones for a peer
// on the local network, the torrent ones otherwise.
func (t *torrent) peerLimiters(addr net.Addr) (download, upload *bandwidth.Limiter) {
	if isLocalPeerAddr(addr) && t.bucketLocalDownload != nil {
		return t.bucketLocalDownload, t.bucketLocalUpload
	}
	return t.bucketDownload, t.bucketUpload
}

// isLocalPeerAddr reports a peer on the local network: loopback, link-local
// or private (RFC 1918, IPv6 ULA fc00::/7), over TCP or uTP.
func isLocalPeerAddr(addr net.Addr) bool {
	var ip net.IP
	switch a := addr.(type) {
	case *net.TCPAddr:
		ip = a.IP
	case *net.UDPAddr:
		ip = a.IP
	default:
		if addr == nil {
			return false
		}
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return false
		}
		ip = net.ParseIP(host)
	}
	return ip != nil && isLocalNetworkAddr(ip)
}
