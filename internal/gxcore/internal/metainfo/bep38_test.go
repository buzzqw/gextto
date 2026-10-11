package metainfo

import (
	"bytes"
	"testing"

	"github.com/zeebo/bencode"
)

// torrentWithHints builds a minimal single-file v1 torrent whose info dictionary
// and top level carry the given BEP 38 hints.
func torrentWithHints(t *testing.T, infoCollections, infoSimilar, topCollections, topSimilar []string) []byte {
	t.Helper()
	pieces := make([]byte, 20) // one SHA-1 piece, contents irrelevant here
	info := struct {
		Name        string   `bencode:"name"`
		PieceLength int      `bencode:"piece length"`
		Pieces      []byte   `bencode:"pieces"`
		Length      int64    `bencode:"length"`
		Collections []string `bencode:"collections,omitempty"`
		Similar     []string `bencode:"similar,omitempty"`
	}{
		Name:        "file.bin",
		PieceLength: 16384,
		Pieces:      pieces,
		Length:      16384,
		Collections: infoCollections,
		Similar:     infoSimilar,
	}
	infoBytes, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatalf("encode info: %v", err)
	}
	torrent := struct {
		Info        bencode.RawMessage `bencode:"info"`
		Collections []string           `bencode:"collections,omitempty"`
		Similar     []string           `bencode:"similar,omitempty"`
	}{
		Info:        infoBytes,
		Collections: topCollections,
		Similar:     topSimilar,
	}
	torrentBytes, err := bencode.EncodeBytes(torrent)
	if err != nil {
		t.Fatalf("encode torrent: %v", err)
	}
	return torrentBytes
}

func hash20(b byte) string {
	h := make([]byte, 20)
	for i := range h {
		h[i] = b + byte(i)
	}
	return string(h)
}

func TestBEP38HintsInsideInfoDict(t *testing.T) {
	raw := torrentWithHints(t, []string{"com.example.s1"}, []string{hash20(1)}, nil, nil)
	mi, err := New(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := mi.Info.Collections; len(got) != 1 || got[0] != "com.example.s1" {
		t.Fatalf("collections = %v", got)
	}
	if len(mi.Info.Similar) != 1 {
		t.Fatalf("similar = %v", mi.Info.Similar)
	}
}

func TestBEP38HintsAtTopLevel(t *testing.T) {
	raw := torrentWithHints(t, nil, nil, []string{"com.example.s2"}, []string{hash20(2)})
	mi, err := New(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := mi.Info.Collections; len(got) != 1 || got[0] != "com.example.s2" {
		t.Fatalf("collections = %v", got)
	}
	if len(mi.Info.Similar) != 1 {
		t.Fatalf("similar = %v", mi.Info.Similar)
	}
}

// TestBEP38HintsMergedAndDeduplicated checks the spec rule that hints inside and
// outside the info dict are merged, without duplicates.
func TestBEP38HintsMergedAndDeduplicated(t *testing.T) {
	raw := torrentWithHints(t,
		[]string{"com.example.s1"},
		[]string{hash20(1)},
		[]string{"com.example.s1", "com.example.s2"},
		[]string{hash20(1), hash20(2)},
	)
	mi, err := New(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cols := mi.Info.Collections
	if len(cols) != 2 || cols[0] != "com.example.s1" || cols[1] != "com.example.s2" {
		t.Fatalf("collections = %v, want [s1 s2]", cols)
	}
	if len(mi.Info.Similar) != 2 {
		t.Fatalf("similar = %v, want 2 unique hashes", mi.Info.Similar)
	}
}

// TestBEP38InvalidHintsIgnored checks that malformed info hashes and empty
// collection identifiers are dropped instead of failing the parse.
func TestBEP38InvalidHintsIgnored(t *testing.T) {
	raw := torrentWithHints(t,
		[]string{"", "  ", "ok"},
		[]string{"short", hash20(3)}, // "short" is not 20 bytes
		nil, nil,
	)
	mi, err := New(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := mi.Info.Collections; len(got) != 1 || got[0] != "ok" {
		t.Fatalf("collections = %v, want [ok]", got)
	}
	if len(mi.Info.Similar) != 1 {
		t.Fatalf("similar = %v, want exactly the valid hash", mi.Info.Similar)
	}
}

// TestBEP38ExportKeepsHints checks that exporting a torrent writes the hints
// back out at the top level, so they survive a round trip.
func TestBEP38ExportKeepsHints(t *testing.T) {
	raw := torrentWithHints(t, nil, nil, []string{"com.example.series"}, []string{hash20(9)})
	mi, err := New(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := NewBytesWithHints(mi.Info.Bytes, nil, nil, nil, "", mi.Info.Collections, mi.Info.Similar)
	if err != nil {
		t.Fatalf("NewBytesWithHints: %v", err)
	}
	back, err := New(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if got := back.Info.Collections; len(got) != 1 || got[0] != "com.example.series" {
		t.Fatalf("exported collections = %v", got)
	}
	if len(back.Info.Similar) != 1 {
		t.Fatalf("exported similar = %v", back.Info.Similar)
	}
}
