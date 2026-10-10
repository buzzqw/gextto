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
	if pd, ok := t.pieceDownloaders[pe]; ok {
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
}

func (t *torrent) closeWebseedDownloader(src *webseedsource.WebseedSource) {
	t.piecePicker.CloseWebseedDownloader(src)
}

func (t *torrent) closePieceDownloader(pd *piecedownloader.PieceDownloader) {
	pe := pd.Peer.(*peer.Peer)
	_, open := t.pieceDownloaders[pe]
	if !open {
		return
	}
	delete(t.pieceDownloaders, pe)
	delete(t.pieceDownloadersSnubbed, pe)
	delete(t.pieceDownloadersChoked, pe)
	if t.piecePicker != nil {
		t.piecePicker.HandleCancelDownload(pe, pd.Piece.Index)
	}
	pe.Downloading = false
	if t.session.ram != nil {
		t.session.ram.Release(int64(t.info.PieceLength))
	}
}

func (t *torrent) closeInfoDownloader(id *infodownloader.InfoDownloader) {
	delete(t.infoDownloaders, id.Peer.(*peer.Peer))
	delete(t.infoDownloadersSnubbed, id.Peer.(*peer.Peer))
}
