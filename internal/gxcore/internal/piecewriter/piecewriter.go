package piecewriter

import (
	"context"

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
	w.run(context.Background(), writesPerSecond, writeBytesPerSecond, sem)
	select {
	case resultC <- w:
	case <-closeC:
	}
}

// RunContext performs the hash and write without delivering a result. The
// torrent owns delivery so it can release the buffer itself when a stop wins.
// Cancellation prevents queued writes from starting; a write already handed
// to storage is allowed to finish before its file is closed.
func (w *PieceWriter) RunContext(ctx context.Context, writesPerSecond, writeBytesPerSecond metrics.Meter, sem *semaphore.Semaphore) {
	w.run(ctx, writesPerSecond, writeBytesPerSecond, sem)
}

func (w *PieceWriter) run(ctx context.Context, writesPerSecond, writeBytesPerSecond metrics.Meter, sem *semaphore.Semaphore) {
	if ctx.Err() != nil {
		w.Error = ctx.Err()
		return
	}
	w.HashOK = w.Piece.Verify(w.Buffer.Data)
	if ctx.Err() != nil {
		w.Error = ctx.Err()
		return
	}
	if w.HashOK {
		writesPerSecond.Mark(1)
		writeBytesPerSecond.Mark(int64(len(w.Buffer.Data)))
		if sem != nil {
			if err := sem.WaitContext(ctx); err != nil {
				w.Error = err
				return
			}
			defer sem.Signal()
		}
		// The slot may have been granted at the same instant as the stop.
		if err := ctx.Err(); err != nil {
			w.Error = err
			return
		}
		_, w.Error = w.Piece.Data.Write(w.Buffer.Data)
	}
}
