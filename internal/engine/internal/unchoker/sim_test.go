package unchoker

// sim_test.go is a deterministic harness for the choking algorithm (gextto
// fork). It drives the real Unchoker with synthetic peers whose download and
// upload speeds are scripted, so the allocation decisions can be characterized
// and compared across algorithm changes without a real swarm.
//
// The unchoker is the core of "choking quality": who gets an upload slot and
// for how long. This harness pins that down repeatably. Throughput on a real
// swarm depends on many more factors, so cross-client comparisons
// (libtorrent/qBittorrent) still need a real swarm; see
// docs/evoluzione.md (section 6).

import (
	"fmt"
	"testing"
)

// simPeer is a synthetic peer with scripted speeds and observed untouched
// spans. It implements the Peer interface used by the Unchoker.
type simPeer struct {
	name          string
	interested    bool
	choking       bool
	optimistic    bool
	downloadSpeed int
	uploadSpeed   int

	unchokedTicks int
	optimisticHit int
}

func (p *simPeer) Choke()               { p.choking = true }
func (p *simPeer) Unchoke()             { p.choking = false }
func (p *simPeer) Choking() bool        { return p.choking }
func (p *simPeer) Interested() bool     { return p.interested }
func (p *simPeer) SetOptimistic(v bool) { p.optimistic = v }
func (p *simPeer) Optimistic() bool     { return p.optimistic }
func (p *simPeer) DownloadSpeed() int   { return p.downloadSpeed }
func (p *simPeer) UploadSpeed() int     { return p.uploadSpeed }

func (p *simPeer) String() string {
	return fmt.Sprintf("%s(dl=%d ul=%d)", p.name, p.downloadSpeed, p.uploadSpeed)
}

// simulation drives an Unchoker over a set of peers.
type simulation struct {
	u     *Unchoker
	peers []*simPeer
}

func newSimulation(numUnchoked, numOptimistic int, peers ...*simPeer) *simulation {
	s := &simulation{u: New(numUnchoked, numOptimistic), peers: peers}
	for _, p := range peers {
		p.choking = true
	}
	return s
}

// peersOf returns a fresh slice of the Peer interface. The unchoker filters it
// in place, so a fresh slice per tick keeps the tests independent.
func (s *simulation) peersOf() []Peer {
	out := make([]Peer, len(s.peers))
	for i, p := range s.peers {
		out[i] = p
	}
	return out
}

// tick advances one unchoke period (10 s in the real engine).
func (s *simulation) tick(completed bool) {
	s.u.TickUnchoke(s.peersOf(), completed)
	for _, p := range s.peers {
		if !p.choking {
			p.unchokedTicks++
			if p.optimistic {
				p.optimisticHit++
			}
		}
	}
}

func (s *simulation) unchoked() []*simPeer {
	var out []*simPeer
	for _, p := range s.peers {
		if !p.choking {
			out = append(out, p)
		}
	}
	return out
}

func names(peers []*simPeer) []string {
	out := make([]string, len(peers))
	for i, p := range peers {
		out[i] = p.name
	}
	return out
}

func hasName(peers []*simPeer, name string) bool {
	for _, p := range peers {
		if p.name == name {
			return true
		}
	}
	return false
}

// makePeers builds n interested, choked peers with descending speeds.
func makeSpeeds(prefix string, download []int, upload []int) []*simPeer {
	peers := make([]*simPeer, len(download))
	for i := range download {
		peers[i] = &simPeer{
			name:          fmt.Sprintf("%s%d", prefix, i),
			interested:    true,
			choking:       true,
			downloadSpeed: download[i],
			uploadSpeed:   upload[i],
		}
	}
	return peers
}

// TestUnchokerPrefersFastDownloadersWhileDownloading pins the leech-side
// policy: with no upload reciprocation to reward, slots go to the fastest
// downloaders.
func TestUnchokerPrefersFastDownloadersWhileDownloading(t *testing.T) {
	peers := makeSpeeds("p", []int{100, 90, 80, 10, 5}, []int{10, 20, 30, 40, 50})
	s := newSimulation(2, 0, peers...)
	s.tick(false)
	got := s.unchoked()
	if len(got) != 2 || !hasName(got, "p0") || !hasName(got, "p1") {
		t.Fatalf("downloading slots = %v, want the two fastest downloaders p0,p1", names(got))
	}
}

// TestUnchokerPrefersFastUploadersWhenSeeding pins the seed-side policy: once
// complete, slots reward the peers that upload the most to us.
func TestUnchokerPrefersFastUploadersWhenSeeding(t *testing.T) {
	peers := makeSpeeds("p", []int{5, 4, 3, 2, 1}, []int{10, 90, 80, 20, 70})
	s := newSimulation(2, 0, peers...)
	s.tick(true)
	got := s.unchoked()
	if len(got) != 2 || !hasName(got, "p1") || !hasName(got, "p2") {
		t.Fatalf("seeding slots = %v, want the two fastest uploaders p1,p2", names(got))
	}
}

// TestUnchokerFastUnchokeFillsFreeSlot checks a newly interested peer is
// unchoked at once while a regular slot is free, without waiting a period.
func TestUnchokerFastUnchokeFillsFreeSlot(t *testing.T) {
	a := &simPeer{name: "a", interested: true, choking: true, downloadSpeed: 100}
	b := &simPeer{name: "b", interested: true, choking: true, downloadSpeed: 50}
	s := newSimulation(2, 0, a, b)
	s.tick(false)
	if !hasName(s.unchoked(), "a") || !hasName(s.unchoked(), "b") {
		t.Fatal("both peers should hold the two free slots")
	}

	c := &simPeer{name: "c", interested: true, choking: true, downloadSpeed: 200}
	s.peers = append(s.peers, c)
	s.u.SetNumUnchoked(3)
	s.u.FastUnchoke(c)
	if !hasName(s.unchoked(), "c") {
		t.Fatal("FastUnchoke must take the free slot immediately")
	}
}

// TestUnchokerOptimisticNeverStealsRegularSlot checks the optimistic unchoke is
// chosen from the peers outside the regular slots.
func TestUnchokerOptimisticNeverStealsRegularSlot(t *testing.T) {
	peers := makeSpeeds("p", []int{100, 80, 60, 20, 10}, []int{1, 1, 1, 1, 1})
	s := newSimulation(2, 1, peers...)
	// Round 0 is the optimistic round; run a full cycle so we hit it.
	optimisticSeen := map[string]int{}
	for i := 0; i < 300; i++ {
		s.tick(false)
		for _, p := range s.peers {
			if p.optimistic {
				optimisticSeen[p.name]++
				if p.name == "p0" || p.name == "p1" {
					t.Fatalf("optimistic pick %s must not be a regular slot", p.name)
				}
			}
		}
	}
	if len(optimisticSeen) < 2 {
		t.Fatalf("optimistic unchoke did not rotate across peers: %v", optimisticSeen)
	}
}

// TestUnchokerNeverExceedsSlots checks the total number of unchoked peers stays
// within the regular plus optimistic budgets.
func TestUnchokerNeverExceedsSlots(t *testing.T) {
	const numUnchoked, numOptimistic = 3, 2
	var all []*simPeer
	for i := 0; i < 20; i++ {
		all = append(all, &simPeer{name: fmt.Sprintf("p%d", i), interested: true, choking: true, downloadSpeed: 100, uploadSpeed: 100})
	}
	s := newSimulation(numUnchoked, numOptimistic, all...)
	for i := 0; i < 100; i++ {
		s.tick(false)
		got := s.unchoked()
		if len(got) > numUnchoked+numOptimistic {
			t.Fatalf("tick %d: %d unchoked, budget is %d", i, len(got), numUnchoked+numOptimistic)
		}
	}
}

// TestUnchokerFairnessSeeding measures how evenly slots are shared among a set
// of equal uploaders in seed mode: over many ticks each should hold a slot.
func TestUnchokerFairnessSeeding(t *testing.T) {
	const n = 6
	var all []*simPeer
	for i := 0; i < n; i++ {
		all = append(all, &simPeer{name: fmt.Sprintf("p%d", i), interested: true, choking: true, uploadSpeed: 100})
	}
	s := newSimulation(3, 1, all...)
	for i := 0; i < 300; i++ {
		s.tick(true)
	}
	for _, p := range s.peers {
		if p.unchokedTicks == 0 {
			t.Fatalf("%s never got an upload slot in %d ticks", p.name, 300)
		}
	}
}

// BenchmarkTickUnchokeSeeding is a stable baseline for the seed-side tick cost.
func BenchmarkTickUnchokeSeeding(b *testing.B) {
	var all []*simPeer
	for i := 0; i < 50; i++ {
		all = append(all, &simPeer{name: fmt.Sprintf("p%d", i), interested: true, choking: true, uploadSpeed: 100})
	}
	s := newSimulation(5, 2, all...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.u.TickUnchoke(s.peersOf(), true)
	}
}
