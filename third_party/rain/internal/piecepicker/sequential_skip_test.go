package piecepicker

import (
	"testing"

	"github.com/cenkalti/rain/v2/internal/bitfield"
	"github.com/cenkalti/rain/v2/internal/filesection"
	"github.com/cenkalti/rain/v2/internal/peer"
	"github.com/cenkalti/rain/v2/internal/piece"
)

// These tests guard the gextto file-selection integration across rain
// upgrades. Upstream picks pieces in several different code paths; every one of
// them must honour piece.Piece.Skip, otherwise the fork would download pieces
// that belong only to excluded files. When merging a new rain release, run
// `go test ./internal/piecepicker/` and keep these green.

const testPieceLength = 16 * 1024

// A skipped piece must never be handed out in sequential mode, even when it is
// the lowest-index piece the peer has.
func TestSequentialNeverPicksSkippedPieces(t *testing.T) {
	pieces := []piece.Piece{
		{Index: 0, Length: testPieceLength, Skip: true},
		{Index: 1, Length: testPieceLength},
		{Index: 2, Length: testPieceLength},
	}
	pe := &peer.Peer{Bitfield: bitfield.New(uint32(len(pieces)))}
	pp := New(pieces, 2, nil, true)
	for i := range pieces {
		pp.HandleHave(pe, uint32(i))
	}
	got, _ := pp.PickFor(pe)
	if got == nil || got.Index != 1 {
		t.Fatalf("sequential picked %v, want piece 1", got)
	}
}

// A skipped piece that sits at a file edge (the first or last piece of a file)
// must not be picked through the file-edge path either.
func TestFileEdgeNeverPicksSkippedPieces(t *testing.T) {
	pieces := []piece.Piece{
		{
			Index: 0, Length: testPieceLength, Skip: true,
			Data: filesection.Piece{{Name: "f", Offset: 0, Length: testPieceLength}},
		},
		{Index: 1, Length: testPieceLength},
	}
	pe := &peer.Peer{Bitfield: bitfield.New(uint32(len(pieces)))}
	pp := New(pieces, 2, nil, true)
	for i := range pieces {
		pp.HandleHave(pe, uint32(i))
	}
	// Piece 0 is a file head but skipped, so the picker must fall through to
	// piece 1.
	got, _ := pp.PickFor(pe)
	if got == nil || got.Index != 1 {
		t.Fatalf("file edge picked %v, want piece 1", got)
	}
}
