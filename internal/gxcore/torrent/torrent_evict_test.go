package torrent

import (
	"testing"
	"time"
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
