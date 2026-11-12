package torrent

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// External sources of a merge: a file outside the session that holds the
// same bytes as one file of the torrent — the same episode downloaded by
// another program, e.g. aMule's .part file. Its bytes are read at the
// offsets of that torrent file, verified against the torrent's own piece
// hashes and adopted like the pieces of a local sibling, so a wrong or
// half-written file can only add nothing. Pieces that span another file of
// the torrent are left to the swarm.

// ExternalSource names a file holding the content of one torrent file.
type ExternalSource struct {
	FileIndex int
	Path      string
}

// externalPiecesTotal counts the pieces adopted from external files.
var externalPiecesTotal atomic.Int64

// ErrNotDownloading is returned when a merge is asked of a torrent that is
// not downloading (complete, stopped, checking or without metadata).
var ErrNotDownloading = errors.New("the torrent is not downloading")

// MergeExternal asks the torrent to adopt, from each source, the pieces that
// pass its hash check. It returns at once: the merge runs like a later local
// merge (in-flight fetches are paused for it) and its outcome shows in the
// torrent's progress. A source whose size is not the torrent file's is
// refused, since its bytes cannot sit at the same offsets.
func (t *Torrent) MergeExternal(sources []ExternalSource) error {
	return query(t.torrent, func() error {
		tt := t.torrent
		if tt.info() == nil || tt.pieces == nil {
			return ErrNotDownloading
		}
		if tt.status() != Downloading || tt.wantedComplete() {
			return ErrNotDownloading
		}
		files := tt.info().Files
		for _, src := range sources {
			if src.FileIndex < 0 || src.FileIndex >= len(files) || src.FileIndex >= len(tt.files) || files[src.FileIndex].Padding {
				return fmt.Errorf("the torrent has no file %d", src.FileIndex)
			}
			info, err := os.Stat(src.Path)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%s is not a regular file", src.Path)
			}
			if info.Size() != files[src.FileIndex].Length {
				return fmt.Errorf("%s is %d bytes, the torrent file is %d", src.Path, info.Size(), files[src.FileIndex].Length)
			}
		}
		tt.externalSources = append(tt.externalSources, sources...)
		tt.handleLaterMergeRequest()
		return nil
	})
}

// prefillExternal adopts the pieces the external sources hold. It runs in
// the verifier's Prepare, like mergeFrom, and reads t.pieces and t.files
// only (fixed once allocated).
func (t *torrent) prefillExternal(sources []ExternalSource, mergedIdx map[uint32]bool, skip []bool, stop <-chan struct{}) int {
	merged := 0
	var buf []byte
	for _, src := range sources {
		if src.FileIndex < 0 || src.FileIndex >= len(t.files) {
			continue
		}
		target := t.files[src.FileIndex].Name
		f, err := os.Open(src.Path)
		if err != nil {
			continue
		}
		count := 0
		for i := range t.pieces {
			dst := &t.pieces[i]
			if dst.Done || skipped(dst, skip) || mergedIdx[dst.Index] {
				continue
			}
			if stopped(stop) {
				break
			}
			if !pieceWithin(dst, target) {
				continue
			}
			if cap(buf) < int(dst.Length) {
				buf = make([]byte, dst.Length)
			}
			buf = buf[:dst.Length]
			if !readExternalPiece(dst, target, f, buf) {
				continue
			}
			if !verifyPieceBytes(dst, buf) {
				continue
			}
			if _, err := dst.Data.Write(buf); err != nil {
				continue
			}
			mergedIdx[dst.Index] = true
			count++
		}
		f.Close()
		if count > 0 && t.log != nil {
			t.log.Infof("external merge: reused %d piece(s) from %s", count, src.Path)
		}
		merged += count
	}
	externalPiecesTotal.Add(int64(merged))
	return merged
}

// pieceWithin reports whether every byte of p belongs to target or is
// padding (and at least one belongs to target).
func pieceWithin(p *piece.Piece, target string) bool {
	found := false
	for _, sec := range p.Data {
		if sec.Padding {
			continue
		}
		if sec.Name != target {
			return false
		}
		found = true
	}
	return found
}

// readExternalPiece fills buf with the bytes of p read from f at the target
// file's offsets, zeros for padding. A piece whose bytes are all zero (a
// region the other program has not written yet) is not worth hashing.
func readExternalPiece(p *piece.Piece, target string, f *os.File, buf []byte) bool {
	pos := 0
	nonZero := false
	for _, sec := range p.Data {
		part := buf[pos : pos+int(sec.Length)]
		pos += int(sec.Length)
		if sec.Padding || sec.Name != target {
			clear(part)
			continue
		}
		if n, _ := f.ReadAt(part, sec.Offset); n != len(part) {
			return false
		}
		if !nonZero {
			for _, b := range part {
				if b != 0 {
					nonZero = true
					break
				}
			}
		}
	}
	return nonZero
}
