package peerwriter

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// TestServedWindowBounded verifies the engine change: the dedup window is
// bounded, evicts the oldest, and stops rejecting blocks re-requested later.
func TestServedWindowBounded(t *testing.T) {
	w := newServedWindow(3)
	r := func(i uint32) peerprotocol.RequestMessage {
		return peerprotocol.RequestMessage{Index: i}
	}

	if w.seen(r(0)) {
		t.Fatal("first sight of block 0 reported as duplicate")
	}
	if !w.seen(r(0)) {
		t.Fatal("immediate re-request of block 0 not reported as duplicate")
	}
	w.seen(r(1))
	w.seen(r(2))

	// Adding a fourth entry evicts the oldest (block 0).
	w.seen(r(3))
	if w.seen(r(0)) {
		t.Fatal("block 0 still within the window after eviction")
	}
	if !w.seen(r(2)) {
		t.Fatal("block 2 was evicted too early")
	}

	// It never grows without bound.
	for i := uint32(0); i < 5000; i++ {
		w.seen(r(i))
	}
	if len(w.m) > 3 || len(w.q) > 3 {
		t.Fatalf("window grew to map=%d queue=%d, want <= 3", len(w.m), len(w.q))
	}
}
