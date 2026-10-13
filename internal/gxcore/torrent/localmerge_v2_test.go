package torrent

import (
	"bytes"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/zeebo/bencode"
)

// v2FileSpec is one file of a synthetic v2 torrent.
type v2FileSpec struct {
	name string
	data []byte
}

func v2Concat(l [][32]byte) []byte {
	var b []byte
	for _, h := range l {
		b = append(b, h[:]...)
	}
	return b
}

// testV2Info builds a v2-only info dictionary for the given files and piece
// length, with the piece layers attached (as a .torrent would carry them).
func testV2Info(t testing.TB, pieceLength int, specs []v2FileSpec) *metainfo.Info {
	t.Helper()
	tree := map[string]any{}
	layers := map[string][]byte{}
	for _, s := range specs {
		leaves := merkle.LeafHashes(s.data)
		root := merkle.Root(leaves)
		tree[s.name] = map[string]any{"": map[string]any{"length": len(s.data), "pieces root": root[:]}}
		layers[string(root[:])] = v2Concat(merkle.PieceLayer(leaves, pieceLength))
	}
	raw, err := bencode.EncodeBytes(map[string]any{
		"name":         "v2set",
		"piece length": pieceLength,
		"meta version": 2,
		"file tree":    tree,
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := metainfo.NewV2Info(raw)
	if err != nil {
		t.Fatalf("NewV2Info: %v", err)
	}
	if err := info.AttachV2Pieces(layers); err != nil {
		t.Fatalf("AttachV2Pieces: %v", err)
	}
	return info
}

// Phase 2: two v2 torrents with the same file but a different piece length share
// the file. The destination is filled only if every destination piece verifies
// against its own hash.
func TestPrefillV2MergesDifferentLayout(t *testing.T) {
	block := merkle.BlockSize
	content := make([]byte, 5*block)
	for i := range content {
		content[i] = byte(i*31 + 7)
	}
	specs := []v2FileSpec{{name: "a.bin", data: content}}

	// Source: 2 blocks per piece (3 pieces).
	srcInfo := testV2Info(t, 2*block, specs)
	filesSrc := testFiles(t, t.TempDir(), srcInfo)
	if _, err := filesSrc[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bf := bitfield.New(srcInfo.NumPieces)
	for i := uint32(0); i < srcInfo.NumPieces; i++ {
		bf.Set(i)
	}
	src := &torrent{pieces: piece.NewPieces(srcInfo, filesSrc), bitfield: bf}
	src.setInfo(srcInfo)
	src.publishLocalData()

	// Destination: 1 block per piece (5 pieces), same content.
	dstInfo := testV2Info(t, block, specs)
	filesDst := testFiles(t, t.TempDir(), dstInfo)
	dst := &torrent{pieces: piece.NewPieces(dstInfo, filesDst)}
	dst.setInfo(dstInfo)

	if dstInfo.NumPieces != 5 || srcInfo.NumPieces != 3 {
		t.Fatalf("layouts: src=%d dst=%d pieces, want 3/5", srcInfo.NumPieces, dstInfo.NumPieces)
	}
	if got := dst.prefillFrom([]*Torrent{{torrent: src}}); got != int(dstInfo.NumPieces) {
		t.Fatalf("merged %d pieces, want %d", got, dstInfo.NumPieces)
	}
	if len(dst.mergedPieces) != int(dstInfo.NumPieces) {
		t.Fatalf("mergedPieces = %d, want %d (the verifier must skip them)", len(dst.mergedPieces), dstInfo.NumPieces)
	}
	out := make([]byte, len(content))
	if _, err := filesDst[0].Storage.ReadAt(out, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, content) {
		t.Fatal("destination file does not hold the reused content")
	}
}

// A destination piece is reused only when every overlapping source piece is
// verified: with just the first source piece set, only the destination pieces
// that fall inside it are filled.
func TestPrefillV2RequiresVerifiedSourcePieces(t *testing.T) {
	block := merkle.BlockSize
	content := make([]byte, 5*block)
	for i := range content {
		content[i] = byte(i*13 + 1)
	}
	specs := []v2FileSpec{{name: "a.bin", data: content}}

	srcInfo := testV2Info(t, 2*block, specs) // piece 0 = blocks 0..1
	filesSrc := testFiles(t, t.TempDir(), srcInfo)
	if _, err := filesSrc[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bf := bitfield.New(srcInfo.NumPieces)
	bf.Set(0) // only the first source piece is verified
	src := &torrent{pieces: piece.NewPieces(srcInfo, filesSrc), bitfield: bf}
	src.setInfo(srcInfo)
	src.publishLocalData()

	dstInfo := testV2Info(t, block, specs) // five 1-block pieces
	dst := &torrent{pieces: piece.NewPieces(dstInfo, testFiles(t, t.TempDir(), dstInfo))}
	dst.setInfo(dstInfo)

	if got := dst.prefillFrom([]*Torrent{{torrent: src}}); got != 2 {
		t.Fatalf("merged %d pieces, want 2 (blocks 0 and 1)", got)
	}
}

// Files with different content have different Merkle roots: nothing is reused.
func TestPrefillV2SkipsDifferentFiles(t *testing.T) {
	block := merkle.BlockSize
	srcData := bytes.Repeat([]byte{0x5A}, 4*block)
	dstData := bytes.Repeat([]byte{0xA5}, 4*block)

	srcInfo := testV2Info(t, 2*block, []v2FileSpec{{name: "a.bin", data: srcData}})
	filesSrc := testFiles(t, t.TempDir(), srcInfo)
	if _, err := filesSrc[0].Storage.WriteAt(srcData, 0); err != nil {
		t.Fatal(err)
	}
	bf := bitfield.New(srcInfo.NumPieces)
	for i := uint32(0); i < srcInfo.NumPieces; i++ {
		bf.Set(i)
	}
	src := &torrent{pieces: piece.NewPieces(srcInfo, filesSrc), bitfield: bf}
	src.setInfo(srcInfo)
	src.publishLocalData()

	dstInfo := testV2Info(t, block, []v2FileSpec{{name: "a.bin", data: dstData}})
	dst := &torrent{pieces: piece.NewPieces(dstInfo, testFiles(t, t.TempDir(), dstInfo))}
	dst.setInfo(dstInfo)

	if got := dst.prefillFrom([]*Torrent{{torrent: src}}); got != 0 {
		t.Fatalf("merged %d pieces with different content, want 0", got)
	}
}

// BEP 27 also guards the Phase 2 path: a private torrent never mixes swarms.
func TestPrefillV2RespectsPrivacy(t *testing.T) {
	block := merkle.BlockSize
	content := bytes.Repeat([]byte{0x11}, 4*block)

	info := testV2Info(t, 2*block, []v2FileSpec{{name: "a.bin", data: content}})
	filesSrc := testFiles(t, t.TempDir(), info)
	if _, err := filesSrc[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bf := bitfield.New(info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		bf.Set(i)
	}
	src := &torrent{pieces: piece.NewPieces(info, filesSrc), bitfield: bf}
	src.setInfo(info)
	src.localData.Store(&localDataSnapshot{pieces: src.pieces, private: true, length: info.Length, pieceLength: info.PieceLength})

	dstInfo := testV2Info(t, block, []v2FileSpec{{name: "a.bin", data: content}})
	dst := &torrent{pieces: piece.NewPieces(dstInfo, testFiles(t, t.TempDir(), dstInfo))}
	dst.setInfo(dstInfo)
	if got := dst.prefillFrom([]*Torrent{{torrent: src}}); got != 0 {
		t.Fatalf("merged %d pieces from a private swarm, want 0", got)
	}
}

// TestPrefillV2DeduplicatesCandidates: two sources with the same file but other
// layouts share the work — a later candidate fills only the pieces a previous
// one could not, and no piece is read or written twice.
func TestPrefillV2DeduplicatesCandidates(t *testing.T) {
	block := merkle.BlockSize
	content := make([]byte, 5*block)
	for i := range content {
		content[i] = byte(i*17 + 3)
	}
	specs := []v2FileSpec{{name: "a.bin", data: content}}

	// Candidate 1: 2 blocks per piece, only piece 0 verified (blocks 0..1).
	src1Info := testV2Info(t, 2*block, specs)
	files1 := testFiles(t, t.TempDir(), src1Info)
	if _, err := files1[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bf1 := bitfield.New(src1Info.NumPieces)
	bf1.Set(0)
	cand1 := &torrent{pieces: piece.NewPieces(src1Info, files1), bitfield: bf1}
	cand1.setInfo(src1Info)
	cand1.publishLocalData()

	// Candidate 2: the whole file in one piece, fully verified.
	src2Info := testV2Info(t, 8*block, specs)
	files2 := testFiles(t, t.TempDir(), src2Info)
	if _, err := files2[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bf2 := bitfield.New(src2Info.NumPieces)
	for i := uint32(0); i < src2Info.NumPieces; i++ {
		bf2.Set(i)
	}
	cand2 := &torrent{pieces: piece.NewPieces(src2Info, files2), bitfield: bf2}
	cand2.setInfo(src2Info)
	cand2.publishLocalData()

	dstInfo := testV2Info(t, block, specs)
	filesDst := testFiles(t, t.TempDir(), dstInfo)
	dst := &torrent{pieces: piece.NewPieces(dstInfo, filesDst)}
	dst.setInfo(dstInfo)

	got := dst.prefillFrom([]*Torrent{{torrent: cand1}, {torrent: cand2}})
	if got != int(dstInfo.NumPieces) {
		t.Fatalf("merged %d pieces, want %d (deduplicated)", got, dstInfo.NumPieces)
	}
	out := make([]byte, len(content))
	if _, err := filesDst[0].Storage.ReadAt(out, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, content) {
		t.Fatal("destination file does not hold the reused content")
	}
}

// BenchmarkPrefillFromUnrelatedV2 measures the one-shot allocation-time cost of
// the Phase 2 scan when no file matches: it is a price every torrent pays once,
// with no steady-state (picker, peer, piece-write) path involved.
func BenchmarkPrefillFromUnrelatedV2(b *testing.B) {
	block := merkle.BlockSize
	dstData := bytes.Repeat([]byte{0x01}, 4*block)
	dstInfo := testV2Info(b, block, []v2FileSpec{{name: "a.bin", data: dstData}})
	dst := &torrent{pieces: piece.NewPieces(dstInfo, testFiles(b, b.TempDir(), dstInfo))}
	dst.setInfo(dstInfo)

	const n = 16
	cands := make([]*Torrent, 0, n)
	for i := 0; i < n; i++ {
		data := bytes.Repeat([]byte{byte(i + 2)}, 4*block)
		si := testV2Info(b, 2*block, []v2FileSpec{{name: "a.bin", data: data}})
		files := testFiles(b, b.TempDir(), si)
		bf := bitfield.New(si.NumPieces)
		for k := uint32(0); k < si.NumPieces; k++ {
			bf.Set(k)
		}
		s := &torrent{pieces: piece.NewPieces(si, files), bitfield: bf}
		s.setInfo(si)
		s.publishLocalData()
		cands = append(cands, &Torrent{torrent: s})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := dst.prefillFrom(cands); got != 0 {
			b.Fatalf("merged %d pieces, want 0", got)
		}
	}
}
