package torrent

import (
	"fmt"

	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecepicker"
)

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

	// If we already have bitfield from resume db, skip verification and start downloading.
	if t.bitfield != nil && !al.HasMissing {
		for i := uint32(0); i < t.bitfield.Len(); i++ {
			t.pieces[i].Done = t.bitfield.Test(i)
		}
		t.resetCompletionIfWantedMissing()
		if t.checkCompletion() && t.stopAfterDownload {
			t.stopAndSetStoppedOnComplete()
			return
		}
		t.processQueuedMessages()
		t.addFixedPeers()
		t.startAcceptor()
		t.startAnnouncers()
		t.startPieceDownloaders()
		return
	}

	// Swarm Merging: other local torrents that could lend verified pieces.
	// Only the cheap checks run here; the reads, hashes and writes happen in
	// the verifier goroutine (startMergeVerifier), off the run loop.
	var candidates []*Torrent
	if t.session != nil {
		candidates = t.mergeCandidates(t.session.ListTorrents())
	}

	// No need to verify files if they didn't exist when we create them and
	// nothing can be merged.
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

	// Some files exists on the disk (or can be merged), need to verify pieces
	// to create a correct bitfield. Storage created empty holds only what the
	// merge writes, so then nothing else is read.
	t.startMergeVerifier(candidates, !al.HasExisting)
}
