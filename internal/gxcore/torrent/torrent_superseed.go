package torrent

// torrent_superseed.go implements BEP 16 super-seeding (gextto fork).
//
// While a completed torrent is super-seeding it does not advertise its full
// bitfield. It tells each peer about a piece at a time and serves only the
// pieces it has offered to that peer, so the peers have to exchange data among
// themselves instead of downloading everything from the seed. This is meant for
// initial seeding only: it deliberately reduces the seed's upload throughput,
// as BEP 16 itself warns.
//
// Super-seeding is a seeding strategy: it never touches the queue, the seed
// policy or the bandwidth limits, and it does nothing while the torrent is
// still downloading.
//
// The pieces offered to a peer are tracked and never offered twice, which is
// what keeps the algorithm from stalling: clients skip the "have" message for a
// piece the seed already has, so the seed often cannot learn what the peer
// downloaded. Advancing on "not interested" (the peer has nothing left to ask
// for) plus a retry tick, and never repeating a piece, guarantees progress
// until every piece has been offered, after which the peer is released with the
// full bitfield.

import (
	"math/rand/v2"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// superSeedRetryInterval is how long a peer may stay uninterested before the
// seed offers it another piece.
const superSeedRetryInterval = 10 * time.Second

// superSeedPeer is the super-seeding state of one peer.
type superSeedPeer struct {
	// current and previous are the most recently advertised piece indices;
	// -1 means none. Requests for either are served.
	current  int32
	previous int32
	// retryAt is when to offer a new piece if the peer is not interested.
	retryAt time.Time
	// offered is the set of pieces already offered to this peer, so no piece
	// is offered twice. Monotonic: it only grows.
	offered map[uint32]struct{}
	// done is set once there is nothing left to offer: the peer was released
	// with the full bitfield.
	done bool
}

func newSuperSeedPeer(now time.Time) *superSeedPeer {
	return &superSeedPeer{current: -1, previous: -1, retryAt: now, offered: make(map[uint32]struct{})}
}

// advance records that newPiece has just been offered to the peer, replacing
// replace when it was the current one. The replaced piece is kept as previous,
// so a request already in flight for it is still served (like libtorrent).
func (st *superSeedPeer) advance(replace, newPiece int32) {
	if replace >= 0 && st.current == replace {
		st.current, st.previous = st.previous, st.current
	}
	st.previous = st.current
	st.current = newPiece
	st.offered[uint32(newPiece)] = struct{}{}
}

// superSeedActive reports whether super-seeding is in effect right now.
func (t *torrent) superSeedActive() bool {
	return t.superSeeding && t.completed
}

// setSuperSeeding toggles super-seeding and applies it to the running torrent.
// It runs in the torrent goroutine.
func (t *torrent) setSuperSeeding(v bool) {
	if t.superSeeding == v {
		return
	}
	t.superSeeding = v
	if err := t.session.resumer.WriteSuperSeeding(t.id, v); err != nil {
		t.log.Errorf("cannot write super-seeding flag to resume db: %s", err)
	}
	if !t.superSeedActive() {
		return // while downloading it takes effect on completion
	}
	if v {
		t.superSeedAllPeers()
	} else {
		t.superSeedStopAllPeers()
	}
}

// superSeedState returns (creating it if needed) the state of a peer.
func (t *torrent) superSeedState(pe *peer.Peer) *superSeedPeer {
	st, ok := t.superSeedPeers[pe]
	if !ok {
		st = newSuperSeedPeer(time.Now())
		t.superSeedPeers[pe] = st
	}
	return st
}

// superSeedAdvertised reports whether index is one of the two most recently
// advertised pieces of pe.
func (t *torrent) superSeedAdvertised(pe *peer.Peer, index int32) bool {
	st, ok := t.superSeedPeers[pe]
	if !ok {
		return false
	}
	return st.current == index || st.previous == index
}

// superSeedOffered reports whether index was ever offered to pe and the peer
// has not been released yet.
func (t *torrent) superSeedOffered(pe *peer.Peer, index int32) bool {
	if index < 0 {
		return false
	}
	st, ok := t.superSeedPeers[pe]
	if !ok {
		return false
	}
	if st.done {
		return true
	}
	_, ok = st.offered[uint32(index)]
	return ok
}

// superSeedStopPeer ends super-seeding for one connection, sending the full
// piece set so the peer may pick any piece again.
func (t *torrent) superSeedStopPeer(pe *peer.Peer) {
	delete(t.superSeedPeers, pe)
	t.sendBitfield(pe)
}

// superSeedStopAllPeers ends super-seeding for every connected peer.
func (t *torrent) superSeedStopAllPeers() {
	for pe := range t.peers {
		t.superSeedStopPeer(pe)
	}
}

// superSeedFinishPeer releases a peer with the full bitfield once every piece
// has been offered to it, so it can fetch whatever is still missing.
func (t *torrent) superSeedFinishPeer(pe *peer.Peer) {
	st, ok := t.superSeedPeers[pe]
	if !ok || st.done {
		return
	}
	st.done = true
	t.sendBitfield(pe)
}

// superSeedStartPeer bootstraps super-seeding on a peer. With fresh it also
// pretends to have nothing (a "have none" message), like libtorrent does for a
// new connection; peers already connected during the download are not told so,
// because a mid-stream "have none" would be wrong.
func (t *torrent) superSeedStartPeer(pe *peer.Peer, fresh bool) {
	if fresh && pe.FastEnabled {
		pe.SendMessage(peerprotocol.HaveNoneMessage{})
	}
	for i := 0; i < 2; i++ {
		if t.getPieceToSuperSeed(pe) < 0 {
			break
		}
		t.superSeedOfferNext(pe)
	}
	t.superSeedState(pe) // ensure state exists even if there was nothing to send
}

// superSeedAllPeers (re)bootstraps super-seeding on every connected peer, used
// when the mode is enabled or the torrent completes.
func (t *torrent) superSeedAllPeers() {
	for pe := range t.peers {
		t.superSeedStartPeer(pe, false)
	}
}

// superSeedAssign advertises a new piece to pe and remembers the replaced one,
// so a request already in flight for an earlier piece is still served. A
// negative newPiece finishes super-seeding for the peer.
func (t *torrent) superSeedAssign(pe *peer.Peer, replace int32, newPiece int32) {
	if newPiece < 0 {
		t.superSeedFinishPeer(pe)
		return
	}
	st := t.superSeedState(pe)
	pe.SendMessage(peerprotocol.HaveMessage{Index: uint32(newPiece)})
	st.advance(replace, newPiece)
	st.retryAt = time.Now()
}

// superSeedOfferNext offers pe the next piece it does not already have and that
// was not offered before, or releases it when everything has been offered.
func (t *torrent) superSeedOfferNext(pe *peer.Peer) {
	t.superSeedAssign(pe, -1, t.getPieceToSuperSeed(pe))
}

// superSeedHandleHave is called when a peer reports having a piece: if it is a
// piece we advertised, move on to a new one.
func (t *torrent) superSeedHandleHave(pe *peer.Peer, index int32) {
	if !t.superSeedAdvertised(pe, index) {
		return
	}
	t.superSeedOfferNext(pe)
}

// superSeedTick offers another piece to peers that stay uninterested and are
// not done yet.
func (t *torrent) superSeedTick(now time.Time) {
	if !t.superSeedActive() {
		return
	}
	for pe := range t.peers {
		st, ok := t.superSeedPeers[pe]
		if !ok {
			t.superSeedStartPeer(pe, false)
			continue
		}
		if st.done {
			continue
		}
		// A peer that is actively downloading the offered pieces is left alone;
		// one that is interested but stalled (speed back to zero) or not
		// interested at all gets another piece, so a peer can never be left
		// waiting for a piece the seed never offered it.
		if pe.PeerInterested && pe.DownloadSpeed() > 0 {
			st.retryAt = now
			continue
		}
		if now.Sub(st.retryAt) < superSeedRetryInterval {
			continue
		}
		t.superSeedOfferNext(pe)
	}
}

// getPieceToSuperSeed returns a piece the peer does not have and that was not
// offered to it yet, preferring the rarest and avoiding pieces currently
// advertised to another peer. It returns -1 when there is nothing to offer.
func (t *torrent) getPieceToSuperSeed(pe *peer.Peer) int32 {
	if t.bitfield == nil {
		return -1
	}
	st := t.superSeedState(pe)
	bits := pe.Bitfield
	const unavailable = 9999
	minAvailability := unavailable
	var candidates []int32
	for i := uint32(0); i < t.bitfield.Len(); i++ {
		if bits != nil && bits.Test(i) {
			continue // the peer already has it
		}
		if _, done := st.offered[i]; done {
			continue // never offer the same piece twice
		}
		availability := 0
		for other := range t.peers {
			if other == pe {
				continue
			}
			ost, ok := t.superSeedPeers[other]
			if !ok {
				continue
			}
			if ost.current == int32(i) || ost.previous == int32(i) {
				// Currently advertised to another peer: avoid duplicating it,
				// but keep it as a last resort.
				availability = unavailable
				break
			}
			if other.Bitfield != nil && other.Bitfield.Test(i) {
				availability++
			}
		}
		if availability > minAvailability {
			continue
		}
		if availability == minAvailability {
			candidates = append(candidates, int32(i))
			continue
		}
		minAvailability = availability
		candidates = candidates[:0]
		candidates = append(candidates, int32(i))
	}
	if len(candidates) == 0 {
		return -1
	}
	return candidates[rand.IntN(len(candidates))]
}
