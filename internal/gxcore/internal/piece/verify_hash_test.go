package piece

import (
	"crypto/sha1"
	"crypto/sha256"
	"testing"
)

// TestVerifyHashSHA256 locks in that the piece verification works with SHA-256
// (BitTorrent v2) as well as SHA-1: the hash algorithm is chosen by the caller.
func TestVerifyHashSHA256(t *testing.T) {
	buf := []byte("the quick brown fox")
	sum := sha256.Sum256(buf)
	p := Piece{Length: uint32(len(buf)), Hash: sum[:]}
	if !p.VerifyHash(buf, sha256.New()) {
		t.Fatal("a valid SHA-256 piece was rejected")
	}
	p.Hash[0] ^= 0xff
	if p.VerifyHash(buf, sha256.New()) {
		t.Fatal("a corrupted SHA-256 piece was accepted")
	}

	// The same buffer with a SHA-1 hash still verifies with SHA-1.
	sum1 := sha1.Sum(buf)
	q := Piece{Length: uint32(len(buf)), Hash: sum1[:]}
	if !q.VerifyHash(buf, sha1.New()) {
		t.Fatal("a valid SHA-1 piece was rejected")
	}
	if q.VerifyHash(buf, sha256.New()) {
		t.Fatal("a SHA-1 hash must not verify under SHA-256")
	}
}
