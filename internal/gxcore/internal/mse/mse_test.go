package mse

import (
	"bytes"
	"crypto/rc4"
	"testing"
)

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestInPlaceStreamWriterRoundTrip verifies the engine writer encrypts in
// place and that the peer can decrypt the ciphertext.
func TestInPlaceStreamWriterRoundTrip(t *testing.T) {
	key := make([]byte, 16)
	enc, err := rc4.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := rc4.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	w := &inPlaceStreamWriter{s: enc, w: &out}

	plain := []byte("the quick brown fox")
	in := append([]byte(nil), plain...)
	n, err := w.Write(in)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(plain) {
		t.Fatalf("Write = %d bytes, want %d", n, len(plain))
	}
	if bytes.Equal(in, plain) {
		t.Fatal("source buffer was not modified: not encrypting in place")
	}

	got := make([]byte, out.Len())
	dec.XORKeyStream(got, out.Bytes())
	if !bytes.Equal(got, plain) {
		t.Fatalf("decrypted = %q, want %q", got, plain)
	}
}

// TestInPlaceStreamWriterNoAllocs is the point of the change: writing a block
// must not allocate, unlike cipher.StreamWriter.
func TestInPlaceStreamWriterNoAllocs(t *testing.T) {
	enc, err := rc4.NewCipher(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	w := &inPlaceStreamWriter{s: enc, w: discardWriter{}}
	buf := make([]byte, 16*1024)

	allocs := testing.AllocsPerRun(100, func() {
		if _, err := w.Write(buf); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Write allocated %v times per call, want 0", allocs)
	}
}
