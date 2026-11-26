package main

import (
	"fmt"
	"testing"
	"time"
)

func benchFeedSeenDaemon(n int) *Daemon {
	d := &Daemon{}
	d.state.FeedSeen = make(map[string]int64, n+1024)
	added := time.Now().Add(-time.Hour)
	for i := 0; i < n; i++ {
		d.state.FeedSeen[fmt.Sprintf("feed|item-%040x", i)] = added.Unix()
	}
	return d
}

// BenchmarkMarkFeedSeenPruneEveryInsert is the previous behaviour: every added
// item walked the whole FeedSeen map under d.mu, so a poll that added hundreds
// of items paid the map size hundreds of times.
func BenchmarkMarkFeedSeenPruneEveryInsert(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			d := benchFeedSeenDaemon(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				d.feedSeenPruned = time.Time{} // force the scan, as before
				d.markFeedSeen("feed", fmt.Sprintf("new-%d", i%1000))
			}
		})
	}
}

// BenchmarkMarkFeedSeenAmortised is the current behaviour: the scan runs once
// per feedSeenPruneEvery, so an insert only touches the map.
func BenchmarkMarkFeedSeenAmortised(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			d := benchFeedSeenDaemon(n)
			d.feedSeenPruned = time.Now()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				d.markFeedSeen("feed", fmt.Sprintf("new-%d", i%1000))
			}
		})
	}
}
