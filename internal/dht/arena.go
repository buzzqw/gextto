package dht

// arena is a channel-backed free list of fixed-size scratch buffers for
// datagram IO: popping recycles without allocating, pushing restores the
// full capacity first. Popped slices are NOT zeroed — callers must only read
// what they wrote (shorten to the Write count).
type arena chan []byte

// newArena pre-fills numBlocks buffers of blockSize bytes.
func newArena(blockSize int, numBlocks int) arena {
	blocks := make(arena, numBlocks)
	for i := 0; i < numBlocks; i++ {
		blocks <- make([]byte, blockSize)
	}
	return blocks
}

// Pop borrows a buffer, blocking while all are checked out.
func (a arena) Pop() (x []byte) {
	return <-a
}

// Push returns a buffer, re-extended to its full capacity.
func (a arena) Push(x []byte) {
	x = x[:cap(x)]
	a <- x
}
