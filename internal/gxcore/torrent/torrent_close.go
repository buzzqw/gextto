package torrent

import (
	"errors"

	"github.com/buzzqw/gextto/internal/gxcore/internal/infodownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
)

var errClosed = errors.New("torrent is closed")

func (t *torrent) close() {
	// Stop if running.
	t.stop(errClosed)

	// Maybe we are in "Stopping" state. Leave the "stopped" event announcer running. Closing it
	// cancels the announce in flight, so the trackers never learn how much we have uploaded since
	// the last periodical announce. It stops by itself after TrackerStopTimeout.
	// Session.Close waits for it before closing the tracker transports.
	if a := t.stoppedEventAnnouncer; a != nil {
		t.session.detachedAnnouncers.Go(func() { <-a.Done() })
	}

	t.downloadSpeed.Stop()
	t.uploadSpeed.Stop()
}

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
			// Put the chunk back so another peer fetches it.
			req.file.failChunk(req.index / req.file.chunk)
		}
	}
	delete(t.v2NoHashPeers, pe)
	if t.piecePicker != nil {
		t.piecePicker.HandleDisconnect(pe)
	}
	t.unchoker.HandleDisconnect(pe)
	t.pexDropPeer(pe.Addr())
	t.dialAddresses()
	t.session.metrics.Peers.Dec(1)
	// A peer that goes away while the torrent is running is worth reporting;
	// the mass close during a stop is not (it would flood subscribers).
	if s := t.status(); s != Stopping && s != Stopped {
		t.emitPeer(EventPeerDisconnected, pe.Addr().String())
	}
}

func (t *torrent) closeWebseedDownloader(src *webseedsource.WebseedSource) {
	t.piecePicker.CloseWebseedDownloader(src)
}

func (t *torrent) closePieceDownloader(pd *piecedownloader.PieceDownloader) {
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
	if t.session.ram != nil {
		t.session.ram.Release(int64(t.info().PieceLength))
	}
}

func (t *torrent) closeInfoDownloader(id *infodownloader.InfoDownloader) {
	delete(t.infoDownloaders, id.Peer.(*peer.Peer))
	delete(t.infoDownloadersSnubbed, id.Peer.(*peer.Peer))
}
