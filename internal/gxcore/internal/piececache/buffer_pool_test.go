package piececache

// buffer_pool_test.go covers the block buffers the cache recycles: a block
// that leaves the cache gives its memory to the next read, and a block a
// reader is still copying out of does not.

import (
	"errors"
	"testing"
	"time"
)

var errTestRead = errors.New("read failed")

const poolBlockSize = 16 << 10

// TestCacheRecyclesTheBufferOfAnEvictedBlock: the second block is read into
// the memory of the first one, which the cache had to evict.
func TestCacheRecyclesTheBufferOfAnEvictedBlock(t *testing.T) {
	c := New(poolBlockSize, time.Minute, 4)
	defer c.Close()

	var buffers [][]byte
	loadInto := func(target *[]byte) Loader {
		return func() ([]byte, error) {
			buf := c.TakeBuffer(poolBlockSize)
			*target = buf
			return buf, nil
		}
	}
	// Each block evicts the previous one after its own buffer is taken, so the
	// reuse shows from the third read on: the steady state of an upload.
	for _, key := range []string{"a", "b", "c"} {
		buffers = append(buffers, nil)
		if _, err := c.Get(key, loadInto(&buffers[len(buffers)-1])); err != nil {
			t.Fatal(err)
		}
	}
	if buffers[2] == nil || &buffers[0][0] != &buffers[2][0] {
		t.Fatal("the third block was allocated instead of reusing the buffer of the evicted first one")
	}
}

// TestPinnedBlockIsNotRecycledUnderItsReader: an upload copying out of a
// block keeps its memory even when another request evicts the item, and the
// memory goes back to the pool only after the copy.
func TestPinnedBlockIsNotRecycledUnderItsReader(t *testing.T) {
	c := New(poolBlockSize, time.Minute, 4)
	defer c.Close()

	block, release, err := c.GetPinned("a", func() ([]byte, error) {
		buf := c.TakeBuffer(poolBlockSize)
		copy(buf, "payload")
		return buf, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("b", func() ([]byte, error) { return c.TakeBuffer(poolBlockSize), nil }); err != nil {
		t.Fatal(err)
	}
	if got := string(block[:7]); got != "payload" {
		t.Fatalf("the block was recycled under its reader: %q", got)
	}
	release()
	reused := c.TakeBuffer(poolBlockSize)
	if &reused[0] != &block[0] {
		t.Fatal("the released buffer did not go back to the pool")
	}
}

// TestTakeBufferKeepsSizesApart: the short last block of a piece must not
// come back as a full block, or the loader would read past its end.
func TestTakeBufferKeepsSizesApart(t *testing.T) {
	c := New(1<<20, time.Minute, 1)
	defer c.Close()

	block := c.TakeBuffer(poolBlockSize)
	if len(block) != poolBlockSize {
		t.Fatalf("buffer length = %d, want %d", len(block), poolBlockSize)
	}
	c.buffers.put(block)
	if got := c.TakeBuffer(poolBlockSize - 1); len(got) != poolBlockSize-1 {
		t.Fatalf("short buffer length = %d, want %d", len(got), poolBlockSize-1)
	} else if &got[0] == &block[0] {
		t.Fatal("a full block was handed out for a shorter read")
	}
	if got := c.TakeBuffer(poolBlockSize); &got[0] != &block[0] {
		t.Fatal("the pooled block was not reused for a read of its own size")
	}
}

// TestFailedLoadRecyclesItsBuffer: a read that fails must not lose the buffer
// it was reading into.
func TestFailedLoadRecyclesItsBuffer(t *testing.T) {
	c := New(poolBlockSize, time.Minute, 1)
	defer c.Close()

	var buf []byte
	if _, err := c.Get("a", func() ([]byte, error) {
		buf = c.TakeBuffer(poolBlockSize)
		return buf, errTestRead
	}); err == nil {
		t.Fatal("a failed load was served as a value")
	}
	if got := c.TakeBuffer(poolBlockSize); &got[0] != &buf[0] {
		t.Fatal("the buffer of a failed load did not go back to the pool")
	}
	if c.Len() != 0 {
		t.Fatalf("a failed load left %d items in the cache", c.Len())
	}
}

// TestClearRecyclesEverything: resizing or dropping the cache gives its
// memory back instead of leaving it to the collector.
func TestClearRecyclesEverything(t *testing.T) {
	c := New(1<<20, time.Minute, 1)
	defer c.Close()

	var buf []byte
	if _, err := c.Get("a", func() ([]byte, error) {
		buf = c.TakeBuffer(poolBlockSize)
		return buf, nil
	}); err != nil {
		t.Fatal(err)
	}
	c.Clear()
	if got := c.TakeBuffer(poolBlockSize); &got[0] != &buf[0] {
		t.Fatal("Clear left the block buffers to the collector")
	}
}
