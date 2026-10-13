package torrent

import (
	"time"

	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// BenchmarkUpdateInterestNothingWanted is the worst case of the interest check
// that runs for every peer at every completed piece: a peer that holds only
// what we already have forces a scan of the whole bitfield (100k pieces).
func BenchmarkUpdateInterestNothingWanted(b *testing.B) {
	const n = 100_000
	tr := &torrent{pieces: make([]piece.Piece, n), bitfield: bitfield.New(n)}
	pe := &peer.Peer{Bitfield: bitfield.New(n)}
	for i := uint32(0); i < n; i += 2 {
		tr.bitfield.Set(i)
		pe.Bitfield.Set(i)
	}
	b.ReportAllocs()
	for b.Loop() {
		tr.updateInterestedState(pe)
	}
	if pe.ClientInterested {
		b.Fatal("interested in a peer with nothing we miss")
	}
}

// TestInterestIgnoresDeselectedPieces: a peer holding only pieces of
// deselected files is not interesting (we would hold one of its unchoke slots
// for nothing), and becomes so once it has a wanted piece.
func TestInterestIgnoresDeselectedPieces(t *testing.T) {
	const n = 200
	tr := &torrent{pieces: make([]piece.Piece, n), bitfield: bitfield.New(n), skipBits: bitfield.New(n)}
	pe := &peer.Peer{Bitfield: bitfield.New(n)}
	for i := uint32(100); i < n; i++ {
		tr.skipBits.Set(i)
		pe.Bitfield.Set(i)
	}
	if tr.peerHasWanted(pe) {
		t.Fatal("deselected pieces made the peer interesting")
	}
	pe.Bitfield.Set(7)
	if !tr.peerHasWanted(pe) {
		t.Fatal("a wanted piece was not seen")
	}
}

// TestPeerUsefulness: a fresh connection is kept during its grace period, a
// seed (full or partial) keeps only interested peers, and a downloader keeps
// peers that hold a wanted piece.
func TestPeerUsefulness(t *testing.T) {
	const n = 64
	old := time.Now().Add(-time.Hour)
	tr := &torrent{pieces: make([]piece.Piece, n), bitfield: bitfield.New(n)}
	pe := &peer.Peer{Bitfield: bitfield.New(n), ConnectedAt: old}
	if tr.peerUseful(pe) {
		t.Fatal("a peer with nothing we want is useful")
	}
	pe.ConnectedAt = time.Now()
	if !tr.peerUseful(pe) {
		t.Fatal("a fresh connection was judged before its grace period")
	}
	pe.ConnectedAt = old
	pe.Bitfield.Set(3)
	if !tr.peerUseful(pe) {
		t.Fatal("a peer with a wanted piece is useless")
	}
	tr.completed = true
	if tr.peerUseful(pe) {
		t.Fatal("a seed kept a peer that is not interested")
	}
	pe.PeerInterested = true
	if !tr.peerUseful(pe) {
		t.Fatal("a seed dropped an interested peer")
	}
}
