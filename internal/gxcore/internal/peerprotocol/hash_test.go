package peerprotocol

import (
	"bytes"
	"testing"
)

func testHeader() HashRequestHeader {
	var h HashRequestHeader
	for i := range h.Root {
		h.Root[i] = byte(i)
	}
	h.Base = 2
	h.Index = 512
	h.Length = 512
	h.ProofLayers = 7
	return h
}

func TestHashRequestHeaderRoundTrip(t *testing.T) {
	h := testHeader()
	var buf [HashRequestHeaderSize]byte
	if n := h.MarshalTo(buf[:]); n != HashRequestHeaderSize {
		t.Fatalf("MarshalTo = %d", n)
	}
	var got HashRequestHeader
	if err := got.UnmarshalBinary(buf[:]); err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Fatalf("round trip mismatch: %+v != %+v", got, h)
	}
	if err := (&HashRequestHeader{}).UnmarshalBinary(buf[:10]); err == nil {
		t.Fatal("short header accepted")
	}
}

func TestHashMessagesWireFormat(t *testing.T) {
	h := testHeader()
	hashes := [][HashSize]byte{{1}, {2}, {3}}
	msg := HashesMessage{HashRequestHeader: h, Hashes: hashes}
	var buf bytes.Buffer
	n, err := msg.WriteTo(&buf)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(HashRequestHeaderSize + 3*HashSize)
	if n != want || int64(buf.Len()) != want {
		t.Fatalf("WriteTo = %d, buffer %d, want %d", n, buf.Len(), want)
	}
	if msg.ID() != Hashes {
		t.Fatalf("hashes message ID = %d", msg.ID())
	}

	// Requests and rejects are just the fixed header.
	var rbuf bytes.Buffer
	if _, err := (HashRequestMessage{HashRequestHeader: h}).WriteTo(&rbuf); err != nil {
		t.Fatal(err)
	}
	if rbuf.Len() != HashRequestHeaderSize {
		t.Fatalf("hash request size = %d", rbuf.Len())
	}
	if (HashRequestMessage{}).ID() != HashRequest || (HashRejectMessage{}).ID() != HashReject {
		t.Fatal("unexpected hash message ids")
	}
}

// FuzzHashRequestHeader feeds arbitrary bytes to the header parser: it must
// never panic, only accept a 48-byte buffer or return an error.
func FuzzHashRequestHeader(f *testing.F) {
	f.Add(make([]byte, HashRequestHeaderSize))
	f.Add(make([]byte, HashRequestHeaderSize-1))
	f.Fuzz(func(t *testing.T, b []byte) {
		var h HashRequestHeader
		_ = h.UnmarshalBinary(b)
	})
}
