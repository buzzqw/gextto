package piecewriter

import (
	"crypto/sha1"
	"hash"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/semaphore"
	"github.com/rcrowley/go-metrics"
)

// PieceWriter writes the data in the buffer to disk.
type PieceWriter struct {
	Piece  *piece.Piece
	Source any
	Buffer bufferpool.Buffer

	HashOK bool
	Error  error

	newHash func() hash.Hash
}

// New returns new PieceWriter for a given piece. newHash is the piece hash
// constructor (SHA-1 by default, SHA-256 for v2); nil means SHA-1.
func New(p *piece.Piece, source any, buf bufferpool.Buffer, newHash func() hash.Hash) *PieceWriter {
	if newHash == nil {
		newHash = sha1.New
	}
	return &PieceWriter{
		Piece:   p,
		Source:  source,
		Buffer:  buf,
		newHash: newHash,
	}
}

// Run checks the hash, then writes the data in the buffer to the disk.
func (w *PieceWriter) Run(resultC chan *PieceWriter, closeC chan struct{}, writesPerSecond, writeBytesPerSecond metrics.Meter, sem *semaphore.Semaphore) {
	w.HashOK = w.Piece.VerifyHash(w.Buffer.Data, w.newHash())
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
