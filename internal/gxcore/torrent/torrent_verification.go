package torrent

import (
	"fmt"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/verifier"
)

// handleVerifyCommand re-verifies on demand: immediately when stopped (the
// bitfield is dropped and the torrent restarts into verification), or by
// stopping first when running.
func (t *torrent) handleVerifyCommand() {
	t.log.Info("verifying")
	t.doVerify = true
	if t.status() == Stopped {
		t.mBitfield.Lock()
		t.bitfield = nil
		t.mBitfield.Unlock()
		t.start()
	} else {
		t.stop(nil)
	}
}

// handleVerificationDone installs the verified bitfield and resumes the
// pipeline: haves fan out to peers, downloads restart, or the torrent stops
// when this was a manual verification or a completed stop-after-download.
func (t *torrent) handleVerificationDone(ve *verifier.Verifier) {
	if t.verifier != ve {
		t.crash("invalid verifier")
	}
	t.verifier = nil
	if out := t.mergeOut; out != nil {
		t.mergeOut = nil
		t.partialBlocks = out.partial
	}

	if ve.Error != nil {
		t.stop(fmt.Errorf("file verification error: %w", ve.Error))
		return
	}
	t.mBitfield.Lock()
	t.bitfield = ve.Bitfield
	t.mBitfield.Unlock()

	if err := t.writeBitfield(); err != nil {
		t.stop(err)
		return
	}

	var haveMessages []peerprotocol.HaveMessage
	for i := uint32(0); i < t.bitfield.Len(); i++ {
		if t.bitfield.Test(i) {
			t.pieces[i].Done = true
			haveMessages = append(haveMessages, peerprotocol.HaveMessage{Index: i})
		}
	}

	// Verification may demote a seed back to downloader when wanted pieces
	// turn out missing.
	if !t.wantedComplete() {
		t.completed = false
		if t.completeC == nil {
			t.completeC = make(chan struct{})
		}
	}

	if t.doVerify {
		t.doVerify = false
		t.stop(nil)
		return
	}
	for pe := range t.peers {
		for _, msg := range haveMessages {
			pe.SendMessage(msg)
		}
		t.updateInterestedState(pe)
	}
	if t.checkCompletion() && t.stopAfterDownload {
		t.stopAndSetStoppedOnComplete()
		return
	}
	t.processQueuedMessages()
	t.addFixedPeers()
	t.startAcceptor()
	t.startAnnouncers()
	t.startPieceDownloaders()
}
