package main

// measure_pipeline_test.go is an opt-in harness for the adaptive request queue
// (pipelining): it runs the local seeding swarm with a shallow queue fixed at a
// low depth and with deeper windows, and reports the seed upload and the time
// all leechers took to finish.
//
// On loopback the RTT is close to zero, so the depth barely matters by
// construction: this harness is a regression and sanity check of the queue
// plumbing, not a WAN demonstration. A real-swarm campaign needs latency (see
// docs/evoluzione.md section 6).
//
// Run it with:
//
//	GX_MEASURE_PIPELINE=1 go test ./cmd/gx-torrent/ -run MeasurePipelining -v

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMeasurePipeliningLocalSwarm(t *testing.T) {
	if os.Getenv("GX_MEASURE_PIPELINE") != "1" {
		t.Skip("set GX_MEASURE_PIPELINE=1 to run the pipelining measurement")
	}
	const size = 8 << 20 // 8 MiB: enough blocks to exercise the queue
	leechers := measureCount("GX_MEASURE_LEECHERS", 2)

	profiles := []struct {
		name string
		def  int
		max  int
	}{
		{"shallow", 4, 4},    // fixed depth 4, no growth
		{"default", 50, 250}, // engine defaults
		{"deep", 200, 500},   // deeper window
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			uploaded, elapsed := runPipelineSwarm(t, size, leechers, p.def, p.max)
			kib := 0.0
			if elapsed > 0 {
				kib = float64(uploaded) / 1024 / elapsed.Seconds()
			}
			t.Logf("profile=%s def=%d max=%d uploaded=%d bytes elapsed=%s (%.0f KiB/s)",
				p.name, p.def, p.max, uploaded, elapsed.Round(time.Millisecond), kib)
			if uploaded < int64(size) {
				t.Fatalf("seeded %d bytes, want at least one corpus (%d)", uploaded, size)
			}
		})
	}
}

// runPipelineSwarm runs one seeder and `leechers` leechers with the given
// request-queue depth and returns the bytes the seed uploaded and the time all
// leechers took to complete.
func runPipelineSwarm(t *testing.T, size int, leechers, def, max int) (int64, time.Duration) {
	t.Helper()
	seeder := newTestDaemonWith(t, NetworkOptions{
		PortBegin: 44700, PortEnd: 44799, Encryption: 1,
		DefaultRequestsOut: def, MaxRequestsOut: max,
	})
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "pipe.bin", size)
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })

	swarm := make([]measureNode, 0, leechers)
	for i := 0; i < leechers; i++ {
		ip := fmt.Sprintf("127.0.0.%d", 10+i)
		d := newTestDaemonWith(t, NetworkOptions{
			OutgoingInterface: ip,
			PortBegin:         uint16(44800 + i*100), PortEnd: uint16(44899 + i*100),
			Encryption: 1, DefaultRequestsOut: def, MaxRequestsOut: max,
		})
		dst := filepath.Join(t.TempDir(), fmt.Sprintf("dst%d", i))
		if _, _, err := d.add(addRequest{TorrentData: data, Destination: dst, SeedRatio: -1, SeedDays: -1}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, fmt.Sprintf("leecher %d running", i), func() bool {
			s := stateOf(d, hash)
			return s == "downloading" || s == "stalled"
		})
		swarm = append(swarm, measureNode{daemon: d, ip: ip, port: d.peerPort})
	}
	for i := range swarm {
		swarm[i].daemon.mu.Lock()
		handle, _ := swarm[i].daemon.findLocked(hash)
		swarm[i].daemon.mu.Unlock()
		if err := handle.AddPeer(fmt.Sprintf("127.0.0.1:%d", seeder.peerPort)); err != nil {
			t.Fatalf("add peer: %v", err)
		}
	}

	start := time.Now()
	deadline := start.Add(measureDeadline)
	for time.Now().Before(deadline) {
		done := true
		for _, n := range swarm {
			info, _ := findInfo(n.daemon, hash)
			if info.Progress != 100 {
				done = false
				break
			}
		}
		if done {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	elapsed := time.Since(start)
	seedInfo, _ := findInfo(seeder, hash)
	return seedInfo.Uploaded, elapsed
}
