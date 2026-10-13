package piecedownloader

import (
	"errors"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

var (
	// ErrBlockDuplicate rejects an already-received block.
	ErrBlockDuplicate = errors.New("received duplicate block")
	// ErrBlockNotRequested flags data that arrived without a request. The
	// bytes are still kept; the error only notifies the caller.
	ErrBlockNotRequested = errors.New("received not requested block")
	// ErrBlockInvalid rejects blocks outside the piece's block map.
	ErrBlockInvalid = errors.New("received block is invalid")
)

// PieceDownloader pulls every block of one piece from one peer into a
// buffer. Padding ranges never travel: they are absent from the block map.
type PieceDownloader struct {
	Piece       *piece.Piece
	Peer        Peer
	AllowedFast bool
	Buffer      bufferpool.Buffer

	blocks    map[uint32]uint32   // begin -> length
	remaining []uint32            // unrequested begins, in piece order
	pending   map[uint32]struct{} // in-flight begins
	done      map[uint32]struct{} // received begins
}

// Peer abstracts the connection a piece is fetched from.
type Peer interface {
	RequestPiece(index, begin, length uint32)
	CancelPiece(index, begin, length uint32)
	EnabledFast() bool
}

// New plans the block map of pi (padding excluded) for pe.
func New(pi *piece.Piece, pe Peer, allowedFast bool, buf bufferpool.Buffer) *PieceDownloader {
	blocks := pi.CalculateBlocks()
	byBegin := make(map[uint32]uint32, len(blocks))
	order := make([]uint32, len(blocks))
	for i, blk := range blocks {
		byBegin[blk.Begin] = blk.Length
		order[i] = blk.Begin
	}
	return &PieceDownloader{
		Piece:       pi,
		Peer:        pe,
		AllowedFast: allowedFast,
		Buffer:      buf,
		blocks:      byBegin,
		remaining:   order,
		pending:     make(map[uint32]struct{}, len(blocks)),
		done:        make(map[uint32]struct{}, len(blocks)),
	}
}

// Choked requeues the in-flight requests — unless allowed-fast applies or
// the peer speaks fast (it will cancel for us).
func (d *PieceDownloader) Choked() {
	if d.AllowedFast || d.Peer.EnabledFast() {
		return
	}
	for begin := range d.pending {
		delete(d.pending, begin)
		d.remaining = append(d.remaining, begin)
	}
}

// GotBlock stores one received block, validating map membership, size,
// duplication and solicitation in that order.
func (d *PieceDownloader) GotBlock(begin uint32, data []byte) error {
	length, ok := d.blocks[begin]
	if !ok || length != uint32(len(data)) {
		return ErrBlockInvalid
	}
	if _, ok := d.done[begin]; ok {
		return ErrBlockDuplicate
	}
	copy(d.Buffer.Data[begin:begin+uint32(len(data))], data)
	d.done[begin] = struct{}{}
	if _, ok := d.pending[begin]; !ok {
		return ErrBlockNotRequested
	}
	delete(d.pending, begin)
	return nil
}

// Prefill marks the block at begin as already present, copying data into the
// piece buffer: it is never requested from the peer. It is for blocks a local
// source already holds (swarm merge at block granularity); the whole piece is
// still verified against its hash once complete. It must be called before the
// first RequestBlocks and reports whether the block was taken.
func (d *PieceDownloader) Prefill(begin uint32, data []byte) bool {
	if l, ok := d.blocks[begin]; !ok || l != uint32(len(data)) {
		return false
	}
	if _, ok := d.done[begin]; ok {
		return false
	}
	if _, ok := d.pending[begin]; ok {
		return false
	}
	copy(d.Buffer.Data[begin:begin+uint32(len(data))], data)
	d.done[begin] = struct{}{}
	for i, b := range d.remaining {
		if b == begin {
			d.remaining = append(d.remaining[:i], d.remaining[i+1:]...)
			break
		}
	}
	return true
}

// Rejected requeues one refused request, reporting whether it was ours.
func (d *PieceDownloader) Rejected(begin, length uint32) bool {
	if l, ok := d.blocks[begin]; !ok || l != length {
		return false
	}
	delete(d.pending, begin)
	d.remaining = append(d.remaining, begin)
	return true
}

// CancelPending withdraws every in-flight request. Call it when the
// remaining blocks move to another peer.
func (d *PieceDownloader) CancelPending() {
	for begin := range d.pending {
		length, ok := d.blocks[begin]
		if !ok {
			panic("cannot get block")
		}
		d.Peer.CancelPiece(d.Piece.Index, begin, length)
	}
}

// RequestBlocks issues up to queueLength new requests in piece order,
// skipping already-received blocks, and returns how many went out — so the
// caller can share one in-flight budget across a peer's pieces.
func (d *PieceDownloader) RequestBlocks(queueLength int) int {
	requested := 0
	for _, begin := range d.remaining {
		if len(d.pending) >= queueLength {
			break
		}
		length, ok := d.blocks[begin]
		if !ok {
			panic("cannot get block")
		}
		if _, ok := d.done[begin]; !ok {
			d.Peer.RequestPiece(d.Piece.Index, begin, length)
		}
		d.remaining = d.remaining[1:]
		d.pending[begin] = struct{}{}
		requested++
	}
	return requested
}

// Pending counts the in-flight requests.
func (d *PieceDownloader) Pending() int { return len(d.pending) }

// Remaining counts the never-requested blocks.
func (d *PieceDownloader) Remaining() int { return len(d.remaining) }

// Done reports a fully received piece.
func (d *PieceDownloader) Done() bool {
	return len(d.done) == len(d.blocks)
}
