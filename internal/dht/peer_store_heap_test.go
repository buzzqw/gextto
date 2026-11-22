package dht

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"
)

// TestPeerStoreHeapAtCap measures the live heap at the default cap: 2048 info
// hashes times 256 contacts. The store used to keep three structures per
// contact (a set entry, a ring node and a separate per-IP seed map) for about
// 85 MB; the contact is now one slice entry plus one map position.
func TestPeerStoreHeapAtCap(t *testing.T) {
	const hashes, perHash = 2048, 256
	store := newPeerStore(hashes, perHash)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for h := 0; h < hashes; h++ {
		key := make([]byte, 20)
		binary.BigEndian.PutUint32(key[:4], uint32(h))
		ih := InfoHash(key)
		for p := 0; p < perHash; p++ {
			n := h*perHash + p
			addr := fmt.Sprintf("10.%d.%d.%d:6881", (n>>16)&255, (n>>8)&255, n&255)
			contact := encodeCompactPeerAddr(addr)
			if contact == "" {
				t.Fatalf("contact %q did not encode", addr)
			}
			store.addContact(ih, contact, p%2 == 0)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	// Keep the store live past the measurement: otherwise the collector may
	// reclaim it as dead and the heap would show no contacts at all.
	live := store.count(InfoHash(make([]byte, 20)))

	if live == 0 || after.HeapAlloc <= before.HeapAlloc {
		t.Fatalf("the store holds no contacts or allocated no heap: live=%d before %d, after %d", live, before.HeapAlloc, after.HeapAlloc)
	}
	perContact := (after.HeapAlloc - before.HeapAlloc) / (hashes * perHash)
	if perContact > 100 {
		t.Fatalf("%d bytes per contact at the cap, want under 100 (the old three structures cost about 163)", perContact)
	}
}
