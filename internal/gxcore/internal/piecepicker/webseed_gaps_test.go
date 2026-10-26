package piecepicker

import (
	"cmp"
	"math/rand"
	"slices"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
)

// pickLastPieceOfSmallestGapReference is the allocate-and-copy version the
// scratch-buffer one replaced; the test holds the two to the same choice.
func (p *PiecePicker) pickLastPieceOfSmallestGapReference(pe *peer.Peer) *myPiece {
	gaps := p.findGaps()
	if len(gaps) == 0 {
		return nil
	}
	slices.SortStableFunc(gaps, func(a, b Range) int { return cmp.Compare(a.Len(), b.Len()) })
	for _, gap := range gaps {
		for i := int(gap.End - 1); i >= int(gap.Begin); i-- {
			mp := &p.pieces[i]
			if mp.Requested.Len() > 0 || !mp.heldBy(pe) {
				continue
			}
			if pe.PeerChoking && !pe.ReceivedAllowedFast.Has(mp.Piece) {
				continue
			}
			return mp
		}
	}
	return nil
}

// webseedGapPicker builds n pieces where a webseed holds a few ranges, some
// pieces are done or being written and the peer has most of the rest.
func webseedGapPicker(rng *rand.Rand, n int) (*PiecePicker, *peer.Peer) {
	pp := New(benchPieces(n), 8, nil, false, false)
	src := &webseedsource.WebseedSource{URL: "http://seed/"}
	for i := range pp.pieces {
		switch r := rng.Intn(100); {
		case r < 20:
			pp.pieces[i].Done = true
		case r < 22:
			pp.pieces[i].Writing = true
		}
	}
	for k := 0; k < 6; k++ {
		begin := rng.Intn(n)
		for i := begin; i < min(n, begin+rng.Intn(n/10+1)); i++ {
			pp.pieces[i].RequestedWebseed = src
		}
	}
	pe := testPeer(n)
	for i := 0; i < n; i++ {
		if rng.Intn(10) != 0 {
			pp.HandleHave(pe, uint32(i))
		}
	}
	return pp, pe
}

func TestPickLastPieceOfSmallestGapMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for round := 0; round < 200; round++ {
		n := 1 + rng.Intn(400)
		pp, pe := webseedGapPicker(rng, n)
		for i := range pp.pieces {
			if rng.Intn(15) == 0 {
				pp.pieces[i].Requested.Add(testPeer(n))
			}
		}
		for pick := 0; pick < 5; pick++ {
			got, want := pp.pickLastPieceOfSmallestGap(pe), pp.pickLastPieceOfSmallestGapReference(pe)
			if (got == nil) != (want == nil) {
				t.Fatalf("round %d: got %v, want %v", round, got, want)
			}
			if got == nil {
				break
			}
			// Gaps of equal length may come in either order: the pick must be
			// the tail candidate of a gap as short as the reference's.
			if got.Index != want.Index && gapLenOf(pp, got.Index) != gapLenOf(pp, want.Index) {
				t.Fatalf("round %d: picked %d (gap %d), reference %d (gap %d)", round, got.Index, gapLenOf(pp, got.Index), want.Index, gapLenOf(pp, want.Index))
			}
			want.Requested.Add(pe)
			if got != want {
				got.Requested.Add(pe)
			}
		}
	}
}

// gapLenOf is the length of the webseed gap holding piece i.
func gapLenOf(p *PiecePicker, i uint32) int {
	for _, gap := range p.findGaps() {
		if i >= gap.Begin && i < gap.End {
			return gap.Len()
		}
	}
	return -1
}

// BenchmarkPickerSmallestGapWebseeds is the pick every peer makes while a
// webseed is downloading, on a DVD-sized torrent (15,000 pieces).
func BenchmarkPickerSmallestGapWebseeds(b *testing.B) {
	pp, pe := webseedGapPicker(rand.New(rand.NewSource(1)), 15000)
	b.ReportAllocs()
	for b.Loop() {
		if pp.pickLastPieceOfSmallestGap(pe) == nil {
			b.Fatal("no piece picked")
		}
	}
}
