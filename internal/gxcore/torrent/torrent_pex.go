package torrent

import (
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

func (t *torrent) pexAddPeer(addr *net.TCPAddr) {
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Add(addr)
		}
	}
}

func (t *torrent) pexDropPeer(addr *net.TCPAddr) {
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Drop(addr)
		}
	}
}

// pexCandidates returns the candidate peers of a received ut_pex message (BEP
// 11): only the "added"/"added6" lists. "dropped"/"dropped6" list peers that
// left the swarm and are informational, so they are never added as candidates
// (doing so would make us dial peers that are gone). A malformed list is
// skipped without discarding the other one.
func pexCandidates(msg peerprotocol.ExtensionPEXMessage) (added4, added6 []*net.TCPAddr) {
	if a, err := tracker.DecodePeersCompact([]byte(msg.Added)); err == nil {
		added4 = a
	}
	if a6, err := tracker.DecodePeersCompact6([]byte(msg.Added6)); err == nil {
		added6 = a6
	}
	return added4, added6
}
