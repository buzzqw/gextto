package piecepicker

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// A high-priority piece (file priority 5..7) is requested before a more
// available, normal piece: the rarest-first order must not delay it.
func TestHighPriorityPickedBeforeRarest(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength},
		{Index: 1, Length: testPieceLength, Priority: piece.PriorityHigh},
	}
	pe := testPeer(len(pieces))
	other1 := testPeer(len(pieces))
	other2 := testPeer(len(pieces))
	pp := New(pieces, 2, nil, false, false)
	// The peer has both pieces; two more peers make piece 1 the least rare
	// (3 holders vs 1), so plain rarest-first would pick piece 0.
	pp.HandleHave(pe, 0)
	pp.HandleHave(pe, 1)
	pp.HandleHave(other1, 1)
	pp.HandleHave(other2, 1)
	got, _ := pp.PickFor(pe)
	if got == nil || got.Index != 1 {
		t.Fatalf("first pick = %v, want high-priority piece 1", got)
	}
	// Once the high piece is requested, the normal piece follows.
	got, _ = pp.PickFor(pe)
	if got == nil || got.Index != 0 {
		t.Fatalf("second pick = %v, want piece 0", got)
	}
}

// A skipped piece is never picked even when it carries a high priority.
func TestSkippedHighPriorityNeverPicked(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength, Skip: true, Priority: piece.PriorityHigh},
		{Index: 1, Length: testPieceLength},
	}
	pe := testPeer(len(pieces))
	pp := New(pieces, 2, nil, false, false)
	pp.HandleHave(pe, 0)
	pp.HandleHave(pe, 1)
	got, _ := pp.PickFor(pe)
	if got == nil || got.Index != 1 {
		t.Fatalf("pick = %v, want piece 1", got)
	}
}
