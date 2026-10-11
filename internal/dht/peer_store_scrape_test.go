package dht

import (
	"testing"
)

// TestPeerStoreScrapeFilters checks the BEP 33 filters split seeds from peers.
func TestPeerStoreScrapeFilters(t *testing.T) {
	h := newPeerStore(10, 100)
	ih := InfoHash("01234567890123456789")

	h.addContact(ih, encodeCompactPeerAddr("1.2.3.4:6881"), false)
	h.addContact(ih, encodeCompactPeerAddr("1.2.3.5:6882"), false)
	h.addContact(ih, encodeCompactPeerAddr("5.6.7.8:6883"), true)

	pe, sd := h.scrapeFilters(ih)
	if pe == nil || sd == nil {
		t.Fatal("scrape filters are nil")
	}
	if got := pe.estimate(); got < 1.5 || got > 2.5 {
		t.Fatalf("peer estimate = %.2f, want ~2", got)
	}
	if got := sd.estimate(); got < 0.5 || got > 1.5 {
		t.Fatalf("seed estimate = %.2f, want ~1", got)
	}

	// An empty infohash has no filters.
	if pe, sd := h.scrapeFilters(InfoHash("aaaaaaaaaaaaaaaaaaaa")); pe != nil || sd != nil {
		t.Fatal("filters for an unknown infohash are not nil")
	}
}

// TestPeerStoreNoSeed checks that noseed excludes the seeding contact.
func TestPeerStoreNoSeed(t *testing.T) {
	h := newPeerStore(10, 100)
	ih := InfoHash("01234567890123456789")
	h.addContact(ih, encodeCompactPeerAddr("5.6.7.8:6883"), true)

	if all := h.peerContacts(ih, false); len(all) != 1 {
		t.Fatalf("all contacts = %d, want 1", len(all))
	}
	if nos := h.peerContacts(ih, true); len(nos) != 0 {
		t.Fatalf("noseed contacts = %d, want 0 (only a seed is stored)", len(nos))
	}
}

// TestPeerStoreSeedStatusUpdated: a seed that re-announces as a peer becomes a
// peer again (the latest status wins).
func TestPeerStoreSeedStatusUpdated(t *testing.T) {
	h := newPeerStore(10, 100)
	ih := InfoHash("01234567890123456789")
	c := encodeCompactPeerAddr("5.6.7.8:6883")
	h.addContact(ih, c, true)
	h.addContact(ih, c, false)

	_, sd := h.scrapeFilters(ih)
	for _, b := range sd {
		if b != 0 {
			t.Fatal("the seed filter is not empty after the peer stopped seeding")
		}
	}
}
