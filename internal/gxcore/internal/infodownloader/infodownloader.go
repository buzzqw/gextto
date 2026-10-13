package infodownloader

import "fmt"

// blockSize is the BEP 9 metadata block size: the info dict travels in
// 16 KiB extension-protocol pieces.
const blockSize = 16 * 1024

// InfoDownloader fetches a torrent's metadata (BEP 9) from one peer,
// block by block, reassembling it into Bytes.
type InfoDownloader struct {
	Peer  Peer
	Bytes []byte

	blocks         []block
	pending        int // requested but not yet received
	nextBlockIndex uint32
}

// block tracks one metadata slice through request and delivery.
type block struct {
	size      uint32
	requested bool
	received  bool
}

// Peer abstracts the single connection metadata is pulled from.
type Peer interface {
	MetadataSize() uint32
	RequestMetadataPiece(index uint32)
}

// New sizes the reassembly buffer from the peer's advertised metadata size
// and plans the block map, last block possibly short.
func New(pe Peer) *InfoDownloader {
	d := &InfoDownloader{
		Peer:  pe,
		Bytes: make([]byte, pe.MetadataSize()),
	}
	total, rem := pe.MetadataSize()/blockSize, pe.MetadataSize()%blockSize
	d.blocks = make([]block, total)
	for i := range d.blocks {
		d.blocks[i].size = blockSize
	}
	if rem != 0 {
		d.blocks = append(d.blocks, block{size: rem})
	}
	return d
}

// GotBlock stores one delivered block. Out-of-range, unsolicited or
// wrong-sized blocks are peer misbehavior and rejected; a duplicate delivery
// is acknowledged but counted once, so Done cannot trip early.
func (d *InfoDownloader) GotBlock(index uint32, data []byte) error {
	if index >= uint32(len(d.blocks)) {
		return fmt.Errorf("peer sent invalid metadata piece index: %d", index)
	}
	b := &d.blocks[index]
	if !b.requested {
		return fmt.Errorf("peer sent unrequested index for metadata message: %d", index)
	}
	if uint32(len(data)) != b.size {
		return fmt.Errorf("peer sent invalid size for metadata message: %d", len(data))
	}
	if b.received {
		return nil
	}
	b.received = true
	d.pending--
	copy(d.Bytes[index*blockSize:], data)
	return nil
}

// RequestBlocks tops the in-flight window up to queueLength, in order.
func (d *InfoDownloader) RequestBlocks(queueLength int) {
	for ; d.nextBlockIndex < uint32(len(d.blocks)) && d.pending < queueLength; d.nextBlockIndex++ {
		d.Peer.RequestMetadataPiece(d.nextBlockIndex)
		d.blocks[d.nextBlockIndex].requested = true
		d.pending++
	}
}

// Done reports whether every block was requested and received.
func (d *InfoDownloader) Done() bool {
	return d.nextBlockIndex == uint32(len(d.blocks)) && d.pending == 0
}
