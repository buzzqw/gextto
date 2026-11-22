package torrent

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecewriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
)

// sessionBanTTL quarantines a peer that served a corrupt piece from the
// whole session (libtorrent-style smart ban).
const sessionBanTTL = 30 * time.Minute

// maxPendingWrites caps concurrent piece writes of one torrent. The session
// semaphore still bounds disk parallelism globally; this per-torrent ring
// bounds the in-flight piece buffers (RAM) while letting the network flow
// during writes. The picker skips Writing pieces, so none is written twice.
const maxPendingWrites = 4

// beginPieceWrite opens a write slot, suspending the piece feeds when the
// ring fills.
func (t *torrent) beginPieceWrite() {
	t.pendingWrites++
	if t.pendingWrites >= maxPendingWrites {
		t.pieceMessagesC.Suspend()
		t.webseedPieceResultC.Suspend()
	}
}

// endPieceWrite closes a write slot, resuming the feeds on room.
func (t *torrent) endPieceWrite() {
	if t.pendingWrites > 0 {
		t.pendingWrites--
	}
	if t.pendingWrites < maxPendingWrites {
		t.pieceMessagesC.Resume()
		t.webseedPieceResultC.Resume()
	}
}

// cancelDuplicateDownloads releases the peers still fetching piece i now
// that one complete copy is being written (endgame or stalled duplicates).
// Their slots move to other pieces since the picker skips Writing ones; a
// failed hash re-picks the piece as usual. The requester set is cloned:
// closing a downloader mutates it (swap-with-last) and would skip peers.
func (t *torrent) cancelDuplicateDownloads(i uint32) {
	if t.piecePicker == nil {
		return
	}
	for _, pe := range slices.Clone(t.piecePicker.RequestedPeers(i)) {
		pd := t.pieceDownloaders[pe][i]
		if pd == nil {
			continue
		}
		t.closePieceDownloader(pd)
		pd.CancelPending()
		t.fillPeerDownloads(pe)
	}
}

// webseedPieceWrite is the source of a piece a web seed delivered. Like
// peerPieceWrite it carries the write-cache charge the buffer took, so the
// write refunds it when the bytes are out.
type webseedPieceWrite struct {
	downloader *urldownloader.URLDownloader
	units      int64
}

// peerPieceWrite is the source of a piece a peer handed to the write ring. It
// keeps the peer for reporting and carries the write-cache charge the buffer
// still owns: the reservation its downloader took when it opened.
//
// The charge follows the buffer. The downloader refunds it when the buffer
// dies with the downloader (handOffPieceDownloader is the only path that hands
// the buffer over); from there the write owns it, because the bytes stay
// resident until the writer releases them. A webseed piece keeps its
// downloader as source and carries no charge: it never took one.
type peerPieceWrite struct {
	peer  *peer.Peer
	units int64
}

// writeSourcePeer names the peer behind a piece write, when a peer delivered
// it. Both forms are accepted so a hand-built writer (tests, or a caller that
// does not own a charge) keeps working.
func writeSourcePeer(source any) (*peer.Peer, bool) {
	switch src := source.(type) {
	case peerPieceWrite:
		return src.peer, true
	case *peer.Peer:
		return src, true
	}
	return nil, false
}

// pieceRAMUnits is one piece buffer's write-cache reservation. It is always a
// full piece length, even for the short tail piece (see
// startPieceDownloaderFor), so every refund must use the same unit.
func (t *torrent) pieceRAMUnits() int64 {
	if info := t.info(); info != nil {
		return int64(info.PieceLength)
	}
	return 0
}

// releaseWrittenPiece returns a finished piece buffer to the pool and refunds
// the write-cache charge it carried. The order matters: the budget must not
// show the bytes free while they are still resident. It is the only refund for
// a piece that reached the write ring, so success, hash failure, write error
// and a partial-merge retry all pass through it exactly once.
func (t *torrent) releaseWrittenPiece(pw *piecewriter.PieceWriter) {
	pw.Buffer.Release()
	if t.session == nil || t.session.ram == nil {
		return
	}
	switch w := pw.Source.(type) {
	case peerPieceWrite:
		if w.units > 0 {
			t.session.ram.Release(w.units)
		}
	case webseedPieceWrite:
		if w.units > 0 {
			t.session.ram.Release(w.units)
		}
	}
}

// runPieceWrite hands one finished piece to the write ring on its own
// goroutine and forwards the outcome to the run loop. The private result
// channel never drops a piece: when the torrent closes before the loop takes
// it, the buffer and its charge are released here, because nothing else would.
func (t *torrent) runPieceWrite(pw *piecewriter.PieceWriter) {
	resultC := make(chan *piecewriter.PieceWriter, 1)
	go func() {
		pw.Run(resultC, nil, t.session.metrics.WritesPerSecond, t.session.metrics.SpeedWrite, t.session.semWrite)
		select {
		case t.pieceWriterResultC <- pw:
		case <-t.doneC:
			t.releaseWrittenPiece(pw)
		}
	}()
}

// handlePieceWriteDone settles one finished write: corrupt pieces ban or
// disable their source and restart downloads; storage errors stop the
// torrent with the enriched error; good pieces complete, fan out haves, and
// may complete the torrent.
func (t *torrent) handlePieceWriteDone(pw *piecewriter.PieceWriter) {
	pw.Piece.Writing = false
	t.endPieceWrite()
	t.releaseWrittenPiece(pw)

	_, prefilled := t.partialBlocks[pw.Piece.Index]
	delete(t.partialBlocks, pw.Piece.Index)
	if _, fromPeer := writeSourcePeer(pw.Source); !pw.HashOK && prefilled && fromPeer {
		// Part of the piece came from local data (Phase 3 merge): the
		// mismatch cannot be blamed on the peer. Drop the local blocks and
		// download the piece whole; a corrupt peer is caught on that copy.
		// (A webseed fetches whole pieces, so its failure stays its own.)
		t.bytesWasted.Inc(int64(len(pw.Buffer.Data)))
		t.log.Infof("piece #%d with reused local blocks failed its hash: downloading it whole", pw.Piece.Index)
		t.startPieceDownloaders()
		return
	}
	if !pw.HashOK {
		t.punishCorruptSource(pw)
		t.startPieceDownloaders()
		return
	}
	if pw.Error != nil {
		t.stop(t.storageWriteError(pw.Piece.Index, pw.Error))
		return
	}
	t.adoptVerifiedPiece(pw)
}

// punishCorruptSource reacts to a failed hash: peers are disconnected and
// session-banned, webseeds disabled, anything else crashes (a bug — every
// source kind is known).
func (t *torrent) punishCorruptSource(pw *piecewriter.PieceWriter) {
	t.bytesWasted.Inc(int64(len(pw.Buffer.Data)))
	pe, fromPeer := writeSourcePeer(pw.Source)
	urlSrc, fromURL := pw.Source.(*urldownloader.URLDownloader)
	if t.session != nil {
		ev := t.baseEvent(EventPieceHashFailed)
		ev.Piece = pw.Piece.Index
		if fromPeer {
			ev.Peer = pe.Addr().String()
		} else if fromURL {
			ev.Source = urlSrc.URL
		}
		t.session.emit(ev)
	}
	switch {
	case fromPeer:
		t.log.Debugln("received corrupt piece from peer", pe.String())
		t.closePeer(pe)
		rememberPeerIP(t.bannedPeerIPs, pe.IP())
		if t.session != nil {
			t.session.BanIP(pe.IP(), sessionBanTTL)
		}
	case fromURL:
		t.log.Debugln("received corrupt piece from webseed", urlSrc.URL)
		// disableSource closes the downloader and frees its slot.
		t.disableSource(urlSrc.URL, errors.New("corrupt piece"), false, 0)
	default:
		t.crash("unhandled piece source")
	}
}

// adoptVerifiedPiece records a good piece: bitfield, peer notifications
// (haves to whoever lacks it, interest refresh for all), webseed cursor
// advance, duplicate-fetch cancellation, and the completion check.
func (t *torrent) adoptVerifiedPiece(pw *piecewriter.PieceWriter) {
	index := pw.Piece.Index
	pw.Piece.Done = true
	if t.bitfield.Test(index) {
		t.crash(fmt.Sprintf("already have the piece #%d", index))
	}
	t.mBitfield.Lock()
	t.bitfield.Set(index)
	t.mBitfield.Unlock()
	t.emitPiece(index)

	if t.piecePicker != nil {
		t.advanceWebseedPast(pw.Source, index)
		t.cancelDuplicateDownloads(index)
	}
	for pe := range t.peers {
		t.updateInterestedState(pe)
		if pe.Bitfield.Test(index) {
			continue // they have it: spare the have message
		}
		pe.SendMessage(peerprotocol.HaveMessage{Index: index})
	}
	if t.checkCompletion() {
		t.log.Info("download completed")
		t.emit(EventTorrentCompleted, nil)
		if err := t.writeBitfield(); err != nil {
			t.stop(err)
		} else if t.stopAfterDownload {
			t.stopAndSetStoppedOnComplete()
		}
	}
}

// advanceWebseedPast moves a webseed cursor past a piece the swarm delivered
// first. Only pieces the webseed itself was not serving qualify; when the
// cursor closes the downloader its slot is freed and a new range restarts on
// it.
func (t *torrent) advanceWebseedPast(source any, index uint32) {
	_, fromWebseed := source.(*urldownloader.URLDownloader)
	src := t.piecePicker.RequestedWebseedSource(index)
	if fromWebseed || src == nil {
		return
	}
	if closed := t.piecePicker.WebseedStopAt(src, index); closed {
		t.log.Debugf("closed webseed downloader: %s", src.URL)
		t.webseedActiveDownloads--
		t.startPieceDownloaderForWebseed(src)
	}
}

// storageWriteError tags a piece-write failure with the storage root — a bad
// disk or mount reads differently from a network problem — counts it for the
// session, and keeps the cause for errors.Is/As.
func (t *torrent) storageWriteError(index uint32, err error) error {
	if t.storage == nil {
		return err
	}
	if t.session != nil {
		t.session.storageErrors.Add(1)
	}
	return fmt.Errorf("storage %q could not write piece #%d: %w", t.storage.RootDir(), index, err)
}
