package torrent

import (
	"errors"

	"github.com/buzzqw/gextto/internal/gxcore/internal/infodownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
)

var errClosed = errors.New("torrent is closed")

// close tears the torrent down: stop first, then park the in-flight stopped
// announce where Session.Close can still wait for it (cancelling it would
// hide our final upload totals from the trackers), then stop the meters.
func (t *torrent) close() {
	t.stop(errClosed)
	// The "stopped" announce goes out first, or the goodbye would travel on a
	// socket already closed: these trackers are released once it is done, and a
	// WebSocket tracker owns a socket nothing else would close before the
	// session ends.
	if a := t.stoppedEventAnnouncer; a != nil {
		t.session.detachedAnnouncers.Go(func() {
			<-a.Done()
			t.releaseTrackers()
		})
	} else {
		t.releaseTrackers()
	}
	t.downloadSpeed.Stop()
	t.uploadSpeed.Stop()
}

// releaseTrackers closes every tracker this torrent announced to.
func (t *torrent) releaseTrackers() {
	if t.session.trackerManager == nil {
		return
	}
	for _, tr := range t.trackers {
		t.session.trackerManager.Release(tr)
	}
}

// closePeer disconnects one peer and unwinds everything attached to it:
// piece and metadata downloads (v2 hash chunks go back to their file for
// another peer), picker/unchoker/PEX bookkeeping, IP and id registries, and
// the session peer count. Departures from a running torrent notify
// subscribers; the mass close of a stop stays quiet.
func (t *torrent) closePeer(pe *peer.Peer) {
	if pe.Closed {
		return
	}
	pe.Close()
	pe.Closed = true
	for _, pd := range t.pieceDownloaders[pe] {
		t.closePieceDownloader(pd)
	}
	if id, ok := t.infoDownloaders[pe]; ok {
		t.closeInfoDownloader(id)
	}
	delete(t.peers, pe)
	delete(t.incomingPeers, pe)
	delete(t.outgoingPeers, pe)
	delete(t.peerIDs, pe.ID)
	delete(t.connectedPeerIPs, pe.IP())
	delete(t.superSeedPeers, pe)
	if req, ok := t.v2Pending[pe]; ok {
		delete(t.v2Pending, pe)
		if req != nil && req.file != nil {
			req.file.failChunk(req.index / req.file.chunk)
		}
	}
	delete(t.v2NoHashPeers, pe)
	if t.piecePicker != nil {
		t.piecePicker.HandleDisconnect(pe)
	}
	t.unchoker.HandleDisconnect(pe)
	t.pexDropPeer(pe.WatchAddr())
	t.dialAddresses()
	t.session.metrics.Peers.Dec(1)
	if s := t.status(); s != Stopping && s != Stopped {
		t.emitPeer(EventPeerDisconnected, pe.Addr().String())
	}
}

// closeWebseedDownloader detaches a webseed source from the picker.
func (t *torrent) closeWebseedDownloader(src *webseedsource.WebseedSource) {
	t.piecePicker.CloseWebseedDownloader(src)
}

// closePieceDownloader retires one piece fetch: registry cleanup, download
// flag and concurrency slot release, picker cancellation, and the RAM
// budget refund for the piece buffer. The buffer dies with the downloader
// here (choke, cancel, peer loss, endgame duplicate), so the refund is due.
func (t *torrent) closePieceDownloader(pd *piecedownloader.PieceDownloader) {
	t.retirePieceDownloader(pd, true)
}

// handOffPieceDownloader retires a piece fetch whose buffer moves to the write
// ring (completePieceDownload). The buffer is still resident, so the
// write-cache reservation must not be refunded here: ownership follows the
// buffer to the write, which refunds it in releaseWrittenPiece once the bytes
// are out — or earlier, when the torrent closes before the run loop takes the
// result. Refunding at handoff is what let maxPendingWrites piece buffers per
// torrent sit outside the budget.
func (t *torrent) handOffPieceDownloader(pd *piecedownloader.PieceDownloader) {
	t.retirePieceDownloader(pd, false)
}

// retirePieceDownloader drops one downloader from the registry and releases
// its picker and peer bookkeeping. releaseRAM is true when the buffer is
// dying with the downloader; false only for the handoff to the write ring,
// which inherits the reservation.
func (t *torrent) retirePieceDownloader(pd *piecedownloader.PieceDownloader, releaseRAM bool) {
	pe := pd.Peer.(*peer.Peer)
	m := t.pieceDownloaders[pe]
	if m == nil {
		return
	}
	if _, ok := m[pd.Piece.Index]; !ok {
		return
	}
	delete(m, pd.Piece.Index)
	pe.PipeRefillTried = false
	if len(m) == 0 {
		delete(t.pieceDownloaders, pe)
		pe.Downloading = false
	}
	if pe.ConcurrentPieces > 0 {
		pe.ConcurrentPieces--
	}
	if t.piecePicker != nil {
		t.piecePicker.HandleCancelDownload(pe, pd.Piece.Index)
	}
	if releaseRAM {
		t.releasePieceRAM()
	}
}

// releasePieceRAM refunds one piece buffer's write-cache reservation. The
// reservation is always a full piece length, even for the short tail piece
// (startPieceDownloaderFor asks for t.info().PieceLength), so the refund uses
// the same unit or the budget drifts.
func (t *torrent) releasePieceRAM() {
	if t.session == nil || t.session.ram == nil {
		return
	}
	if units := t.pieceRAMUnits(); units > 0 {
		t.session.ram.Release(units)
	}
}

// closeInfoDownloader forgets a metadata fetch, snubbed or not.
func (t *torrent) closeInfoDownloader(id *infodownloader.InfoDownloader) {
	delete(t.infoDownloaders, id.Peer.(*peer.Peer))
	delete(t.infoDownloadersSnubbed, id.Peer.(*peer.Peer))
}
