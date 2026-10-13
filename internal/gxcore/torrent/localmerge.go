package torrent

import (
	"bytes"
	"crypto/sha1"
	"sync/atomic"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// mergedPiecesTotal and mergedBytesTotal count, since start, the pieces and
// bytes reused from other local torrents by Swarm Merging. They feed
// SessionStats.MergedPieces/MergedBytes and are therefore visible in the
// daemon's session stats and in Gextto.
var (
	mergedPiecesTotal atomic.Int64
	mergedBytesTotal  atomic.Int64
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
	// mergedIdx records the pieces already reused in this pass, so a piece is
	// read and written once even when several candidates hold the same content
	// and so a later candidate can still fill the gaps a previous one missed.
	mergedIdx := make(map[uint32]bool)
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
		// Without a bitfield the other torrent has verified nothing yet (it is
		// allocating or checking): reading and hashing its storage would cost
		// a full read for no piece.
		have := other.torrent.haveBits()
		if len(have) == 0 {
			continue
		}
		for i := range t.pieces {
			dst := &t.pieces[i]
			if dst.Done || dst.Skip || i >= len(snap.pieces) || mergedIdx[dst.Index] {
				continue
			}
			// The other torrent must have verified this piece.
			byteIndex := i / 8
			if byteIndex >= len(have) || have[byteIndex]&(1<<(7-uint(i)%8)) == 0 {
				continue
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
			mergedIdx[dst.Index] = true
			merged++
		}
	}
	// Phase 2: the same-file-different-layout case (BitTorrent v2), matched by
	// the per-file Merkle root instead of the piece index.
	merged += t.prefillV2From(candidates, mergedIdx)
	t.mergedPieces = mergedIdx
	if merged > 0 {
		var mergedBytes int64
		for idx := range mergedIdx {
			mergedBytes += int64(t.pieces[idx].Length)
		}
		mergedPiecesTotal.Add(int64(merged))
		mergedBytesTotal.Add(mergedBytes)
	}
	if merged > 0 && t.log != nil {
		t.log.Infof("swarm merge: reused %d piece(s) from local data", merged)
	}
	return merged
}

// bitSet reports whether bit i is set in a BEP 3 bitfield: index 0 is the most
// significant bit of the first byte.
func bitSet(bits []byte, i int) bool {
	if i < 0 {
		return false
	}
	byteIndex := i / 8
	if byteIndex >= len(bits) {
		return false
	}
	return bits[byteIndex]&(1<<(7-uint(i)%8)) != 0
}

// prefillV2From is the Phase 2 pass over the same candidate set (Swarm Merging,
// docs/evoluzione.md §15.2): it reuses the data of another torrent's v2 files
// matched by their per-file Merkle root, so identical files are shared even
// when the two torrents use a different piece length. Every destination piece
// is verified against its own hash before it is written, so no unverified byte
// ever reaches this torrent's storage.
func (t *torrent) prefillV2From(candidates []*Torrent, mergedIdx map[uint32]bool) int {
	info := t.info()
	if info == nil || !info.V2 || info.Private || len(t.pieces) == 0 {
		return 0
	}
	merged := 0
	var buf, tmp []byte
	for _, other := range candidates {
		if other == nil || other.torrent == t {
			continue
		}
		oinfo := other.torrent.info()
		if oinfo == nil || !oinfo.V2 || oinfo.Private {
			continue
		}
		snap := other.torrent.localData.Load()
		if snap == nil || len(snap.pieces) == 0 {
			continue
		}
		// BEP 27: the private flag may be recorded on either the snapshot or
		// the parsed info; both must be checked.
		if oinfo.Private || snap.private {
			continue
		}
		// An identical layout is Phase 1's job (whole-piece match by index).
		if snap.pieceLength == info.PieceLength && snap.length == info.Length {
			continue
		}
		have := other.torrent.haveBits()
		if len(have) == 0 {
			continue
		}
		merged += t.mergeV2Files(other.torrent, oinfo, snap, have, mergedIdx, &buf, &tmp)
	}
	return merged
}

// mergeV2Files reuses the destination pieces of every file whose Merkle root and
// length match a file of the source torrent.
func (t *torrent) mergeV2Files(src *torrent, sinfo *metainfo.Info, snap *localDataSnapshot, have []byte, mergedIdx map[uint32]bool, bufp, tmpp *[]byte) int {
	info := t.info()
	// Index the source files by their Merkle root: equal roots imply equal
	// bytes, so a duplicate entry is harmless.
	byRoot := make(map[[32]byte]int, len(sinfo.V2Files))
	for si := range sinfo.V2Files {
		f := sinfo.V2Files[si]
		if f.Length > 0 && f.HasRoot {
			byRoot[f.PiecesRoot] = si
		}
	}
	if len(byRoot) == 0 {
		return 0
	}
	merged := 0
	for fi := range info.V2Files {
		df := info.V2Files[fi]
		if df.Length == 0 || !df.HasRoot {
			continue
		}
		si, ok := byRoot[df.PiecesRoot]
		if !ok || sinfo.V2Files[si].Length != df.Length {
			continue
		}
		sstart, _ := src.v2FilePieceRange(si)
		start, count := t.v2FilePieceRange(fi)
		for j := 0; j < count; j++ {
			if start+j >= len(t.pieces) {
				break
			}
			dst := &t.pieces[start+j]
			if dst.Done || dst.Skip || len(dst.Data) == 0 || mergedIdx[dst.Index] {
				continue
			}
			n := int(dst.Length)
			if n <= 0 {
				continue
			}
			if cap(*bufp) < n {
				*bufp = make([]byte, n)
			}
			buf := (*bufp)[:n]
			if !copyV2PieceFromSource(buf, dst.Data[0].Offset, snap, sstart, sinfo, have, tmpp) {
				continue
			}
			if !verifyPieceBytes(dst, buf) {
				continue
			}
			if _, err := dst.Data.Write(buf); err != nil {
				continue
			}
			mergedIdx[dst.Index] = true
			merged++
		}
	}
	return merged
}

// copyV2PieceFromSource fills dst with the source file bytes covering the
// destination piece's file-local range [dOff, dOff+len(dst)). The source pieces
// that overlap the range must be verified by the source torrent (bitfield bit
// set), which is what makes the copied bytes trustworthy; the caller then
// re-verifies the assembled destination piece against its own hash.
func copyV2PieceFromSource(dst []byte, dOff int64, snap *localDataSnapshot, sstart int, sinfo *metainfo.Info, have []byte, tmpp *[]byte) bool {
	srcPL := int64(sinfo.PieceLength)
	if srcPL <= 0 {
		return false
	}
	end := dOff + int64(len(dst))
	for off := dOff; off < end; {
		k := int(off / srcPL)
		g := sstart + k
		if g < 0 || g >= len(snap.pieces) || !bitSet(have, g) {
			return false
		}
		sp := &snap.pieces[g]
		if len(sp.Data) == 0 || sp.Length == 0 {
			return false
		}
		if cap(*tmpp) < int(sp.Length) {
			*tmpp = make([]byte, sp.Length)
		}
		tmp := (*tmpp)[:sp.Length]
		if _, err := sp.Data.ReadAt(tmp, 0); err != nil {
			return false
		}
		segStart := int64(k) * srcPL
		lo := max(off, segStart)
		hi := min(end, segStart+int64(sp.Length))
		if hi <= lo {
			return false
		}
		copy(dst[lo-dOff:hi-dOff], tmp[lo-segStart:hi-segStart])
		off = hi
	}
	return len(dst) > 0
}

// verifyPieceBytes checks a piece's bytes against its hash: the SHA-1 of the
// piece for v1, the merkle node for v2.
func verifyPieceBytes(p *piece.Piece, buf []byte) bool {
	if p.V2 {
		return p.VerifyV2(buf)
	}
	return p.VerifyHash(buf, sha1.New())
}
