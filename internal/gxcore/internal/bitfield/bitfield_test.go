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

// TestHasAnyNotIn checks the "does the peer have a piece we are missing" test
// against a bit-by-bit reference across word boundaries.
func TestHasAnyNotIn(t *testing.T) {
	for _, length := range []uint32{0, 1, 7, 8, 9, 63, 64, 65, 127, 128, 129, 1000} {
		for iter := 0; iter < 50; iter++ {
			have := New(length)
			peer := New(length)
			for i := uint32(0); i < length; i++ {
				if rand.Intn(2) == 0 {
					have.Set(i)
				}
				if rand.Intn(2) == 0 {
					peer.Set(i)
				}
			}
			want := false
			for i := uint32(0); i < length; i++ {
				if peer.Test(i) && !have.Test(i) {
					want = true
					break
				}
			}
			if got := peer.HasAnyNotIn(have); got != want {
				t.Fatalf("length %d: HasAnyNotIn = %v, want %v", length, got, want)
			}
		}
	}
}

func BenchmarkHasAnyNotIn(b *testing.B) {
	const n = 1 << 20 // 1M pieces
	have := New(n)
	peer := New(n)
	for i := uint32(0); i < n; i += 4 {
		have.Set(i)
	}
	for i := uint32(1); i < n; i += 3 {
		peer.Set(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !peer.HasAnyNotIn(have) {
			b.Fatal("expected a useful piece")
		}
	}
}

// TestHasAnyOutside: a bit counts only when neither have nor skip holds it,
// both in the word-wise part and in the byte tail.
func TestHasAnyOutside(t *testing.T) {
	for _, n := range []uint32{5, 64, 130} {
		peer, have, skip := New(n), New(n), New(n)
		last := n - 1
		peer.Set(last)
		if !peer.HasAnyOutside(have, nil) || !peer.HasAnyOutside(have, skip) {
			t.Fatalf("n=%d: wanted bit not seen", n)
		}
		skip.Set(last)
		if peer.HasAnyOutside(have, skip) {
			t.Fatalf("n=%d: skipped bit counted", n)
		}
		skip.Clear(last)
		have.Set(last)
		if peer.HasAnyOutside(have, skip) {
			t.Fatalf("n=%d: bit we have counted", n)
		}
	}
}
