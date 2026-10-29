package torrent

import (
	"fmt"
	"net"
	"strconv"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/cachedpiece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn/peerreader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn/peerwriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecewriter"
)

// handlePieceMessage settles one downloaded block: admission guards, block
// accounting, adaptive queue growth, and the write pipeline on completion.
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
	if t.absorbBlock(pe, pd, msg, l) {
		return
	}
	msg.Buffer.Release()
	if !pd.Done() {
		t.onPieceProgress(pe, pd)
		return
	}
	t.completePieceDownload(pe, pd, msg)
}

// absorbBlock feeds one block to its downloader, classifying the outcome.
// True means the block ends here (released, possibly with the peer closed);
// false means processing continues toward completion.
func (t *torrent) absorbBlock(pe *peer.Peer, pd *piecedownloader.PieceDownloader, msg peerreader.Piece, l int64) (stop bool) {
	err := pd.GotBlock(msg.Begin, msg.Buffer.Data)
	switch err {
	case piecedownloader.ErrBlockInvalid:
		pe.Logger().Errorln("received invalid piece index:", msg.Index, "begin:", msg.Begin, "length:", len(msg.Buffer.Data))
		t.bytesWasted.Inc(l)
		t.closePeer(pe)
		msg.Buffer.Release()
		return true
	case piecedownloader.ErrBlockDuplicate:
		t.logDuplicateBlock(pe, msg, "received duplicate block index: %d begin: %d length: %d")
		t.bytesWasted.Inc(l)
		msg.Buffer.Release()
		return true
	case piecedownloader.ErrBlockNotRequested:
		t.logDuplicateBlock(pe, msg, "received not requested block index: %d begin: %d length: %d")
		return false
	case nil:
		return false
	default:
		pe.Logger().Error(err)
		t.closePeer(pe)
		msg.Buffer.Release()
		return true
	}
}

// logDuplicateBlock reports a duplicate/unsolicited block: loudly for fast
// peers (which should not resend), quietly otherwise (non-fast peers
// legitimately resend after choke/unchoke cycles re-request).
func (t *torrent) logDuplicateBlock(pe *peer.Peer, msg peerreader.Piece, format string) {
	if pe.FastEnabled {
		pe.Logger().Warningf(format, msg.Index, msg.Begin, len(msg.Buffer.Data))
	} else {
		pe.Logger().Debugf(format, msg.Index, msg.Begin, len(msg.Buffer.Data))
	}
}

// onPieceProgress grows the adaptive queue on delivery and refills the pipe
// once when the window has room no current piece can fill.
func (t *torrent) onPieceProgress(pe *peer.Peer, pd *piecedownloader.PieceDownloader) {
	pe2 := pd.Peer.(*peer.Peer)
	if pd.AllowedFast || !pe2.PeerChoking {
		t.boostRequestQueue(pe2)
		if t.requestBlocks(pe2) && !pe2.PipeRefillTried {
			pe2.PipeRefillTried = true
			t.fillPeerDownloads(pe2)
		}
		pe2.ResetSnubTimer()
	}
}

// completePieceDownload writes a fully received piece: duplicate suppression
// against concurrent copies, optimistic pipelining of the next piece, and
// the bounded write ring.
func (t *torrent) completePieceDownload(pe *peer.Peer, pd *piecedownloader.PieceDownloader, msg peerreader.Piece) {
	piece := pd.Piece
	t.log.Debugf("piece #%d downloaded from %s", msg.Index, pe.IP())
	t.closePieceDownloader(pd)
	pe.StopSnubTimer()

	if piece.Writing {
		t.crash("piece is already writing")
	}
	piece.Writing = true
	t.cancelDuplicateDownloads(piece.Index)
	t.fillPeerDownloads(pe)
	t.beginPieceWrite()

	pw := piecewriter.New(piece, pe, pd.Buffer)
	go pw.Run(t.pieceWriterResultC, t.doneC, t.session.metrics.WritesPerSecond, t.session.metrics.SpeedWrite, t.session.semWrite)
}

// handlePeerMessage dispatches one protocol message to its handler. Unknown
// types are a bug and crash: every message the reader can emit is covered.
func (t *torrent) handlePeerMessage(pm peer.Message) {
	pe := pm.Peer
	switch msg := pm.Message.(type) {
	case peerprotocol.HaveMessage:
		t.noteHave(pe, msg)
	case peerprotocol.BitfieldMessage:
		t.noteBitfield(pe, msg)
	case peerprotocol.HaveAllMessage:
		t.noteHaveAll(pe, msg)
	case peerprotocol.HaveNoneMessage:
	case peerprotocol.AllowedFastMessage:
		t.noteAllowedFast(pe, msg)
	case peerprotocol.UnchokeMessage:
		t.noteUnchoke(pe)
	case peerprotocol.ChokeMessage:
		t.noteChoke(pe)
	case peerprotocol.InterestedMessage:
		pe.PeerInterested = true
		t.unchoker.FastUnchoke(pe)
	case peerprotocol.NotInterestedMessage:
		pe.PeerInterested = false
		// A super-seeding peer that took the advertised pieces and wants
		// nothing else goes quiet (it may skip the redundant "have"): offer
		// the next piece so it asks again.
		if t.superSeedActive() {
			t.superSeedOfferNext(pe)
		}
	case peerprotocol.RequestMessage:
		t.serveRequest(pe, msg)
	case peerprotocol.RejectMessage:
		t.noteReject(pe, msg)
	case peerprotocol.CancelMessage:
		t.noteCancel(pe, msg)
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
		t.noteExtensionHandshake(pe, msg)
	case peerprotocol.ExtensionMetadataMessage:
		t.handleMetadataMessage(pe, msg)
	case peerprotocol.ExtensionPEXMessage:
		t.notePEX(pe, msg)
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

// noteHave records one announced piece, queueing pre-metadata announcements
// for replay, and feeds the picker, super-seeding and interest.
func (t *torrent) noteHave(pe *peer.Peer, msg peerprotocol.HaveMessage) {
	if t.pieces == nil || t.bitfield == nil {
		pe.Messages = append(pe.Messages, msg)
		return
	}
	if msg.Index >= t.info().NumPieces {
		pe.Logger().Errorln("unexpected piece index:", msg.Index)
		t.closePeer(pe)
		return
	}
	// The picker sets the peer's bit with its holder list; without one the
	// bit alone is the record. Super-seeding reads the bit, so it comes after.
	if t.piecePicker != nil {
		t.piecePicker.HandleHave(pe, msg.Index)
	} else if pe.Bitfield != nil {
		pe.Bitfield.Set(msg.Index)
	}
	if t.superSeedActive() {
		t.superSeedHandleHave(pe, int32(msg.Index))
	}
	if t.closeIfBothSeeds(pe) {
		return
	}
	t.updateInterestedState(pe)
	t.fillPeerDownloads(pe)
}

// noteBitfield records a full availability map: empty ones are ignored, bad
// lengths drop the peer, and every set bit feeds the picker (plus the
// seed-side remote tracking super-seeding needs without a picker).
func (t *torrent) noteBitfield(pe *peer.Peer, msg peerprotocol.BitfieldMessage) {
	if t.pieces == nil || t.bitfield == nil {
		pe.Messages = append(pe.Messages, msg)
		return
	}
	if len(msg.Data) == 0 {
		pe.Logger().Debugln("received bitfield length of zero")
		return
	}
	bf, err := bitfield.NewBytes(msg.Data, t.info().NumPieces)
	if err != nil {
		pe.Logger().Errorf("%s [len(bitfield)=%d] [numPieces=%d]", err, len(msg.Data), t.info().NumPieces)
		t.closePeer(pe)
		return
	}
	if log := pe.Logger(); log.DebugEnabled() {
		log.Debugln("Received bitfield:", bf.Hex())
	}
	for i := uint32(0); i < bf.Len(); i++ {
		if !bf.Test(i) {
			continue
		}
		if t.piecePicker != nil {
			t.piecePicker.HandleHave(pe, i)
		} else if pe.Bitfield != nil {
			pe.Bitfield.Set(i)
		}
	}
	if t.closeIfBothSeeds(pe) {
		return
	}
	t.updateInterestedState(pe)
	t.fillPeerDownloads(pe)
}

// noteHaveAll marks every piece present on the peer.
func (t *torrent) noteHaveAll(pe *peer.Peer, msg peerprotocol.HaveAllMessage) {
	if t.pieces == nil || t.bitfield == nil {
		pe.Messages = append(pe.Messages, msg)
		return
	}
	if t.piecePicker != nil {
		for _, pi := range t.pieces {
			t.piecePicker.HandleHave(pe, pi.Index)
		}
	} else if pe.Bitfield != nil {
		// No picker (we seed): the bit map is the only record of the peer.
		for i := uint32(0); i < pe.Bitfield.Len(); i++ {
			pe.Bitfield.Set(i)
		}
	}
	if t.closeIfBothSeeds(pe) {
		return
	}
	t.updateInterestedState(pe)
	t.fillPeerDownloads(pe)
}

// noteAllowedFast records one fast-exception piece, queueing pre-metadata
// ones for replay.
func (t *torrent) noteAllowedFast(pe *peer.Peer, msg peerprotocol.AllowedFastMessage) {
	if t.pieces == nil || t.bitfield == nil {
		pe.Messages = append(pe.Messages, msg)
		return
	}
	if msg.Index >= t.info().NumPieces {
		pe.Logger().Errorln("invalid allowed fast piece index:", msg.Index)
		t.closePeer(pe)
		return
	}
	if log := pe.Logger(); log.DebugEnabled() {
		log.Debug("Peer ", pe.String(), " has allowed fast for piece #", msg.Index)
	}
	if t.piecePicker != nil {
		t.piecePicker.HandleAllowedFast(pe, msg.Index)
	}
}

// noteUnchoke resumes fetching: fresh fills when idle, otherwise queue
// refill plus unchoke notifications for the non-fast downloads.
func (t *torrent) noteUnchoke(pe *peer.Peer) {
	pe.PeerChoking = false
	if len(t.pieceDownloaders[pe]) == 0 {
		t.fillPeerDownloads(pe)
		return
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
}

// noteChoke parks fetching: the queue target drops to base and every
// non-fast download is told, then the swarm refills elsewhere.
func (t *torrent) noteChoke(pe *peer.Peer) {
	pe.PeerChoking = true
	t.resetRequestQueue(pe)
	if len(t.pieceDownloaders[pe]) == 0 {
		return
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
}

// serveRequest serves one block request: metadata-less, out-of-range or
// malformed asks drop the peer; super-seeding serves only offered pieces;
// missing pieces and choked non-fast peers get rejects; the rest stream
// from the read cache.
func (t *torrent) serveRequest(pe *peer.Peer, msg peerprotocol.RequestMessage) {
	if t.pieces == nil || t.bitfield == nil {
		pe.Logger().Error("request received but we don't have info")
		t.closePeer(pe)
		return
	}
	if msg.Index >= t.info().NumPieces {
		pe.Logger().Errorln("invalid request index:", msg.Index)
		t.closePeer(pe)
		return
	}
	if !validPieceRequest(msg.Begin, msg.Length, t.pieces[msg.Index].Length) {
		pe.Logger().Errorln("invalid request begin:", msg.Begin, "length:", msg.Length)
		t.closePeer(pe)
		return
	}
	if t.superSeedActive() && !t.superSeedOffered(pe, int32(msg.Index)) {
		pe.SendMessage(peerprotocol.RejectMessage{RequestMessage: msg})
		return
	}
	pi := &t.pieces[msg.Index]
	if !pi.Done {
		pe.SendMessage(peerprotocol.RejectMessage{RequestMessage: msg})
		return
	}
	if pe.ClientChoking {
		if pe.FastEnabled && pe.SentAllowedFast.Has(pi) {
			t.sendPiece(pe, msg, pi)
		} else if pe.FastEnabled {
			pe.SendMessage(peerprotocol.RejectMessage{RequestMessage: msg})
		}
		return
	}
	t.sendPiece(pe, msg, pi)
}

// sendPiece queues one block upload from the read cache. In seed mode a
// piece not yet hashed is held back: the request waits for the piece's first
// hash, so unverified bytes never leave.
func (t *torrent) sendPiece(pe *peer.Peer, msg peerprotocol.RequestMessage, pi *piece.Piece) {
	if t.seedUnverified(msg.Index) {
		t.queueSeedModeRequest(pe, msg)
		return
	}
	pe.SendPiece(msg, cachedpiece.New(pi, t.session.pieceCache, t.session.config.ReadCacheBlockSize, t.peerID))
}

// noteReject requeues one refused block; unknown or malformed rejects drop
// the peer.
func (t *torrent) noteReject(pe *peer.Peer, msg peerprotocol.RejectMessage) {
	if t.pieces == nil || t.bitfield == nil {
		pe.Logger().Error("reject received but we don't have info")
		t.closePeer(pe)
		return
	}
	if msg.Index >= t.info().NumPieces {
		pe.Logger().Errorln("invalid reject index:", msg.Index)
		t.closePeer(pe)
		return
	}
	pd := t.pieceDownloaders[pe][msg.Index]
	if pd == nil {
		return
	}
	if ok := pd.Rejected(msg.Begin, msg.Length); !ok {
		pe.Logger().Errorln("invalid reject index:", msg.Index, "begin:", msg.Begin, "length:", msg.Length)
		t.closePeer(pe)
		return
	}
}

// noteCancel withdraws one queued upload, answering with a reject on fast
// peers; out-of-range cancels are only logged.
func (t *torrent) noteCancel(pe *peer.Peer, msg peerprotocol.CancelMessage) {
	if t.pieces == nil || t.bitfield == nil {
		pe.Logger().Error("cancel received but we don't have info")
		t.closePeer(pe)
		return
	}
	if msg.Index >= t.info().NumPieces {
		pe.Logger().Debugln("invalid cancel index:", msg.Index)
		return
	}
	pe.CancelRequest(msg)
	if pe.FastEnabled {
		pe.SendMessage(peerprotocol.RejectMessage{RequestMessage: peerprotocol.RequestMessage{
			Index:  msg.Index,
			Begin:  msg.Begin,
			Length: msg.Length,
		}})
	}
}

// noteExtensionHandshake adopts the peer's extension map once (later ones
// only log), learns our reflected address, and boots the metadata, v2-layer
// and PEX exchanges the peer offers — PEX skipped on private swarms.
func (t *torrent) noteExtensionHandshake(pe *peer.Peer, msg peerprotocol.ExtensionHandshakeMessage) {
	if log := pe.Logger(); log.DebugEnabled() {
		log.Debugln("extension handshake received:", msg)
	}
	if pe.ExtensionHandshake != nil {
		pe.Logger().Debugln("peer changed extensions")
		return
	}
	pe.ExtensionHandshake = &msg

	if len(msg.YourIP) == 4 || len(msg.YourIP) == 16 {
		t.externalIP = net.IP(msg.YourIP)
	}
	if _, ok := msg.M[peerprotocol.ExtensionKeyMetadata]; ok {
		t.startInfoDownloaders()
	}
	t.startV2LayerDownload()
	if t.session.config.PEXEnabled {
		if _, ok := msg.M[peerprotocol.ExtensionKeyPEX]; ok {
			if !t.privateSwarm() {
				pe.StartPEX(t.peers, &t.recentlySeen)
			}
		}
	}
}

// notePEX absorbs gossiped addresses (added lists only) on public swarms
// with PEX enabled.
func (t *torrent) notePEX(_ *peer.Peer, msg peerprotocol.ExtensionPEXMessage) {
	if !t.session.config.PEXEnabled {
		return
	}
	if t.privateSwarm() {
		return
	}
	added4, added6 := pexCandidates(msg)
	if len(added4) > 0 {
		t.handleNewPeers(added4, peersource.PEX)
	}
	if len(added6) > 0 {
		t.handleNewPeers(added6, peersource.PEX)
	}
}

// validPieceRequest bounds-checks a block ask in 64 bits: 32-bit begin plus
// length could otherwise wrap past the piece end.
func validPieceRequest(begin, length, pieceLength uint32) bool {
	return length != 0 && uint64(begin)+uint64(length) <= uint64(pieceLength)
}

// updateInterestedState syncs our interest in a peer with whether it holds
// anything we still want (deselected pieces excluded): transitions emit
// exactly one message.
func (t *torrent) updateInterestedState(pe *peer.Peer) {
	if t.pieces == nil || t.bitfield == nil {
		return
	}
	interested := !t.completed && t.peerHasWanted(pe)
	if !pe.ClientInterested && interested {
		pe.ClientInterested = true
		pe.SendMessage(peerprotocol.InterestedMessage{})
		return
	}
	if pe.ClientInterested && !interested {
		pe.ClientInterested = false
		pe.SendMessage(peerprotocol.NotInterestedMessage{})
		return
	}
}

// handleDontHave applies a BEP 54 notice: the piece leaves the peer's map
// and the picker, interest refreshes, and without fast-extension rejects the
// in-flight fetch of that piece is cancelled as on choke.
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
	t.updateInterestedState(pe)
	if pe.FastEnabled {
		return
	}
	if pd := t.pieceDownloaders[pe][msg.Index]; pd != nil {
		t.closePieceDownloader(pd)
		t.startPieceDownloaders()
	}
}
