package torrent

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// TestWorseRank checks the ordering used to pick the connection to drop: each
// criterion is a tie-break of the previous one, so the result is deterministic
// and never depends on map iteration order.
func TestWorseRank(t *testing.T) {
	now := time.Now()
	old := now.Add(-time.Hour)

	base := peerRank{useful: true, speed: 1000, connectedAt: now, priority: 5, addr: "10.0.0.1:1"}

	mut := func(f func(*peerRank)) peerRank {
		r := base
		f(&r)
		return r
	}

	cases := []struct {
		name string
		a, b peerRank
		want bool // a is worse than b
	}{
		{"useless before useful", mut(func(r *peerRank) { r.useful = false }), base, true},
		{"useful not worse than useless", base, mut(func(r *peerRank) { r.useful = false }), false},
		{"snubbed before productive", mut(func(r *peerRank) { r.snubbed = true }), base, true},
		{"slower is worse", mut(func(r *peerRank) { r.speed = 10 }), base, true},
		{"newer is worse", mut(func(r *peerRank) { r.connectedAt = now.Add(time.Minute) }), base, true},
		{"older is not worse", mut(func(r *peerRank) { r.connectedAt = old }), base, false},
		{"lower priority is worse", mut(func(r *peerRank) { r.priority = 1 }), base, true},
		{"address breaks the tie", mut(func(r *peerRank) { r.addr = "10.0.0.0:1" }), base, true},
	}
	for _, c := range cases {
		if got := worseRank(c.a, c.b); got != c.want {
			t.Errorf("%s: worseRank = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestWorseRankTotalOrder ensures two different connections always have a
// defined drop order (no equivalence classes that would make eviction
// nondeterministic).
func TestWorseRankTotalOrder(t *testing.T) {
	now := time.Now()
	a := peerRank{useful: true, speed: 1, connectedAt: now, priority: 1, addr: "10.0.0.1:1"}
	b := peerRank{useful: true, speed: 1, connectedAt: now, priority: 1, addr: "10.0.0.2:2"}
	if worseRank(a, b) == worseRank(b, a) {
		t.Fatalf("equal order for distinct peers: worseRank(a,b)=%v worseRank(b,a)=%v",
			worseRank(a, b), worseRank(b, a))
	}
}

// TestPeerWantsNothing: a seed recognises a peer that already holds every piece
// it holds (full or partial seed) and keeps one that still misses something;
// a downloader never applies the rule, and an unknown availability is kept.
func TestPeerWantsNothing(t *testing.T) {
	const n = 64
	tr := &torrent{pieces: make([]piece.Piece, n), bitfield: bitfield.New(n)}
	for i := uint32(0); i < 32; i++ {
		tr.bitfield.Set(i) // a partial seed: the second half is deselected
	}
	pe := &peer.Peer{Bitfield: bitfield.New(n)}
	for i := uint32(0); i < n; i++ {
		pe.Bitfield.Set(i)
	}
	if tr.peerWantsNothing(pe) {
		t.Fatal("a downloader applied the seed-to-seed rule")
	}
	tr.completed = true
	if !tr.peerWantsNothing(pe) {
		t.Fatal("a full seed was kept by a seed")
	}
	pe.Bitfield.Clear(40) // still holds all of ours
	if !tr.peerWantsNothing(pe) {
		t.Fatal("a peer with every piece we hold was kept")
	}
	pe.Bitfield.Clear(5)
	if tr.peerWantsNothing(pe) {
		t.Fatal("a peer missing one of our pieces was dropped")
	}
	if tr.peerWantsNothing(&peer.Peer{}) {
		t.Fatal("a peer without a known bitfield was judged")
	}
}
