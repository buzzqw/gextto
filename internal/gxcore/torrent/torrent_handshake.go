package torrent

import (
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/incominghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
)

// getSKey returns the torrent's info hash as the MSE stream key when the
// peer's key hash matches ours, else nil (no key for strangers).
func (t *torrent) getSKey(sKeyHash [20]byte) []byte {
	if sKeyHash == t.sKeyHash {
		return t.infoHash[:]
	}
	return nil
}

// checkInfoHash admits only handshakes offering our own info hash.
func (t *torrent) checkInfoHash(infoHash [20]byte) bool {
	return infoHash == t.infoHash
}

// handleIncomingHandshakeDone retires the handshaker and either starts the
// peer or forgets its IP, depending on the outcome.
func (t *torrent) handleIncomingHandshakeDone(ih *incominghandshaker.IncomingHandshaker) {
	delete(t.incomingHandshakers, ih)
	if ih.Error != nil {
		delete(t.connectedPeerIPs, ih.Conn.RemoteAddr().(*net.TCPAddr).IP.String())
		return
	}
	t.startPeer(ih.Conn, peersource.Incoming, t.incomingPeers, ih.PeerID, ih.Extensions, ih.Cipher)
}

// handleOutgoingHandshakeDone retires the handshaker and either starts the
// peer or, on a failed dial, asks the swarm for a holepunch introduction
// (BEP 55) and resumes the normal dial pool.
func (t *torrent) handleOutgoingHandshakeDone(oh *outgoinghandshaker.OutgoingHandshaker) {
	delete(t.outgoingHandshakers, oh)
	if oh.Error != nil {
		delete(t.connectedPeerIPs, oh.Addr.IP.String())
		t.tryHolepunchRendezvous(oh.Addr)
		t.dialAddresses()
		return
	}
	t.startPeer(oh.Conn, oh.Source, t.outgoingPeers, oh.PeerID, oh.Extensions, oh.Cipher)
}
