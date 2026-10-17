package piecepicker

import (
	"math/rand"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// TestHoldersMatchAReferenceModel pins the books heldBy and the holder count
// rest on: through any sequence of have, don't-have and disconnect, every
// connected peer's bitfield matches an independent model of who has what, each
// piece counts exactly its holders, and the availability count follows. A
// disconnect repeated by mistake must not uncount twice.
func TestHoldersMatchAReferenceModel(t *testing.T) {
	const n, peers, steps = 64, 12, 20000
	rng := rand.New(rand.NewSource(7))
	pp := New(benchPieces(n), 8, nil, false, false)
	all := make([]*peer.Peer, peers)
	model := make([]map[uint32]bool, peers)
	for i := range all {
		all[i] = testPeer(n)
		model[i] = map[uint32]bool{}
	}
	for step := 0; step < steps; step++ {
		k := rng.Intn(peers)
		i := uint32(rng.Intn(n))
		switch op := rng.Intn(20); {
		case op == 0:
			pp.HandleDisconnect(all[k])
			pp.HandleDisconnect(all[k]) // a repeated disconnect is harmless
			all[k] = testPeer(n)        // a reconnect is a new peer
			model[k] = map[uint32]bool{}
		case op < 6:
			pp.HandleDontHave(all[k], i)
			delete(model[k], i)
		default:
			pp.HandleHave(all[k], i)
			model[k][i] = true
		}
		if step%97 != 0 {
			continue
		}
		var available uint32
		for j := range pp.pieces {
			mp := &pp.pieces[j]
			var holders uint32
			for k, pe := range all {
				want := model[k][uint32(j)]
				if want {
					holders++
				}
				if got := mp.heldBy(pe); got != want {
					t.Fatalf("step %d piece %d peer %d: heldBy=%v, model %v", step, j, k, got, want)
				}
			}
			if mp.holders != holders {
				t.Fatalf("step %d piece %d: holders=%d, model %d", step, j, mp.holders, holders)
			}
			if holders > 0 {
				available++
			}
		}
		if available != pp.available {
			t.Fatalf("step %d: available=%d, model %d", step, pp.available, available)
		}
	}
}

// TestPickHighFollowsRefreshPriorities pins the high-priority counter: no scan
// while no piece is high, and a piece raised by a priority change is picked
// first once the picker is refreshed.
func TestPickHighFollowsRefreshPriorities(t *testing.T) {
	pieces := benchPieces(8)
	pp := New(pieces, 8, nil, false, false)
	pe := testPeer(8)
	for i := uint32(0); i < 8; i++ {
		pp.HandleHave(pe, i)
	}
	if pp.highPriority != 0 || pp.pickHigh(pe) != nil {
		t.Fatal("no piece is high priority yet")
	}
	pieces[5].Priority = piece.PriorityHigh
	pp.RefreshPriorities()
	if mp := pp.pickHigh(pe); mp == nil || mp.Index != 5 {
		t.Fatalf("pickHigh = %v, want piece 5", mp)
	}
	pieces[5].Skip = true
	pp.RefreshPriorities()
	if pp.highPriority != 0 {
		t.Fatal("a skipped piece is not high priority")
	}
}
