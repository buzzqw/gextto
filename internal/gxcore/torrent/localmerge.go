package torrent

import (
	"bytes"
	"crypto/sha1"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// Swarm Merging, Fase 1 (docs/evoluzione.md §15.2): reuse the pieces of another
// torrent already in the session when the piece layout matches exactly — the
// same piece length and the same piece hash and length at the same index. The
// bytes are read from the other torrent's storage, verified against this
// torrent's hash, and written into this torrent's storage, so the normal
// verifier marks them done and they are never downloaded again. The accounting
// stays per torrent.

// localDataSnapshot is an immutable view of a torrent's pieces, published once
// the pieces exist. Another torrent reads it directly (no run loop, no
// cross-torrent query, so no deadlock); only the bitfield is read under its
// lock.
type localDataSnapshot struct {
	pieces      []piece.Piece
	private     bool
	length      int64
	pieceLength uint32
}

// publishLocalData records the snapshot once the pieces are built. It is called
// from the run loop in handleAllocationDone.
func (t *torrent) publishLocalData() {
	if t.info() == nil || len(t.pieces) == 0 {
		return
	}
	t.localData.Store(&localDataSnapshot{
		pieces:      t.pieces,
		private:     t.info().Private,
		length:      t.info().Length,
		pieceLength: t.info().PieceLength,
	})
}

// haveBits returns a copy of the verified piece bitfield, or nil when the
// torrent has no bitfield yet. 0 is the most significant bit (BEP 3).
func (t *torrent) haveBits() []byte {
	t.mBitfield.RLock()
	defer t.mBitfield.RUnlock()
	if t.bitfield == nil {
		return nil
	}
	return append([]byte(nil), t.bitfield.Bytes()...)
}

// prefillLocalData copies verified pieces from the other torrents in the session
// and returns how many pieces were written. It runs in the run loop right after
// the pieces are created, before the verifier: the verifier then reads this
// torrent's storage (now containing the reused bytes) and marks them done.
func (t *torrent) prefillLocalData() int {
	if t.session == nil {
		return 0
	}
	return t.prefillFrom(t.session.ListTorrents())
}

// prefillFrom is prefillLocalData over an explicit candidate set, so it can be
// tested without a session.
func (t *torrent) prefillFrom(candidates []*Torrent) int {
	if t.info() == nil || len(t.pieces) == 0 {
		return 0
	}
	merged := 0
	var buf []byte
	for _, other := range candidates {
		if other == nil || other.torrent == t {
			continue
		}
		snap := other.torrent.localData.Load()
		if snap == nil || len(snap.pieces) == 0 {
			continue
		}
		// BEP 27: never mix a swarm that is private on either side.
		if snap.private || t.info().Private {
			continue
		}
		// Only an identical layout: same piece length and same content length.
		if snap.pieceLength != t.info().PieceLength || snap.length != t.info().Length {
			continue
		}
		have := other.torrent.haveBits()
		for i := range t.pieces {
			dst := &t.pieces[i]
			if dst.Done || dst.Skip || i >= len(snap.pieces) {
				continue
			}
			// The other torrent must have verified this piece.
			if len(have) > 0 {
				byteIndex := i / 8
				if byteIndex >= len(have) || have[byteIndex]&(1<<(7-uint(i)%8)) == 0 {
					continue
				}
			}
			src := &snap.pieces[i]
			if src.Length != dst.Length || !bytes.Equal(src.Hash, dst.Hash) {
				continue
			}
			if cap(buf) < int(dst.Length) {
				buf = make([]byte, dst.Length)
			}
			buf = buf[:dst.Length]
			if _, err := src.Data.ReadAt(buf, 0); err != nil {
				continue
			}
			if !verifyPieceBytes(dst, buf) {
				continue
			}
			if _, err := dst.Data.Write(buf); err != nil {
				continue
			}
			merged++
		}
	}
	if merged > 0 && t.log != nil {
		t.log.Infof("swarm merge: reused %d piece(s) from local data", merged)
	}
	return merged
}

// verifyPieceBytes checks a piece's bytes against its hash: the SHA-1 of the
// piece for v1, the merkle node for v2.
func verifyPieceBytes(p *piece.Piece, buf []byte) bool {
	if p.V2 {
		return p.VerifyV2(buf)
	}
	return p.VerifyHash(buf, sha1.New())
}
