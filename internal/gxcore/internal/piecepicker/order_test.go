package piecepicker

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/filesection"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// SetOrder switches the piece order on a running picker. Turning
// sequential or first_last on must mark the file edges that were not marked at
// construction time; turning them off must stop using them.
func TestSetOrderMarksFileEdgesAtRuntime(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength, Data: filesection.Piece{{Name: "f", Offset: 0, Length: testPieceLength}}},
		{Index: 1, Length: testPieceLength, Data: filesection.Piece{{Name: "f", Offset: testPieceLength, Length: testPieceLength}}},
		{Index: 2, Length: testPieceLength, Data: filesection.Piece{{Name: "f", Offset: 2 * testPieceLength, Length: testPieceLength}}},
	}
	pp := New(pieces, 2, nil, false, false)
	if pp.pieces[0].FileHead || pp.pieces[2].FileTail {
		t.Fatal("edges must not be marked when no order is active")
	}

	pp.SetOrder(true, false)
	if !pp.sequential || pp.firstLast {
		t.Fatalf("order flags: sequential=%v firstLast=%v", pp.sequential, pp.firstLast)
	}
	if !pp.pieces[0].FileHead || !pp.pieces[2].FileTail {
		t.Fatalf("SetOrder did not mark the edges: head=%v tail=%v", pp.pieces[0].FileHead, pp.pieces[2].FileTail)
	}

	pe := testPeer(len(pieces))
	for i := range pieces {
		pp.HandleHave(pe, uint32(i))
	}
	// Sequential with edges marked picks head, tail, then the middle.
	for _, want := range []uint32{0, 2, 1} {
		got, _ := pp.PickFor(pe)
		if got == nil || got.Index != want {
			t.Fatalf("after SetOrder picked %v, want piece %d", got, want)
		}
	}

	pp.SetOrder(false, false)
	if pp.sequential || pp.firstLast {
		t.Fatalf("SetOrder did not clear the order: sequential=%v firstLast=%v", pp.sequential, pp.firstLast)
	}
}

// PieceDownloading reports the pieces with an active downloader,
// used by the daemon's piece diagnostics.
func TestPieceDownloadingTracksWritingAndRequests(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength},
		{Index: 1, Length: testPieceLength},
		{Index: 2, Length: testPieceLength},
	}
	pp := New(pieces, 2, nil, false, false)
	if pp.PieceDownloading(0) {
		t.Fatal("an idle piece was reported as downloading")
	}
	pp.pieces[1].Writing = true
	if !pp.PieceDownloading(1) {
		t.Fatal("a writing piece was not reported as downloading")
	}
	if pp.PieceDownloading(3) {
		t.Fatal("an out-of-range index was reported as downloading")
	}
}

// the streaming window makes the picker request those pieces
// before any other.
func TestStreamWindowPicksThosePiecesFirst(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength}, {Index: 1, Length: testPieceLength},
		{Index: 2, Length: testPieceLength}, {Index: 3, Length: testPieceLength},
		{Index: 4, Length: testPieceLength},
	}
	pe := testPeer(len(pieces))
	pp := New(pieces, 2, nil, false, false)
	for i := range pieces {
		pp.HandleHave(pe, uint32(i))
	}
	pp.SetStreamWindow(3, 5)
	for _, want := range []uint32{3, 4} {
		got, _ := pp.PickFor(pe)
		if got == nil || got.Index != want {
			t.Fatalf("stream window picked %v, want piece %d", got, want)
		}
	}
}
