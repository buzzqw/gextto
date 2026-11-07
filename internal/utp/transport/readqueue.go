package transport

import "sync"

// readChunkSize is the unit the received bytes of a connection are stored in.
// A chunk holds about 46 full datagrams; a drained chunk goes back to a pool
// shared by every connection, so an idle connection holds no read memory and a
// busy one holds only the chunks its unread bytes need.
const readChunkSize = 64 << 10

var readChunkPool = sync.Pool{New: func() any {
	b := make([]byte, readChunkSize)
	return &b
}}

// readQueue is the bytes received in order and not yet read by the
// application. It is a list of fixed-size chunks: appending never reallocates
// or moves what is already buffered, and reading returns each drained chunk to
// readChunkPool. A single growing slice, the previous form, was dropped after
// every burst and grown again by doubling, which made a uTP download allocate
// more garbage than the data it carried.
type readQueue struct {
	chunks []*[]byte
	head   int // read offset in chunks[0]
	tail   int // write offset in the last chunk
	n      int // buffered bytes
}

// Len is how many bytes wait to be read.
func (q *readQueue) Len() int { return q.n }

// Write appends p.
func (q *readQueue) Write(p []byte) {
	for len(p) > 0 {
		if len(q.chunks) == 0 || q.tail == readChunkSize {
			q.chunks = append(q.chunks, readChunkPool.Get().(*[]byte))
			q.tail = 0
		}
		last := *q.chunks[len(q.chunks)-1]
		m := copy(last[q.tail:], p)
		q.tail += m
		q.n += m
		p = p[m:]
	}
}

// Read moves up to len(b) buffered bytes into b and returns how many.
func (q *readQueue) Read(b []byte) int {
	read := 0
	for len(b) > 0 && q.n > 0 {
		first := *q.chunks[0]
		end := readChunkSize
		if len(q.chunks) == 1 {
			end = q.tail
		}
		m := copy(b, first[q.head:end])
		q.head += m
		q.n -= m
		read += m
		b = b[m:]
		if q.head == end {
			q.dropFirst()
		}
	}
	return read
}

// dropFirst returns the drained first chunk to the pool.
func (q *readQueue) dropFirst() {
	readChunkPool.Put(q.chunks[0])
	// Shift instead of reslicing, so the list keeps its array: a window of at
	// most readBufferLen holds 17 chunks, and a reslice would creep through
	// the array and reallocate it.
	last := len(q.chunks) - 1
	copy(q.chunks, q.chunks[1:])
	q.chunks[last] = nil
	q.chunks = q.chunks[:last]
	q.head = 0
	if last == 0 {
		q.tail = 0
	}
}

// Bytes copies the buffered bytes out, without consuming them (tests).
func (q *readQueue) Bytes() []byte {
	out := make([]byte, 0, q.n)
	for i, c := range q.chunks {
		start, end := 0, readChunkSize
		if i == 0 {
			start = q.head
		}
		if i == len(q.chunks)-1 {
			end = q.tail
		}
		out = append(out, (*c)[start:end]...)
	}
	return out
}

// appendFIFO appends v to q, a queue its owner consumes from the front by
// reslicing (q = q[1:]); base is the whole array behind q. Reslicing makes the
// window creep towards the end of the array, so a plain append reallocated it
// over and over while the queue stayed short: about one allocation per packet
// on a busy connection. Here a window that reached the end of base moves back
// to the front of it first, and base follows a real reallocation.
func appendFIFO[T any](base *[]T, q []T, v T) []T {
	if len(q) == cap(q) && cap(q) < cap(*base) {
		full := (*base)[:cap(*base)]
		n := copy(full, q)
		clear(full[n:]) // drop the moved copies, so nothing stays reachable twice
		q = full[:n]
	}
	q = append(q, v)
	if cap(q) > cap(*base) {
		*base = q[:0]
	}
	return q
}
