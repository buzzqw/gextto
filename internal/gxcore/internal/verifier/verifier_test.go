package verifier

import (
	"crypto/sha1"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/filesection"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// countingFile counts how many times a piece's bytes are read, so a skipped
// piece can be proven not to be re-read.
type countingFile struct {
	data  []byte
	reads int
}

func (f *countingFile) ReadAt(p []byte, off int64) (int, error) {
	f.reads++
	return copy(p, f.data[off:]), nil
}

func (f *countingFile) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }

func makePiece(index uint32, data []byte) (piece.Piece, *countingFile) {
	sum := sha1.Sum(data)
	f := &countingFile{data: data}
	p := piece.Piece{
		Index:  index,
		Length: uint32(len(data)),
		Hash:   sum[:],
		Data:   filesection.Piece{{File: f, Offset: 0, Length: int64(len(data))}},
	}
	return p, f
}

func runVerifier(t *testing.T, pieces []piece.Piece, skip func(uint32) bool) (*Verifier, int) {
	t.Helper()
	v := New()
	v.Skip = skip
	progressC := make(chan Progress, len(pieces)+1)
	resultC := make(chan *Verifier, 1)
	v.Run(pieces, progressC, resultC)
	close(progressC)
	n := 0
	for range progressC {
		n++
	}
	return <-resultC, n
}

// TestSkipsAlreadyVerifiedPieces: a piece marked by Skip is set without being
// read or hashed, the others are verified normally, and every piece still
// reports progress.
func TestSkipsAlreadyVerifiedPieces(t *testing.T) {
	pa, fa := makePiece(0, []byte("piece-a-16-bytes"))
	pb, fb := makePiece(1, []byte("piece-b-16-bytes"))

	v, progress := runVerifier(t, []piece.Piece{pa, pb}, func(i uint32) bool { return i == 0 })

	if v.Bitfield == nil || !v.Bitfield.Test(0) || !v.Bitfield.Test(1) {
		t.Fatalf("bitfield does not hold both pieces: %v", v.Bitfield)
	}
	if fa.reads != 0 {
		t.Errorf("skipped piece was read %d times, want 0", fa.reads)
	}
	if fb.reads != 1 {
		t.Errorf("verified piece read %d times, want 1", fb.reads)
	}
	if progress != 2 {
		t.Errorf("progress reports = %d, want 2", progress)
	}
}

// TestVerifiesWithoutSkip is the control: without Skip every piece is read.
func TestVerifiesWithoutSkip(t *testing.T) {
	pa, fa := makePiece(0, []byte("0123456789abcdef"))
	v, _ := runVerifier(t, []piece.Piece{pa}, nil)
	if !v.Bitfield.Test(0) {
		t.Fatal("piece not verified")
	}
	if fa.reads != 1 {
		t.Errorf("reads = %d, want 1", fa.reads)
	}
}

// TestPrepareAndOnly: Prepare runs first in the verifier goroutine and may set
// Skip/Only; with Only, a piece outside the set is neither read nor marked,
// which is what lets an empty, just-created storage skip hashing its holes.
func TestPrepareAndOnly(t *testing.T) {
	pa, fa := makePiece(0, []byte("piece-a-16-bytes"))
	pb, fb := makePiece(1, []byte("piece-b-16-bytes"))

	v := New()
	prepared := false
	v.Prepare = func(stop <-chan struct{}) {
		prepared = true
		v.Skip = func(i uint32) bool { return i == 0 }
		v.Only = func(i uint32) bool { return i == 0 }
	}
	resultC := make(chan *Verifier, 1)
	v.Run([]piece.Piece{pa, pb}, make(chan Progress, 4), resultC)
	<-resultC
	if !prepared {
		t.Fatal("Prepare did not run")
	}
	if !v.Bitfield.Test(0) || v.Bitfield.Test(1) {
		t.Fatalf("bitfield = %v/%v, want only piece 0", v.Bitfield.Test(0), v.Bitfield.Test(1))
	}
	if fa.reads != 0 || fb.reads != 0 {
		t.Fatalf("reads = %d/%d, want none", fa.reads, fb.reads)
	}
}

// TestCloseDuringPrepare: closing the verifier while Prepare runs stops it
// before any piece is read, and Close does not wait for the whole check.
func TestCloseDuringPrepare(t *testing.T) {
	pa, fa := makePiece(0, []byte("piece-a-16-bytes"))
	v := New()
	entered := make(chan struct{})
	v.Prepare = func(stop <-chan struct{}) {
		close(entered)
		<-stop
	}
	go v.Run([]piece.Piece{pa}, make(chan Progress, 1), make(chan *Verifier))
	<-entered
	v.Close()
	if fa.reads != 0 {
		t.Fatalf("piece read %d time(s) after close", fa.reads)
	}
}
