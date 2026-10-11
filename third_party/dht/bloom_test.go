package dht

import (
	"encoding/hex"
	"math"
	"testing"
)

// TestBloomVector checks the BEP 33 test vector: inserting 192.0.2.0-192.0.2.255
// and 2001:DB8::-2001:DB8::3E7 must produce the documented filter, whose estimate
// is ~1224.9308.
func TestBloomVector(t *testing.T) {
	var b scrapeBloom
	for i := 0; i < 256; i++ {
		b.insert([]byte{192, 0, 2, byte(i)})
	}
	for i := 0; i <= 0x3E7; i++ {
		ip := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, byte(i >> 8), byte(i)}
		b.insert(ip)
	}

	const wantHex = "" +
		"F6C3F5EAA07FFD91BDE89F777F26FB2BFF37BDB8FB2BBAA2FD3DDDE7BACFFF75EE7CCBAE" +
		"FE5EEDB1FBFAFF67F6ABFF5E43DDBCA3FD9B9FFDF4FFD3E9DFF12D1BDF59DB53DBE9FA5B" +
		"7FF3B8FDFCDE1AFB8BEDD7BE2F3EE71EBBBFE93BCDEEFE148246C2BC5DBFF7E7EFDCF24F" +
		"D8DC7ADFFD8FFFDFDDFFF7A4BBEEDF5CB95CE81FC7FCFF1FF4FFFFDFE5F7FDCBB7FD79B3" +
		"FA1FC77BFE07FFF905B7B7FFC7FEFEFFE0B8370BB0CD3F5B7F2BD93FEB4386CFDD6F7FD5" +
		"BFAF2E9EBFFFFEECD67ADBF7C67F17EFD5D75EBA6FFEBA7FFF47A91EB1BFBB53E8ABFB57" +
		"62ABE8FF237279BFEFBFEEF5FFC5FEBFDFE5ADFFADFEE1FB737FFFFBFD9F6AEFFEEE76B6" +
		"FD8F72EF"
	if len(wantHex) != 512 {
		t.Fatalf("wantHex has %d hex chars, expected 512", len(wantHex))
	}
	want, err := hex.DecodeString(wantHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 256 {
		t.Fatalf("want %d bytes, expected 256", len(want))
	}
	if got := hex.EncodeToString(b[:]); got != hex.EncodeToString(want) {
		t.Fatalf("filter mismatch:\n got %s\nwant %s", got, hex.EncodeToString(want))
	}

	got := b.estimate()
	if math.Abs(got-1224.9308) > 0.5 {
		t.Fatalf("estimate = %.4f, want ~1224.9308", got)
	}
}

// TestBloomUnion checks that ORing two filters raises the estimate.
func TestBloomUnion(t *testing.T) {
	var a, b scrapeBloom
	for i := 0; i < 100; i++ {
		a.insert([]byte{10, 0, 0, byte(i)})
		b.insert([]byte{10, 0, 1, byte(i)})
	}
	ea, eb := a.estimate(), b.estimate()
	a.union(&b)
	if got := a.estimate(); got < ea || got < eb {
		t.Fatalf("union estimate %.1f is not >= singles (%.1f, %.1f)", got, ea, eb)
	}
}

// TestEstimateScrape checks the client-side union of BFsd/BFpe filters.
func TestEstimateScrape(t *testing.T) {
	var seedsA, seedsB, peersA scrapeBloom
	for i := 0; i < 50; i++ {
		seedsA.insert([]byte{10, 1, 0, byte(i)})
		seedsB.insert([]byte{10, 1, 1, byte(i)})
		peersA.insert([]byte{10, 2, 0, byte(i)})
	}
	seeders, peers, err := EstimateScrape([][]byte{seedsA[:], seedsB[:]}, [][]byte{peersA[:]})
	if err != nil {
		t.Fatal(err)
	}
	if seeders < 90 || seeders > 110 {
		t.Fatalf("seeders estimate = %.1f, want ~100", seeders)
	}
	if peers < 40 || peers > 60 {
		t.Fatalf("peers estimate = %.1f, want ~50", peers)
	}
	if _, _, err := EstimateScrape([][]byte{{1, 2, 3}}, nil); err == nil {
		t.Fatal("a wrong-sized filter should be rejected")
	}
}
