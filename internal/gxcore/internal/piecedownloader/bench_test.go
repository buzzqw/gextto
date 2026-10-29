package piecedownloader

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/filesection"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// BenchmarkPieceDownloaderLifecycle is one 4 MiB piece (256 blocks) fetched
// from one peer as the torrent loop does it: plan, request in windows of 64,
// receive every block, done. It runs once per downloaded piece.
func BenchmarkPieceDownloaderLifecycle(b *testing.B) {
	const length = 4 << 20
	pi := &piece.Piece{
		Length: length,
		Data:   filesection.Piece{{File: fakeFile{}, Offset: 0, Length: length}},
	}
	buf := bufferpool.Buffer{Data: make([]byte, length)}
	block := make([]byte, piece.BlockSize)
	pe := &fakePeer{requested: make([]uint32, 0, 256)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pe.requested = pe.requested[:0]
		d := New(pi, pe, false, buf)
		next := 0
		for !d.Done() {
			d.RequestBlocks(d.Pending() + 64)
			for ; next < len(pe.requested); next++ {
				if err := d.GotBlock(pe.requested[next], block); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}
