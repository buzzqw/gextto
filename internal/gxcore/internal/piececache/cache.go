package piececache

import (
	"container/heap"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/semaphore"
	"github.com/rcrowley/go-metrics"
)

// Cache is a size-bounded LRU of read blocks with lazy TTL expiry: a single
// periodic sweeper reaps expired items instead of one timer per block.
// Concurrent loads of the same key share one loader call; oversized values
// bypass the cache but are still served.
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

// Loader fetches a block from storage on a cache miss.
type Loader func() ([]byte, error)

// sweepEvery derives the sweeper period from the TTL, floored and capped so
// tiny TTLs do not spin and huge ones still reap.
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

// New starts a cache of maxSize bytes, TTL per item, parallelReads concurrent
// loads.
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

// sweepLoop reaps expired items on the derived period.
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

// sweep drops every expired heap member. Index -1 items (loading or already
// removed) are skipped by removeItem's guard.
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
		c.removeItem(i)
	}
}

// Close stops the sweeper, drops everything and stops the meters.
func (c *Cache) Close() {
	close(c.stopC)
	c.Clear()
	c.NumCached.Stop()
	c.NumTotal.Stop()
	c.NumLoad.Stop()
	c.NumLoadedBytes.Stop()
}

// Clear drops all items at once.
func (c *Cache) Clear() {
	c.m.Lock()
	c.items = make(map[string]*item)
	c.accessList = nil
	c.size = 0
	c.m.Unlock()
}

// Len counts the cached items (of varying sizes).
func (c *Cache) Len() int {
	c.m.RLock()
	defer c.m.RUnlock()
	return len(c.items)
}

// LoadsActive counts the running loader calls.
func (c *Cache) LoadsActive() int {
	return c.sem.Len()
}

// LoadsWaiting counts the queued loader calls.
func (c *Cache) LoadsWaiting() int {
	return c.sem.Waiting()
}

// Size totals the cached bytes.
func (c *Cache) Size() int64 {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.size
}

// Utilization is the cache hit ratio in percent.
func (c *Cache) Utilization() int {
	total := c.NumTotal.Rate1()
	if total == 0 {
		return 0
	}
	return int((100 * c.NumCached.Rate1()) / total)
}

// Get serves key from the cache, loading and storing on a miss.
func (c *Cache) Get(key string, loader Loader) ([]byte, error) {
	return c.getValue(c.claim(key), loader)
}

// claim returns the resident item (counting the hit), or registers a fresh
// loading placeholder. Expired residents are dropped so the caller reloads;
// loading placeholders (index -1) survive.
func (c *Cache) claim(key string) *item {
	c.m.Lock()
	defer c.m.Unlock()
	c.NumTotal.Mark(1)
	if i, ok := c.items[key]; ok {
		if i.index == -1 || !time.Now().After(i.expireAt) {
			c.NumCached.Mark(1)
			return i
		}
		c.removeItem(i)
	}
	i := &item{key: key, index: -1}
	c.items[key] = i
	return i
}

// getValue serves a claimed item: loaded ones refresh their access time
// (hits), others run the loader under the read semaphore exactly once even
// under concurrent Gets of the same key (the item mutex serializes them).
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
	return c.storeNew(i)
}

// storeNew files a freshly loaded item: failures and superseded placeholders
// are dropped (but still served), oversized values bypass, the rest evict
// LRU room and join the heap with a fresh TTL.
func (c *Cache) storeNew(i *item) ([]byte, error) {
	c.m.Lock()
	defer c.m.Unlock()
	if i.err != nil {
		if c.items[i.key] == i {
			delete(c.items, i.key)
		}
		return nil, i.err
	}
	if c.items[i.key] != i {
		return i.value, nil
	}
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

// updateAccessTime refreshes recency and TTL, re-sinking the heap entry.
// Items the sweeper removed meanwhile (index -1) are left alone.
func (c *Cache) updateAccessTime(i *item) {
	c.m.Lock()
	defer c.m.Unlock()
	if i.index == -1 {
		return
	}
	i.lastAccessed = time.Now()
	i.expireAt = i.lastAccessed.Add(c.ttl)
	heap.Fix(&c.accessList, i.index)
}

// makeRoom evicts LRU items until value i fits.
func (c *Cache) makeRoom(i *item) {
	for c.maxSize-c.size < int64(len(i.value)) {
		c.removeItem(c.accessList[0])
	}
}

// removeItem unlinks i idempotently: already-removed or replaced items are
// no-ops.
func (c *Cache) removeItem(i *item) {
	if i.index == -1 || c.items[i.key] != i {
		return
	}
	delete(c.items, i.key)
	heap.Remove(&c.accessList, i.index)
	c.size -= int64(len(i.value))
}

// SetMaxSize retunes the capacity live, evicting the LRU overflow.
func (c *Cache) SetMaxSize(maxSize int64) {
	c.m.Lock()
	defer c.m.Unlock()
	c.maxSize = maxSize
	for c.size > c.maxSize && len(c.accessList) > 0 {
		c.removeItem(c.accessList[0])
	}
}
