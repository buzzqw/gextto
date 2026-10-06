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
	defer mu.Unlock()
	state.data[key] = value
	state.next++
	state.seq[key] = state.next
	state.dirty = true
	if len(state.data) > maxEntries {
		// Keep the evictTo most recently set entries.
		keys := make([]string, 0, len(state.data))
		for k := range state.data {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return state.seq[keys[i]] > state.seq[keys[j]] })
		kept := make(map[string]string, evictTo)
		keptSeq := make(map[string]uint64, evictTo)
		for _, k := range keys[:evictTo] {
			kept[k] = state.data[k]
			if order, ok := state.seq[k]; ok {
				keptSeq[k] = order
			}
		}
		state.data = kept
		state.seq = keptSeq
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
	payload, err := json.Marshal(state.data)
	state.dirty = false
	mu.Unlock()
	if err != nil {
		return
	}
	if err := utils.AtomicWrite(path, payload); err != nil {
		logWarn("magnet cache write failed", path, err)
	}
}

func logWarn(msg, path string, err error) {
	// Kept dependency-free to avoid an import cycle with logging.
	_ = msg
	_ = path
	_ = err
}
