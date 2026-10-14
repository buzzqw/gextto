package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// v2ServeFixture builds a v2 file of 6 pieces of 4 blocks (24 blocks of
// 16 KiB), all full, with the given pieces marked verified and the disk data
// written. It returns the torrent and the file's root.
func v2ServeFixture(t *testing.T, verified ...uint32) (*torrent, [merkle.HashSize]byte) {
	t.Helper()
	return v2ServeFixtureSized(t, 24*merkle.BlockSize, verified...)
}

// v2ServeFixtureSized is v2ServeFixture with a file of size bytes (pieces of 4
// blocks).
func v2ServeFixtureSized(t *testing.T, size int, verified ...uint32) (*torrent, [merkle.HashSize]byte) {
	t.Helper()
	block := merkle.BlockSize
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i*7 + 1)
	}
	info := testV2Info(t, 4*block, []v2FileSpec{{name: "a.bin", data: content}})
	files := testFiles(t, t.TempDir(), info)
	if _, err := files[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bf := bitfield.New(info.NumPieces)
	for _, i := range verified {
		bf.Set(i)
	}
	tr := &torrent{pieces: piece.NewPieces(info, files), bitfield: bf}
	tr.setInfo(info)
	return tr, info.V2Files[0].PiecesRoot
}

// TestServeHashesPartialBlockLayer: a partly downloaded v2 file serves a
// base-0 hash request that spans whole verified pieces, anchored to the root.
func TestServeHashesPartialBlockLayer(t *testing.T) {
	tr, root := v2ServeFixture(t, 0, 1, 2) // partial: pieces 3..5 missing
	if tr.v2FileComplete(0) {
		t.Fatal("the fixture must be partial")
	}
	paddedLeaves := merkle.NextPowerOfTwo(24) // 32
	height := merkle.Log2(paddedLeaves)       // 5
	// A full proof from base 0 (as the downloader asks): proof = height-1,
	// uncles at levels 2..4.
	req := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 0, Length: 4, ProofLayers: uint32(height - 1)}
	hashes, ok := tr.serveHashes(req)
	if !ok {
		t.Fatal("serveHashes refused a request fully covered by verified pieces")
	}
	if len(hashes) != 4+3 {
		t.Fatalf("hashes = %d, want 4 leaves + 3 uncles", len(hashes))
	}
	if !merkle.VerifyHashes(root, paddedLeaves, 0, 0, 4, hashes[:4], hashes[4:]) {
		t.Fatal("the proof does not anchor to the file root")
	}

	// A later range (piece 1) is served too.
	req = peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 4, Length: 4, ProofLayers: uint32(height - 1)}
	hashes, ok = tr.serveHashes(req)
	if !ok || !merkle.VerifyHashes(root, paddedLeaves, 0, 4, 4, hashes[:4], hashes[4:]) {
		t.Fatal("a later range was not served or did not verify")
	}
}

// TestServeHashesPartialOnlyVerifiedPieces: a request touching a missing piece
// is refused, and one fully inside a verified piece is served.
func TestServeHashesPartialOnlyVerifiedPieces(t *testing.T) {
	tr, root := v2ServeFixture(t, 0) // only piece 0 (blocks 0..3)
	paddedLeaves := merkle.NextPowerOfTwo(24)
	height := merkle.Log2(paddedLeaves)

	// Blocks 0..3 are in the verified piece 0.
	okReq := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 0, Length: 4, ProofLayers: uint32(height - 1)}
	if _, ok := tr.serveHashes(okReq); !ok {
		t.Fatal("a range inside a verified piece must be served")
	}
	// Blocks 4..7 are in the missing piece 1.
	missing := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 4, Length: 4, ProofLayers: uint32(height - 1)}
	if _, ok := tr.serveHashes(missing); ok {
		t.Fatal("a range touching a missing piece must be refused")
	}
	// Blocks 0..7 span piece 0 (verified) and piece 1 (missing).
	span := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 0, Length: 8, ProofLayers: uint32(height - 1)}
	if _, ok := tr.serveHashes(span); ok {
		t.Fatal("a range spanning a missing piece must be refused")
	}
	// A range folding below the piece layer (m < level = 2) cannot be proved
	// from data we do not have.
	small := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 0, Length: 2, ProofLayers: 1}
	if _, ok := tr.serveHashes(small); ok {
		t.Fatal("a sub-piece range must be refused while the file is partial")
	}
}

// TestServeHashesRejectsAbuse: hostile request headers (bogus root, bad
// length, huge proofs, overflow) must be refused, never panic and never be
// served from unrelated data.
func TestServeHashesRejectsAbuse(t *testing.T) {
	tr, root := v2ServeFixture(t, 0, 1, 2)
	bad := []peerprotocol.HashRequestHeader{
		{Root: [merkle.HashSize]byte{0xff}, Base: 0, Index: 0, Length: 4, ProofLayers: 3}, // unknown root
		{Root: root, Base: 0, Index: 0, Length: 3, ProofLayers: 3},                        // length not a power of two
		{Root: root, Base: 0, Index: 1, Length: 4, ProofLayers: 3},                        // index not aligned
		{Root: root, Base: 0, Index: 0, Length: 1 << 20, ProofLayers: 3},                  // length past the tree
		{Root: root, Base: 0, Index: 0, Length: 4, ProofLayers: 1 << 20},                  // huge proof
		{Root: root, Base: 0, Index: 0, Length: 4, ProofLayers: 5},                        // proof reaching past the root
		{Root: root, Base: 0, Index: 1 << 31, Length: 4, ProofLayers: 0},                  // index past the padded layer
		{Root: root, Base: 1 << 20, Index: 0, Length: 1, ProofLayers: 0},                  // base past the height
	}
	for i, req := range bad {
		if _, ok := tr.serveHashes(req); ok {
			t.Errorf("request %d was served but must be refused: %+v", i, req)
		}
	}
}

// TestServeHashesPieceLayerWithoutData: base >= piece layer is served from the
// attached piece hashes even with no block data at all.
func TestServeHashesPieceLayerWithoutData(t *testing.T) {
	tr, root := v2ServeFixture(t) // nothing verified
	if tr.v2FileComplete(0) {
		t.Fatal("the fixture must be partial")
	}
	req := peerprotocol.HashRequestHeader{Root: root, Base: 2, Index: 0, Length: 2, ProofLayers: 2}
	hashes, ok := tr.serveHashes(req)
	if !ok {
		t.Fatal("the piece layer must be served from the attached piece hashes")
	}
	// The file tree has 32 padded leaves (24 blocks); base 2 (the piece layer),
	// length 2 leaves, then the uncles to the root.
	paddedLeaves := merkle.NextPowerOfTwo(24)
	if !merkle.VerifyHashes(root, paddedLeaves, 2, 0, 2, hashes[:2], hashes[2:]) {
		t.Fatal("the piece-layer proof does not anchor to the file root")
	}
}

// TestServeHashesPartialMatchesFullTree: for every request a partly downloaded
// file can answer, the reply is exactly what the full block tree returns, so a
// downloader cannot tell the two apart; ranges reaching into the zero padding
// past the end of the file are covered too.
func TestServeHashesPartialMatchesFullTree(t *testing.T) {
	block := merkle.BlockSize
	size := 21*block + 100 // 22 blocks: 6 pieces of 4, the last one short
	tr, root := v2ServeFixtureSized(t, size, 0, 1, 2, 3, 4, 5)
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i*7 + 1)
	}
	full := merkle.NewLayerTree(0, merkle.LeafHashes(content))
	if full.Root() != root {
		t.Fatal("the reference tree does not match the file root")
	}
	// Pretend the file is partial for the serving path: drop a piece we never
	// request below, so v2FileComplete is false.
	partial, _ := v2ServeFixtureSized(t, size, 0, 1, 2, 3, 5)
	height := full.Height()
	served := 0
	for _, length := range []int{4, 8, 16, 32} {
		for index := 0; index+length <= 1<<height; index += length {
			for proof := 0; proof < height; proof++ {
				req := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: uint32(index), Length: uint32(length), ProofLayers: uint32(proof)}
				want := full.GetHashes(0, index, length, proof)
				got, ok := tr.servePartialBlockHashes(0, 2, req)
				if !ok || !equalHashes(got, want) {
					t.Fatalf("index=%d length=%d proof=%d: partial reply differs from the full tree (ok=%v, %d vs %d hashes)", index, length, proof, ok, len(got), len(want))
				}
				// Piece 4 holds blocks 16..19: only ranges avoiding it are served.
				got, ok = partial.serveHashes(req)
				touches := index < 20 && index+length > 16
				if ok == touches {
					t.Fatalf("index=%d length=%d proof=%d: served=%v with piece 4 missing", index, length, proof, ok)
				}
				if ok {
					served++
					if !equalHashes(got, want) {
						t.Fatalf("index=%d length=%d proof=%d: partial reply differs from the full tree", index, length, proof)
					}
				}
			}
		}
	}
	if served == 0 {
		t.Fatal("no request was served from the partial file")
	}
}

// TestServeHashesPartialCapsLength: a range larger than v2ChunkMax hashes is
// refused, so one request cannot make the torrent loop read a huge file span.
func TestServeHashesPartialCapsLength(t *testing.T) {
	tr, root := v2ServeFixture(t, 0, 1, 2)
	req := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 0, Length: 2 * v2ChunkMax}
	if _, ok := tr.servePartialBlockHashes(0, 2, req); ok {
		t.Fatal("a request above v2ChunkMax must be refused")
	}
}

func equalHashes(a, b [][merkle.HashSize]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
