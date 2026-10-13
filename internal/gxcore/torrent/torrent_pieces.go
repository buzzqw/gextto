package torrent

import (
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
)

// syncPieces flushes every open data file of the torrent to stable storage. It
// must run on the torrent's run loop: t.files is owned by it. It is called
// before the resume bitfield is persisted so a bit that survives a crash always
// refers to piece data already on disk (the engine no longer relies on O_SYNC).
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

// writeBitfield persists the verified bitfield to the resume database.
// Invariant: snapshot the bitfield, then flush the data, then write the
// bitfield, so a persisted bit never anticipates data that is not yet durable.
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
	err := t.session.resumer.WriteBitfield(t.id, snapshot)
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
	// BEP 21: we are now a (partial) seed. Tell the connected peers we are not
	// interested in downloading anything, so they stop offering us pieces.
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
