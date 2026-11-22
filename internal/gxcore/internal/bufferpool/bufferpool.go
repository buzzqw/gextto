package bufferpool

// Pool recycles fixed-size scratch buffers (protocol reads) through a bounded
// free list. Borrowed memory travels inside a Buffer, which knows how to hand
// itself back, so callers cannot forget the pool it came from. The list is
// bounded: a burst of connections can no longer leave an unbounded number of
// buffers reachable, which is what the heap of a shaped-line download showed.
type Pool struct {
	buflen int
	free   chan *[]byte
}

// defaultMaxRetained is the number of buffers the pool keeps at once. The
// block pool uses small buffers, so this is about a megabyte of retained
// memory; the piece pool passes its own, smaller cap.
const defaultMaxRetained = 64

// New builds a pool of buflen-byte backing arrays with the default cap.
func New(buflen int) *Pool {
	return NewBounded(buflen, defaultMaxRetained)
}

// NewBounded builds a pool that keeps at most maxRetained buffers. A buffer
// released past the cap is left to the collector instead of being retained.
func NewBounded(buflen, maxRetained int) *Pool {
	if maxRetained < 1 {
		maxRetained = 1
	}
	return &Pool{buflen: buflen, free: make(chan *[]byte, maxRetained)}
}

// Retained reports how many buffers are currently held for reuse.
func (p *Pool) Retained() int { return len(p.free) }

// TrimTo drops free buffers down to keep, returning how many were released to
// the collector. A caller with a quiet torrent calls this after a write so the
// memory of a burst does not stay reachable until the next collection.
func (p *Pool) TrimTo(keep int) int {
	dropped := 0
	for len(p.free) > keep {
		select {
		case <-p.free:
			dropped++
		default:
			return dropped
		}
	}
	return dropped
}

// get borrows a backing array, allocating one only when the free list is empty.
func (p *Pool) get() *[]byte {
	select {
	case b := <-p.free:
		return b
	default:
		b := make([]byte, p.buflen)
		return &b
	}
}

// Get borrows a Buffer exposing datalen bytes. Release it when done.
// datalen must not exceed the pool's buflen.
func (p *Pool) Get(datalen int) Buffer {
	backing := p.get()
	b := Buffer{Data: (*backing)[:datalen], buf: backing, pool: p}
	clear(b.Data)
	return b
}

// GetUncleared is Get without zeroing Data. It is only for a caller that
// overwrites every byte of Data before anything reads it — a block read with
// io.ReadFull, a piece whose blocks cover it whole — and saves a clear of the
// buffer per block and per piece on the download path.
func (p *Pool) GetUncleared(datalen int) Buffer {
	backing := p.get()
	return Buffer{Data: (*backing)[:datalen], buf: backing, pool: p}
}

// Buffer is a pooled byte slice. Data is the working window; buf is the
// full backing array returned to the pool on Release.
type Buffer struct {
	Data []byte
	buf  *[]byte
	pool *Pool
}

// Release hands the backing array back to the pool, unless the cap is full:
// in that case the buffer is left to the collector instead of being retained.
func (b Buffer) Release() {
	select {
	case b.pool.free <- b.buf:
	default:
	}
}
