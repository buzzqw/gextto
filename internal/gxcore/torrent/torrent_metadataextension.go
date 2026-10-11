package torrent

import (
	"errors"
	"fmt"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
)

func (t *torrent) handleMetadataMessage(pe *peer.Peer, msg peerprotocol.ExtensionMetadataMessage) {
	switch msg.Type {
	case peerprotocol.ExtensionMetadataMessageTypeRequest:
		if pe.ExtensionHandshake == nil {
			// Peer sent a request without sending handshake first.
			break
		}
		extMsgID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata]
		if !ok {
			break
		}
		if t.info == nil {
			t.sendMetadataReject(pe, msg.Piece, extMsgID)
			break
		}
		data, ok := metadataPiece(t.info.Bytes, msg.Piece)
		if !ok {
			t.sendMetadataReject(pe, msg.Piece, extMsgID)
			break
		}
		dataMsg := peerprotocol.ExtensionMetadataMessage{
			Type:      peerprotocol.ExtensionMetadataMessageTypeData,
			Piece:     msg.Piece,
			TotalSize: len(t.info.Bytes),
			Data:      data,
		}
		extDataMsg := peerprotocol.ExtensionMessage{
			ExtendedMessageID: extMsgID,
			Payload:           dataMsg,
		}
		pe.SendMessage(extDataMsg)
	case peerprotocol.ExtensionMetadataMessageTypeData:
		id, ok := t.infoDownloaders[pe]
		if !ok {
			break
		}
		err := id.GotBlock(msg.Piece, msg.Data)
		if err != nil {
			pe.Logger().Error(err)
			t.closePeer(pe)
			t.startInfoDownloaders()
			break
		}
		if !id.Done() {
			pe.Snubbed = false
			delete(t.infoDownloadersSnubbed, pe)
			id.RequestBlocks(t.maxAllowedRequests(pe))
			pe.ResetSnubTimer()
			break
		}
		pe.StopSnubTimer()

		// A v2 torrent is identified by the first 20 bytes of the SHA-256 of
		// the info dict; v1 (and the v1 side of a hybrid) by its SHA-1.
		if !t.matchesInfoHash(id.Bytes) {
			pe.Logger().Errorln("received info does not match with hash")
			t.closePeer(id.Peer.(*peer.Peer))
			t.startInfoDownloaders()
			break
		}
		t.stopInfoDownloaders()

		info, err := t.session.parseInfo(id.Bytes, boltdbresumer.LatestVersion, nil)
		if err != nil {
			t.stop(fmt.Errorf("cannot parse info bytes: %w", err))
			break
		}
		if info.Private {
			t.stop(errors.New("private torrent from magnet"))
			break
		}
		t.info = info
		t.piecePool = bufferpool.New(int(info.PieceLength))
		err = t.session.resumer.WriteInfo(t.id, t.info.Bytes)
		if err != nil {
			t.stop(fmt.Errorf("cannot write resume info: %w", err))
			break
		}
		select {
		case <-t.completeMetadataC:
		default:
			close(t.completeMetadataC)
		}
		if t.stopAfterMetadata {
			t.stopAndSetStoppedOnMetadata()
			break
		}
		if info.NeedsV2Layers() && !info.HasPieceHashes() {
			// A v2 magnet: the info dict is known but the per-file piece
			// layers still have to come from peers (BEP 52) before any piece
			// can be built or verified.
			t.startV2LayerDownload()
			break
		}
		t.startAllocator()
	case peerprotocol.ExtensionMetadataMessageTypeReject:
		id, ok := t.infoDownloaders[pe]
		if ok {
			t.closePeer(id.Peer.(*peer.Peer))
			t.startInfoDownloaders()
		}
	}
}

// metadataPieceSize is the size of one ut_metadata piece (BEP 9).
const metadataPieceSize = 16 * 1024

// metadataPiece returns the bytes of the metadata piece `piece` out of the full
// info dictionary `data`, and whether that piece exists. The offset is computed
// in 64 bits: a hostile 32-bit piece index must not wrap around and serve the
// wrong piece instead of being rejected.
func metadataPiece(data []byte, piece uint32) ([]byte, bool) {
	start := uint64(piece) * metadataPieceSize
	if start >= uint64(len(data)) {
		return nil, false
	}
	end := start + metadataPieceSize
	if end > uint64(len(data)) {
		end = uint64(len(data))
	}
	return data[start:end], true
}

func (t *torrent) sendMetadataReject(pe *peer.Peer, i uint32, msgID uint8) {
	dataMsg := peerprotocol.ExtensionMetadataMessage{
		Type:  peerprotocol.ExtensionMetadataMessageTypeReject,
		Piece: i,
	}
	extDataMsg := peerprotocol.ExtensionMessage{
		ExtendedMessageID: msgID,
		Payload:           &dataMsg,
	}
	pe.SendMessage(extDataMsg)
}
