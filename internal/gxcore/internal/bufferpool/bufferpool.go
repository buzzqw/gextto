package bufferpool

import "sync"

// Pool recycles fixed-size scratch buffers (protocol reads) through a
// sync.Pool. Borrowed memory travels inside a Buffer, which knows how to
// hand itself back, so callers cannot forget the pool it came from.
type Pool struct {
	pool sync.Pool
}

// New builds a pool of buflen-byte backing arrays.
func New(buflen int) *Pool {
	return &Pool{
		pool: sync.Pool{
			New: func() any {
				b := make([]byte, buflen)
				return &b
			},
		},
	}
}

// Get borrows a Buffer exposing datalen bytes. Release it when done.
// datalen must not exceed the pool's buflen.
func (p *Pool) Get(datalen int) Buffer {
	backing := p.pool.Get().(*[]byte)
	b := Buffer{Data: (*backing)[:datalen], buf: backing, pool: p}
	clear(b.Data)
	return b
}

// Buffer is a pooled byte slice. Data is the working window; buf is the
// full backing array returned to the pool on Release.
type Buffer struct {
	Data []byte
	buf  *[]byte
	pool *Pool
}

// Release hands the backing array back to the pool.
func (b Buffer) Release() {
	// Put takes the pointer, not the slice header, to avoid an allocation.
	b.pool.pool.Put(b.buf)
}
