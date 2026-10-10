package piececache

import (
	"sync"
	"time"
)

type item struct {
	key          string
	value        []byte
	loaded       bool
	err          error
	lastAccessed time.Time
	// expireAt replaces the per-item time.Timer of upstream (gextto fork): the
	// cache sweeps expired items periodically instead of arming one timer per
	// block accessed. Guarded by Cache.m.
	expireAt time.Time
	index    int
	sync.Mutex
}

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
	n := len(*l)
	i := x.(*item)
	i.index = n
	*l = append(*l, i)
}

func (l *accessList) Pop() any {
	old := *l
	n := len(old)
	i := old[n-1]
	i.index = -1 // for safety
	*l = old[0 : n-1]
	return i
}
