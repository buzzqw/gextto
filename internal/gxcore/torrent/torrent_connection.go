package torrent

import (
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/incominghandshaker"
)

// handleNewConnection vets an accepted socket through the admission gates —
// capacity, blocklist, duplicate IP, session ban — and handshakes the
// survivors. Rejections close the socket immediately and say why at debug
// level; the IP is only pinned once a handshaker owns it.
func (t *torrent) handleNewConnection(conn net.Conn) {
	remote := conn.RemoteAddr().String()
	if len(t.incomingHandshakers)+len(t.incomingPeers) >= t.session.config.MaxPeerAccept ||
		(t.maxConnections > 0 && len(t.peers) >= t.maxConnections) {
		t.log.Debugln("peer limit reached, rejecting peer", remote)
		conn.Close()
		return
	}
	ip := conn.RemoteAddr().(*net.TCPAddr).IP
	ipstr := ip.String()
	if t.session.config.BlocklistEnabledForIncomingConnections && t.session.blocklist != nil && t.session.blocklist.Blocked(ip) {
		t.log.Debugln("peer is blocked:", remote)
		conn.Close()
		return
	}
	if _, ok := t.connectedPeerIPs[ipstr]; ok {
		t.log.Debugln("received duplicate connection from same IP: ", ipstr)
		conn.Close()
		return
	}
	if t.ipBanned(ipstr) {
		t.log.Debugln("connection attempt from banned IP: ", ipstr)
		conn.Close()
		return
	}
	h := incominghandshaker.New(conn)
	t.incomingHandshakers[h] = struct{}{}
	t.connectedPeerIPs[ipstr] = struct{}{}
	go h.Run(
		t.peerID,
		t.getSKey,
		t.checkInfoHash,
		t.incomingHandshakerResultC,
		t.session.config.PeerHandshakeTimeout,
		t.ourExtensions(),
		t.session.config.ForceIncomingEncryption,
	)
}
