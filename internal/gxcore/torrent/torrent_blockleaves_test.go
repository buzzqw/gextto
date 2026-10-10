package torrent

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
)

// TestAppendBlockLeavesMatchesContiguousHashes pins that concatenating the block
// hashes of a file's pieces in order yields the same leaves as hashing the whole
// file's data at once: that is what the base=0 seed serving relies on.
func TestAppendBlockLeavesMatchesContiguousHashes(t *testing.T) {
	// In BEP 52 the piece length is a power-of-two multiple of the 16 KiB block,
	// so only the file's last piece can be shorter (and not block-aligned). A
	// piece length of 2 blocks with a 3-block-and-7-byte file mirrors that.
	pieceLength := 2 * merkle.BlockSize
	first := bytes.Repeat([]byte{0x11}, pieceLength)
	second := bytes.Repeat([]byte{0x22}, merkle.BlockSize+7)
	got := appendBlockLeaves(nil, first)
	got = appendBlockLeaves(got, second)

	whole := append(append([]byte{}, first...), second...)
	want := merkle.LeafHashes(whole)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("block leaves differ: got %d, want %d", len(got), len(want))
	}
	if len(got) != 4 { // 3 full blocks + the short tail block
		t.Fatalf("leaves = %d, want 4", len(got))
	}
	// The tree built from the leaves anchors to the file's leaf-layer root.
	if merkle.NewLayerTree(0, got) == nil {
		t.Fatal("nil layer tree")
	}
}
