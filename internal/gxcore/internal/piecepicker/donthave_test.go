package piecepicker

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// A BEP 54 DontHave clears the piece availability at the peer, in the bitfield
// and in the picker indexes, so the piece is not offered to it again.
func TestHandleDontHaveClearsAvailability(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength},
		{Index: 1, Length: testPieceLength},
	}
	pe := testPeer(len(pieces))
	pp := New(pieces, 2, nil, false, false)
	pp.HandleHave(pe, 0)
	if !pe.Bitfield.Test(0) {
		t.Fatal("HandleHave did not set the bitfield")
	}
	pp.HandleDontHave(pe, 0)
	if pe.Bitfield.Test(0) {
		t.Fatal("HandleDontHave did not clear the bitfield")
	}
	if got, _ := pp.PickFor(pe); got != nil {
		t.Fatalf("picked %v after DontHave, want nil", got)
	}
}
