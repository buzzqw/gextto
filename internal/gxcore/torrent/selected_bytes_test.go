package torrent

import (
	"math/rand/v2"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

// TestSelectedBytesPopcountMatchesPerPiece pins the popcount path of
// selectedBytes (no skipped file) to the per-piece sum it replaces.
func TestSelectedBytesPopcountMatchesPerPiece(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, tc := range []struct {
		numPieces   uint32
		pieceLength uint32
		lastLength  int64
	}{
		{1, 16384, 100}, {1, 16384, 16384}, {7, 32768, 1}, {64, 16384, 16384}, {1001, 262144, 12345},
	} {
		length := int64(tc.pieceLength)*int64(tc.numPieces-1) + tc.lastLength
		info := &metainfo.Info{NumPieces: tc.numPieces, PieceLength: tc.pieceLength, Length: length,
			Files: []metainfo.File{{Path: "f", Length: length}}}
		for round := 0; round < 20; round++ {
			bits := bitfield.New(tc.numPieces)
			for i := uint32(0); i < tc.numPieces; i++ {
				if round == 1 || (round > 1 && rng.IntN(3) == 0) {
					bits.Set(i)
				}
			}
			tor := &torrent{session: &Session{}, bitfield: bits}
			tor.setInfo(info)
			total, done := tor.selectedBytes()
			var wantTotal, wantDone int64
			for i := uint32(0); i < tc.numPieces; i++ {
				size := int64(tc.pieceLength)
				if i == tc.numPieces-1 {
					size = tc.lastLength
				}
				wantTotal += size
				if bits.Test(i) {
					wantDone += size
				}
			}
			if total != wantTotal || done != wantDone {
				t.Fatalf("%d pieces, round %d: got %d/%d, want %d/%d", tc.numPieces, round, done, total, wantDone, wantTotal)
			}
		}
		tor := &torrent{session: &Session{}}
		tor.setInfo(info)
		if total, done := tor.selectedBytes(); total != length || done != 0 {
			t.Fatalf("without a bitfield: %d/%d, want 0/%d", done, total, length)
		}
	}
}
