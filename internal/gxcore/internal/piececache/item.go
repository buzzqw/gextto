package piececache

import (
	"sync"
	"sync/atomic"
	"time"
)

// item is one cached block: key, payload, load outcome, and the metadata the
// eviction heap orders by. expireAt implements lazy TTL — the sweeper reaps
// expired items periodically instead of arming a timer per block. All fields
// except the embedded mutex are guarded by Cache.m.
type item struct {
	key          string
	value        []byte
	loaded       bool
	err          error
	lastAccessed time.Time
	expireAt     time.Time
	index        int
	// inUse counts the readers that hold this block: a reader copies out of
	// the cached bytes after Get returned, and the buffer must not go back to
	// the pool under a copy. retired says the item left the cache, and
	// recycled says its buffer already went back (the two are raced on
	// purpose: only one of the paths may recycle it).
	inUse    atomic.Int32
	retired  atomic.Bool
	recycled atomic.Bool
	sync.Mutex
}

// pin marks one reader. It runs with the cache lock held, so an item cannot
// leave the cache between the lookup and the pin.
func (i *item) pin() {
	i.inUse.Add(1)
}

// unpin releases one reader; the last reader of an item that already left the
// cache gives its buffer back.
func (i *item) unpin(pool *bufferPool) {
	if i.inUse.Add(-1) != 0 {
		return
	}
	if i.retired.Load() {
		i.recycle(pool)
	}
}

// retire marks the item as gone from the cache: its buffer returns to the
// pool as soon as no reader is copying out of it.
func (i *item) retire(pool *bufferPool) {
	i.retired.Store(true)
	if i.inUse.Load() == 0 {
		i.recycle(pool)
	}
}

// recycle hands the buffer back exactly once, whichever path arrives last.
func (i *item) recycle(pool *bufferPool) {
	if !i.recycled.CompareAndSwap(false, true) {
		return
	}
	if len(i.value) > 0 {
		pool.put(i.value)
		i.value = nil
	}
}

// accessList is the LRU heap of items, ordered by last access.
type accessList []*item

func (l accessList) Len() int { return len(l) }

func (l accessList) Less(i, j int) bool {
	return l[i].lastAccessed.Before(l[j].lastAccessed)
}

func (l accessList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
	l[i].index = i
	l[j].index = j
}

func (l *accessList) Push(x any) {
	i := x.(*item)
	i.index = len(*l)
	*l = append(*l, i)
}

func (l *accessList) Pop() any {
	old := *l
	i := old[len(old)-1]
	i.index = -1 // for safety
	*l = old[:len(old)-1]
	return i
}

// bufferPool keeps the block buffers the cache no longer owns. A block read
// for an upload then loads into memory that is already there, instead of
// allocating it and clearing it first, which the collector pays for twice.
type bufferPool struct {
	mu   sync.Mutex
	free map[int][][]byte
	kept int
	max  int
}

// newBufferPool sizes the pool by block count; a handful of buffers is enough
// to cover the evictions between two loads.
func newBufferPool(max int) *bufferPool {
	if max < 8 {
		max = 8
	}
	return &bufferPool{free: make(map[int][][]byte), max: max}
}

// get returns a buffer of exactly size bytes, a recycled one when available.
func (p *bufferPool) get(size int) []byte {
	if p == nil || size <= 0 {
		return make([]byte, size)
	}
	p.mu.Lock()
	list := p.free[size]
	if len(list) == 0 {
		p.mu.Unlock()
		return make([]byte, size)
	}
	buf := list[len(list)-1]
	p.free[size] = list[:len(list)-1]
	p.kept--
	p.mu.Unlock()
	return buf
}

// put files one buffer; a full pool lets it go to the collector.
func (p *bufferPool) put(buf []byte) {
	if p == nil || len(buf) == 0 {
		return
	}
	p.mu.Lock()
	if p.kept < p.max {
		p.free[len(buf)] = append(p.free[len(buf)], buf)
		p.kept++
	}
	p.mu.Unlock()
}
