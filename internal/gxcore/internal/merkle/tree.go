package merkle

// BEP 52 hash request support. A "hash request" asks for a range of hashes at
// one layer of a file's merkle tree plus the uncle hashes that anchor that
// range to the file's pieces root. A "hashes" reply carries the requested
// hashes followed by those uncles, ordered from the sibling of the requested
// subtree up to (but excluding) the root.

// Log2 returns log2 of n, which must be a positive power of two.
func Log2(n int) int {
	l := 0
	for n > 1 {
		n >>= 1
		l++
	}
	return l
}

// NextPowerOfTwo returns the smallest power of two greater than or equal to n.
func NextPowerOfTwo(n int) int { return nextPowerOfTwo(n) }

// PieceLevel returns the absolute tree level whose hashes cover one piece: 0
// when a piece is a single 16 KiB block. It returns -1 when pieceLength is not
// a power-of-two multiple of BlockSize.
func PieceLevel(pieceLength int) int {
	if pieceLength < BlockSize || pieceLength%BlockSize != 0 {
		return -1
	}
	ratio := pieceLength / BlockSize
	if ratio&(ratio-1) != 0 {
		return -1
	}
	return Log2(ratio)
}

// LayerTree is the part of a file's merkle tree at and above a base layer. It
// is built from the hashes of that layer, so a v2 seeder that only knows its
// "piece layers" can still answer BEP 52 requests with base >= the piece layer.
type LayerTree struct {
	// Base is the absolute level of layers[0] (0 is the block layer).
	Base int
	// layers[0] is the base layer padded to a power of two; each following
	// entry is the layer one level closer to the root. The last has one node.
	layers [][][HashSize]byte
}

// NewLayerTree builds the tree at and above base from the real hashes of that
// layer. Missing nodes needed to pad the layer to a power of two are the zero
// subtree root for that level (BEP 52). It returns nil for an empty layer or a
// negative base.
func NewLayerTree(base int, hashes [][HashSize]byte) *LayerTree {
	if base < 0 || len(hashes) == 0 {
		return nil
	}
	n := nextPowerOfTwo(len(hashes))
	layer := make([][HashSize]byte, n)
	copy(layer, hashes)
	pad := ZeroRoot(base)
	for i := len(hashes); i < n; i++ {
		layer[i] = pad
	}
	t := &LayerTree{Base: base}
	for {
		t.layers = append(t.layers, layer)
		if len(layer) == 1 {
			break
		}
		layer = collapse(layer)
	}
	return t
}

// Root returns the root hash of the tree.
func (t *LayerTree) Root() [HashSize]byte {
	return t.layers[len(t.layers)-1][0]
}

// Height returns the absolute level of the root (the block layer is 0).
func (t *LayerTree) Height() int {
	return t.Base + len(t.layers) - 1
}

// GetHashes returns the payload of a "hashes" reply: length hashes at absolute
// level base starting at index, then the uncle hashes from the requested
// subtree up to proofLayers. It returns nil when the request cannot be served
// (base below the tree base or above the root, a range out of bounds, or a
// length that is not a power of two). It mirrors libtorrent's
// merkle_tree::get_hashes.
func (t *LayerTree) GetHashes(base, index, length, proofLayers int) [][HashSize]byte {
	if t == nil || base < t.Base || length < 1 || index < 0 || proofLayers < 0 {
		return nil
	}
	if length&(length-1) != 0 || index%length != 0 {
		return nil
	}
	height := t.Height()
	if base >= height || base+proofLayers >= height {
		return nil
	}
	k := base - t.Base
	layer := t.layers[k]
	if index+length > len(layer) {
		return nil
	}
	ret := make([][HashSize]byte, 0, length+proofLayers)
	ret = append(ret, layer[index:index+length]...)

	m := Log2(length)
	nodeIdx := index >> m
	for i := m; i <= proofLayers; i++ {
		lvl := t.layers[k+i]
		ret = append(ret, lvl[nodeIdx^1])
		nodeIdx >>= 1
	}
	return ret
}

// VerifyHashes reports whether a "hashes" reply is anchored to root. hashes are
// the length requested hashes at absolute level base starting at index;
// uncles are the proof hashes in the order the reply carries them. paddedLeaves
// is the number of leaves of the padded tree (a power of two).
func VerifyHashes(root [HashSize]byte, paddedLeaves, base, index, length int, hashes, uncles [][HashSize]byte) bool {
	if paddedLeaves < 1 || paddedLeaves&(paddedLeaves-1) != 0 {
		return false
	}
	if length < 1 || length&(length-1) != 0 || len(hashes) != length || index < 0 {
		return false
	}
	total := Log2(paddedLeaves)
	if base < 0 || base >= total {
		return false
	}
	if index%length != 0 {
		return false
	}
	node := foldLayer(hashes)
	idx := index >> Log2(length)
	level := base + Log2(length)
	for _, uncle := range uncles {
		if level >= total {
			return false
		}
		if idx&1 == 0 {
			node = hashPair(node, uncle)
		} else {
			node = hashPair(uncle, node)
		}
		idx >>= 1
		level++
	}
	return level == total && idx == 0 && node == root
}

func foldLayer(hashes [][HashSize]byte) [HashSize]byte {
	layer := hashes
	for len(layer) > 1 {
		layer = collapse(layer)
	}
	return layer[0]
}
