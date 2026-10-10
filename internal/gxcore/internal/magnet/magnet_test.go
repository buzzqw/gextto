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

func TestV2OnlyMagnetRejected(t *testing.T) {
	_, err := New("magnet:?xt=urn:btmh:1220" + strings.Repeat("cd", 32))
	if err == nil {
		t.Fatal("btmh-only magnet must be rejected while v2 download is unsupported")
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
