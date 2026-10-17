package piecepicker

import (
	"math/rand"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// Benchmarks for the engine, to measure the cost of the picker paths that
// re-scan on every call. Run with:
//
//	go test -run '^$' -bench 'BenchmarkPicker' -benchmem \
//	    github.com/buzzqw/gextto/internal/gxcore/internal/piecepicker
//
// The point is to compare a realistic steady state (availability changes
// slowly, so the availability order is nearly sorted) with a worst case.

func benchPieces(n int) []piece.Piece {
	pieces := make([]piece.Piece, n)
	for i := range pieces {
		pieces[i] = piece.Piece{Index: uint32(i), Length: testPieceLength}
	}
	return pieces
}

// benchRarest builds a rarest-first picker where the requesting peer has every
// piece and three extra peers give each piece an availability of 1..3.
func benchRarest(n int) (*PiecePicker, *peer.Peer) {
	pp := New(benchPieces(n), 8, nil, false, false)
	req := testPeer(n)
	for i := 0; i < n; i++ {
		pp.HandleHave(req, uint32(i))
	}
	for p := 0; p < 3; p++ {
		other := testPeer(n)
		for i := 0; i < n; i++ {
			if rand.Intn(2) == 0 {
				pp.HandleHave(other, uint32(i))
			}
		}
	}
	return pp, req
}

func BenchmarkPickerRarestSteady(b *testing.B) {
	pp, pe := benchRarest(27250)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if pp.pickRarest(pe) == nil {
			b.Fatal("no piece picked")
		}
	}
}

// BenchmarkPickerRarestShuffled is the worst case: the availability order is
// fully scrambled before every pick, so the sort cannot exploit adaptivity.
func BenchmarkPickerRarestShuffled(b *testing.B) {
	pp, pe := benchRarest(27250)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rand.Shuffle(len(pp.piecesByAvailability), func(x, y int) {
			pp.piecesByAvailability[x], pp.piecesByAvailability[y] = pp.piecesByAvailability[y], pp.piecesByAvailability[x]
		})
		if pp.pickRarest(pe) == nil {
			b.Fatal("no piece picked")
		}
	}
}

// BenchmarkPickerSequentialNearlyDone marks every piece but the last as done:
// the sequential scan walks the whole table before finding the frontier.
func BenchmarkPickerSequentialNearlyDone(b *testing.B) {
	const n = 27250
	pieces := benchPieces(n)
	for i := range pieces {
		pieces[i].Done = i < n-1
	}
	pp := New(pieces, 8, nil, true, false)
	pe := testPeer(n)
	pp.HandleHave(pe, n-1) // the peer only has the last piece
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if pp.pickSequential(pe) == nil {
			b.Fatal("no piece picked")
		}
	}
}

// BenchmarkPickerFileEdgeFullScan forces pickFileEdge to visit every piece
// (no pickable edge), which is the worst case of the edge pass.
func BenchmarkPickerFileEdgeFullScan(b *testing.B) {
	const n = 27250
	pp := New(benchPieces(n), 8, nil, false, true)
	pe := testPeer(n)
	for i := 0; i < n; i++ {
		pp.HandleHave(pe, uint32(i))
	}
	// Every piece is requested, so nothing is pickable: full scan.
	for i := range pp.pieces {
		pp.pieces[i].Requested.Add(pe)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if pp.pickFileEdge(pe) != nil {
			b.Fatal("expected no pickable edge piece")
		}
	}
}

// benchSwarm builds a rarest-first picker over n pieces with peers holders, as
// on a saturated line (168 peers measured on 2026-10-17): each peer has about
// 90 % of the pieces, so every piece has well over a hundred holders.
func benchSwarm(n, peers int) (*PiecePicker, []*peer.Peer) {
	rng := rand.New(rand.NewSource(1))
	pp := New(benchPieces(n), 8, nil, false, false)
	all := make([]*peer.Peer, peers)
	for p := range all {
		all[p] = testPeer(n)
		for i := 0; i < n; i++ {
			if rng.Intn(10) != 0 {
				pp.HandleHave(all[p], uint32(i))
			}
		}
	}
	return pp, all
}

// BenchmarkPickerRarestManyPeers picks for a peer in a swarm of 150 and gives
// the piece back, so every pick walks the same holder sets.
func BenchmarkPickerRarestManyPeers(b *testing.B) {
	pp, all := benchSwarm(4000, 150)
	pe := all[0]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pi, _ := pp.PickFor(pe)
		if pi == nil {
			b.Fatal("no piece picked")
		}
		pp.HandleCancelDownload(pe, pi.Index)
	}
}

// BenchmarkPickerBitfieldManyPeers is a new peer's bitfield arriving in a
// swarm of 150: one HandleHave per piece it has, then its disconnect.
func BenchmarkPickerBitfieldManyPeers(b *testing.B) {
	const n = 4000
	pp, _ := benchSwarm(n, 150)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pe := testPeer(n)
		for j := 0; j < n; j++ {
			if j%10 != 0 {
				pp.HandleHave(pe, uint32(j))
			}
		}
		pp.HandleDisconnect(pe)
	}
}
