package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
)

// A BEP 54 DontHave clears the peer's view of the piece (fast extension not
// negotiated, so there is no outstanding download to cancel here).
func TestHandleDontHaveClearsPeerBitfield(t *testing.T) {
	tr := &torrent{
		bitfield:                bitfield.New(2),
		pieces:                  []piece.Piece{{Index: 0}, {Index: 1}},
		pieceDownloaders:        map[*peer.Peer]*piecedownloader.PieceDownloader{},
		pieceDownloadersSnubbed: map[*peer.Peer]*piecedownloader.PieceDownloader{},
		pieceDownloadersChoked:  map[*peer.Peer]*piecedownloader.PieceDownloader{},
	}
	tr.setInfo(&metainfo.Info{NumPieces: 2})
	pe := &peer.Peer{Bitfield: bitfield.New(2)}
	pe.Bitfield.Set(1)
	tr.handleDontHave(pe, peerprotocol.DontHaveMessage{Index: 1})
	if pe.Bitfield.Test(1) {
		t.Fatal("DontHave did not clear the peer bitfield bit")
	}
}

// A DontHave from a peer that never sent a bitfield or have must not panic
// (the interest is re-evaluated after the message).
func TestHandleDontHaveWithoutPeerBitfield(t *testing.T) {
	tr := &torrent{
		bitfield:                bitfield.New(2),
		pieces:                  []piece.Piece{{Index: 0}, {Index: 1}},
		pieceDownloaders:        map[*peer.Peer]*piecedownloader.PieceDownloader{},
		pieceDownloadersSnubbed: map[*peer.Peer]*piecedownloader.PieceDownloader{},
		pieceDownloadersChoked:  map[*peer.Peer]*piecedownloader.PieceDownloader{},
	}
	tr.setInfo(&metainfo.Info{NumPieces: 2})
	tr.handleDontHave(&peer.Peer{}, peerprotocol.DontHaveMessage{Index: 0})
}
