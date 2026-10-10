package torrent

import (
	"bytes"
	"reflect"
	"testing"
	"time"

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

// TestV2BlockCacheEvictsLeastRecentlyUsed pins the memory bound: the oldest
// tree is dropped first when the cache goes over budget, and a single tree that
// alone exceeds the budget is still kept.
func TestV2BlockCacheEvictsLeastRecentlyUsed(t *testing.T) {
	tr := &torrent{
		v2BlockTrees: map[string]*v2BlockTreeEntry{
			"a": {bytes: v2BlockCacheBudget, lastUsed: time.Unix(1, 0)},
			"b": {bytes: v2BlockCacheBudget, lastUsed: time.Unix(2, 0)},
		},
		v2BlockBytes: 2 * v2BlockCacheBudget,
	}
	tr.evictV2BlockTreesLocked()
	if _, ok := tr.v2BlockTrees["a"]; ok {
		t.Fatal("the least recently used tree must be evicted")
	}
	if _, ok := tr.v2BlockTrees["b"]; !ok {
		t.Fatal("the most recently used tree must be kept")
	}

	only := &torrent{
		v2BlockTrees: map[string]*v2BlockTreeEntry{
			"big": {bytes: 2 * v2BlockCacheBudget, lastUsed: time.Unix(1, 0)},
		},
		v2BlockBytes: 2 * v2BlockCacheBudget,
	}
	only.evictV2BlockTreesLocked()
	if len(only.v2BlockTrees) != 1 {
		t.Fatal("a single tree over the budget must still be cached")
	}
}
