package metainfo

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"testing"

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
