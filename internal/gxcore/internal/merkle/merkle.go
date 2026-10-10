// Package merkle implements the SHA-256 merkle trees used by BitTorrent v2
// torrents (BEP 52).
//
// A file is split into 16 KiB blocks; the SHA-256 of each block is a leaf of a
// binary tree and every inner node is the SHA-256 of its two children
// concatenated. The root hash of the tree is the "pieces root" of the file, and
// the hashes of one layer of the tree form the "piece layers" entries.
package merkle

import "crypto/sha256"

// BlockSize is the size of a leaf block of a BitTorrent v2 merkle tree.
const BlockSize = 16 * 1024

// HashSize is the size in bytes of the SHA-256 hashes used by the tree.
const HashSize = 32

// LeafHashes returns the SHA-256 hash of each BlockSize block of data. The last
// block may be shorter than BlockSize and is hashed over its actual bytes; the
// data is never padded. Empty data yields an empty slice.
func LeafHashes(data []byte) [][HashSize]byte {
	n := (len(data) + BlockSize - 1) / BlockSize
	if n == 0 {
		return nil
	}
	leaves := make([][HashSize]byte, n)
	for i := 0; i < n; i++ {
		start := i * BlockSize
		end := start + BlockSize
		if end > len(data) {
			end = len(data)
		}
		leaves[i] = sha256.Sum256(data[start:end])
	}
	return leaves
}

// Root returns the root hash of the merkle tree built from leaves. Leaf hashes
// missing to reach a power of two are 32 zero bytes, and every inner node is
// the SHA-256 of its two children concatenated. An empty slice yields the all
// zero hash; a single leaf yields that leaf unchanged.
func Root(leaves [][HashSize]byte) [HashSize]byte {
	switch len(leaves) {
	case 0:
		return [HashSize]byte{}
	case 1:
		return leaves[0]
	}
	layer := padToPowerOfTwo(leaves)
	for len(layer) > 1 {
		layer = collapse(layer)
	}
	return layer[0]
}

// Layer returns the hashes at the given level above the leaves. Level 0 is the
// leaf layer itself, returned as a copy of leaves. For higher levels the leaf
// count is padded to a power of two with zero hashes, so a hash at level L
// covers BlockSize<<L bytes; the length of the returned slice is then
// nextPowerOfTwo(len(leaves))>>level. It returns nil for an empty leaf slice, a
// negative level, or a level above the tree height.
func Layer(leaves [][HashSize]byte, level int) [][HashSize]byte {
	if level < 0 || len(leaves) == 0 {
		return nil
	}
	if level == 0 {
		out := make([][HashSize]byte, len(leaves))
		copy(out, leaves)
		return out
	}
	layer := padToPowerOfTwo(leaves)
	for i := 0; i < level; i++ {
		if len(layer) < 2 {
			return nil
		}
		layer = collapse(layer)
	}
	return layer
}

// PieceLayer returns, in order, the hashes that cover each piece of the file
// described by leaves for the given piece length. pieceLength must be a power
// of two and at least BlockSize, otherwise PieceLayer returns nil.
//
// The level of the tree is chosen so that one hash covers pieceLength bytes;
// only hashes covering real data are returned, so the result has
// ceil(len(leaves)/(pieceLength/BlockSize)) entries. Hashes that exist only to
// balance the tree beyond the end of the file are omitted. When the file is no
// larger than one piece, the single hash is computed with zero padding up to
// the piece boundary.
func PieceLayer(leaves [][HashSize]byte, pieceLength int) [][HashSize]byte {
	if pieceLength < BlockSize || pieceLength%BlockSize != 0 {
		return nil
	}
	ratio := pieceLength / BlockSize
	if ratio&(ratio-1) != 0 {
		return nil
	}
	if len(leaves) == 0 {
		return nil
	}
	level := 0
	for r := ratio; r > 1; r >>= 1 {
		level++
	}
	if level == 0 {
		out := make([][HashSize]byte, len(leaves))
		copy(out, leaves)
		return out
	}
	groupSize := 1 << level
	count := (len(leaves) + groupSize - 1) / groupSize
	out := make([][HashSize]byte, count)
	for i := 0; i < count; i++ {
		start := i * groupSize
		end := start + groupSize
		if end > len(leaves) {
			end = len(leaves)
		}
		group := make([][HashSize]byte, groupSize)
		copy(group, leaves[start:end])
		out[i] = Root(group)
	}
	return out
}

// VerifyProof reports whether leaf at index belongs to the merkle tree with the
// given root. proof holds the sibling hashes ordered from the leaf level up to,
// but excluding, the root. It returns false for a negative index.
func VerifyProof(leaf [HashSize]byte, index int, proof [][HashSize]byte, root [HashSize]byte) bool {
	if index < 0 {
		return false
	}
	h := leaf
	idx := index
	for _, sibling := range proof {
		if idx&1 == 0 {
			h = hashPair(h, sibling)
		} else {
			h = hashPair(sibling, h)
		}
		idx >>= 1
	}
	return h == root
}

// hashPair returns SHA-256(left || right).
func hashPair(left, right [HashSize]byte) [HashSize]byte {
	var buf [2 * HashSize]byte
	copy(buf[:HashSize], left[:])
	copy(buf[HashSize:], right[:])
	return sha256.Sum256(buf[:])
}

// padToPowerOfTwo returns a copy of leaves with the length rounded up to the
// next power of two, filling the missing entries with the zero hash.
func padToPowerOfTwo(leaves [][HashSize]byte) [][HashSize]byte {
	n := nextPowerOfTwo(len(leaves))
	out := make([][HashSize]byte, n)
	copy(out, leaves)
	return out
}

// collapse pairs adjacent hashes and hashes each pair into the next level up.
func collapse(layer [][HashSize]byte) [][HashSize]byte {
	next := make([][HashSize]byte, len(layer)/2)
	for i := range next {
		next[i] = hashPair(layer[2*i], layer[2*i+1])
	}
	return next
}

// nextPowerOfTwo returns the smallest power of two greater than or equal to n.
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}
