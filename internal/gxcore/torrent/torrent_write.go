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

// handlePieceWriteDone settles one finished write: corrupt pieces ban or
// disable their source and restart downloads; storage errors stop the
// torrent with the enriched error; good pieces complete, fan out haves, and
// may complete the torrent.
func (t *torrent) handlePieceWriteDone(pw *piecewriter.PieceWriter) {
	pw.Piece.Writing = false
	t.endPieceWrite()
	pw.Buffer.Release()

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
	switch src := pw.Source.(type) {
	case *peer.Peer:
		t.log.Debugln("received corrupt piece from peer", src.String())
		t.closePeer(src)
		t.bannedPeerIPs[src.IP()] = struct{}{}
		if t.session != nil {
			t.session.BanIP(src.IP(), sessionBanTTL)
		}
	case *urldownloader.URLDownloader:
		t.log.Debugln("received corrupt piece from webseed", src.URL)
		t.disableSource(src.URL, errors.New("corrupt piece"), false)
		// disableSource closed the webseed downloader: free its slot.
		t.webseedActiveDownloads--
	default:
		t.crash("unhandled piece source")
	}
}

// adoptVerifiedPiece records a good piece: bitfield, peer notifications
// (haves to whoever lacks it, interest refresh for all), webseed cursor
// advance, duplicate-fetch cancellation, and the completion check.
func (t *torrent) adoptVerifiedPiece(pw *piecewriter.PieceWriter) {
	index := pw.Piece.Index
	t.pieces[index].Done = true
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
