package piececache

import (
	"container/heap"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/semaphore"
	"github.com/rcrowley/go-metrics"
)

// Cache is a LRU piece cache of certain size.
// Items in the cache are expired after the defined TTL.
type Cache struct {
	size, maxSize int64
	ttl           time.Duration
	items         map[string]*item
	accessList    accessList
	m             sync.RWMutex
	sem           *semaphore.Semaphore
	stopC         chan struct{} // stops the expiry sweeper

	NumCached      metrics.Meter
	NumTotal       metrics.Meter
	NumLoad        metrics.Meter
	NumLoadedBytes metrics.Meter
}

// Loader is a function that loads data from a piece.
type Loader func() ([]byte, error)

// sweepEvery is how often expired items are swept, derived from the TTL
func sweepEvery(ttl time.Duration) time.Duration {
	d := ttl / 2
	if d < 10*time.Millisecond {
		d = 10 * time.Millisecond
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// New returns new Cache.
func New(maxSize int64, ttl time.Duration, parallelReads uint) *Cache {
	c := &Cache{
		maxSize:        maxSize,
		ttl:            ttl,
		items:          make(map[string]*item),
		sem:            semaphore.New(int(parallelReads)),
		stopC:          make(chan struct{}),
		NumCached:      metrics.NewMeter(),
		NumTotal:       metrics.NewMeter(),
		NumLoad:        metrics.NewMeter(),
		NumLoadedBytes: metrics.NewMeter(),
	}
	go c.sweepLoop()
	return c
}

// sweepLoop drops expired items periodically. One timer for the whole cache
// instead of one time.Timer per cached block.
func (c *Cache) sweepLoop() {
	t := time.NewTicker(sweepEvery(c.ttl))
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.sweep()
		case <-c.stopC:
			return
		}
	}
}

func (c *Cache) sweep() {
	now := time.Now()
	c.m.Lock()
	defer c.m.Unlock()
	var expired []*item
	for _, i := range c.accessList {
		if now.After(i.expireAt) {
			expired = append(expired, i)
		}
	}
	for _, i := range expired {
		if i.index != -1 {
			c.removeItem(i)
		}
	}
}

// Close the cache and release all resources. It drops all cached items,
// stopping their expiry timers, then stops the metrics meters.
func (c *Cache) Close() {
	close(c.stopC)
	c.Clear()
	c.NumCached.Stop()
	c.NumTotal.Stop()
	c.NumLoad.Stop()
	c.NumLoadedBytes.Stop()
}

// Clear the cache. Drops all items in cache.
func (c *Cache) Clear() {
	c.m.Lock()
	c.items = make(map[string]*item)
	c.accessList = nil
	c.size = 0
	c.m.Unlock()
}

// Len returns the number of items in the cache. Items may be in different sizes.
func (c *Cache) Len() int {
	c.m.RLock()
	defer c.m.RUnlock()
	return len(c.items)
}

// LoadsActive returns the number of active Loader calls.
func (c *Cache) LoadsActive() int {
	return (c.sem.Len())
}

// LoadsWaiting returns the number of waiting Loader calls.
func (c *Cache) LoadsWaiting() int {
	return (c.sem.Waiting())
}

// Size returns the total size of the items in the cache.
func (c *Cache) Size() int64 {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.size
}

// Utilization is a number between 0 and 100 that returns the hit-ratio of the cache.
func (c *Cache) Utilization() int {
	total := c.NumTotal.Rate1()
	if total == 0 {
		return 0
	}
	return int((100 * c.NumCached.Rate1()) / total)
}

// Get item with the key from cache. If item is not in cache, load by calling `loader` func and put into the cache.
func (c *Cache) Get(key string, loader Loader) ([]byte, error) {
	i := c.getItem(key)
	return c.getValue(i, loader)
}

func (c *Cache) getItem(key string) *item {
	c.m.Lock()
	defer c.m.Unlock()

	c.NumTotal.Mark(1)

	i, ok := c.items[key]
	if ok {
		// Drop an expired item so the caller reloads it (lazy TTL
		// instead of a per-item timer). Items still loading have index -1 and
		// are kept.
		if i.index != -1 && time.Now().After(i.expireAt) {
			c.removeItem(i)
			i = nil
		}
	}
	if i != nil {
		c.NumCached.Mark(1)
		return i
	}
	// index -1 marks an item that is not in the access list yet; the zero value
	// (0) would look like it sits at the head of the heap.
	i = &item{key: key, index: -1}
	c.items[key] = i
	return i
}

func (c *Cache) getValue(i *item, loader Loader) ([]byte, error) {
	i.Lock()
	defer i.Unlock()

	if i.loaded {
		if i.err != nil {
			return nil, i.err
		}
		c.updateAccessTime(i)
		return i.value, nil
	}

	c.sem.Wait()
	i.value, i.err = loader()
	c.sem.Signal()
	i.loaded = true
	c.NumLoad.Mark(1)
	c.NumLoadedBytes.Mark(int64(len(i.value)))

	return c.handleNewItem(i)
}

func (c *Cache) handleNewItem(i *item) ([]byte, error) {
	c.m.Lock()
	defer c.m.Unlock()

	if i.err != nil {
		if c.items[i.key] == i {
			delete(c.items, i.key)
		}
		return nil, i.err
	}

	// The item was evicted or replaced while loading: return the
	// data but do not cache a stale copy under the key.
	if c.items[i.key] != i {
		return i.value, nil
	}

	// Do not cache values larger than cache size.
	if int64(len(i.value)) > c.maxSize {
		delete(c.items, i.key)
		return i.value, nil
	}

	c.makeRoom(i)

	c.size += int64(len(i.value))

	i.lastAccessed = time.Now()
	i.expireAt = i.lastAccessed.Add(c.ttl)
	heap.Push(&c.accessList, i)

	return i.value, nil
}

func (c *Cache) updateAccessTime(i *item) {
	c.m.Lock()
	defer c.m.Unlock()

	// The item may have been removed by the sweeper while it was in use.
	if i.index == -1 {
		return
	}
	i.lastAccessed = time.Now()
	i.expireAt = i.lastAccessed.Add(c.ttl)
	heap.Fix(&c.accessList, i.index)
}

func (c *Cache) makeRoom(i *item) {
	for c.maxSize-c.size < int64(len(i.value)) {
		i := c.accessList[0]
		c.removeItem(i)
	}
}

func (c *Cache) removeItem(i *item) {
	// Idempotent: the item may already have been removed or replaced.
	if i.index == -1 || c.items[i.key] != i {
		return
	}
	delete(c.items, i.key)
	heap.Remove(&c.accessList, i.index)
	c.size -= int64(len(i.value))
}

// SetMaxSize changes the cache size, evicting the least recently used items
// that no longer fit (the read cache is retuned at runtime).
func (c *Cache) SetMaxSize(maxSize int64) {
	c.m.Lock()
	defer c.m.Unlock()
	c.maxSize = maxSize
	for c.size > c.maxSize && len(c.accessList) > 0 {
		c.removeItem(c.accessList[0])
	}
}
