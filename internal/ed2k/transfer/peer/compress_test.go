package peer

import (
	"bytes"
	"testing"
)

func TestCompressRoundTrip(t *testing.T) {
	body := bytes.Repeat([]byte("ed2k "), 1000)
	compressed, err := CompressData(body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecompressData(compressed, int64(len(body)))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("round trip = %d bytes, %v", len(got), err)
	}
	if _, err := DecompressData(compressed, 10); err == nil {
		t.Fatal("the limit must be enforced")
	}
	if _, err := DecompressData([]byte("not zlib"), 100); err == nil {
		t.Fatal("invalid zlib must fail")
	}
}
