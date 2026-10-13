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
	// partialSourcePieces counts the destination pieces reused at block
	// granularity (Phase 3): only part of their source was verified.
	partialSourcePieces atomic.Int64
)

// Swarm Merging, Fase 1 (docs/evoluzione.md §9.2): reuse the pieces of another
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

// mergeCandidates returns the torrents of the list that could lend pieces to
// this one: the cheap per-candidate checks of both phases (published snapshot,
// not private, something verified, same layout or a shared v2 file root),
// without reading any data. It runs in the run loop, so when it returns nothing
// a fresh torrent skips the verifier entirely; the reads, hashes and writes
// happen later off the run loop (startMergeVerifier).
func (t *torrent) mergeCandidates(list []*Torrent) []*Torrent {
	info := t.info()
	if info == nil || info.Private || len(t.pieces) == 0 {
		return nil
	}
	var roots map[[32]byte]bool
	var out []*Torrent
	for _, other := range list {
		if other == nil || other.torrent == t {
			continue
		}
		snap := other.torrent.localData.Load()
		if snap == nil || len(snap.pieces) == 0 || snap.private {
			continue
		}
		if snap.pieceLength == info.PieceLength && snap.length == info.Length {
			if len(other.torrent.haveBits()) > 0 {
				out = append(out, other)
			}
			continue
		}
		oinfo := other.torrent.info()
		if !info.V2 || oinfo == nil || !oinfo.V2 || oinfo.Private {
			continue
		}
		if roots == nil {
			roots = make(map[[32]byte]bool, len(info.V2Files))
			for _, f := range info.V2Files {
				if f.Length > 0 && f.HasRoot {
					roots[f.PiecesRoot] = true
				}
			}
		}
		for _, f := range oinfo.V2Files {
			if f.Length > 0 && f.HasRoot && roots[f.PiecesRoot] {
				if len(other.torrent.haveBits()) > 0 {
					out = append(out, other)
				}
				break
			}
		}
	}
	return out
}

// mergeSkips snapshots the per-piece Skip flags, so a merge running off the run
// loop never reads them while a file-priority command rewrites them.
func (t *torrent) mergeSkips() []bool {
	skip := make([]bool, len(t.pieces))
	for i := range t.pieces {
		skip[i] = t.pieces[i].Skip
	}
	return skip
}

// prefillFrom is the synchronous merge over an explicit candidate set: it
// records the reused pieces in t.mergedPieces for the next verifier. Tests use
// it; the torrent itself merges off the run loop (startMergeVerifier).
func (t *torrent) prefillFrom(candidates []*Torrent) int {
	res := t.mergeFrom(candidates, nil, nil)
	t.mergedPieces = res.pieces
	t.partialBlocks = res.partial
	return res.merged
}

// mergeResult is what a merge reused: whole pieces (verified against this
// torrent's hashes, so the verifier skips them), and per piece the 16 KiB
// blocks written from a partially verified v2 source (Phase 3), which the
// piece downloader takes from disk instead of requesting them.
type mergeResult struct {
	pieces  map[uint32]bool
	partial map[uint32][]uint32
	merged  int
}

// mergeFrom copies verified pieces from the candidates into this torrent's
// storage and returns the reused piece set and its size. skip, when not nil,
// replaces the pieces' own Skip flags (a snapshot taken in the run loop); stop,
// when closed, ends the merge between two pieces.
func (t *torrent) mergeFrom(candidates []*Torrent, skip []bool, stop <-chan struct{}) mergeResult {
	if t.info() == nil || len(t.pieces) == 0 {
		return mergeResult{}
	}
	partial := make(map[uint32][]uint32)
	merged := 0
	// mergedIdx records the pieces already reused in this pass, so a piece is
	// read and written once even when several candidates hold the same content
	// and so a later candidate can still fill the gaps a previous one missed.
	mergedIdx := make(map[uint32]bool)
	var buf []byte
candidates:
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
			if dst.Done || skipped(dst, skip) || i >= len(snap.pieces) || mergedIdx[dst.Index] {
				continue
			}
			if stopped(stop) {
				break candidates
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
	if !stopped(stop) {
		merged += t.prefillV2From(candidates, mergedIdx, partial, skip, stop)
	}
	// A piece a later candidate covered whole needs no block-level reuse.
	var partialBytes int64
	for idx, begins := range partial {
		if mergedIdx[idx] {
			delete(partial, idx)
			continue
		}
		partialBytes += int64(len(begins)) * piece.BlockSize
	}
	if merged > 0 || partialBytes > 0 {
		var mergedBytes int64
		for idx := range mergedIdx {
			mergedBytes += int64(t.pieces[idx].Length)
		}
		mergedPiecesTotal.Add(int64(merged))
		mergedBytesTotal.Add(mergedBytes + partialBytes)
	}
	if merged > 0 && t.log != nil {
		t.log.Infof("swarm merge: reused %d piece(s) from local data", merged)
	}
	if len(partial) > 0 && t.log != nil {
		t.log.Infof("swarm merge: reused ~%d MiB as 16 KiB blocks in %d partial piece(s)", partialBytes>>20, len(partial))
	}
	return mergeResult{pieces: mergedIdx, partial: partial, merged: merged}
}

// skipped reports whether the merge must leave the piece alone: the snapshot
// when there is one, the piece's own flag otherwise.
func skipped(p *piece.Piece, skip []bool) bool {
	if skip != nil && int(p.Index) < len(skip) {
		return skip[p.Index]
	}
	return p.Skip
}

// stopped reports, without blocking, whether stop is closed.
func stopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
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
// docs/evoluzione.md §9.2): it reuses the data of another torrent's v2 files
// matched by their per-file Merkle root, so identical files are shared even
// when the two torrents use a different piece length. Every destination piece
// is verified against its own hash before it is written, so no unverified byte
// ever reaches this torrent's storage.
func (t *torrent) prefillV2From(candidates []*Torrent, mergedIdx map[uint32]bool, partialOut map[uint32][]uint32, skip []bool, stop <-chan struct{}) int {
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
		merged += t.mergeV2Files(other.torrent, oinfo, snap, have, mergedIdx, partialOut, skip, stop, &buf, &tmp)
	}
	return merged
}

// mergeV2Files reuses the destination pieces of every file whose Merkle root and
// length match a file of the source torrent.
func (t *torrent) mergeV2Files(src *torrent, sinfo *metainfo.Info, snap *localDataSnapshot, have []byte, mergedIdx map[uint32]bool, partialOut map[uint32][]uint32, skip []bool, stop <-chan struct{}, bufp, tmpp *[]byte) int {
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
	var spans []span
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
			if dst.Done || skipped(dst, skip) || len(dst.Data) == 0 || mergedIdx[dst.Index] {
				continue
			}
			if stopped(stop) {
				return merged
			}
			n := int(dst.Length)
			if n <= 0 {
				continue
			}
			if cap(*bufp) < n {
				*bufp = make([]byte, n)
			}
			buf := (*bufp)[:n]
			ok, partial := copyV2PieceFromSource(buf, dst.Data[0].Offset, snap, sstart, sinfo, have, tmpp, &spans)
			if partial {
				// Phase 3: reuse the blocks the source verified; only the
				// others are requested from peers. A piece already partly
				// filled by an earlier candidate keeps its first blocks.
				if _, done := partialOut[dst.Index]; !done && partialOut != nil {
					if begins := storePartialBlocks(dst, buf, spans); len(begins) > 0 {
						partialOut[dst.Index] = begins
						partialSourcePieces.Add(1)
					}
				}
				continue
			}
			if !ok {
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
// re-verifies the assembled destination piece against its own hash. It returns
// ok=true only when every overlapping source piece was available, and
// partial=true when at least one but not all were (the case Phase 3 would
// recover at block granularity).
func copyV2PieceFromSource(dst []byte, dOff int64, snap *localDataSnapshot, sstart int, sinfo *metainfo.Info, have []byte, tmpp *[]byte, spans *[]span) (ok, partial bool) {
	*spans = (*spans)[:0]
	srcPL := int64(sinfo.PieceLength)
	if srcPL <= 0 {
		return false, false
	}
	end := dOff + int64(len(dst))
	verified, needed := 0, 0
	// abort drops the spans: a read error or a layout mismatch means none of
	// the copied bytes can be trusted for a block-level reuse.
	abort := func() (bool, bool) {
		*spans = (*spans)[:0]
		return false, false
	}
	for off := dOff; off < end; {
		k := int(off / srcPL)
		g := sstart + k
		segStart := int64(k) * srcPL
		hi := min(end, segStart+srcPL)
		if g < 0 || g >= len(snap.pieces) {
			return abort()
		}
		needed++
		if !bitSet(have, g) {
			off = hi
			continue
		}
		sp := &snap.pieces[g]
		if len(sp.Data) == 0 || sp.Length == 0 {
			return abort()
		}
		if cap(*tmpp) < int(sp.Length) {
			*tmpp = make([]byte, sp.Length)
		}
		tmp := (*tmpp)[:sp.Length]
		if _, err := sp.Data.ReadAt(tmp, 0); err != nil {
			return abort()
		}
		lo := max(off, segStart)
		copy(dst[lo-dOff:hi-dOff], tmp[lo-segStart:hi-segStart])
		*spans = append(*spans, span{lo - dOff, hi - dOff})
		verified++
		off = hi
	}
	if needed == 0 {
		return false, false
	}
	return verified == needed, verified > 0 && verified < needed
}

// span is a byte range [lo, hi) of a destination piece copied from a verified
// source piece.
type span struct{ lo, hi int64 }

// storePartialBlocks writes the verified spans of a destination piece to its
// storage and returns the begin offsets of the 16 KiB blocks they fully cover.
// The bytes are trustworthy at block level: the source and destination files
// share the BEP 52 Merkle root, whose leaves are the file's 16 KiB blocks
// whatever the piece length, and every span comes from a source piece the
// source verified. The piece itself is still verified whole once its other
// blocks arrive from the network.
func storePartialBlocks(dst *piece.Piece, buf []byte, spans []span) []uint32 {
	if len(spans) == 0 {
		return nil
	}
	for _, s := range spans {
		if _, err := dst.Data.WriteAt(buf[s.lo:s.hi], s.lo); err != nil {
			return nil
		}
	}
	var begins []uint32
	for _, b := range dst.CalculateBlocks() {
		lo, hi := int64(b.Begin), int64(b.Begin)+int64(b.Length)
		for _, s := range spans {
			if lo >= s.lo && hi <= s.hi {
				begins = append(begins, b.Begin)
				break
			}
		}
	}
	return begins
}

// verifyPieceBytes checks a piece's bytes against its hash: the SHA-1 of the
// piece for v1, the merkle node for v2.
func verifyPieceBytes(p *piece.Piece, buf []byte) bool {
	if p.V2 {
		return p.VerifyV2(buf)
	}
	return p.VerifyHash(buf, sha1.New())
}
