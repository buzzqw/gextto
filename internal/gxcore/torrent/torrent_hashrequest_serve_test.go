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
	block := merkle.BlockSize
	content := make([]byte, 24*block)
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
	req := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 0, Length: 4, ProofLayers: uint32(height - 2)}
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
	req = peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 4, Length: 4, ProofLayers: uint32(height - 2)}
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
	okReq := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 0, Length: 4, ProofLayers: uint32(height - 2)}
	if _, ok := tr.serveHashes(okReq); !ok {
		t.Fatal("a range inside a verified piece must be served")
	}
	// Blocks 4..7 are in the missing piece 1.
	missing := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 4, Length: 4, ProofLayers: uint32(height - 2)}
	if _, ok := tr.serveHashes(missing); ok {
		t.Fatal("a range touching a missing piece must be refused")
	}
	// Blocks 0..7 span piece 0 (verified) and piece 1 (missing).
	span := peerprotocol.HashRequestHeader{Root: root, Base: 0, Index: 0, Length: 8, ProofLayers: uint32(height - 3)}
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
