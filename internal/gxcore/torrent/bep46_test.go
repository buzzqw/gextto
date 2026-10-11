package torrent

import (
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/magnet"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

func TestBEP46InfoHash(t *testing.T) {
	ih := strings.Repeat("i", 20)
	got, err := bep46InfoHash(map[string]interface{}{"ih": ih})
	if err != nil || string(got[:]) != ih {
		t.Fatalf("bep46InfoHash = %x err=%v", got, err)
	}
	for name, value := range map[string]interface{}{
		"not a dictionary": "x",
		"missing ih":       map[string]interface{}{"v": "x"},
		"short ih":         map[string]interface{}{"ih": strings.Repeat("i", 19)},
	} {
		if _, err := bep46InfoHash(value); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestMagnetInfoHashMatches(t *testing.T) {
	v1 := [20]byte{}
	for i := range v1 {
		v1[i] = byte(i + 1)
	}
	v1ma := &magnet.Magnet{InfoHash: v1, HasV1: true}
	if !magnetInfoHashMatches(v1ma, &metainfo.Info{Hash: v1}) {
		t.Fatal("a matching v1 info hash must match")
	}
	if magnetInfoHashMatches(v1ma, &metainfo.Info{Hash: [20]byte{}}) {
		t.Fatal("a different v1 info hash must not match")
	}

	// A v2 magnet matches on the truncated SHA-256.
	v2ma := &magnet.Magnet{InfoHash: v1, HasV2: true}
	info := &metainfo.Info{}
	copy(info.V2Hash[:], v1[:])
	if !magnetInfoHashMatches(v2ma, info) {
		t.Fatal("a matching v2 info hash must match")
	}
}
