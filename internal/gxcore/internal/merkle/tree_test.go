package merkle

import (
	"bytes"
	"testing"
)

func TestLog2AndPieceLevel(t *testing.T) {
	if Log2(1) != 0 || Log2(2) != 1 || Log2(1024) != 10 {
		t.Fatalf("unexpected Log2: %d %d %d", Log2(1), Log2(2), Log2(1024))
	}
	if NextPowerOfTwo(3) != 4 || NextPowerOfTwo(4) != 4 || NextPowerOfTwo(0) != 1 {
		t.Fatal("unexpected NextPowerOfTwo")
	}
	if PieceLevel(BlockSize) != 0 || PieceLevel(4*BlockSize) != 2 {
		t.Fatal("unexpected PieceLevel")
	}
	if PieceLevel(BlockSize+1) != -1 || PieceLevel(3*BlockSize) != -1 {
		t.Fatal("PieceLevel should reject non power-of-two piece lengths")
	}
}

// TestLayerTreeRoundTrip builds a file tree from real data, then checks that a
// hash request at every layer of the tree is served and accepted by
// VerifyHashes against the file's pieces root.
func TestLayerTreeRoundTrip(t *testing.T) {
	for _, blocks := range []int{1, 2, 3, 4, 5, 7, 8, 9, 16, 31} {
		data := bytes.Repeat([]byte{byte(blocks)}, blocks*BlockSize)
		leaves := LeafHashes(data)
		for _, pieceBlocks := range []int{1, 2, 4, 8} {
			// BEP 52 stores no piece layer for a file that fits in one piece;
			// its single piece hash is the pieces root itself.
			if blocks <= pieceBlocks {
				continue
			}
			pieceLength := pieceBlocks * BlockSize
			pieceLayer := PieceLayer(leaves, pieceLength)
			root := Root(leaves)
			level := PieceLevel(pieceLength)
			tree := NewLayerTree(level, pieceLayer)
			if tree == nil {
				t.Fatalf("blocks=%d pieceBlocks=%d: nil tree", blocks, pieceBlocks)
			}
			if tree.Root() != root {
				t.Fatalf("blocks=%d pieceBlocks=%d: tree root %x != %x", blocks, pieceBlocks, tree.Root(), root)
			}
			padded := nextPowerOfTwo(blocks)
			height := Log2(padded)
			if tree.Height() != height {
				t.Fatalf("blocks=%d pieceBlocks=%d: height %d != %d", blocks, pieceBlocks, tree.Height(), height)
			}
			// Requests below the piece layer cannot be served from the piece
			// layer alone.
			if level > 0 {
				if got := tree.GetHashes(level-1, 0, 2, 0); got != nil {
					t.Fatalf("blocks=%d: served a block-layer request", blocks)
				}
			}
			for base := level; base < height; base++ {
				nodes := padded >> base
				for length := 2; length <= nodes && length <= 512; length <<= 1 {
					proofLayers := height - base - 1
					for index := 0; index+length <= nodes; index += length {
						got := tree.GetHashes(base, index, length, proofLayers)
						if got == nil {
							t.Fatalf("blocks=%d pieceBlocks=%d base=%d index=%d length=%d: not served", blocks, pieceBlocks, base, index, length)
						}
						hashes := got[:length]
						uncles := got[length:]
						if !VerifyHashes(root, padded, base, index, length, hashes, uncles) {
							t.Fatalf("blocks=%d pieceBlocks=%d base=%d index=%d length=%d: verification failed", blocks, pieceBlocks, base, index, length)
						}
						// A tampered hash must not verify.
						bad := append([][HashSize]byte(nil), hashes...)
						bad[0][0] ^= 0xff
						if VerifyHashes(root, padded, base, index, length, bad, uncles) {
							t.Fatalf("blocks=%d pieceBlocks=%d: tampered hash verified", blocks, pieceBlocks)
						}
					}
				}
			}
		}
	}
}

// TestGetHashesRejections checks the server-side bounds of GetHashes.
func TestGetHashesRejections(t *testing.T) {
	data := bytes.Repeat([]byte{1}, 8*BlockSize)
	leaves := LeafHashes(data)
	pieceLayer := PieceLayer(leaves, 2*BlockSize) // 4 pieces
	tree := NewLayerTree(1, pieceLayer)
	if tree.GetHashes(1, 0, 3, 0) != nil { // length not a power of two
		t.Fatal("accepted a non power-of-two length")
	}
	if tree.GetHashes(1, 1, 2, 0) != nil { // index not a multiple of length
		t.Fatal("accepted an unaligned index")
	}
	if tree.GetHashes(1, 4, 2, 0) != nil { // out of range
		t.Fatal("accepted an out-of-range request")
	}
	if tree.GetHashes(1, 0, 2, tree.Height()-1) != nil { // proofs past the root
		t.Fatal("accepted proofs past the root")
	}
}
