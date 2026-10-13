package piecedownloader

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/filesection"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

type fakeFile struct{}

func (fakeFile) ReadAt(p []byte, off int64) (int, error)  { return len(p), nil }
func (fakeFile) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }

type fakePeer struct{ requested []uint32 }

func (p *fakePeer) RequestPiece(index, begin, length uint32) {
	p.requested = append(p.requested, begin)
}
func (p *fakePeer) CancelPiece(index, begin, length uint32) {}
func (p *fakePeer) EnabledFast() bool                       { return false }

// TestRequestBlocksReturnsCount: RequestBlocks reports how many requests it
// sent, so a caller can share one in-flight budget across a peer's pieces.
func TestRequestBlocksReturnsCount(t *testing.T) {
	length := uint32(piece.BlockSize*2 + 100) // three blocks, the last one short
	pi := &piece.Piece{
		Index:  0,
		Length: length,
		Data:   filesection.Piece{{File: fakeFile{}, Offset: 0, Length: int64(length)}},
	}
	if got := len(pi.CalculateBlocks()); got != 3 {
		t.Fatalf("blocks = %d, want 3", got)
	}
	pe := &fakePeer{}
	d := New(pi, pe, false, bufferpool.Buffer{Data: make([]byte, length)})

	if n := d.RequestBlocks(2); n != 2 {
		t.Fatalf("RequestBlocks(2) = %d, want 2", n)
	}
	if len(pe.requested) != 2 {
		t.Fatalf("peer received %d requests, want 2", len(pe.requested))
	}
	// The budget already consumed by this piece must not be overspent.
	if n := d.RequestBlocks(1); n != 0 {
		t.Fatalf("RequestBlocks(1) after two pending = %d, want 0", n)
	}
	if n := d.RequestBlocks(5); n != 1 {
		t.Fatalf("RequestBlocks(5) = %d, want the last remaining block", n)
	}
	if len(pe.requested) != 3 {
		t.Fatalf("peer received %d requests, want 3", len(pe.requested))
	}
}

// TestPrefillSkipsTheBlock: a prefilled block lands in the buffer, is never
// requested, and counts towards Done; a wrong length or a second prefill of
// the same block is refused.
func TestPrefillSkipsTheBlock(t *testing.T) {
	length := uint32(piece.BlockSize*2 + 100)
	pi := &piece.Piece{Index: 0, Length: length, Data: filesection.Piece{{File: fakeFile{}, Offset: 0, Length: int64(length)}}}
	pe := &fakePeer{}
	d := New(pi, pe, false, bufferpool.Buffer{Data: make([]byte, length)})

	block := make([]byte, piece.BlockSize)
	block[0] = 0xAB
	if !d.Prefill(piece.BlockSize, block) {
		t.Fatal("prefill refused")
	}
	if d.Prefill(piece.BlockSize, block) {
		t.Fatal("same block prefilled twice")
	}
	if d.Prefill(0, block[:10]) {
		t.Fatal("block with a wrong length accepted")
	}
	if d.Buffer.Data[piece.BlockSize] != 0xAB {
		t.Fatal("prefilled data not copied into the buffer")
	}
	if n := d.RequestBlocks(10); n != 2 {
		t.Fatalf("requested %d blocks, want the 2 not prefilled", n)
	}
	for _, begin := range pe.requested {
		if begin == piece.BlockSize {
			t.Fatal("the prefilled block was requested")
		}
	}
	if err := d.GotBlock(0, make([]byte, piece.BlockSize)); err != nil {
		t.Fatal(err)
	}
	if err := d.GotBlock(2*piece.BlockSize, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if !d.Done() {
		t.Fatal("piece not done with the prefilled block counted")
	}
}
