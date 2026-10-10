package main

import (
	"runtime"
	"testing"
	"time"
)

// TestManagedFootprintIsBounded is the F0 guard (docs/gx-torrent-evoluto.md
// §8.2). In managed mode the daemon must stay lean and must not start work of
// its own: a standalone module that wakes up (ticker, poller, DB) when Gextto
// is driving the daemon would show up here as extra goroutines or a goroutine
// leak. It is fast and deterministic by design.
func TestManagedFootprintIsBounded(t *testing.T) {
	before := runtime.NumGoroutine()
	start := time.Now()
	newTestDaemon(t)
	ready := time.Since(start)

	// Let the queue loop and the rain session settle, then measure again with
	// no activity: the count must not creep up on its own.
	time.Sleep(1500 * time.Millisecond)
	settled := runtime.NumGoroutine()
	time.Sleep(1500 * time.Millisecond)
	idle := runtime.NumGoroutine()

	if ready > 5*time.Second {
		t.Errorf("managed startup took %s (limit 5s)", ready)
	}
	if delta := settled - before; delta > 80 {
		t.Errorf("managed daemon added %d goroutines (before %d, after %d)", delta, before, settled)
	}
	if idle > settled+8 {
		t.Errorf("goroutines grew while idle: %d -> %d", settled, idle)
	}
	t.Logf("startup=%s goroutines before=%d settled=%d idle=%d", ready, before, settled, idle)
}
