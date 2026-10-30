package torrent

import (
	"errors"

	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// Seed mode (libtorrent seed_mode, qBittorrent "skip hash check"): a torrent
// added over data its owner vouches for starts seeding at once, without the
// full check. Every piece is advertised, but a piece is hashed the first time
// a peer asks for it, and its bytes leave only after that hash matches: no
// unverified byte is ever uploaded. When every piece has been checked the
// torrent leaves seed mode by itself. A piece that fails ends seed mode for
// the whole torrent: the claim was wrong, so the data is checked again from
// disk (the same verification as a manual recheck) and the bad pieces are
// downloaded like any missing piece.
//
// The verified set is not persisted: after a restart in seed mode the pieces
// are claimed again and checked again lazily, which is safe and costs one
// hash per piece served.

// seedModeMaxHashing bounds the lazy hashes in flight per torrent: each holds
// one piece in memory while it is read and hashed.
const seedModeMaxHashing = 4

// seedModeRequest is a block request waiting for its piece's first hash.
type seedModeRequest struct {
	pe  *peer.Peer
	msg peerprotocol.RequestMessage
}

// seedHashResult reports one lazy piece hash to the run loop. gen ties it to
// the seed-mode session that started it, so a result arriving after a stop
// or a restart is dropped.
type seedHashResult struct {
	gen   uint64
	index uint32
	ok    bool
	err   error
}

// errSeedModeMismatch is the hash failure of a piece claimed in seed mode.
var errSeedModeMismatch = errors.New("the data does not match the torrent")

// enterSeedMode claims every piece as present and arms the lazy check. It
// runs when the files are allocated, instead of the full verification.
func (t *torrent) enterSeedMode() {
	n := t.info().NumPieces
	bf := bitfield.New(n)
	for i := uint32(0); i < n; i++ {
		bf.Set(i)
	}
	t.mBitfield.Lock()
	t.bitfield = bf
	t.mBitfield.Unlock()
	t.seedVerified = bitfield.New(n)
	t.seedPending = make(map[uint32][]seedModeRequest)
	t.seedParked = make(map[*peer.Peer]int)
	t.seedHashing = make(map[uint32]struct{})
	t.seedGen++
	t.log.Info("seed mode: the files are trusted as complete without the initial check; each piece is checked the first time a peer asks for it")
}

// seedModeAllocated decides, once the files are open, whether seed mode can
// hold: with missing files the claim is already false, so the torrent leaves
// seed mode and the usual verification runs instead.
func (t *torrent) seedModeAllocated(al *allocator.Allocator) {
	if !t.seedMode {
		return
	}
	if al.HasMissing {
		t.log.Warning("seed mode ended: some files of the torrent are missing on disk, so every file is checked instead")
		t.setSeedMode(false)
		return
	}
	t.enterSeedMode()
}

// seedUnverified reports a piece still claimed but not yet hashed.
func (t *torrent) seedUnverified(i uint32) bool {
	return t.seedMode && t.seedVerified != nil && !t.seedVerified.Has(i)
}

// setSeedMode records the flag in memory and in the resume record.
func (t *torrent) setSeedMode(v bool) {
	t.seedMode = v
	if !v {
		t.seedVerified = nil
		t.seedPending = nil
		t.seedParked = nil
		t.seedHashing = nil
		t.seedGen++
	}
	if err := t.session.resumer.WriteSeedMode(t.id, v); err != nil {
		t.log.Errorf("cannot write seed mode flag to resume db: %s", err)
	}
}

// queueSeedModeRequest parks a request until its piece is hashed, starting
// the hash when a slot is free. A peer parks at most MaxRequestsIn requests,
// the bound its upload queue has anyway: past it the request is refused (or
// dropped without the fast extension), so a peer asking for blocks of
// thousands of unchecked pieces cannot grow the parked set without limit.
func (t *torrent) queueSeedModeRequest(pe *peer.Peer, msg peerprotocol.RequestMessage) {
	if t.seedParked[pe] >= t.session.config.MaxRequestsIn {
		if pe.FastEnabled {
			pe.SendMessage(peerprotocol.RejectMessage{RequestMessage: msg})
		}
		return
	}
	t.seedParked[pe]++
	t.seedPending[msg.Index] = append(t.seedPending[msg.Index], seedModeRequest{pe: pe, msg: msg})
	if _, ok := t.seedHashing[msg.Index]; ok {
		return
	}
	if len(t.seedHashing) < seedModeMaxHashing {
		t.startSeedHash(msg.Index)
	}
}

// wantSeedHash queues the hash of piece i with no request waiting on it (a
// local reader wants it): it starts when a slot is free, like a peer's.
func (t *torrent) wantSeedHash(i uint32) {
	if _, ok := t.seedPending[i]; !ok {
		t.seedPending[i] = nil
	}
	if _, ok := t.seedHashing[i]; ok {
		return
	}
	if len(t.seedHashing) < seedModeMaxHashing {
		t.startSeedHash(i)
	}
}

// startSeedHash reads and hashes one piece off the run loop.
func (t *torrent) startSeedHash(i uint32) {
	t.seedHashing[i] = struct{}{}
	pi := &t.pieces[i]
	gen := t.seedGen
	resultC, closeC := t.seedHashC, t.closeC
	go func() {
		r := seedHashResult{gen: gen, index: i}
		r.ok, r.err = hashStoredPiece(pi)
		select {
		case resultC <- r:
		case <-closeC:
		}
	}()
}

// hashStoredPiece reads a piece back from storage and checks it against the
// torrent's hashes (SHA-1 for v1, the Merkle root for v2), like a downloaded
// piece before it is written.
func hashStoredPiece(pi *piece.Piece) (bool, error) {
	buf := make([]byte, pi.Length)
	if _, err := pi.Data.ReadAt(buf, 0); err != nil {
		return false, err
	}
	ok := pi.Verify(buf)
	return ok, nil
}

// handleSeedHashDone settles a lazy hash: a match serves the waiting
// requests, a mismatch (or a read error) ends seed mode and rechecks the data.
func (t *torrent) handleSeedHashDone(r seedHashResult) {
	if !t.seedMode || r.gen != t.seedGen || t.pieces == nil {
		return // stale: seed mode ended or the torrent restarted meanwhile
	}
	delete(t.seedHashing, r.index)
	waiting := t.seedPending[r.index]
	delete(t.seedPending, r.index)
	if r.err != nil || !r.ok {
		cause := r.err
		if cause == nil {
			cause = errSeedModeMismatch
		}
		t.failSeedMode(r.index, cause)
		return
	}
	for _, req := range waiting {
		if t.seedParked[req.pe]--; t.seedParked[req.pe] <= 0 {
			delete(t.seedParked, req.pe)
		}
	}
	t.seedVerified.Set(r.index)
	for _, req := range waiting {
		if !req.pe.Closed {
			t.serveRequest(req.pe, req.msg)
		}
	}
	if t.seedVerified.All() {
		t.log.Info("seed mode ended: every piece has been checked and matches the torrent")
		t.setSeedMode(false)
		return
	}
	for i := range t.seedPending {
		if len(t.seedHashing) >= seedModeMaxHashing {
			break
		}
		if _, ok := t.seedHashing[i]; !ok {
			t.startSeedHash(i)
		}
	}
}

// failSeedMode handles a piece that does not match what seed mode claimed:
// its bytes were never sent; seed mode ends, the resume record forgets the
// claimed bitfield (so a restart cannot trust it either) and the torrent
// restarts through the full verification, which marks the bad pieces missing
// so they are downloaded again.
func (t *torrent) failSeedMode(i uint32, cause error) {
	t.log.Warningf("seed mode ended: piece %d of the files added without a check failed (%s); nothing of it was sent, and every file is now checked again", i, cause)
	t.setSeedMode(false)
	t.mBitfield.Lock()
	t.bitfield = nil
	t.mBitfield.Unlock()
	if err := t.session.resumer.WriteBitfield(t.id, []byte{}); err != nil {
		t.log.Errorf("cannot clear bitfield in resume db: %s", err)
	}
	t.seedRecheck = true
	t.stop(nil)
}

// seedModeRecheckAfterStop restarts the torrent through the verification
// after failSeedMode stopped it. It reports whether it took over the stop.
func (t *torrent) seedModeRecheckAfterStop() bool {
	if !t.seedRecheck {
		return false
	}
	t.seedRecheck = false
	if t.lastError != nil {
		return false
	}
	t.mBitfield.Lock()
	t.bitfield = nil
	t.mBitfield.Unlock()
	t.start()
	return true
}

// SeedMode reports whether the torrent still trusts its data without the
// full check (pieces are being checked lazily as they are served).
func (t *Torrent) SeedMode() bool {
	return query(t.torrent, func() bool { return t.torrent.seedMode })
}
