package torrent

import (
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerpriority"
)

// peerRank is a snapshot of the signals that decide which connection is the
// cheapest to lose. It is a value type so the ordering can be unit-tested
// without building real peers.
type peerRank struct {
	// useful is true while the peer can still give us something: pieces we are
	// missing, a piece it is serving right now, or (while we seed) its interest
	// in what we hold.
	useful bool
	// snubbed is set when the peer answers too slowly.
	snubbed bool
	// speed is the peer's smoothed upload+download rate in bytes/s: the single
	// "is this connection productive" number.
	speed int
	// connectedAt is when the connection was established.
	connectedAt time.Time
	// priority is the canonical BEP 40 peer priority to us.
	priority peerpriority.Priority
	// addr breaks ties deterministically.
	addr string
}

// worseRank reports whether a should be dropped before b. It is a strict total
// order: useless first, then snubbed, then the slowest, then the newest, then
// the lowest BEP 40 priority, then the address. Every criterion is a tie-break,
// so the result never depends on Go's map iteration order.
func worseRank(a, b peerRank) bool {
	if a.useful != b.useful {
		return !a.useful
	}
	if a.snubbed != b.snubbed {
		return a.snubbed
	}
	if a.speed != b.speed {
		return a.speed < b.speed
	}
	if !a.connectedAt.Equal(b.connectedAt) {
		return a.connectedAt.After(b.connectedAt)
	}
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	return a.addr < b.addr
}

// rankFor builds the ranking of a live peer. It runs in the torrent goroutine,
// where the peer fields are stable.
func (t *torrent) rankFor(pe *peer.Peer) peerRank {
	return peerRank{
		useful:      t.peerUseful(pe),
		snubbed:     pe.Snubbed,
		speed:       pe.DownloadSpeed() + pe.UploadSpeed(),
		connectedAt: pe.ConnectedAt,
		priority:    t.peerPriority(pe),
		addr:        pe.Addr().String(),
	}
}

// newPeerGrace is how long a fresh connection counts as useful whatever it
// advertised: bitfield, interest and unchoke take a few round trips, and
// judging earlier would drop a just-dialled peer at the next tick and dial
// again in a loop.
const newPeerGrace = 30 * time.Second

// peerUseful reports whether keeping the peer can still buy us something. A peer
// we cannot judge (no bitfield yet, e.g. still handshaking, or connected less
// than newPeerGrace ago) is kept: dropping it would only churn the connection.
func (t *torrent) peerUseful(pe *peer.Peer) bool {
	if pe.PeerInterested || pe.Downloading {
		return true
	}
	if pe.Bitfield == nil || t.bitfield == nil || time.Since(pe.ConnectedAt) < newPeerGrace {
		return true
	}
	// A full or partial seed wants nothing: only the peer's interest counts.
	if t.completed {
		return false
	}
	// True when the peer advertises a piece we miss and want (pieces of
	// deselected files do not count).
	return t.peerHasWanted(pe)
}

// peerWantsNothing reports a peer that cannot take anything from a seed: it
// already holds every piece we hold. Both sides only upload, so the
// connection costs a socket, keep-alives and a slot in every per-peer loop for
// nothing; libtorrent closes it the same way. It is judged only once the
// peer's availability is known, and only while we seed.
func (t *torrent) peerWantsNothing(pe *peer.Peer) bool {
	return t.completed && pe.Bitfield != nil && t.bitfield != nil && !t.bitfield.HasAnyNotIn(pe.Bitfield)
}

// closeIfBothSeeds drops a seed-to-seed connection as soon as the peer's
// availability shows it. It runs in the torrent goroutine.
func (t *torrent) closeIfBothSeeds(pe *peer.Peer) bool {
	if pe.Closed || !t.peerWantsNothing(pe) {
		return false
	}
	t.log.Debugf("closing %s: both sides are seeds", pe.Addr().String())
	// Remember it: a seeding torrent dials a few candidates looking for
	// leechers, and this one has nothing to ask for.
	rememberPeerIP(t.seedPeerIPs, pe.IP())
	t.closePeer(pe)
	return true
}

// peerPriority returns the canonical BEP 40 priority of the peer to us, or 0
// when it cannot be computed (no usable external IP).
func (t *torrent) peerPriority(pe *peer.Peer) peerpriority.Priority {
	addr := pe.Addr()
	if addr == nil || t.externalIP == nil || !t.externalIP.IsGlobalUnicast() {
		return 0
	}
	return peerpriority.Calculate(&net.TCPAddr{IP: t.externalIP, Port: t.port}, addr)
}

// worstPeer returns the peer that should be dropped first, or nil if there is
// none. It runs in the torrent goroutine.
func (t *torrent) worstPeer() *peer.Peer {
	var (
		worst     *peer.Peer
		worstRank peerRank
	)
	for pe := range t.peers {
		r := t.rankFor(pe)
		if worst == nil || worseRank(r, worstRank) {
			worst, worstRank = pe, r
		}
	}
	return worst
}

// enforceConnectionLimit closes the worst peers over the per-torrent cap. It
// runs in the torrent goroutine.
func (t *torrent) enforceConnectionLimit() {
	if t.maxConnections <= 0 {
		return
	}
	for len(t.peers) > t.maxConnections {
		worst := t.worstPeer()
		if worst == nil {
			return
		}
		t.log.Debugf("dropping worst peer %s (%d over the cap)", worst.Addr().String(), len(t.peers)-t.maxConnections)
		t.closePeer(worst)
	}
}

// prunePeers enforces the cap and, when a slot is held by a peer that brings us
// nothing while candidate addresses are queued, releases it so a fresh dial (or
// incoming connection) can take its place. It drops at most one peer per call:
// an idle torrent with only useless peers would otherwise reconnect in a tight
// loop. It runs in the torrent goroutine, from the unchoke tick.
func (t *torrent) prunePeers() {
	t.enforceConnectionLimit()
	if t.maxConnections <= 0 || len(t.peers) < t.maxConnections || t.addrList.Len() == 0 {
		return
	}
	if w := t.worstPeer(); w != nil && !t.peerUseful(w) {
		t.log.Debugf("dropping useless peer %s to try a queued address", w.Addr().String())
		t.closePeer(w)
	}
}
