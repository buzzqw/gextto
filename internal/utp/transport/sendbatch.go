package transport

// sendbatch.go collects the datagrams of one burst — the packets a single
// Conn.Write produces — and hands them to the socket together. On Linux the
// kernel splits the buffer into segments with UDP GSO, so a burst of a dozen
// packets costs one syscall instead of a dozen; everywhere else, or when the
// batch does not have the shape GSO needs, the packets go out one by one
// exactly as before. The profile that motivated this: 50 % of the CPU in
// sendto/recvfrom on a loopback transfer (docs/measurements.md).

import (
	"net"
	"sync/atomic"
)

// maxBatchSegments caps one batch: Linux accepts at most 64 UDP GSO segments,
// and a smaller cap keeps the extra copy and the memory bounded. A full batch
// is flushed at once, so the order is never disturbed.
const maxBatchSegments = 32

// gsoDisabled lets a test pin the one-by-one path on a platform that has GSO,
// so both branches stay covered.
var gsoDisabled atomic.Bool

// sendBatch is one burst of datagrams for a single destination.
type sendBatch struct {
	data  []byte
	sizes []int
}

// reset empties the batch but keeps its buffers, so a connection reuses them
// for every write instead of allocating a burst buffer each time.
func (b *sendBatch) reset() {
	b.data = b.data[:0]
	b.sizes = b.sizes[:0]
}

// add copies one packet into the batch, flushing a full batch first to keep
// the packet order.
func (b *sendBatch) add(s *Socket, addr net.Addr, p []byte) error {
	if len(b.sizes) >= maxBatchSegments {
		if err := b.flush(s, addr); err != nil {
			return err
		}
	}
	b.data = append(b.data, p...)
	b.sizes = append(b.sizes, len(p))
	return nil
}

// flush writes every collected packet and empties the batch. One packet takes
// the normal path; a burst tries one segmented write and falls back to one
// write per packet when the kernel or the batch does not allow it.
func (b *sendBatch) flush(s *Socket, addr net.Addr) error {
	if len(b.sizes) == 0 {
		return nil
	}
	data, sizes := b.data, b.sizes
	b.data = b.data[:0]
	b.sizes = b.sizes[:0]
	if len(sizes) == 1 {
		_, err := s.writeTo(data[:sizes[0]], addr)
		return err
	}
	if !gsoDisabled.Load() && s.gsoSend(data, sizes, addr) {
		return nil
	}
	offset := 0
	for _, size := range sizes {
		if _, err := s.writeTo(data[offset:offset+size], addr); err != nil {
			return err
		}
		offset += size
	}
	return nil
}
