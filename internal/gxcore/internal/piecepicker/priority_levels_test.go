package piecepicker

// priority_levels_test.go pins the graduated file priority (0..7): above
// normal the higher level is fetched first, and at equal rarity a higher
// priority wins inside the normal band too. Before, 5..7 shared one band and
// 1..4 were indistinguishable.

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// Each level above normal is fetched before the lower one, whatever the
// rarity says.
func TestPriorityLevelsAreFetchedHighToLow(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength, Priority: piece.PriorityNormal},
		{Index: 1, Length: testPieceLength, Priority: 5},
		{Index: 2, Length: testPieceLength, Priority: 6},
		{Index: 3, Length: testPieceLength, Priority: 7},
	}
	pp := New(pieces, 2, nil, false, false)
	pp.RefreshPriorities()
	pe := testPeer(len(pieces))
	for i := uint32(0); i < uint32(len(pieces)); i++ {
		pp.HandleHave(pe, i)
	}
	// The level-7 piece is made the most available so rarest-first alone would
	// take it last: priority must come first.
	for i := 0; i < 3; i++ {
		pp.HandleHave(testPeer(len(pieces)), 3)
	}
	want := []uint32{3, 2, 1, 0}
	for _, index := range want {
		got, _ := pp.PickFor(pe)
		if got == nil || got.Index != index {
			t.Fatalf("pick = %v, want piece %d (levels 7,6,5,normal)", got, index)
		}
	}
}

// Inside the normal band, a piece of a higher-priority file wins when the two
// are equally rare.
func TestPriorityBreaksTheRarityTie(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength, Priority: 1},
		{Index: 1, Length: testPieceLength, Priority: piece.PriorityNormal},
	}
	pp := New(pieces, 2, nil, false, false)
	pp.RefreshPriorities()
	pe := testPeer(len(pieces))
	pp.HandleHave(pe, 0)
	pp.HandleHave(pe, 1)
	got, _ := pp.PickFor(pe)
	if got == nil || got.Index != 1 {
		t.Fatalf("pick = %v, want the normal-priority piece 1 over the level-1 piece 0", got)
	}
}

// A skipped level is not counted, and the default (every file normal) has a
// single level so the rarest-first pass keeps its early exit.
func TestPriorityLevelsCountOnlyWantedPieces(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength, Priority: piece.PriorityNormal},
		{Index: 1, Length: testPieceLength, Priority: piece.PriorityNormal},
	}
	pp := New(pieces, 2, nil, false, false)
	pp.RefreshPriorities()
	if pp.priorityLevels != 1 {
		t.Fatalf("levels = %d, want 1 for an all-normal torrent", pp.priorityLevels)
	}
	pieces[0].Priority = 7
	pieces[1].Priority = 7
	pieces[1].Skip = true
	pp.RefreshPriorities()
	if pp.priorityLevels != 1 || pp.priorityPieces[7] != 1 {
		t.Fatalf("levels = %d (piece 7 count %d), want 1 level with one piece",
			pp.priorityLevels, pp.priorityPieces[7])
	}
}
