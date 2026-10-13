package filesection

import (
	"bytes"
	"testing"
)

type memFile struct{ data []byte }

func (m *memFile) ReadAt(p []byte, off int64) (int, error)  { return copy(p, m.data[off:]), nil }
func (m *memFile) WriteAt(p []byte, off int64) (int, error) { return copy(m.data[off:], p), nil }

// TestWriteAtAcrossSections: a write starting inside the first section and
// ending in the third lands at the right file offsets, padding untouched.
func TestWriteAtAcrossSections(t *testing.T) {
	a, pad, c := &memFile{make([]byte, 10)}, &memFile{make([]byte, 4)}, &memFile{make([]byte, 10)}
	p := Piece{{File: a, Offset: 2, Length: 6}, {File: pad, Length: 4, Padding: true}, {File: c, Offset: 0, Length: 8}}
	n, err := p.WriteAt([]byte("ABCDEFGHIJ"), 3)
	if err != nil || n != 10 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if !bytes.Equal(a.data, []byte("\x00\x00\x00\x00\x00ABC\x00\x00")) {
		t.Fatalf("a = %q", a.data)
	}
	if !bytes.Equal(pad.data, make([]byte, 4)) {
		t.Fatal("padding written")
	}
	if !bytes.Equal(c.data[:3], []byte("HIJ")) {
		t.Fatalf("c = %q", c.data)
	}
}
