// Package cache is the persistent detail-page -> magnet cache (legacy
// SmartCache, corsaro_cache.json). Scrapers consult it before visiting a
// detail page, so a release already seen in a previous cycle costs no HTTP
// request. The file is written atomically and evicts the oldest half past
// maxEntries.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/buzzqw/gextto/internal/utils"
)

const (
	maxEntries = 5000
	evictTo    = 2500
)

type store struct {
	path  string
	data  map[string]string
	dirty bool
	// seq records the insertion order of keys set in this process, so eviction
	// drops the oldest entries and never the one just added. Entries loaded
	// from disk have sequence 0 and are therefore evicted first.
	seq  map[string]uint64
	next uint64
}

var (
	mu    sync.Mutex
	state = store{data: map[string]string{}, seq: map[string]uint64{}}
)

// Init loads the on-disk cache from the gextto data directory. Safe to call
// once at startup; without it the cache still works in memory only.
func Init(dataDir string) {
	path := filepath.Join(dataDir, "gextto_magnet_cache.json")
	data := map[string]string{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &data)
	}
	mu.Lock()
	state.path = path
	state.data = data
	state.seq = map[string]uint64{}
	state.dirty = false
	mu.Unlock()
}

// Get returns a cached value.
func Get(key string) (string, bool) {
	mu.Lock()
	defer mu.Unlock()
	value, ok := state.data[key]
	return value, ok
}

// Set stores a value and bounds the cache size.
func Set(key, value string) {
	mu.Lock()
	state.data[key] = value
	state.next++
	state.seq[key] = state.next
	state.dirty = true
	if len(state.data) <= maxEntries {
		mu.Unlock()
		return
	}
	// Snapshot the keys and their insertion order, then sort outside the lock:
	// ordering 5000 keys is the slow part, and it used to block every Get and
	// Set of every crawler goroutine.
	keys := make([]string, 0, len(state.data))
	orders := make(map[string]uint64, len(state.data))
	for k := range state.data {
		keys = append(keys, k)
		orders[k] = state.seq[k]
	}
	mu.Unlock()

	sort.Slice(keys, func(i, j int) bool { return orders[keys[i]] > orders[keys[j]] })
	drop := keys[evictTo:]

	mu.Lock()
	defer mu.Unlock()
	for _, k := range drop {
		// Keep entries re-set while the sort ran: their order moved to the top,
		// so they are the most recent and must survive. A later Set trims the
		// few rows still over the limit.
		if state.seq[k] != orders[k] {
			continue
		}
		delete(state.data, k)
		delete(state.seq, k)
	}
}

// Len returns the number of entries.
func Len() int {
	mu.Lock()
	defer mu.Unlock()
	return len(state.data)
}

// Save persists the cache if anything changed. Called once per scrape cycle.
func Save() {
	mu.Lock()
	if !state.dirty {
		mu.Unlock()
		return
	}
	path := state.path
	if path == "" {
		state.dirty = false
		mu.Unlock()
		return
	}
	// Remember which change this write carries: if Set runs while the file is
	// being written, the cache stays dirty so the next Save repeats it. A
	// failed write also stays dirty instead of being forgotten.
	generation := state.next
	payload, err := json.Marshal(state.data)
	mu.Unlock()
	if err != nil {
		return
	}
	if err := utils.AtomicWrite(path, payload); err != nil {
		logWarn("magnet cache write failed", path, err)
		return
	}
	mu.Lock()
	if state.next == generation {
		state.dirty = false
	}
	mu.Unlock()
}

func logWarn(msg, path string, err error) {
	// Kept dependency-free to avoid an import cycle with logging.
	_ = msg
	_ = path
	_ = err
}
