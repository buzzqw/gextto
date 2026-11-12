package main

// measure_idle_test.go measures the cost of a long seeding list at rest: a
// hundred torrents of 2 MiB already complete, the queue on its production
// tick, no peers. It reports the CPU the whole process spent over the window
// and writes a CPU and a heap profile into GX_BENCH_OUT (or the test work
// area), so the busiest idle paths can be read with go tool pprof.
//
//	GX_MEASURE=1 GX_TEST_TICK=3s go test ./cmd/gx-torrent/ -run MeasureIdleTorrents -v -count=1

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"
)

func TestMeasureIdleTorrents(t *testing.T) {
	if os.Getenv("GX_MEASURE") == "" {
		t.Skip("set GX_MEASURE=1 to run the idle measurement")
	}
	out := os.Getenv("GX_BENCH_OUT")
	if out == "" {
		out = t.TempDir()
	}
	count := measureCount("GX_MEASURE_TORRENTS", 100)
	window := 30 * time.Second
	if raw := os.Getenv("GX_MEASURE_WINDOW"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			window = d
		}
	}
	d := newTestDaemon(t)
	// Every torrent seeds, as in the comparison harness: the default queue
	// would keep all but a few paused.
	d.mu.Lock()
	d.state.Config.ActiveSeeds = count
	d.state.Config.ActiveLimit = 2 * count
	d.state.Config.DynamicQueue = false
	d.mu.Unlock()
	root := t.TempDir()
	start := time.Now()
	hashes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		src := filepath.Join(root, fmt.Sprintf("t%03d", i))
		data := makeTorrent(t, src, fmt.Sprintf("payload-%03d.bin", i), measureSize)
		hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash)
	}
	added := time.Since(start)
	// The queue keeps its active-seed limit: a complete torrent may wait queued.
	states := map[string]int{}
	for deadline := time.Now().Add(5 * time.Minute); ; {
		clear(states)
		done := 0
		for _, hash := range hashes {
			info, _ := findInfo(d, hash)
			states[info.State]++
			if info.Progress >= 100 {
				done++
			}
		}
		if done == len(hashes) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d complete after 5 minutes: %v", done, len(hashes), states)
		}
		time.Sleep(500 * time.Millisecond)
	}
	seeding := time.Since(start)
	t.Logf("states once complete: %v", states)
	time.Sleep(10 * time.Second) // settle
	runtime.GC()
	cpuFile, err := os.Create(filepath.Join(out, "idle-100-cpu.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	defer cpuFile.Close()
	before := processCPU()
	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		t.Fatal(err)
	}
	time.Sleep(window)
	pprof.StopCPUProfile()
	spent := processCPU() - before
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	heapFile, err := os.Create(filepath.Join(out, "idle-100-heap.pprof"))
	if err == nil {
		_ = pprof.WriteHeapProfile(heapFile)
		heapFile.Close()
	}
	t.Logf("%d torrents: added in %v, all seeding after %v; at rest %.3f %% of a core over %v; heap in use %.1f MiB, goroutines %d; profiles in %s",
		count, added.Round(time.Millisecond), seeding.Round(time.Millisecond),
		100*spent.Seconds()/window.Seconds(), window, float64(mem.HeapInuse)/(1<<20), runtime.NumGoroutine(), out)
}
