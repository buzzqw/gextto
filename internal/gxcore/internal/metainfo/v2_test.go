package metainfo

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/zeebo/bencode"
)

func hybridInfoBytes(t *testing.T) []byte {
	t.Helper()
	pieces := make([]byte, sha1.Size) // one piece, all zero
	info := map[string]any{
		"name":         "hybrid",
		"piece length": 16384,
		"pieces":       pieces,
		"length":       100,
		"meta version": 2,
		"file tree": map[string]any{
			"hybrid": map[string]any{"": map[string]any{"length": 100}},
		},
	}
	b, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A hybrid torrent carries both the v1 fields and the BEP 52 ones. Both info
// hashes are computed over the same (complete) info dictionary.
func TestHybridInfoHashes(t *testing.T) {
	b := hybridInfoBytes(t)
	info, err := NewInfo(b, true, true)
	if err != nil {
		t.Fatalf("NewInfo: %v", err)
	}
	if info.MetaVersion != 2 || !info.HasV2 {
		t.Fatalf("MetaVersion=%d HasV2=%v, want 2/true", info.MetaVersion, info.HasV2)
	}
	wantV1 := sha1.Sum(b)
	if info.Hash != wantV1 {
		t.Errorf("v1 hash = %x, want %x", info.Hash, wantV1)
	}
	wantV2 := sha256.Sum256(b)
	if info.V2Hash != wantV2 {
		t.Errorf("v2 hash = %x, want %x", info.V2Hash, wantV2)
	}
	// A v1-only torrent has no v2 hash.
	plain := map[string]any{"name": "v1", "piece length": 16384, "pieces": make([]byte, sha1.Size), "length": 10}
	pb, _ := bencode.EncodeBytes(plain)
	pinfo, err := NewInfo(pb, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if pinfo.HasV2 || pinfo.V2Hash != ([32]byte{}) {
		t.Errorf("v1 torrent has v2 data: HasV2=%v", pinfo.HasV2)
	}
}

func TestV2OnlyInfoRejected(t *testing.T) {
	info := map[string]any{
		"name":         "v2only",
		"piece length": 16384,
		"meta version": 2,
		"file tree":    map[string]any{"v2only": map[string]any{"": map[string]any{"length": 10}}},
	}
	b, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewInfo(b, true, true); !errors.Is(err, ErrV2Only) {
		t.Fatalf("err = %v, want ErrV2Only", err)
	}
}

func TestUnknownMetaVersionRejected(t *testing.T) {
	info := map[string]any{
		"name":         "future",
		"piece length": 16384,
		"pieces":       make([]byte, sha1.Size),
		"length":       10,
		"meta version": 3,
	}
	b, _ := bencode.EncodeBytes(info)
	if _, err := NewInfo(b, true, true); !errors.Is(err, ErrUnknownMetaVersion) {
		t.Fatalf("err = %v, want ErrUnknownMetaVersion", err)
	}
}

func TestPieceLayersParsed(t *testing.T) {
	info := hybridInfoBytes(t)
	root := string(make([]byte, net32)) // a 32-byte merkle root key
	layer := make([]byte, net32)        // one hash in the layer
	torrent := struct {
		Info        bencode.RawMessage `bencode:"info"`
		PieceLayers map[string][]byte  `bencode:"piece layers"`
	}{Info: info, PieceLayers: map[string][]byte{root: layer}}
	tb, err := bencode.EncodeBytes(torrent)
	if err != nil {
		t.Fatal(err)
	}
	mi, err := New(bytes.NewReader(tb))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(mi.PieceLayers) != 1 {
		t.Fatalf("piece layers = %d, want 1", len(mi.PieceLayers))
	}
	if got := mi.PieceLayers[root]; !bytes.Equal(got, layer) {
		t.Errorf("layer value = %x, want %x", got, layer)
	}
}

const net32 = 32

func TestV2LayoutAndPieces(t *testing.T) {
	block := merkle.BlockSize
	pieceLength := 2 * block // 32 KiB
	dataA := bytes.Repeat([]byte{0xA1}, 5*block)
	dataB := bytes.Repeat([]byte{0xB2}, block)
	leavesA := merkle.LeafHashes(dataA)
	rootA := merkle.Root(leavesA)
	layerA := merkle.PieceLayer(leavesA, pieceLength) // ceil(5/2) = 3 hashes
	rootB := merkle.Root(merkle.LeafHashes(dataB))    // one block: root = leaf

	info := map[string]any{
		"name":         "v2set",
		"piece length": pieceLength,
		"meta version": 2,
		"file tree": map[string]any{
			"a.bin": map[string]any{"": map[string]any{"length": len(dataA), "pieces root": rootA[:]}},
			"b.bin": map[string]any{"": map[string]any{"length": len(dataB), "pieces root": rootB[:]}},
		},
	}
	raw, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatal(err)
	}
	i, err := NewV2Info(raw)
	if err != nil {
		t.Fatalf("NewV2Info: %v", err)
	}
	if i.NumPieces != 4 {
		t.Fatalf("NumPieces = %d, want 4 (3 for a.bin + 1 for b.bin)", i.NumPieces)
	}
	if i.PieceHashLen != net32 {
		t.Fatalf("PieceHashLen = %d, want 32", i.PieceHashLen)
	}
	// Files: a.bin (5 blocks), a padding file (1 block), b.bin (1 block).
	if len(i.Files) != 3 || !i.Files[1].Padding || i.Files[1].Length != int64(block) {
		t.Fatalf("files = %+v", i.Files)
	}
	if i.Length != int64(len(dataA)+len(dataB)) || i.Padding != int64(block) {
		t.Fatalf("length/padding = %d/%d", i.Length, i.Padding)
	}

	layerBytes := make([]byte, 0, len(layerA)*net32)
	for _, h := range layerA {
		layerBytes = append(layerBytes, h[:]...)
	}
	layers := map[string][]byte{string(rootA[:]): layerBytes}
	if err := i.AttachV2Pieces(layers); err != nil {
		t.Fatalf("AttachV2Pieces: %v", err)
	}
	if len(i.PieceHash(0)) != net32 || len(i.PieceHash(3)) != net32 {
		t.Fatal("piece hashes are not 32 bytes")
	}
	if !bytes.Equal(i.PieceHash(3), rootB[:]) {
		t.Fatalf("last piece hash = %x, want the b.bin root", i.PieceHash(3))
	}
}

func TestParseFileTree(t *testing.T) {
	tree := map[string]any{
		"a.txt": map[string]any{"": map[string]any{"length": 100}},
		"dir": map[string]any{
			"b.bin": map[string]any{"": map[string]any{"length": 200}},
			"c.bin": map[string]any{"": map[string]any{"length": 50}},
		},
	}
	raw, err := bencode.EncodeBytes(tree)
	if err != nil {
		t.Fatal(err)
	}
	files, err := ParseFileTree(raw)
	if err != nil {
		t.Fatalf("ParseFileTree: %v", err)
	}
	want := []struct {
		path   string
		length int64
	}{{"a.txt", 100}, {"dir/b.bin", 200}, {"dir/c.bin", 50}}
	if len(files) != len(want) {
		t.Fatalf("got %d files, want %d", len(files), len(want))
	}
	for i, w := range want {
		if files[i].Path != w.path || files[i].Length != w.length {
			t.Errorf("file %d = %q/%d, want %q/%d", i, files[i].Path, files[i].Length, w.path, w.length)
		}
	}
}

func TestParseFileTreePiecesRoot(t *testing.T) {
	root := make([]byte, net32)
	for i := range root {
		root[i] = byte(i)
	}
	tree := map[string]any{"f": map[string]any{"": map[string]any{"length": 5, "pieces root": root}}}
	raw, _ := bencode.EncodeBytes(tree)
	files, err := ParseFileTree(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !files[0].HasRoot {
		t.Fatalf("expected one file with a pieces root, got %+v", files)
	}
	if files[0].PiecesRoot[net32-1] != net32-1 {
		t.Errorf("pieces root = %x", files[0].PiecesRoot)
	}
}

func TestNewV2InfoOnly(t *testing.T) {
	info := map[string]any{
		"name":         "v2only",
		"piece length": 16384,
		"meta version": 2,
		"file tree": map[string]any{
			"v2only": map[string]any{"": map[string]any{"length": 10}},
		},
	}
	raw, _ := bencode.EncodeBytes(info)
	i, err := NewV2Info(raw)
	if err != nil {
		t.Fatalf("NewV2Info: %v", err)
	}
	if i.MetaVersion != 2 || !i.HasV2 || i.NumPieces != 1 {
		t.Fatalf("MetaVersion=%d HasV2=%v NumPieces=%d", i.MetaVersion, i.HasV2, i.NumPieces)
	}
	if len(i.V2Files) != 1 || i.V2Files[0].Path != "v2only" || i.V2Files[0].Length != 10 {
		t.Fatalf("V2Files = %+v", i.V2Files)
	}
	if i.V2Hash != sha256.Sum256(raw) {
		t.Errorf("v2 hash mismatch")
	}
	if i.Length != 10 {
		t.Errorf("length = %d, want 10", i.Length)
	}
}

func TestVerifyPieceLayers(t *testing.T) {
	block := merkle.BlockSize
	pieceLength := 2 * block
	data := make([]byte, 5*block) // 5 blocks: last layer group is partial
	for i := range data {
		data[i] = byte(i*7 + 1)
	}
	leaves := merkle.LeafHashes(data)
	root := merkle.Root(leaves)
	layer := merkle.PieceLayer(leaves, pieceLength)
	if len(layer) != 3 { // ceil(5 / 2)
		t.Fatalf("piece layer has %d hashes, want 3", len(layer))
	}
	layerBytes := make([]byte, 0, len(layer)*merkle.HashSize)
	for _, h := range layer {
		layerBytes = append(layerBytes, h[:]...)
	}
	files := []V2File{{Path: "f.bin", Length: int64(len(data)), PiecesRoot: root, HasRoot: true}}
	key := string(root[:])

	if err := VerifyPieceLayers(files, pieceLength, map[string][]byte{key: layerBytes}); err != nil {
		t.Fatalf("valid piece layer rejected: %v", err)
	}
	// A corrupted hash must not match the pieces root.
	corrupt := append([]byte{}, layerBytes...)
	corrupt[0] ^= 0xff
	if err := VerifyPieceLayers(files, pieceLength, map[string][]byte{key: corrupt}); err == nil {
		t.Fatal("corrupted piece layer accepted")
	}
	// The wrong number of hashes is rejected.
	if err := VerifyPieceLayers(files, pieceLength, map[string][]byte{key: layerBytes[:merkle.HashSize]}); err == nil {
		t.Fatal("short piece layer accepted")
	}
	// A missing entry is rejected.
	if err := VerifyPieceLayers(files, pieceLength, nil); err == nil {
		t.Fatal("missing piece layer accepted")
	}
}
