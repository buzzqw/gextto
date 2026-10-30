package piececache

import (
	"fmt"
	"testing"
	"time"
)

// benchCacheHit measures a cache hit on a resident block: the read path an
// upload takes for every block a peer asks for. The claim and the LRU touch
// both take the cache lock, so the parallel variant shows the contention the
// upload goroutines see.
func benchCacheHit(b *testing.B, maxSize int64, parallel bool) {
	c := New(maxSize, time.Minute, 64)
	defer c.Close()
	const blockSize = 16 << 10
	value := make([]byte, blockSize)
	for i := 0; i < 64; i++ {
		key := fmt.Sprintf("peer-%d", i)
		if _, err := c.Get(key, func() ([]byte, error) { return value, nil }); err != nil {
			b.Fatal(err)
		}
	}
	key := "peer-0"
	b.SetBytes(blockSize)
	b.ReportAllocs()
	b.ResetTimer()
	if parallel {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := c.Get(key, func() ([]byte, error) { return value, nil }); err != nil {
					b.Fatal(err)
				}
			}
		})
		return
	}
	for i := 0; i < b.N; i++ {
		if _, err := c.Get(key, func() ([]byte, error) { return value, nil }); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCacheGetHit(b *testing.B)         { benchCacheHit(b, 64<<20, false) }
func BenchmarkCacheGetHitParallel(b *testing.B) { benchCacheHit(b, 64<<20, true) }
