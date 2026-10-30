package piecewriter

import (
	"bytes"
	"crypto/sha1"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/filesection"
	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/semaphore"
	"github.com/rcrowley/go-metrics"
)

// benchPieceLength is a typical piece length: what one write hashes.
const benchPieceLength = 4 << 20

// discardFile accepts the piece write and keeps nothing, so a benchmark of
// Run measures the hashing and the pipeline, not the disk.
type discardFile struct{ written int }

func (f *discardFile) ReadAt(p []byte, off int64) (int, error) { return len(p), nil }

func (f *discardFile) WriteAt(p []byte, off int64) (int, error) {
	f.written += len(p)
	return len(p), nil
}

// v1Piece builds a piece whose SHA-1 matches buf.
func v1Piece(buf []byte) piece.Piece {
	sum := sha1.Sum(buf)
	return piece.Piece{Length: uint32(len(buf)), Hash: sum[:]}
}

// v2Piece builds a BEP 52 piece whose merkle root matches buf.
func v2Piece(buf []byte) piece.Piece {
	leaves := merkle.LeafHashes(buf)
	root := merkle.Root(leaves)
	return piece.Piece{Length: uint32(len(buf)), Hash: root[:], V2: true, V2Leaves: len(leaves)}
}

// corrupt returns a copy of buf with its last byte flipped, so a piece built
// from the copy cannot verify against the original.
func corrupt(buf []byte, b byte) []byte {
	out := bytes.Clone(buf)
	last := out[len(out)-1]
	if last == b {
		b = ^b
	}
	out[len(out)-1] = b
	return out
}

func v1PieceWith(buf []byte, b byte) piece.Piece { return v1Piece(corrupt(buf, b)) }

func v2PieceWith(buf []byte, b byte) piece.Piece { return v2Piece(corrupt(buf, b)) }

// benchmarkRun hashes and writes one piece per iteration.
func benchmarkRun(b *testing.B, p *piece.Piece, buf []byte) {
	b.SetBytes(int64(len(buf)))
	p.Data = filesection.Piece{{File: &discardFile{}, Length: int64(len(buf))}}
	pool := bufferpool.New(len(buf))
	borrowed := pool.Get(len(buf))
	copy(borrowed.Data, buf)
	sem := semaphore.New(1)
	closeC := make(chan struct{})
	resultC := make(chan *PieceWriter, 1)
	meters, meterB := metrics.NewMeter(), metrics.NewMeter()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := New(p, nil, borrowed)
		w.Run(resultC, closeC, meters, meterB, sem)
		<-resultC
		if !w.HashOK || w.Error != nil {
			b.Fatalf("piece did not verify: hash ok %v, err %v", w.HashOK, w.Error)
		}
	}
	b.StopTimer()
	borrowed.Release()
}

// TestRunWritesOnlyVerifiedPieces is the invariant the pipeline rests on: a
// corrupt block batch never reaches the disk, for either layout, and the buffer
// is still handed back through the result channel so the caller can release it.
func TestRunWritesOnlyVerifiedPieces(t *testing.T) {
	buf := make([]byte, 2*piece.BlockSize)
	for i := range buf {
		buf[i] = byte(i)
	}
	cases := []struct {
		name  string
		piece piece.Piece
		good  bool
	}{
		{name: "v1-good", piece: v1Piece(buf), good: true},
		{name: "v1-corrupt", piece: v1PieceWith(buf, byte(0xff)), good: false},
		{name: "v2-good", piece: v2Piece(buf), good: true},
		{name: "v2-corrupt", piece: v2PieceWith(buf, byte(0xff)), good: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := &discardFile{}
			p := tc.piece
			p.Data = filesection.Piece{{File: file, Length: int64(len(buf))}}
			pool := bufferpool.New(len(buf))
			borrowed := pool.Get(len(buf))
			defer borrowed.Release()
			copy(borrowed.Data, buf)

			resultC := make(chan *PieceWriter, 1)
			closeC := make(chan struct{})
			w := New(&p, nil, borrowed)
			w.Run(resultC, closeC, metrics.NewMeter(), metrics.NewMeter(), semaphore.New(1))

			select {
			case got := <-resultC:
				if got != w {
					t.Fatal("the writer did not report itself")
				}
			default:
				t.Fatal("Run did not report a result")
			}
			if w.HashOK != tc.good {
				t.Fatalf("HashOK = %v, want %v", w.HashOK, tc.good)
			}
			if tc.good {
				if file.written != len(buf) {
					t.Fatalf("wrote %d bytes, want %d", file.written, len(buf))
				}
			} else if file.written != 0 {
				t.Fatalf("a corrupt piece wrote %d bytes to storage", file.written)
			}
		})
	}
}

func BenchmarkPieceWriterRunV1(b *testing.B) {
	buf := make([]byte, benchPieceLength)
	for i := range buf {
		buf[i] = byte(i)
	}
	p := v1Piece(buf)
	benchmarkRun(b, &p, buf)
}

func BenchmarkPieceWriterRunV2(b *testing.B) {
	buf := make([]byte, benchPieceLength)
	for i := range buf {
		buf[i] = byte(i)
	}
	p := v2Piece(buf)
	benchmarkRun(b, &p, buf)
}
