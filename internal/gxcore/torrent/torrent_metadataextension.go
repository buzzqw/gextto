package torrent

import (
	"errors"
	"fmt"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/infodownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
)

// handleMetadataMessage serves and consumes the BEP 9 metadata exchange.
// Requests need a prior handshake advertising the metadata extension and a
// known info dict (else reject); data blocks feed the active downloader and,
// once complete and hash-matched, become the torrent's info — unless the
// torrent still needs its v2 piece layers (BEP 52) or was told to stop at
// metadata.
func (t *torrent) handleMetadataMessage(pe *peer.Peer, msg peerprotocol.ExtensionMetadataMessage) {
	switch msg.Type {
	case peerprotocol.ExtensionMetadataMessageTypeRequest:
		t.serveMetadataPiece(pe, msg)
	case peerprotocol.ExtensionMetadataMessageTypeData:
		t.ingestMetadataPiece(pe, msg)
	case peerprotocol.ExtensionMetadataMessageTypeReject:
		if id, ok := t.infoDownloaders[pe]; ok {
			t.closePeer(id.Peer.(*peer.Peer))
			t.startInfoDownloaders()
		}
	}
}

// serveMetadataPiece answers one metadata block request, rejecting when the
// peer never handshaked the extension or the info dict is still unknown.
func (t *torrent) serveMetadataPiece(pe *peer.Peer, msg peerprotocol.ExtensionMetadataMessage) {
	if pe.ExtensionHandshake == nil {
		return
	}
	extMsgID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata]
	if !ok {
		return
	}
	if t.info() == nil {
		t.sendMetadataReject(pe, msg.Piece, extMsgID)
		return
	}
	data, ok := metadataPiece(t.info().Bytes, msg.Piece)
	if !ok {
		t.sendMetadataReject(pe, msg.Piece, extMsgID)
		return
	}
	pe.SendMessage(peerprotocol.ExtensionMessage{
		ExtendedMessageID: extMsgID,
		Payload: peerprotocol.ExtensionMetadataMessage{
			Type:      peerprotocol.ExtensionMetadataMessageTypeData,
			Piece:     msg.Piece,
			TotalSize: len(t.info().Bytes),
			Data:      data,
		},
	})
}

// ingestMetadataPiece stores one received block and advances the fetch:
// more requests while incomplete, info installation once complete.
func (t *torrent) ingestMetadataPiece(pe *peer.Peer, msg peerprotocol.ExtensionMetadataMessage) {
	id, ok := t.infoDownloaders[pe]
	if !ok {
		return
	}
	if err := id.GotBlock(msg.Piece, msg.Data); err != nil {
		pe.Logger().Error(err)
		t.closePeer(pe)
		t.startInfoDownloaders()
		return
	}
	if !id.Done() {
		pe.Snubbed = false
		delete(t.infoDownloadersSnubbed, pe)
		id.RequestBlocks(t.maxAllowedRequests(pe))
		pe.ResetSnubTimer()
		return
	}
	pe.StopSnubTimer()
	t.installMetadata(id)
}

// installMetadata validates and adopts a fully downloaded info dict:
// info-hash match, parseable, non-private. Then it persists the info,
// completes the metadata waiters (or stops at metadata), and either starts
// the v2 layer download or the allocator.
func (t *torrent) installMetadata(id *infodownloader.InfoDownloader) {
	// A v2 torrent is identified by the first 20 bytes of the SHA-256 of
	// the info dict; v1 (and the v1 side of a hybrid) by its SHA-1.
	if !t.matchesInfoHash(id.Bytes) {
		pe := id.Peer.(*peer.Peer)
		pe.Logger().Errorln("received info does not match with hash")
		t.closePeer(pe)
		t.startInfoDownloaders()
		return
	}
	t.stopInfoDownloaders()

	info, err := t.session.parseInfo(id.Bytes, boltdbresumer.LatestVersion, nil)
	if err != nil {
		t.stop(fmt.Errorf("cannot parse info bytes: %w", err))
		return
	}
	if info.Private {
		t.stop(errors.New("private torrent from magnet"))
		return
	}
	// The content layout chosen at add time becomes the storage paths now,
	// before anything is allocated.
	if t.applyContentLayout(info) {
		if err := t.session.resumer.WriteFilePaths(t.id, t.filePaths); err != nil {
			t.stop(fmt.Errorf("cannot write resume file paths: %w", err))
			return
		}
	}
	t.setInfo(info)
	t.piecePool = bufferpool.New(int(info.PieceLength))
	if err := t.session.resumer.WriteInfo(t.id, t.info().Bytes); err != nil {
		t.stop(fmt.Errorf("cannot write resume info: %w", err))
		return
	}
	select {
	case <-t.completeMetadataC:
	default:
		close(t.completeMetadataC)
	}
	if t.stopAfterMetadata {
		t.stopAndSetStoppedOnMetadata()
		return
	}
	if info.NeedsV2Layers() && !info.HasPieceHashes() {
		// A v2 magnet: the info dict is known but the per-file piece
		// layers still have to come from peers (BEP 52) before any piece
		// can be built or verified.
		t.startV2LayerDownload()
		return
	}
	t.startAllocator()
}

// metadataPieceSize is the BEP 9 metadata block size.
const metadataPieceSize = 16 * 1024

// metadataPiece slices block piece out of the info dict, or reports absence.
// The offset is 64-bit: a hostile 32-bit index must not wrap and serve the
// wrong block instead of being rejected.
func metadataPiece(data []byte, piece uint32) ([]byte, bool) {
	start := uint64(piece) * metadataPieceSize
	if start >= uint64(len(data)) {
		return nil, false
	}
	end := min(start+metadataPieceSize, uint64(len(data)))
	return data[start:end], true
}

func (t *torrent) sendMetadataReject(pe *peer.Peer, i uint32, msgID uint8) {
	pe.SendMessage(peerprotocol.ExtensionMessage{
		ExtendedMessageID: msgID,
		Payload: &peerprotocol.ExtensionMetadataMessage{
			Type:  peerprotocol.ExtensionMetadataMessageTypeReject,
			Piece: i,
		},
	})
}
