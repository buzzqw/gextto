package cache

import (
	"fmt"
	"testing"
)

func TestStoresReadsAndEvicts(t *testing.T) {
	Set("cache-test-title", "magnet:?xt=urn:btih:abc")
	if got, ok := Get("cache-test-title"); !ok || got != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("get = %q %v", got, ok)
	}
	if _, ok := Get("cache-test-missing"); ok {
		t.Fatal("unexpected hit")
	}
	for index := 0; index < maxEntries+10; index++ {
		Set("evict-"+pad(index), "magnet")
	}
	if Len() > maxEntries {
		t.Fatalf("cache grew to %d", Len())
	}
}

func pad(value int) string {
	out := ""
	if value == 0 {
		return "0"
	}
	for value > 0 {
		out = string(rune('0'+value%10)) + out
		value /= 10
	}
	return out
}

func TestEvictionKeepsNewestEntries(t *testing.T) {
	mu.Lock()
	state = store{data: map[string]string{}, seq: map[string]uint64{}}
	mu.Unlock()
	for i := 0; i <= maxEntries; i++ {
		Set(fmt.Sprintf("k%d", i), "v")
	}
	if Len() != evictTo {
		t.Fatalf("len = %d, want %d", Len(), evictTo)
	}
	last := fmt.Sprintf("k%d", maxEntries)
	if _, ok := Get(last); !ok {
		t.Fatal("the entry just added was evicted")
	}
	if _, ok := Get("k0"); ok {
		t.Fatal("the oldest entry survived the eviction")
	}
}
