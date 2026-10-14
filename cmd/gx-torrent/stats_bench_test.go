package main

// stats_bench_test.go measures the cost the Statistics page adds to the daemon:
// the per-tick history sample, the lifetime folding, the graph snapshot and the
// page translation. The numbers live in docs/measurements.md.

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/webui/assets"
)

func BenchmarkRateHistoryAdd(b *testing.B) {
	h := newRateHistory()
	now := time.Now()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		h.add(now.Add(time.Duration(i)*3*time.Second), 1<<20, 1<<19)
	}
}

func BenchmarkRateHistorySnapshot1h(b *testing.B) {
	h := newRateHistory()
	now := time.Now()
	for i := 0; i < rateTickCapacity; i++ {
		h.add(now.Add(time.Duration(i)*3*time.Second), 1<<20, 1<<19)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = h.snapshot("1h")
	}
}

func BenchmarkLifetimeAccumulate(b *testing.B) {
	d := &Daemon{}
	d.loadLifetime()
	now := time.Now()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d.accumulateLifetimeLocked(now, int64(i)*1000, int64(i)*500)
	}
}

func BenchmarkStatsPageTranslate(b *testing.B) {
	page := assets.StatsPage()
	dict := uiDictionary("it")
	b.SetBytes(int64(len(page)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = uiTranslateHTML(page, dict)
	}
}
