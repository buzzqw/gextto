package torrent

import (
	"fmt"
	"net"
	"strconv"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/cachedpiece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn/peerwriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecewriter"
)

func (t *torrent) handlePieceMessage(pm peer.PieceMessage) {
	msg := pm.Piece
	pe := pm.Peer
	l := int64(len(msg.Buffer.Data))
	if pe.Closed {
		t.bytesWasted.Inc(l)
		msg.Buffer.Release()
		return
	}
	if t.pieces == nil || t.bitfield == nil {
		pe.Logger().Error("piece received but we don't have info")
		t.bytesWasted.Inc(l)
		t.closePeer(pe)
		msg.Buffer.Release()
		return
	}
	if msg.Index >= uint32(len(t.pieces)) {
		pe.Logger().Errorln("invalid piece index:", msg.Index)
		t.bytesWasted.Inc(l)
		t.closePeer(pe)
		msg.Buffer.Release()
		return
	}
	t.downloadSpeed.Mark(l)
	t.bytesDownloaded.Inc(l)
	t.session.metrics.SpeedDownload.Mark(l)
	pd := t.pieceDownloaders[pe][msg.Index]
	if pd == nil {
		t.bytesWasted.Inc(l)
		msg.Buffer.Release()
		return
	}
	piece := pd.Piece
	err := pd.GotBlock(msg.Begin, msg.Buffer.Data)
	switch err {
	case piecedownloader.ErrBlockInvalid:
		pe.Logger().Errorln("received invalid piece index:", msg.Index, "begin:", msg.Begin, "length:", len(msg.Buffer.Data))
		t.bytesWasted.Inc(l)
		t.closePeer(pe)
		msg.Buffer.Release()
		return
	case piecedownloader.ErrBlockDuplicate:
		if pe.FastEnabled {
			pe.Logger().Warningf("received duplicate block index: %d begin: %d length: %d", msg.Index, msg.Begin, len(msg.Buffer.Data))
		} else {
			// If peer does not support fast extension, we cancel all pending requests on choke message.
			// After an unchoke we request them again. Some clients appears to be sending the same block
			// if we request it twice.
			pe.Logger().Debugf("received duplicate block index: %d begin: %d length: %d", msg.Index, msg.Begin, len(msg.Buffer.Data))
		}
		t.bytesWasted.Inc(l)
		msg.Buffer.Release()
		return
	case piecedownloader.ErrBlockNotRequested:
		if pe.FastEnabled {
			pe.Logger().Warningf("received not requested block index: %d begin: %d length: %d", msg.Index, msg.Begin, len(msg.Buffer.Data))
		} else {
			// If peer does not support fast extension, we cancel all pending requests on choke message.
			// That's why we think that we have received an unrequested block.
			pe.Logger().Debugf("received not requested block index: %d begin: %d length: %d", msg.Index, msg.Begin, len(msg.Buffer.Data))
		}
	case nil:
	default:
		pe.Logger().Error(err)
		t.closePeer(pe)
		msg.Buffer.Release()
		return
	}
	msg.Buffer.Release()
	if !pd.Done() {
		pe := pd.Peer.(*peer.Peer)
		if pd.AllowedFast || !pe.PeerChoking {
			t.boostRequestQueue(pe)
			if t.requestBlocks(pe) && !pe.PipeRefillTried {
				// The window has room no current piece can fill: try an
				// extra piece once (multi-piece pipelining).
				pe.PipeRefillTried = true
				t.fillPeerDownloads(pe)
			}
			pe.ResetSnubTimer()
		}
		return
	}
	t.log.Debugf("piece #%d downloaded from %s", msg.Index, pe.IP())
	t.closePieceDownloader(pd)
	pe.StopSnubTimer()

	if piece.Writing {
		t.crash("piece is already writing")
	}
	piece.Writing = true
	t.cancelDuplicateDownloads(piece.Index)

	// Request next piece while writing the completed piece, being optimistic about hash check.
	t.fillPeerDownloads(pe)

	// Bound the write ring buffer: the network keeps flowing while up to
	// maxPendingWrites pieces are written. The picker skips a piece in Writing,
	// so a piece is never written twice.
	t.beginPieceWrite()

	pw := piecewriter.New(piece, pe, pd.Buffer)
	go pw.Run(t.pieceWriterResultC, t.doneC, t.session.metrics.WritesPerSecond, t.session.metrics.SpeedWrite, t.session.semWrite)
}

func (t *torrent) handlePeerMessage(pm peer.Message) {
	pe := pm.Peer
	switch msg := pm.Message.(type) {
	case peerprotocol.HaveMessage:
		// Save have messages for processesing later received while we don't have info yet.
		if t.pieces == nil || t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= t.info().NumPieces {
			pe.Logger().Errorln("unexpected piece index:", msg.Index)
			t.closePeer(pe)
			break
		}
		// pe.Logger().Debug("Peer ", pe.String(), " has piece #", pi.Index)
		if pe.Bitfield != nil {
			pe.Bitfield.Set(msg.Index)
		}
		if t.superSeedActive() {
			t.superSeedHandleHave(pe, int32(msg.Index))
		}
		if t.piecePicker != nil {
			t.piecePicker.HandleHave(pe, msg.Index)
		}
		t.updateInterestedState(pe)
		t.fillPeerDownloads(pe)
	case peerprotocol.BitfieldMessage:
		// Save bitfield messages while we don't have info yet.
		if t.pieces == nil || t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if len(msg.Data) == 0 {
			pe.Logger().Debugln("received bitfield length of zero")
			break
		}
		bf, err := bitfield.NewBytes(msg.Data, t.info().NumPieces)
		if err != nil {
			pe.Logger().Errorf("%s [len(bitfield)=%d] [numPieces=%d]", err, len(msg.Data), t.info().NumPieces)
			t.closePeer(pe)
			break
		}
		pe.Logger().Debugln("Received bitfield:", bf.Hex())
		for i := uint32(0); i < bf.Len(); i++ {
			if !bf.Test(i) {
				continue
			}
			// Track the remote bitfield even when we are a seed and there is
			// no piece picker; super-seeding needs it.
			if pe.Bitfield != nil {
				pe.Bitfield.Set(i)
			}
			if t.piecePicker != nil {
				t.piecePicker.HandleHave(pe, i)
			}
		}
		t.updateInterestedState(pe)
		t.fillPeerDownloads(pe)
	case peerprotocol.HaveAllMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if t.piecePicker != nil {
			for _, pi := range t.pieces {
				t.piecePicker.HandleHave(pe, pi.Index)
			}
		}
		t.updateInterestedState(pe)
		t.fillPeerDownloads(pe)
	case peerprotocol.HaveNoneMessage:
	case peerprotocol.AllowedFastMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= t.info().NumPieces {
			pe.Logger().Errorln("invalid allowed fast piece index:", msg.Index)
			t.closePeer(pe)
			break
		}
		pe.Logger().Debug("Peer ", pe.String(), " has allowed fast for piece #", msg.Index)
		if t.piecePicker != nil {
			t.piecePicker.HandleAllowedFast(pe, msg.Index)
		}
	case peerprotocol.UnchokeMessage:
		pe.PeerChoking = false
		if len(t.pieceDownloaders[pe]) == 0 {
			t.fillPeerDownloads(pe)
			break
		}
		pe.ResetSnubTimer()
		t.requestBlocks(pe)
		t.fillPeerDownloads(pe)
		if t.piecePicker != nil {
			for _, pd := range t.pieceDownloaders[pe] {
				if pd.AllowedFast {
					continue
				}
				t.piecePicker.HandleUnchoke(pe, pd.Piece.Index)
			}
		}
	case peerprotocol.ChokeMessage:
		pe.PeerChoking = true
		t.resetRequestQueue(pe)
		if len(t.pieceDownloaders[pe]) == 0 {
			break
		}
		pe.StopSnubTimer()
		if t.piecePicker != nil {
			for _, pd := range t.pieceDownloaders[pe] {
				if pd.AllowedFast {
					continue
				}
				pd.Choked()
				t.piecePicker.HandleChoke(pe, pd.Piece.Index)
			}
		}
		t.startPieceDownloaders()
	case peerprotocol.InterestedMessage:
		pe.PeerInterested = true
		t.unchoker.FastUnchoke(pe)
	case peerprotocol.NotInterestedMessage:
		pe.PeerInterested = false
		// A super-seeding peer that downloaded the advertised pieces and has
		// nothing else to ask for goes "not interested" (it may have skipped the
		// redundant "have" because it knows the seed already has them): offer it
		// a new piece so it becomes interested again.
		if t.superSeedActive() {
			t.superSeedOfferNext(pe)
		}
	case peerprotocol.RequestMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Logger().Error("request received but we don't have info")
			t.closePeer(pe)
			break
		}
		if msg.Index >= t.info().NumPieces {
			pe.Logger().Errorln("invalid request index:", msg.Index)
			t.closePeer(pe)
			break
		}
		if !validPieceRequest(msg.Begin, msg.Length, t.pieces[msg.Index].Length) {
			pe.Logger().Errorln("invalid request begin:", msg.Begin, "length:", msg.Length)
			t.closePeer(pe)
			break
		}
		// While super-seeding we serve only the pieces offered to this peer
		if t.superSeedActive() && !t.superSeedOffered(pe, int32(msg.Index)) {
			pe.SendMessage(peerprotocol.RejectMessage{RequestMessage: msg})
			break
		}
		pi := &t.pieces[msg.Index]
		if !pi.Done {
			m := peerprotocol.RejectMessage{RequestMessage: msg}
			pe.SendMessage(m)
			break
		}
		if pe.ClientChoking {
			if pe.FastEnabled {
				if pe.SentAllowedFast.Has(pi) {
					pe.SendPiece(msg, cachedpiece.New(pi, t.session.pieceCache, t.session.config.ReadCacheBlockSize, t.peerID))
				} else {
					m := peerprotocol.RejectMessage{RequestMessage: msg}
					pe.SendMessage(m)
				}
			}
		} else {
			pe.SendPiece(msg, cachedpiece.New(pi, t.session.pieceCache, t.session.config.ReadCacheBlockSize, t.peerID))
		}
	case peerprotocol.RejectMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Logger().Error("reject received but we don't have info")
			t.closePeer(pe)
			break
		}

		if msg.Index >= t.info().NumPieces {
			pe.Logger().Errorln("invalid reject index:", msg.Index)
			t.closePeer(pe)
			break
		}
		pd := t.pieceDownloaders[pe][msg.Index]
		if pd == nil {
			break
		}
		ok := pd.Rejected(msg.Begin, msg.Length)
		if !ok {
			pe.Logger().Errorln("invalid reject index:", msg.Index, "begin:", msg.Begin, "length:", msg.Length)
			t.closePeer(pe)
			break
		}
	case peerprotocol.CancelMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Logger().Error("cancel received but we don't have info")
			t.closePeer(pe)
			break
		}

		if msg.Index >= t.info().NumPieces {
			pe.Logger().Debugln("invalid cancel index:", msg.Index)
			break
		}
		pe.CancelRequest(msg)
		if pe.FastEnabled {
			pe.SendMessage(peerprotocol.RejectMessage{RequestMessage: peerprotocol.RequestMessage{
				Index:  msg.Index,
				Begin:  msg.Begin,
				Length: msg.Length,
			}})
		}
	case peerprotocol.PortMessage:
		if t.session.dht != nil {
			t.session.dht.AddNode(net.JoinHostPort(pe.IP(), strconv.Itoa(int(msg.Port))))
		}
	case peerwriter.BlockUploaded:
		l := int64(msg.Length)
		t.uploadSpeed.Mark(l)
		t.bytesUploaded.Inc(l)
		t.session.metrics.SpeedUpload.Mark(l)
	case peerprotocol.ExtensionHandshakeMessage:
		pe.Logger().Debugln("extension handshake received:", msg)
		if pe.ExtensionHandshake != nil {
			pe.Logger().Debugln("peer changed extensions")
			break
		}
		pe.ExtensionHandshake = &msg

		if len(msg.YourIP) == 4 || len(msg.YourIP) == 16 {
			t.externalIP = net.IP(msg.YourIP)
		}
		if _, ok := msg.M[peerprotocol.ExtensionKeyMetadata]; ok {
			t.startInfoDownloaders()
		}
		// A v2 magnet needs its piece layers from a peer, once the info dict is
		// known (BEP 52).
		t.startV2LayerDownload()
		if t.session.config.PEXEnabled {
			if _, ok := msg.M[peerprotocol.ExtensionKeyPEX]; ok {
				if !t.privateSwarm() {
					pe.StartPEX(t.peers, &t.recentlySeen)
				}
			}
		}
	case peerprotocol.ExtensionMetadataMessage:
		t.handleMetadataMessage(pe, msg)
	case peerprotocol.ExtensionPEXMessage:
		if !t.session.config.PEXEnabled {
			break
		}
		// BEP 27: never use PEX for a private torrent (neither send nor receive).
		if t.privateSwarm() {
			break
		}
		// BEP 11: only the "added"/"added6" lists are candidates; the "dropped"
		// lists are informational (see pexCandidates).
		added4, added6 := pexCandidates(msg)
		if len(added4) > 0 {
			t.handleNewPeers(added4, peersource.PEX)
		}
		if len(added6) > 0 {
			t.handleNewPeers(added6, peersource.PEX)
		}
	case peerprotocol.HolepunchMessage:
		t.handleHolepunchMessage(pe, msg)
	case peerprotocol.DontHaveMessage:
		t.handleDontHave(pe, msg)
	case peerprotocol.HashRequestMessage:
		t.handleHashRequest(pe, msg)
	case peerprotocol.HashesMessage:
		t.handleHashes(pe, msg)
	case peerprotocol.HashRejectMessage:
		t.handleHashReject(pe, msg)
	default:
		t.crash(fmt.Sprintf("unhandled peer message type: %T", msg))
	}
}

// validPieceRequest reports whether a peer's "request" message for a block of
// the given piece length is in bounds. begin and length are attacker-controlled
// uint32 values, so the sum is computed in uint64 to avoid an overflow that
// would let a request with a large begin slip past the bounds check.
func validPieceRequest(begin, length, pieceLength uint32) bool {
	return length != 0 && uint64(begin)+uint64(length) <= uint64(pieceLength)
}

func (t *torrent) updateInterestedState(pe *peer.Peer) {
	if t.pieces == nil || t.bitfield == nil {
		return
	}
	// A peer that sent no bitfield/have yet has nothing we can want; pieces of
	// deselected files do not count.
	interested := !t.completed && t.peerHasWanted(pe)
	if !pe.ClientInterested && interested {
		pe.ClientInterested = true
		msg := peerprotocol.InterestedMessage{}
		pe.SendMessage(msg)
		return
	}
	if pe.ClientInterested && !interested {
		pe.ClientInterested = false
		msg := peerprotocol.NotInterestedMessage{}
		pe.SendMessage(msg)
		return
	}
}

// handleDontHave applies a BEP 54 DontHave message: the peer no longer has the
// piece, so it must not be requested from it again. If the Fast extension is
// not negotiated the peer will not reject our outstanding requests, so the
// active download of that piece from it is cancelled (as for a choke).
func (t *torrent) handleDontHave(pe *peer.Peer, msg peerprotocol.DontHaveMessage) {
	if t.pieces == nil || t.bitfield == nil {
		return
	}
	if msg.Index >= t.info().NumPieces {
		pe.Logger().Errorln("invalid dont-have index:", msg.Index)
		t.closePeer(pe)
		return
	}
	if pe.Bitfield != nil {
		pe.Bitfield.Clear(msg.Index)
	}
	if t.piecePicker != nil {
		t.piecePicker.HandleDontHave(pe, msg.Index)
	}
	// The peer may have nothing we need any more: stay interested only if it
	// still does, or we would hold one of its unchoke slots for nothing.
	t.updateInterestedState(pe)
	if pe.FastEnabled {
		return
	}
	if pd := t.pieceDownloaders[pe][msg.Index]; pd != nil {
		t.closePieceDownloader(pd)
		t.startPieceDownloaders()
	}
}
