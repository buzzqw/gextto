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
