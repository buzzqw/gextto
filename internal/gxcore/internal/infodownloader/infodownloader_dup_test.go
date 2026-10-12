package infodownloader

import "testing"

type fakePeer struct {
	size      uint32
	requested []uint32
}

func (p *fakePeer) MetadataSize() uint32              { return p.size }
func (p *fakePeer) RequestMetadataPiece(index uint32) { p.requested = append(p.requested, index) }

// TestGotBlockIgnoresDuplicates: a block sent twice must not make the download
// look complete while another block is still missing.
func TestGotBlockIgnoresDuplicates(t *testing.T) {
	pe := &fakePeer{size: 2 * blockSize}
	d := New(pe)
	d.RequestBlocks(4)
	block := make([]byte, blockSize)
	if err := d.GotBlock(0, block); err != nil {
		t.Fatal(err)
	}
	if err := d.GotBlock(0, block); err != nil {
		t.Fatal(err)
	}
	if d.Done() {
		t.Fatal("Done() after a duplicate block while block 1 is missing")
	}
	if err := d.GotBlock(1, block); err != nil {
		t.Fatal(err)
	}
	if !d.Done() {
		t.Fatal("not done after both blocks")
	}
}
