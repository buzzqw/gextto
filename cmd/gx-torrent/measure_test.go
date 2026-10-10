package main

// measure_test.go is an opt-in local-swarm measurement harness (gextto fork).
//
// Unlike the deterministic unchoker harness
// (third_party/rain/internal/unchoker/sim_test.go), this runs real daemons over
// loopback: one seeder and several leechers. Each peer uses its own 127.0.0.x
// address, because rain drops a second connection from the same IP to one
// torrent. By default the leechers dial only the seeder, which measures the
// seed-side upload (throughput, ratio) that super-seeding is meant to change.
// With GX_MEASURE_MESH=1 the leechers also dial each other, so the swarm can
// exchange pieces peer-to-peer and the super-seeding benefit shows.
//
// The daemon keeps its normal queue and auto-management, so the numbers reflect
// the system as it actually runs; the harness never disables the queue, the seed
// policy or the bandwidth scheduler to make a measurement easier.
//
// Run it with:
//
//	GX_MEASURE=1 go test ./cmd/gx-torrent/ -run MeasureSeeding -v
//
// Set GX_MEASURE_SUPERSEED=1 to compare against BEP 16 super-seeding,
// GX_MEASURE_MESH=1 for peer-to-peer exchange (2 leechers is reliable on one
// host), and GX_MEASURE_LEECHERS=N for the swarm size. See
// docs/evoluzione.md (section 6) for the methodology.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const measureSize = 2 << 20 // 2 MiB

// measureDeadline is generous: super-seeding advances on a 10 s retry tick, so
// a stuck peer can take two ticks to move on. The harness reports what it saw
// instead of failing on a single slow peer.
const measureDeadline = 90 * time.Second

func measureCount(env string, def int) int {
	if raw := os.Getenv(env); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// measureNode is one peer of the local swarm.
type measureNode struct {
	daemon *Daemon
	ip     string
	port   int
}

func (n measureNode) addr() string { return fmt.Sprintf("%s:%d", n.ip, n.port) }

func TestMeasureSeedingLocalSwarm(t *testing.T) {
	if os.Getenv("GX_MEASURE") != "1" {
		t.Skip("set GX_MEASURE=1 to run the seeding measurement")
	}
	superSeed := os.Getenv("GX_MEASURE_SUPERSEED") == "1"
	mesh := os.Getenv("GX_MEASURE_MESH") == "1"
	leechers := measureCount("GX_MEASURE_LEECHERS", 3)

	seederNet := NetworkOptions{PortBegin: 43700, PortEnd: 43799, Encryption: 1}
	if mesh {
		seederNet.ListenInterface = "127.0.0.1"
	}
	seeder := newTestDaemonWith(t, seederNet)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "measure.bin", measureSize)
	hash, _, err := seeder.add(addRequest{
		TorrentData: data, Destination: src,
		SuperSeeding: superSeed, SeedRatio: -1, SeedDays: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })
	seederNode := measureNode{daemon: seeder, ip: "127.0.0.1", port: seeder.peerPort}

	swarm := make([]measureNode, 0, leechers)
	for i := 0; i < leechers; i++ {
		ip := fmt.Sprintf("127.0.0.%d", 10+i)
		net := NetworkOptions{
			// A distinct loopback source per leecher: rain drops a second
			// connection from the same IP to one torrent.
			OutgoingInterface: ip,
			PortBegin:         uint16(43800 + i*100), PortEnd: uint16(43899 + i*100), Encryption: 1,
		}
		if mesh {
			// A distinct listener too, so the other leechers can dial it.
			net.ListenInterface = ip
		}
		d := newTestDaemonWith(t, net)
		dst := filepath.Join(t.TempDir(), fmt.Sprintf("dst%d", i))
		if _, _, err := d.add(addRequest{TorrentData: data, Destination: dst, SeedRatio: -1, SeedDays: -1}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, fmt.Sprintf("leecher %d running", i), func() bool {
			state := stateOf(d, hash)
			return state == "downloading" || state == "stalled"
		})
		swarm = append(swarm, measureNode{daemon: d, ip: ip, port: d.peerPort})
	}

	addPeer := func(from, to measureNode) {
		from.daemon.mu.Lock()
		handle, _ := from.daemon.findLocked(hash)
		from.daemon.mu.Unlock()
		if err := handle.AddPeer(to.addr()); err != nil {
			t.Fatalf("add peer %s -> %s: %v", from.addr(), to.addr(), err)
		}
	}
	for i := range swarm {
		addPeer(swarm[i], seederNode)
		if mesh {
			for j := range swarm {
				if i != j {
					addPeer(swarm[i], swarm[j])
				}
			}
		}
	}

	start := time.Now()
	done := make([]time.Duration, len(swarm))
	remaining := len(swarm)
	for remaining > 0 && time.Since(start) < measureDeadline {
		for i := range swarm {
			if done[i] != 0 {
				continue
			}
			if info, _ := findInfo(swarm[i].daemon, hash); info.Progress == 100 {
				done[i] = time.Since(start)
				remaining--
			}
		}
		if remaining > 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	elapsed := time.Since(start)

	seedInfo, _ := findInfo(seeder, hash)
	rate := float64(seedInfo.Uploaded) / elapsed.Seconds()
	t.Logf("super-seeding=%v mesh=%v leechers=%d size=%d elapsed=%s", superSeed, mesh, leechers, measureSize, elapsed.Round(time.Millisecond))
	t.Logf("seeder: uploaded=%d bytes (%.2fx corpus) avg=%.0f KiB/s", seedInfo.Uploaded,
		float64(seedInfo.Uploaded)/float64(measureSize), rate/1024)
	var leecherUpload int64
	for i := range swarm {
		info, _ := findInfo(swarm[i].daemon, hash)
		leecherUpload += info.Uploaded
		status := done[i].Round(time.Millisecond).String()
		if done[i] == 0 {
			status = fmt.Sprintf("NOT COMPLETE (%.1f%%)", info.Progress)
		}
		t.Logf("leecher %d: downloaded=%d uploaded=%d completed_in=%s", i, info.Downloaded, info.Uploaded, status)
	}
	t.Logf("leechers uploaded %d bytes total (peer-to-peer share)", leecherUpload)
	if remaining != 0 {
		t.Logf("warning: %d/%d leechers did not finish within %s", remaining, leechers, measureDeadline)
	}
	if seedInfo.Uploaded < measureSize {
		t.Fatalf("seeder uploaded less than the corpus (%d < %d)", seedInfo.Uploaded, measureSize)
	}
}
