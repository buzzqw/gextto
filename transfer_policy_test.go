package gextto

import (
	"runtime"
	"testing"
	"time"
)

// TestTrimMemoryReturnsIdleHeap pins the behaviour the callers rely on: after
// a burst the free heap must go back to the operating system instead of
// staying resident until the next allocation. A plain collection is not
// enough, because the pages freed by the collector stay in the process until
// they are handed back.
func TestTrimMemoryReturnsIdleHeap(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates and collects tens of MB")
	}
	hold := make([][]byte, 0, 32)
	for i := 0; i < 32; i++ {
		hold = append(hold, make([]byte, 1<<20))
	}
	hold = nil
	runtime.GC()

	deadline := time.Now().Add(2 * time.Second)
	for {
		TrimMemory()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		resident := stats.HeapIdle - stats.HeapReleased
		if resident <= 8<<20 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("TrimMemory left %d MiB of idle heap resident", resident>>20)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
