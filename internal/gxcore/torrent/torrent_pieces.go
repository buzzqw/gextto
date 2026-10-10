package torrent

import (
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
)

func (t *torrent) writeBitfield() error {
	err := t.session.resumer.WriteBitfield(t.id, t.bitfield.Bytes())
	if err != nil {
		t.log.Errorf("cannot write bitfield to resume db: %s", err)
	}
	return err
}

func (t *torrent) checkCompletion() bool {
	if t.completed {
		return true
	}
	if !t.wantedComplete() {
		return false
	}
	t.completed = true
	close(t.completeC)
	for h := range t.outgoingHandshakers {
		h.Close()
		// a closed handshaker never reports back; release its IP.
		delete(t.connectedPeerIPs, h.Addr.IP.String())
	}
	t.outgoingHandshakers = make(map[*outgoinghandshaker.OutgoingHandshaker]struct{})
	t.stopWebseedDownloads()
	for pe := range t.peers {
		if !pe.PeerInterested {
			t.closePeer(pe)
		}
	}
	t.addrList.Reset()
	for _, pd := range t.pieceDownloaders {
		t.closePieceDownloader(pd)
		pd.CancelPending()
	}
	t.piecePicker = nil
	t.updateSeedDuration(time.Now())
	if t.superSeeding {
		// The torrent just became a seed: start super-seeding the peers that
		// are still connected.
		t.superSeedAllPeers()
	}
	if !t.completeCmdRun && len(t.session.config.OnCompleteCmd) > 0 {
		go t.session.runOnCompleteCmd(t)
		t.completeCmdRun = true
		err := t.session.resumer.WriteCompleteCmdRun(t.id)
		if err != nil {
			t.stop(err)
		}
	}
	return true
}
