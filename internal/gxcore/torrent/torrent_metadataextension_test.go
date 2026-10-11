package torrent

import (
	"bytes"
	"testing"
)

// TestMetadataPiece covers the BEP 9 metadata slicing, including the 32-bit
// overflow case: a huge piece index must be rejected, not served as the wrong
// piece.
func TestMetadataPiece(t *testing.T) {
	data := make([]byte, metadataPieceSize+100)
	for i := range data {
		data[i] = byte(i)
	}

	first, ok := metadataPiece(data, 0)
	if !ok || len(first) != metadataPieceSize {
		t.Fatalf("piece 0: ok=%v len=%d", ok, len(first))
	}
	tail, ok := metadataPiece(data, 1)
	if !ok || len(tail) != 100 || !bytes.Equal(tail, data[metadataPieceSize:]) {
		t.Fatalf("piece 1: ok=%v len=%d", ok, len(tail))
	}
	if _, ok := metadataPiece(data, 2); ok {
		t.Fatal("piece 2 must not exist")
	}
	if _, ok := metadataPiece(data, 1<<18); ok {
		t.Fatal("an overflowing piece index must be rejected")
	}
	if _, ok := metadataPiece(nil, 0); ok {
		t.Fatal("empty metadata has no pieces")
	}
}
