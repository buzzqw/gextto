package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// TestPiecesDone covers the targeted range check used by gx-torrent's
// streaming wait loop (gextto fork).
func TestPiecesDone(t *testing.T) {
	tr := &torrent{pieces: []piece.Piece{
		{Done: true}, // 0 have
		{Skip: true}, // 1 skipped
		{},           // 2 missing
		{Done: true}, // 3 have
		{Done: true}, // 4 have
	}}

	cases := []struct {
		begin, end uint32
		want       bool
	}{
		{0, 2, true},   // have + skipped
		{0, 3, false},  // includes the missing piece
		{2, 3, false},  // the missing piece alone
		{3, 5, true},   // tail
		{4, 100, true}, // end clamped past the last piece
		{5, 9, true},   // begin at/after the last piece
		{0, 0, true},   // empty range
	}
	for _, c := range cases {
		if got := tr.piecesDone(c.begin, c.end); got != c.want {
			t.Errorf("piecesDone(%d,%d) = %v, want %v", c.begin, c.end, got, c.want)
		}
	}

	// No metadata yet: nothing to report as done.
	if (&torrent{}).piecesDone(0, 1) {
		t.Error("piecesDone on an empty piece table = true, want false")
	}
}
