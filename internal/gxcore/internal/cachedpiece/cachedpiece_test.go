package cachedpiece

import (
	"bytes"
	"crypto/rand"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/filesection"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piececache"
)

// countedFile counts the reads that reach storage.
type countedFile struct {
	data  []byte
	reads atomic.Int32
}

func (f *countedFile) ReadAt(p []byte, off int64) (int, error) {
	f.reads.Add(1)
	return copy(p, f.data[off:]), nil
}

func (f *countedFile) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }

func newPiece(t *testing.T, size int) (*piece.Piece, *countedFile, *piececache.Cache) {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	f := &countedFile{data: data}
	cache := piececache.New(1<<20, time.Minute, 1)
	t.Cleanup(cache.Close)
	return &piece.Piece{
		Length: uint32(size),
		Data:   filesection.Piece{{File: f, Offset: 0, Length: int64(size)}},
	}, f, cache
}

func waitReads(t *testing.T, f *countedFile, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.reads.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("%d reads, want %d", f.reads.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestPrefetchLoadsTheNextBlockOnce: starting a block loads the next one in the
// background, and serving that block later does not read the disk again.
func TestPrefetchLoadsTheNextBlockOnce(t *testing.T) {
	const block = 4096
	pi, f, cache := newPiece(t, 3*block)
	c := New(pi, cache, block, [20]byte{1}, true)

	got := make([]byte, block)
	if _, err := c.ReadAt(got, 0); err != nil || !bytes.Equal(got, f.data[:block]) {
		t.Fatalf("block 0: %v", err)
	}
	waitReads(t, f, 2)
	if _, err := c.ReadAt(got, block); err != nil || !bytes.Equal(got, f.data[block:2*block]) {
		t.Fatalf("block 1: %v", err)
	}
	waitReads(t, f, 3)
	if _, err := c.ReadAt(got, 2*block); err != nil || !bytes.Equal(got, f.data[2*block:]) {
		t.Fatalf("block 2: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := f.reads.Load(); n != 3 {
		t.Fatalf("%d disk reads for 3 blocks", n)
	}
}

// TestPrefetchStopsAtThePieceEnd: no load past the last block, and a read inside
// a block does not start one.
func TestPrefetchStopsAtThePieceEnd(t *testing.T) {
	const block = 4096
	pi, f, cache := newPiece(t, block)
	c := New(pi, cache, block, [20]byte{1}, true)
	got := make([]byte, 16)
	if _, err := c.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadAt(got, 100); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := f.reads.Load(); n != 1 {
		t.Fatalf("%d disk reads for a one-block piece", n)
	}
}

// TestPrefetchOff: a mapped file reads only what the request needs.
func TestPrefetchOff(t *testing.T) {
	const block = 4096
	pi, f, cache := newPiece(t, 2*block)
	c := New(pi, cache, block, [20]byte{1}, false)
	if _, err := c.ReadAt(make([]byte, 16), 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := f.reads.Load(); n != 1 {
		t.Fatalf("%d disk reads with prefetch off", n)
	}
}
