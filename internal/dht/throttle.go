package dht

import (
	"container/list"
	"sync"
	"time"
)

// clientThrottle drops packets from hosts that send too many of them per
// minute. It keeps per-host activity counters plus the set of hosts already
// blocked; both are bounded tables, so a large swarm cannot grow them without
// limit.
type clientThrottle struct {
	mu           sync.Mutex
	maxPerMinute int
	activity     *lruTable
	blocked      *lruTable
	stop         chan struct{}
	stopOnce     sync.Once
}

// newClientThrottle allows maxPerMinute requests per host per minute and
// remembers up to maxHosts hosts.
func newClientThrottle(maxPerMinute int, maxHosts int64) *clientThrottle {
	t := &clientThrottle{
		maxPerMinute: maxPerMinute,
		activity:     newLRUTable(int(maxHosts)),
		blocked:      newLRUTable(int(maxHosts)),
		stop:         make(chan struct{}),
	}
	go t.cleanup()
	return t
}

// Stop ends the background cleanup goroutine.
func (t *clientThrottle) Stop() {
	t.stopOnce.Do(func() { close(t.stop) })
}

// CheckBlock reports whether the host is still within its per-minute budget.
// It returns false for a host that has already exceeded it.
func (t *clientThrottle) CheckBlock(host string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.blocked.get(host); ok {
		// A host that exceeded the limit stays blocked while it is in the
		// table.
		return false
	}

	remaining, ok := t.activity.get(host)
	if !ok {
		remaining = 59
	} else {
		remaining--
	}
	if remaining < 60-t.maxPerMinute {
		t.activity.set(host, remaining-300)
		t.blocked.set(host, remaining)
		return false
	}
	t.activity.set(host, remaining)
	return true
}

// cleanup refills the per-host counters and forgets hosts that have been quiet
// long enough. It runs faster than the one-minute window to keep the tables
// small.
func (t *clientThrottle) cleanup() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.mu.Lock()
			for _, key := range t.activity.keys() {
				v, _ := t.activity.get(key)
				if v+5 > 60 {
					t.activity.delete(key)
					continue
				}
				t.activity.set(key, v+5)
			}
			t.mu.Unlock()
		case <-t.stop:
			return
		}
	}
}

// lruTable is a small map with least-recently-used eviction. It is not safe for
// concurrent use on its own; the caller serializes access.
type lruTable struct {
	max   int
	order *list.List
	items map[string]*list.Element
}

type lruItem struct {
	key   string
	value int
}

func newLRUTable(max int) *lruTable {
	return &lruTable{max: max, order: list.New(), items: make(map[string]*list.Element)}
}

func (l *lruTable) get(key string) (int, bool) {
	el, ok := l.items[key]
	if !ok {
		return 0, false
	}
	l.order.MoveToFront(el)
	return el.Value.(*lruItem).value, true
}

func (l *lruTable) set(key string, value int) {
	if el, ok := l.items[key]; ok {
		el.Value.(*lruItem).value = value
		l.order.MoveToFront(el)
		return
	}
	el := l.order.PushFront(&lruItem{key: key, value: value})
	l.items[key] = el
	if l.max > 0 && l.order.Len() > l.max {
		l.evictOldest()
	}
}

func (l *lruTable) delete(key string) {
	if el, ok := l.items[key]; ok {
		l.removeElement(el)
	}
}

func (l *lruTable) evictOldest() {
	if el := l.order.Back(); el != nil {
		l.removeElement(el)
	}
}

func (l *lruTable) removeElement(el *list.Element) {
	l.order.Remove(el)
	delete(l.items, el.Value.(*lruItem).key)
}

func (l *lruTable) keys() []string {
	keys := make([]string, 0, len(l.items))
	for k := range l.items {
		keys = append(keys, k)
	}
	return keys
}
