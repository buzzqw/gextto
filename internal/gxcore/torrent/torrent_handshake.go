package torrent

import (
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/incominghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
)

func (t *torrent) getSKey(sKeyHash [20]byte) []byte {
	if sKeyHash == t.sKeyHash {
		return t.infoHash[:]
	}
	return nil
}

func (t *torrent) checkInfoHash(infoHash [20]byte) bool {
	return infoHash == t.infoHash
}

func (t *torrent) handleIncomingHandshakeDone(ih *incominghandshaker.IncomingHandshaker) {
	delete(t.incomingHandshakers, ih)
	if ih.Error != nil {
		delete(t.connectedPeerIPs, ih.Conn.RemoteAddr().(*net.TCPAddr).IP.String())
		return
	}
	t.startPeer(ih.Conn, peersource.Incoming, t.incomingPeers, ih.PeerID, ih.Extensions, ih.Cipher)
}

func (t *torrent) handleOutgoingHandshakeDone(oh *outgoinghandshaker.OutgoingHandshaker) {
	delete(t.outgoingHandshakers, oh)
	if oh.Error != nil {
		delete(t.connectedPeerIPs, oh.Addr.IP.String())
		// A direct dial failed: ask a connected peer to introduce us to the
		// endpoint (gextto fork, BEP 55), then keep trying the normal pool.
		t.tryHolepunchRendezvous(oh.Addr)
		t.dialAddresses()
		return
	}
	t.startPeer(oh.Conn, oh.Source, t.outgoingPeers, oh.PeerID, oh.Extensions, oh.Cipher)
}
