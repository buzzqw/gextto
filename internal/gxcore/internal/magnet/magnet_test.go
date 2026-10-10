package magnet

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestHybridMagnetCarriesV2Hash(t *testing.T) {
	v1 := strings.Repeat("ab", 20) // 20 bytes hex
	v2 := strings.Repeat("cd", 32) // 32 bytes hex
	m, err := New("magnet:?xt=urn:btmh:1220" + v2 + "&xt=urn:btih:" + v1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !m.HasV2 {
		t.Fatal("HasV2 = false, want true")
	}
	wantV2, _ := hex.DecodeString(v2)
	if hex.EncodeToString(m.V2InfoHash[:]) != hex.EncodeToString(wantV2) {
		t.Errorf("v2 hash = %x, want %s", m.V2InfoHash, v2)
	}
	wantV1, _ := hex.DecodeString(v1)
	if hex.EncodeToString(m.InfoHash[:]) != hex.EncodeToString(wantV1) {
		t.Errorf("v1 hash = %x, want %s", m.InfoHash, v1)
	}
}

func TestV2OnlyMagnetUsesTruncatedHash(t *testing.T) {
	v2 := strings.Repeat("cd", 32)
	m, err := New("magnet:?xt=urn:btmh:1220" + v2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.HasV1 {
		t.Error("v2-only magnet reported a v1 hash")
	}
	if !m.HasV2 {
		t.Fatal("HasV2 = false, want true")
	}
	want, _ := hex.DecodeString(v2)
	if hex.EncodeToString(m.V2InfoHash[:]) != v2 {
		t.Errorf("v2 hash = %x, want %s", m.V2InfoHash, v2)
	}
	if hex.EncodeToString(m.InfoHash[:]) != hex.EncodeToString(want[:20]) {
		t.Errorf("info hash = %x, want truncated %x", m.InfoHash, want[:20])
	}
	// The link round-trips as a btmh topic, not a bogus btih one.
	if got := m.String(); !strings.Contains(got, "urn:btmh:1220"+v2) || strings.Contains(got, "btih") {
		t.Errorf("String() = %q", got)
	}
}

func TestPlainMagnetHasNoV2(t *testing.T) {
	m, err := New("magnet:?xt=urn:btih:" + strings.Repeat("ab", 20))
	if err != nil {
		t.Fatal(err)
	}
	if m.HasV2 {
		t.Error("plain v1 magnet reported a v2 hash")
	}
}
