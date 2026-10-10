package torrent

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
)

func allPieces(n uint32) *bitfield.Bitfield {
	bf := bitfield.New(n)
	for i := uint32(0); i < n; i++ {
		bf.Set(i)
	}
	return bf
}

func bitsWith(n uint32, set ...uint32) *bitfield.Bitfield {
	bf := bitfield.New(n)
	for _, i := range set {
		bf.Set(i)
	}
	return bf
}

// superSeedTorrent builds the minimal torrent state getPieceToSuperSeed reads.
func superSeedTorrent(pieces uint32) *torrent {
	return &torrent{
		bitfield:       allPieces(pieces),
		peers:          map[*peer.Peer]struct{}{},
		superSeedPeers: map[*peer.Peer]*superSeedPeer{},
	}
}

// superSeedPeerWith adds a peer with the given remote bitfield and state.
func superSeedPeerWith(tr *torrent, remote *bitfield.Bitfield, current int32) *peer.Peer {
	pe := &peer.Peer{Bitfield: remote}
	tr.peers[pe] = struct{}{}
	st := newSuperSeedPeer(time.Time{})
	st.current = current
	tr.superSeedPeers[pe] = st
	return pe
}

func TestSuperSeedAdvance(t *testing.T) {
	st := newSuperSeedPeer(time.Time{})

	// Bootstrap: two pieces advertised, most recent first.
	st.advance(-1, 10)
	st.advance(-1, 11)
	if st.current != 11 || st.previous != 10 {
		t.Fatalf("after bootstrap current=%d previous=%d, want 11/10", st.current, st.previous)
	}

	// The peer completed the current piece: the older advertised piece is kept
	// so an in-flight request for it is still served.
	st.advance(11, 12)
	if st.current != 12 || st.previous != 10 {
		t.Fatalf("after replacing current current=%d previous=%d, want 12/10", st.current, st.previous)
	}

	// Replacing the previous piece keeps the current as the tail.
	st.advance(10, 13)
	if st.current != 13 || st.previous != 12 {
		t.Fatalf("after replacing previous current=%d previous=%d, want 13/12", st.current, st.previous)
	}

	// A retry (replace = -1) just pushes the new piece.
	st.advance(-1, 14)
	if st.current != 14 || st.previous != 13 {
		t.Fatalf("after retry current=%d previous=%d, want 14/13", st.current, st.previous)
	}

	// Every offered piece is remembered, so it is never offered twice.
	for _, i := range []uint32{10, 11, 12, 13, 14} {
		if _, ok := st.offered[i]; !ok {
			t.Fatalf("piece %d not recorded as offered", i)
		}
	}
}

func TestGetPieceToSuperSeedSkipsPeerHave(t *testing.T) {
	tr := superSeedTorrent(4)
	// The peer already has 0, 1 and 2: only 3 is left to offer.
	pe := superSeedPeerWith(tr, bitsWith(4, 0, 1, 2), -1)
	for i := 0; i < 100; i++ {
		if got := tr.getPieceToSuperSeed(pe); got != 3 {
			t.Fatalf("got piece %d, want 3", got)
		}
	}
}

func TestGetPieceToSuperSeedAvoidsAdvertised(t *testing.T) {
	tr := superSeedTorrent(4)
	other := superSeedPeerWith(tr, bitfield.New(4), 0)
	_ = other
	pe := superSeedPeerWith(tr, bitfield.New(4), -1)
	for i := 0; i < 100; i++ {
		got := tr.getPieceToSuperSeed(pe)
		if got == 0 {
			t.Fatalf("piece 0 is already advertised to another peer, must be avoided")
		}
	}
}

func TestGetPieceToSuperSeedPrefersRarest(t *testing.T) {
	tr := superSeedTorrent(4)
	// Another peer has piece 1, making it the most available; 1 must be avoided.
	superSeedPeerWith(tr, bitsWith(4, 1), -1)
	pe := superSeedPeerWith(tr, bitfield.New(4), -1)
	for i := 0; i < 100; i++ {
		if got := tr.getPieceToSuperSeed(pe); got == 1 {
			t.Fatalf("piece 1 is the most available, must not be preferred")
		}
	}
}

func TestGetPieceToSuperSeedNeverRepeats(t *testing.T) {
	tr := superSeedTorrent(3)
	pe := superSeedPeerWith(tr, bitfield.New(3), -1)
	seen := map[int32]bool{}
	for i := 0; i < 3; i++ {
		got := tr.getPieceToSuperSeed(pe)
		if got < 0 || seen[got] {
			t.Fatalf("offer %d invalid or repeated (seen=%v)", got, seen)
		}
		seen[got] = true
		tr.superSeedState(pe).advance(-1, got)
	}
	if got := tr.getPieceToSuperSeed(pe); got != -1 {
		t.Fatalf("got %d, want -1 after every piece was offered", got)
	}
}

func TestGetPieceToSuperSeedNothingToSend(t *testing.T) {
	tr := superSeedTorrent(3)
	// The peer already has every piece.
	pe := superSeedPeerWith(tr, bitsWith(3, 0, 1, 2), -1)
	if got := tr.getPieceToSuperSeed(pe); got != -1 {
		t.Fatalf("got %d, want -1 when the peer has everything", got)
	}
}

func TestSuperSeedAdvertised(t *testing.T) {
	tr := superSeedTorrent(4)
	pe := &peer.Peer{Bitfield: bitfield.New(4)}
	if tr.superSeedAdvertised(pe, 0) {
		t.Fatal("a peer without state must not report any advertised piece")
	}
	st := newSuperSeedPeer(time.Time{})
	st.current, st.previous = 2, 1
	st.offered[1] = struct{}{}
	st.offered[2] = struct{}{}
	tr.superSeedPeers[pe] = st
	if !tr.superSeedAdvertised(pe, 2) || !tr.superSeedAdvertised(pe, 1) {
		t.Fatal("current and previous must both be advertised")
	}
	if tr.superSeedAdvertised(pe, 0) {
		t.Fatal("only current and previous are advertised")
	}
	if !tr.superSeedOffered(pe, 2) || !tr.superSeedOffered(pe, 1) || tr.superSeedOffered(pe, 0) {
		t.Fatal("only offered pieces are reported as offered")
	}
}
