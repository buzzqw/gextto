package bitfield

import (
	"math/rand"
	"testing"
)

func naiveCount(b *Bitfield) uint32 {
	var n uint32
	for i := uint32(0); i < b.Len(); i++ {
		if b.Test(i) {
			n++
		}
	}
	return n
}

// TestCountMatchesNaive checks the hardware-popcount Count
// against a bit-by-bit reference across word boundaries.
func TestCountMatchesNaive(t *testing.T) {
	for _, length := range []uint32{0, 1, 7, 8, 9, 63, 64, 65, 127, 128, 129, 1000, 1023, 1024, 1025} {
		for iter := 0; iter < 50; iter++ {
			b := New(length)
			for i := uint32(0); i < length; i++ {
				if rand.Intn(2) == 0 {
					b.Set(i)
				}
			}
			if got, want := b.Count(), naiveCount(b); got != want {
				t.Fatalf("length %d: Count() = %d, want %d", length, got, want)
			}
		}
	}
}

func TestCountAllAndNone(t *testing.T) {
	const n = 100
	b := New(n)
	if b.Count() != 0 {
		t.Fatalf("empty Count() = %d, want 0", b.Count())
	}
	for i := uint32(0); i < n; i++ {
		b.Set(i)
	}
	if b.Count() != n {
		t.Fatalf("full Count() = %d, want %d", b.Count(), n)
	}
	if !b.All() {
		t.Fatal("All() = false for a full bitfield")
	}
}
