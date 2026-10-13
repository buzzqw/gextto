package piececache

import (
	"sync"
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
	sync.Mutex
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
