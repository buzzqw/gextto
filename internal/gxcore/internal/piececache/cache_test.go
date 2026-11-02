package piececache

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheGetHitAndMiss(t *testing.T) {
	c := New(1<<20, time.Minute, 1)
	defer c.Close()

	var loads atomic.Int32
	loader := func() ([]byte, error) {
		loads.Add(1)
		return []byte("value"), nil
	}
	if got, err := c.Get("k", loader); err != nil || string(got) != "value" {
		t.Fatalf("first Get = %q, %v", got, err)
	}
	if got, err := c.Get("k", loader); err != nil || string(got) != "value" {
		t.Fatalf("second Get = %q, %v", got, err)
	}
	if n := loads.Load(); n != 1 {
		t.Fatalf("loader ran %d times, want 1", n)
	}
}

// TestCacheSweepsExpiredWithoutAccess is the engine point: expiry is
// handled by one sweeper, not one goroutine/timer per cached block.
func TestCacheSweepsExpiredWithoutAccess(t *testing.T) {
	c := New(1<<20, 30*time.Millisecond, 1)
	defer c.Close()

	if _, err := c.Get("k", func() ([]byte, error) { return []byte("v"), nil }); err != nil {
		t.Fatal(err)
	}
	if c.Len() != 1 || c.Size() != 1 {
		t.Fatalf("after Get len=%d size=%d, want 1/1", c.Len(), c.Size())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && c.Len() > 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if c.Len() != 0 || c.Size() != 0 {
		t.Fatalf("expired item not swept: len=%d size=%d", c.Len(), c.Size())
	}
}

// TestCacheReloadsExpired checks an expired item is not served from the cache.
func TestCacheReloadsExpired(t *testing.T) {
	c := New(1<<20, 30*time.Millisecond, 1)
	defer c.Close()

	var loads atomic.Int32
	loader := func() ([]byte, error) {
		loads.Add(1)
		return []byte("v"), nil
	}
	c.Get("k", loader)
	c.Get("k", loader)
	if n := loads.Load(); n != 1 {
		t.Fatalf("loader ran %d times before expiry, want 1", n)
	}
	time.Sleep(60 * time.Millisecond)
	c.Get("k", loader)
	if n := loads.Load(); n != 2 {
		t.Fatalf("loader ran %d times after expiry, want 2", n)
	}
}

// TestCacheConcurrent exercises the replaced-item guards under short TTLs.
func TestCacheConcurrent(t *testing.T) {
	c := New(1<<20, 5*time.Millisecond, 2)
	defer c.Close()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("k%d", i%3)
				if _, err := c.Get(key, func() ([]byte, error) {
					return []byte(key), nil
				}); err != nil {
					t.Errorf("Get(%s): %v", key, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestCacheSetMaxSizeEvicts(t *testing.T) {
	c := New(1<<20, time.Minute, 1)
	defer c.Close()

	for i := 0; i < 4; i++ {
		k := fmt.Sprintf("k%d", i)
		if _, err := c.Get(k, func() ([]byte, error) { return []byte(k), nil }); err != nil {
			t.Fatal(err)
		}
	}
	c.SetMaxSize(2) // only two single-byte items fit
	if c.Size() > 2 {
		t.Fatalf("size after SetMaxSize = %d, want <= 2", c.Size())
	}
}

// TestPrefetchIsNotARequest: a background load fills the cache without moving the
// hit ratio, a failed one leaves nothing behind, and a key already resident is not
// loaded twice.
func TestPrefetchIsNotARequest(t *testing.T) {
	c := New(1<<20, time.Minute, 1)
	defer c.Close()
	loads := make(chan struct{}, 4)
	c.Prefetch("a", func() ([]byte, error) { loads <- struct{}{}; return []byte("value"), nil })
	<-loads
	deadline := time.Now().Add(5 * time.Second)
	for c.Size() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the prefetched value never landed")
		}
		time.Sleep(time.Millisecond)
	}
	if n := c.NumTotal.Count(); n != 0 {
		t.Fatalf("%d requests counted for a prefetch", n)
	}
	c.Prefetch("a", func() ([]byte, error) { t.Error("a resident key was loaded again"); return nil, nil })
	got, err := c.Get("a", func() ([]byte, error) { t.Error("a hit ran the loader"); return nil, nil })
	if err != nil || string(got) != "value" {
		t.Fatalf("got %q, %v", got, err)
	}

	c.Prefetch("bad", func() ([]byte, error) { loads <- struct{}{}; return nil, errors.New("read failed") })
	<-loads
	for c.Len() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("the failed load stayed: %d items", c.Len())
		}
		time.Sleep(time.Millisecond)
	}
	got, err = c.Get("bad", func() ([]byte, error) { return []byte("second try"), nil })
	if err != nil || string(got) != "second try" {
		t.Fatalf("after a failed prefetch: %q, %v", got, err)
	}
}
