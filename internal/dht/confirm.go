package dht

import (
	"expvar"
	"time"
)

// Multi-path confirmation observability (docs/measurements.md §4): track which
// responding nodes reported each peer for an info-hash, so the engine can say
// how often a peer is confirmed by more than one path. It is read-only
// instrumentation — it does not change which peers are used — and it is the
// datum that decides whether a full multi-path lookup is worth its cost.

const (
	// peerConfirmWindow is how long a peer/reporter observation stays valid.
	peerConfirmWindow = 2 * time.Minute
	// peerConfirmMax bounds the memory (entries); new keys are dropped under
	// pressure so a hostile swarm cannot grow the map without bound.
	peerConfirmMax = 4096
)

var peersConfirmedMulti = expvar.NewInt("totalPeersConfirmedMulti")

type peerConfirmKey struct {
	ih   InfoHash
	peer string
}

type peerConfirmEntry struct {
	reporters map[string]struct{}
	seen      time.Time
}

// reporterKey is the identity of a reporting path: the node's subnet (the
// anti-Sybil unit of the routing table), so one host on many ports or a whole
// /24 counts as a single path and cannot confirm its own peers. A node without
// a usable IP falls back to its address.
func reporterKey(node *remoteNode) string {
	if k := subnetKey(node.address.IP); k != "" {
		return k
	}
	return node.addressBinaryFormat
}

// notePeerReport records that the path reporterAddr (see reporterKey) reported peer for ih,
// and counts the peer once when a second distinct reporter confirms it. Runs in
// the DHT run loop.
func (d *DHT) notePeerReport(ih InfoHash, peer, reporterAddr string) {
	if d.confirm == nil {
		return
	}
	key := peerConfirmKey{ih: ih, peer: peer}
	e := d.confirm[key]
	if e == nil {
		if len(d.confirm) >= peerConfirmMax {
			return
		}
		e = &peerConfirmEntry{reporters: make(map[string]struct{}, 2)}
		d.confirm[key] = e
	}
	e.seen = time.Now()
	if _, ok := e.reporters[reporterAddr]; ok {
		return
	}
	e.reporters[reporterAddr] = struct{}{}
	if len(e.reporters) == 2 {
		peersConfirmedMulti.Add(1)
	}
}

// expirePeerConfirm drops the entries not updated within the window. Runs in
// the run loop (cleanup tick).
func (d *DHT) expirePeerConfirm(now time.Time) {
	for k, e := range d.confirm {
		if now.Sub(e.seen) > peerConfirmWindow {
			delete(d.confirm, k)
		}
	}
}
