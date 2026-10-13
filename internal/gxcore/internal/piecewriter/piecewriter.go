package piecewriter

import (
	"crypto/sha1"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/semaphore"
	"github.com/rcrowley/go-metrics"
)

// PieceWriter verifies one downloaded piece buffer and persists it. HashOK
// and Error report the outcome on resultC; the buffer itself is owned by the
// caller.
type PieceWriter struct {
	Piece  *piece.Piece
	Source any
	Buffer bufferpool.Buffer

	HashOK bool
	Error  error
}

// New binds a piece and its downloaded bytes to a writer.
func New(p *piece.Piece, source any, buf bufferpool.Buffer) *PieceWriter {
	return &PieceWriter{
		Piece:  p,
		Source: source,
		Buffer: buf,
	}
}

// Run hashes the buffer (SHA-1 for v1, Merkle for v2) and, on a match,
// writes it to storage under the write-slot semaphore. Meters and result
// delivery mirror the surrounding pipeline's accounting.
func (w *PieceWriter) Run(resultC chan *PieceWriter, closeC chan struct{}, writesPerSecond, writeBytesPerSecond metrics.Meter, sem *semaphore.Semaphore) {
	w.HashOK = w.Piece.VerifyHash(w.Buffer.Data, sha1.New())
	if w.Piece.V2 {
		w.HashOK = w.Piece.VerifyV2(w.Buffer.Data)
	}
	if w.HashOK {
		writesPerSecond.Mark(1)
		writeBytesPerSecond.Mark(int64(len(w.Buffer.Data)))
		sem.Wait()
		_, w.Error = w.Piece.Data.Write(w.Buffer.Data)
		sem.Signal()
	}
	select {
	case resultC <- w:
	case <-closeC:
	}
}
