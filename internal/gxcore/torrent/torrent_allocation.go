package torrent

import (
	"fmt"

	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecepicker"
)

// handleAllocationDone turns opened files into pieces, the picker and the
// starting state. From a resume bitfield with no missing files the torrent
// resumes downloading directly; from an empty storage with no mergeable
// neighbors it starts with a blank bitfield; otherwise the verifier (plus
// the off-loop swarm merge) establishes the truth first.
func (t *torrent) handleAllocationDone(al *allocator.Allocator) {
	if t.allocator != al {
		t.crash("invalid allocator")
	}
	t.allocator = nil

	if al.Error != nil {
		t.stop(fmt.Errorf("file allocation error: %w", al.Error))
		return
	}
	if t.files != nil {
		t.crash("files exist")
	}
	t.files = al.Files

	if t.pieces != nil {
		t.crash("pieces exists")
	}
	pieces := piece.NewPieces(t.info(), t.files)
	if len(pieces) == 0 {
		t.stop(fmt.Errorf("torrent has zero pieces"))
		return
	}
	t.pieces = pieces
	t.applySkippedPieces()
	t.publishLocalData()

	for pe := range t.peers {
		pe.GenerateAndSendAllowedFastMessages(t.session.config.AllowedFastSet, t.info().NumPieces, t.infoHash, t.pieces)
	}
	if t.piecePicker != nil {
		t.crash("piece picker exists")
	}
	t.piecePicker = piecepicker.New(t.pieces, t.session.config.EndgameMaxDuplicateDownloads, t.webseedSources, t.sequential, t.firstLast)
	for pe := range t.peers {
		pe.Bitfield = bitfield.New(t.info().NumPieces)
	}

	// Seed mode claims every piece instead of verifying: it then follows the
	// resume path below with a full bitfield.
	t.seedModeAllocated(al)

	if t.bitfield != nil && !al.HasMissing {
		for i := uint32(0); i < t.bitfield.Len(); i++ {
			t.pieces[i].Done = t.bitfield.Test(i)
		}
		t.resetCompletionIfWantedMissing()
		if t.checkCompletion() && t.stopAfterDownload {
			t.stopAndSetStoppedOnComplete()
			return
		}
		// Swarm Merging on the resume path: the merge is idempotent and
		// re-derives the Phase 3 partial blocks of the pieces still missing,
		// which a restart would otherwise download again. It runs off the run
		// loop with the reloaded bitfield as the already-verified set, so no
		// piece is re-read or re-hashed (Verifier.Skip/Only).
		if t.session != nil && !t.wantedComplete() {
			if candidates := t.mergeCandidates(t.session.ListTorrents()); len(candidates) > 0 {
				resume := append([]byte(nil), t.bitfield.Bytes()...)
				t.startMergeVerifier(candidates, false, resume)
				return
			}
		}
		t.processQueuedMessages()
		t.addFixedPeers()
		t.startAcceptor()
		t.startAnnouncers()
		t.startPieceDownloaders()
		return
	}

	// Swarm Merging: sibling torrents that could lend verified pieces. Only
	// the cheap candidate filter runs here; reads, hashes and writes happen
	// in the verifier goroutine (startMergeVerifier), off the run loop.
	var candidates []*Torrent
	if t.session != nil {
		candidates = t.mergeCandidates(t.session.ListTorrents())
	}
	if !al.HasExisting && len(candidates) == 0 {
		t.mBitfield.Lock()
		t.bitfield = bitfield.New(t.info().NumPieces)
		t.mBitfield.Unlock()
		t.processQueuedMessages()
		t.addFixedPeers()
		t.startAcceptor()
		t.startAnnouncers()
		t.startPieceDownloaders()
		return
	}

	// On-disk data (or mergeable neighbors) exists: verify into a correct
	// bitfield. A storage created empty this run can only hold what the
	// merge writes, so nothing else is hashed.
	t.startMergeVerifier(candidates, !al.HasExisting, nil)
}
