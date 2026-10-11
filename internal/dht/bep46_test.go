package dht

import (
	"encoding/hex"
	"testing"
)

// TestBEP46TargetVectors checks MutableTarget against the official BEP 46 test
// vectors: the target is the SHA-1 of the public key, plus the salt when given.
func TestBEP46TargetVectors(t *testing.T) {
	pub := mustHex(t, "8543d3e6115f0f98c944077a4493dcd543e49c739fd998550a1f614ab36ed63e")
	if got := MutableTarget(pub, nil); hex.EncodeToString(got[:]) != "cc3f9d90b572172053626f9980ce261a850d050b" {
		t.Fatalf("target (no salt) = %x", got)
	}
	if got := MutableTarget(pub, []byte{0x6e}); hex.EncodeToString(got[:]) != "59ee7c2cb9b4f7eb1986ee2d18fd2fdb8a56554f" {
		t.Fatalf("target (salt 6e) = %x", got)
	}
}
