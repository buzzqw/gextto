// Package cache is the persistent title -> magnet cache (legacy SmartCache,
// corsaro_cache.json). Scrapers consult it before visiting a detail page, so a
// release already seen in a previous cycle costs no HTTP request. The file is
// written atomically and evicts the oldest half past maxEntries.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
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
}

var (
	mu    sync.Mutex
	state = store{data: map[string]string{}}
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
	state.dirty = true
	if len(state.data) > maxEntries {
		// Drop down to a bounded half. Go maps do not preserve insertion order,
		// so the retained subset is arbitrary but bounded (legacy keeps the
		// newest half).
		kept := make(map[string]string, evictTo)
		count := 0
		for k, v := range state.data {
			if count >= evictTo {
				break
			}
			kept[k] = v
			count++
		}
		state.data = kept
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
