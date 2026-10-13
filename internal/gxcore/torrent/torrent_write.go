package torrent

import (
	"errors"
	"fmt"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecewriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
)

// sessionBanTTL is how long a peer that sent a corrupt piece stays banned from
// the whole session (libtorrent-style smart ban).
const sessionBanTTL = 30 * time.Minute

// maxPendingWrites bounds how many pieces of one torrent may be written at the
// same time. Config.ParallelWrites (the session's semWrite) still caps the disk
// parallelism globally; this per-torrent bound limits the in-flight piece
// buffers (RAM). With it above 1 the network keeps flowing while pieces are
// written, instead of the old stop-the-world "one write per torrent". The picker
// skips a piece marked Writing, so no piece is ever written twice.
const maxPendingWrites = 4

// beginPieceWrite records a piece write in flight and suspends the piece
// channels once the ring buffer is full.
func (t *torrent) beginPieceWrite() {
	t.pendingWrites++
	if t.pendingWrites >= maxPendingWrites {
		t.pieceMessagesC.Suspend()
		t.webseedPieceResultC.Suspend()
	}
}

// endPieceWrite records a finished piece write and resumes the piece channels
// when the ring buffer has room again.
func (t *torrent) endPieceWrite() {
	if t.pendingWrites > 0 {
		t.pendingWrites--
	}
	if t.pendingWrites < maxPendingWrites {
		t.pieceMessagesC.Resume()
		t.webseedPieceResultC.Resume()
	}
}

func (t *torrent) handlePieceWriteDone(pw *piecewriter.PieceWriter) {
	pw.Piece.Writing = false

	t.endPieceWrite()

	pw.Buffer.Release()

	if !pw.HashOK {
		t.bytesWasted.Inc(int64(len(pw.Buffer.Data)))
		switch src := pw.Source.(type) {
		case *peer.Peer:
			t.log.Debugln("received corrupt piece from peer", src.String())
			t.closePeer(src)
			t.bannedPeerIPs[src.IP()] = struct{}{}
			// ban the corrupt peer session-wide for a while, so it
			// cannot poison the other torrents either (libtorrent-style smart ban).
			if t.session != nil {
				t.session.BanIP(src.IP(), sessionBanTTL)
			}
		case *urldownloader.URLDownloader:
			t.log.Debugln("received corrupt piece from webseed", src.URL)
			t.disableSource(src.URL, errors.New("corrupt piece"), false)
			// disableSource closed the webseed downloader, so free its slot.
			t.webseedActiveDownloads--
		default:
			t.crash("unhandled piece source")
		}
		t.startPieceDownloaders()
		return
	}
	if pw.Error != nil {
		t.stop(t.storageWriteError(pw.Piece.Index, pw.Error))
		return
	}

	pw.Piece.Done = true
	if t.bitfield.Test(pw.Piece.Index) {
		t.crash(fmt.Sprintf("already have the piece #%d", pw.Piece.Index))
	}
	t.mBitfield.Lock()
	t.bitfield.Set(pw.Piece.Index)
	t.mBitfield.Unlock()
	t.emitPiece(pw.Piece.Index)

	if t.piecePicker != nil {
		_, ok := pw.Source.(*urldownloader.URLDownloader)
		src := t.piecePicker.RequestedWebseedSource(pw.Piece.Index)
		if !ok && src != nil {
			closed := t.piecePicker.WebseedStopAt(src, pw.Piece.Index)
			if closed {
				t.log.Debugf("closed webseed downloader: %s", src.URL)
				// WebseedStopAt closed the downloader, so free its slot before
				// trying to start a new range (which re-takes a slot on success).
				t.webseedActiveDownloads--
				t.startPieceDownloaderForWebseed(src)
			}
		}

		for _, pe := range t.piecePicker.RequestedPeers(pw.Piece.Index) {
			pd2 := t.pieceDownloaders[pe][pw.Piece.Index]
			if pd2 == nil {
				continue
			}
			t.closePieceDownloader(pd2)
			pd2.CancelPending()
			t.fillPeerDownloads(pe)
		}
	}

	// Tell everyone that we have this piece
	for pe := range t.peers {
		t.updateInterestedState(pe)
		if pe.Bitfield.Test(pw.Piece.Index) {
			// Skip peers having the piece to save bandwidth
			continue
		}
		msg := peerprotocol.HaveMessage{Index: pw.Piece.Index}
		pe.SendMessage(msg)
	}

	completed := t.checkCompletion()
	if completed {
		t.log.Info("download completed")
		t.emit(EventTorrentCompleted, nil)
		err := t.writeBitfield()
		if err != nil {
			t.stop(err)
		} else if t.stopAfterDownload {
			t.stopAndSetStoppedOnComplete()
		}
	}
}

// storageWriteError wraps a piece-write failure with the storage root, so a bad
// disk or mount is identifiable (and not mistaken for a network problem), and
// counts it in the session. The original error is kept for errors.Is/As.
func (t *torrent) storageWriteError(index uint32, err error) error {
	if t.storage == nil {
		return err
	}
	if t.session != nil {
		t.session.storageErrors.Add(1)
	}
	return fmt.Errorf("storage %q could not write piece #%d: %w", t.storage.RootDir(), index, err)
}
