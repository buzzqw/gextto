package torrent

import (
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// pexAddPeer gossips a discovered address to every PEX-capable peer.
func (t *torrent) pexAddPeer(addr *net.TCPAddr) {
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Add(addr)
		}
	}
}

// pexDropPeer gossips a vanished address to every PEX-capable peer.
func (t *torrent) pexDropPeer(addr *net.TCPAddr) {
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Drop(addr)
		}
	}
}

// pexCandidates extracts dial candidates from a ut_pex message (BEP 11):
// the added/added6 lists only. The dropped lists describe peers that left
// and are never dialled. Each family decodes independently, so one corrupt
// list does not discard the other.
func pexCandidates(msg peerprotocol.ExtensionPEXMessage) (added4, added6 []*net.TCPAddr) {
	if a, err := tracker.DecodePeersCompact([]byte(msg.Added)); err == nil {
		added4 = a
	}
	if a6, err := tracker.DecodePeersCompact6([]byte(msg.Added6)); err == nil {
		added6 = a6
	}
	return added4, added6
}
