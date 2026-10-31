package main

import (
	"testing"
	"time"
)

// BenchmarkAutoSpeedUpdate measures one step of the automatic speed
// controller: the probe median, the latency comparison and the cap move. It is
// the work the queue tick does every three seconds while the option is on.
func BenchmarkAutoSpeedUpdate(b *testing.B) {
	a := &autoSpeed{}
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < autoSpeedRecent; i++ {
		a.record(base.Add(time.Duration(i)*time.Second), 20*time.Millisecond)
	}
	now := base.Add(10 * time.Second)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.update(now.Add(time.Duration(i)*time.Second), 0, 0, 0, 0, 50, 1000*1024, 500*1024)
	}
}
