package unchoker

import "testing"

type fakePeer struct {
	interested bool
	choking    bool
	optimistic bool
}

func (p *fakePeer) Choke()               { p.choking = true }
func (p *fakePeer) Unchoke()             { p.choking = false }
func (p *fakePeer) Choking() bool        { return p.choking }
func (p *fakePeer) Interested() bool     { return p.interested }
func (p *fakePeer) SetOptimistic(v bool) { p.optimistic = v }
func (p *fakePeer) Optimistic() bool     { return p.optimistic }
func (p *fakePeer) DownloadSpeed() int   { return 0 }
func (p *fakePeer) UploadSpeed() int     { return 0 }

// SetNumUnchoked changes how many interested peers are unchoked,
// for the per-torrent upload slots.
func TestSetNumUnchokedChangesSlots(t *testing.T) {
	u := New(2, 0)
	peers := make([]Peer, 5)
	for i := range peers {
		peers[i] = &fakePeer{interested: true, choking: true}
	}

	unchoked := func() int {
		count := 0
		for _, p := range peers {
			if !p.Choking() {
				count++
			}
		}
		return count
	}

	u.SetNumUnchoked(5)
	u.TickUnchoke(peers, false)
	if got := unchoked(); got != 5 {
		t.Fatalf("after SetNumUnchoked(5): %d unchoked, want 5", got)
	}

	u.SetNumUnchoked(1)
	u.TickUnchoke(peers, false)
	if got := unchoked(); got != 1 {
		t.Fatalf("after SetNumUnchoked(1): %d unchoked, want 1", got)
	}
}
