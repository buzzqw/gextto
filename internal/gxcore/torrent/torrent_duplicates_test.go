package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/filesection"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecepicker"
)

// TestCompletedPieceCancelsDuplicateDownloads locks the endgame fix for the
// write ring buffer: once one copy of a piece is being written, every other
// peer still fetching it is cancelled at once. Piece messages keep flowing
// during the write, so a duplicate left running would complete a second copy
// and crash the torrent with "piece is already writing". Three duplicates also
// cover the swap-with-last removal that skipped peers while iterating.
func TestCompletedPieceCancelsDuplicateDownloads(t *testing.T) {
	pieces := []piece.Piece{{Index: 0, Length: 16 << 10}}
	tr := &torrent{
		session:          &Session{},
		pieces:           pieces,
		pieceDownloaders: map[*peer.Peer]map[uint32]*piecedownloader.PieceDownloader{},
	}
	tr.piecePicker = piecepicker.New(tr.pieces, 4, nil, false, false)
	peers := []*peer.Peer{{Bitfield: bitfield.New(1)}, {Bitfield: bitfield.New(1)}, {Bitfield: bitfield.New(1)}, {Bitfield: bitfield.New(1)}}
	for _, pe := range peers {
		tr.piecePicker.HandleHave(pe, 0)
		pi, _ := tr.piecePicker.PickFor(pe)
		if pi == nil {
			t.Fatal("endgame did not hand the piece to every peer")
		}
		tr.pieceDownloaders[pe] = map[uint32]*piecedownloader.PieceDownloader{0: {Piece: pi, Peer: pe}}
		pe.ConcurrentPieces, pe.Downloading = 1, true
	}

	// peers[0] completed the piece: its own downloader is closed first.
	tr.closePieceDownloader(tr.pieceDownloaders[peers[0]][0])
	tr.pieces[0].Writing = true
	tr.cancelDuplicateDownloads(0)

	if len(tr.pieceDownloaders) != 0 {
		t.Fatalf("%d peer(s) still download the piece being written", len(tr.pieceDownloaders))
	}
	if n := len(tr.piecePicker.RequestedPeers(0)); n != 0 {
		t.Fatalf("picker still lists %d requester(s)", n)
	}
	for i, pe := range peers {
		if pe.Downloading || pe.ConcurrentPieces != 0 {
			t.Fatalf("peer %d left in a downloading state", i)
		}
	}
}

type pipeFile struct{}

func (pipeFile) ReadAt(p []byte, off int64) (int, error)  { return len(p), nil }
func (pipeFile) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }

type pipePeer struct{ requests int }

func (p *pipePeer) RequestPiece(index, begin, length uint32) { p.requests++ }
func (p *pipePeer) CancelPiece(index, begin, length uint32)  {}
func (p *pipePeer) EnabledFast() bool                        { return false }

func pipePiece(index uint32, blocks int) *piece.Piece {
	length := uint32(blocks) * piece.BlockSize
	return &piece.Piece{
		Index:  index,
		Length: length,
		Data:   filesection.Piece{{File: pipeFile{}, Offset: 0, Length: int64(length)}},
	}
}

// TestRequestBlocksSharesOneBudget: with two pieces per peer the in-flight
// window stays maxAllowedRequests in total, not per piece, and the pipe only
// reports room (the trigger for an extra piece) once every block of the
// current pieces is requested and the window is not full.
func TestRequestBlocksSharesOneBudget(t *testing.T) {
	tr := &torrent{session: &Session{config: Config{DefaultRequestsOut: 10, MaxRequestsOut: 10}}}
	pe := &peer.Peer{}
	wire := &pipePeer{}

	// A big piece (16 blocks) fills the 10-request window by itself: no room.
	big := piecedownloader.New(pipePiece(0, 16), wire, false, bufferpool.Buffer{})
	tr.pieceDownloaders = map[*peer.Peer]map[uint32]*piecedownloader.PieceDownloader{pe: {0: big}}
	if tr.requestBlocks(pe) || tr.pipeHasRoom(pe) {
		t.Fatal("a piece larger than the window must not open a second one")
	}
	if wire.requests != 10 {
		t.Fatalf("in flight = %d, want 10", wire.requests)
	}

	// A small piece (4 blocks) leaves room in the window.
	wire2 := &pipePeer{}
	pe2 := &peer.Peer{}
	small := piecedownloader.New(pipePiece(1, 4), wire2, false, bufferpool.Buffer{})
	tr.pieceDownloaders[pe2] = map[uint32]*piecedownloader.PieceDownloader{1: small}
	if !tr.requestBlocks(pe2) || !tr.pipeHasRoom(pe2) {
		t.Fatal("a piece smaller than the window must leave room for a second one")
	}
	// The second piece only gets what is left of the shared window.
	second := piecedownloader.New(pipePiece(2, 16), wire2, false, bufferpool.Buffer{})
	tr.pieceDownloaders[pe2][2] = second
	tr.requestBlocks(pe2)
	if wire2.requests != 10 {
		t.Fatalf("in flight across two pieces = %d, want the shared window of 10", wire2.requests)
	}
	// A second refill with everything pending must not exceed the window.
	tr.requestBlocks(pe2)
	if wire2.requests != 10 {
		t.Fatalf("refill overshot the window: %d requests", wire2.requests)
	}
}
