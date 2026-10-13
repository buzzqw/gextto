package torrent

import (
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
)

// syncPieces flushes every open data file. It must run on the run loop —
// t.files belongs to it — and it precedes every resume-bitfield write, so a
// persisted bit always describes bytes already durable (no O_SYNC writes).
func (t *torrent) syncPieces() {
	for _, f := range t.files {
		if f.Storage == nil {
			continue
		}
		if err := f.Storage.Sync(); err != nil {
			t.log.Errorf("cannot sync %q: %s", f.Name, err)
		}
	}
}

// writeBitfield persists a snapshot of the verified bitfield. The order is
// load-bearing: snapshot, then flush data, then write — a stored bit never
// anticipates undurable data.
func (t *torrent) writeBitfield() error {
	var snapshot []byte
	t.mBitfield.RLock()
	if t.bitfield != nil {
		snapshot = append([]byte(nil), t.bitfield.Bytes()...)
	}
	t.mBitfield.RUnlock()
	t.syncPieces()
	if snapshot == nil {
		return nil
	}
	if err := t.session.resumer.WriteBitfield(t.id, snapshot); err != nil {
		t.log.Errorf("cannot write bitfield to resume db: %s", err)
		return err
	}
	return nil
}

// checkCompletion flips a torrent to seed the first time every wanted piece
// lands. The transition tears down the download machinery (handshakers,
// webseeds, picker, address pool), tells peers we lost interest (BEP 21
// re-handshake), drops the uninterested, and fires the completion hook once.
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
		// A closed handshaker never reports back; free its IP now.
		delete(t.connectedPeerIPs, h.Addr.IP.String())
	}
	t.outgoingHandshakers = make(map[*outgoinghandshaker.OutgoingHandshaker]struct{})
	t.stopWebseedDownloads()
	for pe := range t.peers {
		if pe.ExtensionsEnabled {
			t.sendExtensionHandshake(pe)
		}
	}
	for pe := range t.peers {
		if !pe.PeerInterested {
			t.closePeer(pe)
		}
	}
	t.addrList.Reset()
	for _, m := range t.pieceDownloaders {
		for _, pd := range m {
			t.closePieceDownloader(pd)
			pd.CancelPending()
		}
	}
	t.piecePicker = nil
	t.updateSeedDuration(time.Now())
	if t.superSeeding {
		t.superSeedAllPeers()
	}
	if !t.completeCmdRun && len(t.session.config.OnCompleteCmd) > 0 {
		go t.session.runOnCompleteCmd(t)
		t.completeCmdRun = true
		if err := t.session.resumer.WriteCompleteCmdRun(t.id); err != nil {
			t.stop(err)
		}
	}
	return true
}
