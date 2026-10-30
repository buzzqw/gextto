package piece

import (
	"bytes"
	"crypto/sha1"
	"hash"

	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/filesection"
	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

// BlockSize is the wire block size: the smallest span requested from peers.
const BlockSize = 16 * 1024

// File/piece priority levels, on the libtorrent/qBittorrent 0..7 scale the
// daemon and Gextto speak:
//
//   - 0 = skip: excluded from download (Skip set);
//   - 1..4 = normal: downloaded with the rest (4 is the default);
//   - 5..7 = high: downloaded ahead of normal pieces.
const (
	PriorityNormal = 4
	PriorityHigh   = 5
)

// Piece is one torrent piece: its hashes, its file sections, and its
// download state.
type Piece struct {
	Index   uint32            // torrent-wide index
	Length  uint32            // Info.PieceLength, except the tail piece
	Data    filesection.Piece // where downloaded bytes land
	Hash    []byte
	Writing bool
	Done    bool
	// Skip marks pieces holding only unwanted-file bytes.
	Skip bool
	// Priority is the highest priority among the piece's files: it orders
	// picks, with high before normal.
	Priority uint8
	// V2 marks a BitTorrent v2 piece: single-file, hashed as a SHA-256
	// merkle node over V2Leaves 16 KiB block leaves (BEP 52).
	V2       bool
	V2Leaves int
}

// Block is one request-sized span of a piece (never past BlockSize, shorter
// at tails and padding edges).
type Block struct {
	Begin  uint32 // piece offset
	Length uint32
}

// NewPieces maps info files onto pieces: v1 pieces span the concatenated
// files, v2 pieces stay within one file each.
func NewPieces(info *metainfo.Info, files []allocator.File) []Piece {
	if info.V2 {
		return newPiecesV2(info, files)
	}
	var (
		fileIndex  int   // current file in the torrent
		fileLength int64 // its length
		fileEnd    int64 // its absolute end across the concatenation
		fileOffset int64 // read cursor within it
	)
	nextFile := func() {
		fileIndex++
		fileLength = info.Files[fileIndex].Length
		fileEnd += fileLength
		fileOffset = 0
	}
	fileIndex = -1
	nextFile()
	fileLeft := func() int64 { return fileLength - fileOffset }

	var total int64
	pieces := make([]Piece, info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		p := Piece{
			Index: i,
			Hash:  info.PieceHash(i),
		}
		var sections filesection.Piece
		var pieceOffset uint32
		for left := info.PieceLength - pieceOffset; left > 0; {
			n := uint32(min(int64(left), fileLeft()))
			sections = append(sections, filesection.FileSection{
				File:    files[fileIndex].Storage,
				Offset:  fileOffset,
				Length:  int64(n),
				Name:    files[fileIndex].Name,
				Padding: files[fileIndex].Padding,
			})
			left -= n
			p.Length += n
			pieceOffset += n
			fileOffset += int64(n)
			total += int64(n)
			if total == info.Length {
				break
			}
			if fileLeft() == 0 {
				nextFile()
			}
		}
		p.Data = sections
		pieces[i] = p
	}
	return pieces
}

// numBlocks counts the blocks sans padding (capacity hint only).
func (p *Piece) numBlocks() int {
	div, mod := divmod(p.Length, BlockSize)
	if mod != 0 {
		div++
	}
	return int(div)
}

// CalculateBlocks splits the piece into request blocks, skipping padding
// spans (their bytes never travel).
func (p *Piece) CalculateBlocks() []Block {
	return p.calculateBlocks(BlockSize)
}

func (p *Piece) calculateBlocks(blockSize uint32) []Block {
	blocks := make([]Block, 0, p.numBlocks())
	secIndex := 0
	sec := p.Data[secIndex]
	blk := Block{}
	var pieceOffset, secOffset uint32
	blkLeft := func() uint32 { return blockSize - blk.Length }
	secLeft := func() uint32 { return uint32(sec.Length) - secOffset }
	flushBlock := func() {
		if blk.Length == 0 {
			return
		}
		blocks = append(blocks, blk)
		blk.Begin = pieceOffset
		blk.Length = 0
	}
	nextSection := func() bool {
		secIndex++
		if secIndex == len(p.Data) {
			return false
		}
		secOffset = 0
		sec = p.Data[secIndex]
		return true
	}
	for hasSection := true; hasSection; {
		if sec.Padding {
			pieceOffset += uint32(sec.Length)
			flushBlock()
			hasSection = nextSection()
			continue
		}
		n := min(secLeft(), blkLeft())
		blk.Length += n
		pieceOffset += n
		secOffset += n
		if blkLeft() == 0 {
			flushBlock()
		}
		if secLeft() == 0 {
			hasSection = nextSection()
		}
	}
	flushBlock()
	return blocks
}

// Verify checks buf against the piece's hash: the SHA-1 piece hash for v1,
// the merkle node for v2 (BEP 52). It is the entry point for "is this piece
// good?", so a v2 piece never pays for a digest its layout does not use.
func (p *Piece) Verify(buf []byte) bool {
	if p.V2 {
		return p.VerifyV2(buf)
	}
	return p.VerifyHash(buf, sha1.New()) // nolint: gosec
}

// VerifyHash checks buf against the v1 SHA-1 piece hash. Length mismatch
// fails without hashing.
func (p *Piece) VerifyHash(buf []byte, h hash.Hash) bool {
	if uint32(len(buf)) != p.Length {
		return false
	}
	_, _ = h.Write(buf)
	return bytes.Equal(h.Sum(nil), p.Hash)
}

// newPiecesV2 lays v2 pieces file by file: tails run short, none straddles
// files (BEP 52). A file fitting one piece hashes as its pieces root over
// nextPow2 leaves rather than a piece boundary.
func newPiecesV2(info *metainfo.Info, files []allocator.File) []Piece {
	pieceLeaves := int(info.PieceLength / BlockSize)
	pieces := make([]Piece, 0, info.NumPieces)
	for fi, f := range info.Files {
		numBlocks := int((f.Length + BlockSize - 1) / BlockSize)
		span := pieceLeaves
		if numBlocks <= pieceLeaves {
			span = max(nextPow2(numBlocks), 1)
		}
		for off := int64(0); off < f.Length; {
			n := int64(info.PieceLength)
			if left := f.Length - off; left < n {
				n = left
			}
			global := uint32(len(pieces))
			pieces = append(pieces, Piece{
				Index:    global,
				Length:   uint32(n),
				Hash:     info.PieceHash(global),
				V2:       true,
				V2Leaves: span,
				Data: filesection.Piece{{
					File:   files[fi].Storage,
					Offset: off,
					Length: n,
					Name:   files[fi].Name,
				}},
			})
			off += n
		}
	}
	return pieces
}

func nextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// VerifyV2 checks buf against the piece's merkle node: the SHA-256 tree over
// its 16 KiB block leaves, zero-padded to V2Leaves (BEP 52).
func (p *Piece) VerifyV2(buf []byte) bool {
	if uint32(len(buf)) != p.Length {
		return false
	}
	leaves := merkle.LeafHashes(buf)
	n := max(p.V2Leaves, 1)
	full := make([][merkle.HashSize]byte, n)
	copy(full, leaves)
	root := merkle.Root(full)
	return bytes.Equal(root[:], p.Hash)
}

func divmod(a, b uint32) (uint32, uint32) { return a / b, a % b }
