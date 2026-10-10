package merkle

import (
	"bytes"
	"crypto/sha256"
	"reflect"
	"testing"
)

// testHash is an independent SHA-256 witness used to build the expected values
// by hand. It intentionally does not reuse any function of this package.
func testHash(parts ...[]byte) [HashSize]byte {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	var out [HashSize]byte
	copy(out[:], h.Sum(nil))
	return out
}

// testLeaves returns n distinct leaf hashes that are easy to tell apart.
func testLeaves(n int) [][HashSize]byte {
	leaves := make([][HashSize]byte, n)
	for i := range leaves {
		leaves[i] = sha256.Sum256([]byte{byte(i)})
	}
	return leaves
}

var zeroHash = [HashSize]byte{}

func TestLeafHashes(t *testing.T) {
	pattern := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i*7 + 3)
		}
		return b
	}
	tests := []struct {
		name string
		data []byte
		want [][HashSize]byte
	}{
		{name: "empty", data: nil, want: nil},
		{name: "one byte", data: []byte{0xAB}, want: [][HashSize]byte{testHash([]byte{0xAB})}},
		{name: "exactly one block", data: pattern(BlockSize), want: [][HashSize]byte{testHash(pattern(BlockSize))}},
		{
			name: "one block plus one byte",
			data: pattern(BlockSize + 1),
			want: [][HashSize]byte{testHash(pattern(BlockSize)), testHash(pattern(BlockSize + 1)[BlockSize:])},
		},
		{
			name: "two exact blocks",
			data: pattern(2 * BlockSize),
			want: [][HashSize]byte{testHash(pattern(2 * BlockSize)[:BlockSize]), testHash(pattern(2 * BlockSize)[BlockSize:])},
		},
		{
			name: "three blocks with short tail",
			data: pattern(2*BlockSize + 5),
			want: [][HashSize]byte{
				testHash(pattern(2*BlockSize + 5)[:BlockSize]),
				testHash(pattern(2*BlockSize + 5)[BlockSize : 2*BlockSize]),
				testHash(pattern(2*BlockSize + 5)[2*BlockSize:]),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LeafHashes(tt.data)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LeafHashes = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestRoot(t *testing.T) {
	l := testLeaves(5)

	h01 := testHash(l[0][:], l[1][:])
	h23 := testHash(l[2][:], l[3][:])
	h4z := testHash(l[4][:], zeroHash[:])
	hzz := testHash(zeroHash[:], zeroHash[:])
	h2z := testHash(l[2][:], zeroHash[:])
	left := testHash(h01[:], h23[:])
	right := testHash(h4z[:], hzz[:])

	tests := []struct {
		name   string
		leaves [][HashSize]byte
		want   [HashSize]byte
	}{
		{name: "zero leaves", leaves: nil, want: [HashSize]byte{}},
		{name: "one leaf", leaves: l[:1], want: l[0]},
		{name: "two leaves", leaves: l[:2], want: h01},
		{
			name:   "three leaves pads to four",
			leaves: l[:3],
			want:   testHash(h01[:], h2z[:]),
		},
		{
			name:   "four leaves",
			leaves: l[:4],
			want:   testHash(h01[:], h23[:]),
		},
		{
			name:   "five leaves pads to eight",
			leaves: l[:5],
			want:   testHash(left[:], right[:]),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Root(tt.leaves); got != tt.want {
				t.Errorf("Root = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestLayer(t *testing.T) {
	l := testLeaves(5)

	h01 := testHash(l[0][:], l[1][:])
	h23 := testHash(l[2][:], l[3][:])
	h4z := testHash(l[4][:], zeroHash[:])
	hzz := testHash(zeroHash[:], zeroHash[:])
	left := testHash(h01[:], h23[:])
	right := testHash(h4z[:], hzz[:])

	tests := []struct {
		name   string
		leaves [][HashSize]byte
		level  int
		want   [][HashSize]byte
	}{
		{name: "empty", leaves: nil, level: 0, want: nil},
		{name: "negative level", leaves: l[:3], level: -1, want: nil},
		{name: "level zero is the leaves", leaves: l[:3], level: 0, want: l[:3]},
		{
			name:   "level one pads the tail",
			leaves: l[:3],
			level:  1,
			want:   [][HashSize]byte{h01, testHash(l[2][:], zeroHash[:])},
		},
		{
			name:   "level two of five leaves",
			leaves: l[:5],
			level:  2,
			want:   [][HashSize]byte{left, right},
		},
		{
			name:   "level three of five leaves is the root",
			leaves: l[:5],
			level:  3,
			want:   [][HashSize]byte{testHash(left[:], right[:])},
		},
		{name: "level above the tree", leaves: l[:5], level: 4, want: nil},
		{name: "single leaf has no level one", leaves: l[:1], level: 1, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Layer(tt.leaves, tt.level); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Layer(%d) = %x, want %x", tt.level, got, tt.want)
			}
		})
	}
}

func TestPieceLayer(t *testing.T) {
	l := testLeaves(9)

	// Level 3 group roots (pieceLength = 8*BlockSize).
	h01 := testHash(l[0][:], l[1][:])
	h23 := testHash(l[2][:], l[3][:])
	h45 := testHash(l[4][:], l[5][:])
	h67 := testHash(l[6][:], l[7][:])
	ab := testHash(h01[:], h23[:])
	cd := testHash(h45[:], h67[:])
	left8 := testHash(ab[:], cd[:])
	e := testHash(l[8][:], zeroHash[:])
	zz := testHash(zeroHash[:], zeroHash[:])
	gg := testHash(zz[:], zz[:])
	ef := testHash(e[:], zz[:])
	tail8 := testHash(ef[:], gg[:])

	// Level 3 group root for a three-leaf file padded to eight.
	h2z := testHash(l[2][:], zeroHash[:])
	mid3 := testHash(h01[:], h2z[:])
	tail3 := testHash(mid3[:], gg[:])

	tests := []struct {
		name        string
		leaves      [][HashSize]byte
		pieceLength int
		want        [][HashSize]byte
	}{
		{name: "empty leaves", leaves: nil, pieceLength: BlockSize, want: nil},
		{name: "piece length below block size", leaves: l[:3], pieceLength: BlockSize / 2, want: nil},
		{name: "piece length zero", leaves: l[:3], pieceLength: 0, want: nil},
		{name: "piece length not a power of two", leaves: l[:3], pieceLength: 3 * BlockSize, want: nil},
		{name: "piece length not a multiple of block size", leaves: l[:3], pieceLength: BlockSize + 1, want: nil},
		{
			name:        "piece length equals block size returns the leaves",
			leaves:      l[:5],
			pieceLength: BlockSize,
			want:        l[:5],
		},
		{
			name:        "two blocks per piece",
			leaves:      l[:3],
			pieceLength: 2 * BlockSize,
			want:        [][HashSize]byte{testHash(l[0][:], l[1][:]), testHash(l[2][:], zeroHash[:])},
		},
		{
			name:        "eight blocks per piece, tail padded",
			leaves:      l[:9],
			pieceLength: 8 * BlockSize,
			want:        [][HashSize]byte{left8, tail8},
		},
		{
			name:        "file smaller than one piece",
			leaves:      l[:3],
			pieceLength: 8 * BlockSize,
			want:        [][HashSize]byte{tail3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PieceLayer(tt.leaves, tt.pieceLength)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PieceLayer = %x, want %x", got, tt.want)
			}
		})
	}
}

// TestPieceLayerMatchesLayer checks that a piece layer is the leading slice of
// the corresponding full layer whenever that layer exists.
func TestPieceLayerMatchesLayer(t *testing.T) {
	l := testLeaves(9)
	pieces := PieceLayer(l, 8*BlockSize)
	layer := Layer(l, 3)
	if len(layer) < len(pieces) {
		t.Fatalf("Layer has %d hashes, want at least %d", len(layer), len(pieces))
	}
	if !reflect.DeepEqual(pieces, layer[:len(pieces)]) {
		t.Errorf("PieceLayer = %x, Layer[:%d] = %x", pieces, len(pieces), layer[:len(pieces)])
	}
}

func TestRootEqualsTopLayer(t *testing.T) {
	l := testLeaves(5)
	if got, want := Root(l), Layer(l, 3)[0]; got != want {
		t.Errorf("Root = %x, top layer = %x", got, want)
	}
}

func TestVerifyProof(t *testing.T) {
	l := testLeaves(4)
	root := Root(l)
	// Leaf 2: siblings are l[3] at the leaf level and h(l[0],l[1]) above.
	proof := [][HashSize]byte{l[3], testHash(l[0][:], l[1][:])}
	if !VerifyProof(l[2], 2, proof, root) {
		t.Error("valid proof rejected")
	}
	if VerifyProof(l[2], 1, proof, root) {
		t.Error("proof accepted with wrong index")
	}
	if VerifyProof(l[0], 2, proof, root) {
		t.Error("proof accepted with wrong leaf")
	}
	if VerifyProof(l[2], 2, proof, [HashSize]byte{}) {
		t.Error("proof accepted with wrong root")
	}
	if VerifyProof(l[2], -1, proof, root) {
		t.Error("proof accepted with negative index")
	}

	// A padded tree: leaf 2 of three leaves uses the zero hash as its sibling.
	l3 := testLeaves(3)
	root3 := Root(l3)
	proof3 := [][HashSize]byte{zeroHash, testHash(l3[0][:], l3[1][:])}
	if !VerifyProof(l3[2], 2, proof3, root3) {
		t.Error("valid proof for padded tree rejected")
	}
}

func TestLeafHashesNoDataPadding(t *testing.T) {
	// A short final block must be hashed over its actual bytes, not padded.
	short := bytes.Repeat([]byte{0x11}, 10)
	got := LeafHashes(short)
	want := testHash(short)
	if len(got) != 1 || got[0] != want {
		t.Errorf("LeafHashes = %x, want [%x]", got, want)
	}
}
