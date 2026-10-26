package filesection

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// memFile is an in-memory ReadWriterAt.
type memFile struct {
	data []byte
}

func (f *memFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	return copy(p, f.data[off:]), nil
}

func (f *memFile) WriteAt(p []byte, off int64) (int, error) {
	copy(f.data[off:], p)
	return len(p), nil
}

// errFile fails with errTest after serving prefix bytes.
type errFile struct {
	prefix []byte
}

var errTest = errors.New("test storage error")

func (f *errFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(f.prefix)) {
		return 0, errTest
	}
	n := copy(p, f.prefix[off:])
	if n < len(p) {
		return n, errTest
	}
	return n, nil
}

func (f *errFile) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }

func twoSectionPiece() Piece {
	return Piece{
		{File: &memFile{data: []byte("0123456789abcdef")}, Offset: 0, Length: 16, Name: "a"},
		{File: &memFile{data: []byte("GHIJKLMNOPQRSTUV")}, Offset: 0, Length: 16, Name: "b"},
	}
}

func TestReadAtFullAndSpanning(t *testing.T) {
	p := twoSectionPiece()
	for _, tc := range []struct {
		name string
		off  int
		want string
	}{
		{"first section", 0, "0123456789abcdef"},
		{"spanning boundary", 12, "cdefGHIJKLMNOPQRSTUV"[:16]},
		{"second section", 16, "GHIJKLMNOPQRSTUV"},
		{"tail", 24, "OPQRST"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := make([]byte, len(tc.want))
			n, err := p.ReadAt(b, int64(tc.off))
			if err != nil {
				t.Fatalf("ReadAt = (%d, %v), want (%d, nil)", n, err, len(tc.want))
			}
			if n != len(tc.want) || string(b[:n]) != tc.want {
				t.Fatalf("ReadAt = %q, want %q", b[:n], tc.want)
			}
		})
	}
}

func TestReadAtShortReportsUnexpectedEOF(t *testing.T) {
	p := twoSectionPiece() // 32 bytes total
	b := make([]byte, 40)
	n, err := p.ReadAt(b, 0)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want ErrUnexpectedEOF", err)
	}
	if n != 32 {
		t.Fatalf("n = %d, want 32", n)
	}
	if !bytes.Equal(b[:32], []byte("0123456789abcdefGHIJKLMNOPQRSTUV")) {
		t.Fatalf("bytes = %q", b[:n])
	}
}

func TestReadAtStorageErrorPassesThrough(t *testing.T) {
	p := Piece{{File: &errFile{prefix: []byte("01234567")}, Offset: 0, Length: 16, Name: "a"}}
	b := make([]byte, 16)
	n, err := p.ReadAt(b, 0)
	if !errors.Is(err, errTest) {
		t.Fatalf("err = %v, want test storage error", err)
	}
	if n != 8 || string(b[:n]) != "01234567" {
		t.Fatalf("n = %d bytes = %q, want 8 %q", n, b[:n], "01234567")
	}
}

func TestWriteSkipsPadding(t *testing.T) {
	fa := &memFile{data: make([]byte, 4)}
	fb := &memFile{data: make([]byte, 4)}
	p := Piece{
		{File: fa, Offset: 0, Length: 4, Name: "a"},
		{File: nil, Offset: 0, Length: 4, Name: "pad", Padding: true},
		{File: fb, Offset: 0, Length: 4, Name: "b"},
	}
	n, err := p.Write([]byte("aaaappppbbbb"))
	if err != nil || n != 8 {
		t.Fatalf("Write = (%d, %v), want (8, nil)", n, err)
	}
	if string(fa.data) != "aaaa" || string(fb.data) != "bbbb" {
		t.Fatalf("files = %q %q", fa.data, fb.data)
	}
}

func TestHasPadding(t *testing.T) {
	if (Piece{{Length: 4}, {Length: 8}}).HasPadding() {
		t.Fatal("a piece without padding sections reported padding")
	}
	if !(Piece{{Length: 4}, {Length: 2, Padding: true}}).HasPadding() {
		t.Fatal("a padding section was not reported")
	}
}
