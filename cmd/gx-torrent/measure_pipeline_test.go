package main

// measure_pipeline_test.go is an opt-in harness for the adaptive request queue
// (pipelining): it runs the local seeding swarm with a shallow queue fixed at a
// low depth and with deeper windows, and reports the seed upload and the time
// all leechers took to finish.
//
// On loopback the RTT is close to zero, so the depth barely matters by
// construction: this harness is a regression and sanity check of the queue
// plumbing, not a WAN demonstration. A real-swarm campaign needs latency (see
// docs/measurements.md section 3).
//
// Run it with:
//
//	GX_MEASURE_PIPELINE=1 go test ./cmd/gx-torrent/ -run MeasurePipelining -v

import (
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"net"
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
		// Dial while this leecher's interface is still the process-wide one: netx
		// keeps the outgoing interface globally (see measure_test.go), so dialing
		// after every daemon exists would give them all one source address.
		d.mu.Lock()
		handle, _ := d.findLocked(hash)
		d.mu.Unlock()
		if err := handle.AddPeer(fmt.Sprintf("127.0.0.1:%d", seeder.peerPort)); err != nil {
			t.Fatalf("add peer: %v", err)
		}
		waitActive(d, hash, 5*time.Second)
		swarm = append(swarm, measureNode{daemon: d, ip: ip, port: d.peerPort})
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

// latencyProxy listens locally and forwards every connection to target,
// releasing each chunk at arrival+delay so it adds RTT without limiting
// bandwidth. This emulates a WAN link without root (no tc/netem) and preserves
// TCP order. Returns the listener (dial it instead of the target) and a stop
// function.
func latencyProxy(target string, delay time.Duration) (net.Listener, func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go proxyConn(c, target, delay)
		}
	}()
	return ln, func() { _ = ln.Close() }
}

func proxyConn(c net.Conn, target string, delay time.Duration) {
	u, err := net.Dial("tcp", target)
	if err != nil {
		_ = c.Close()
		return
	}
	go delayCopy(u, c, delay)
	delayCopy(c, u, delay)
}

func delayCopy(dst, src net.Conn, delay time.Duration) {
	defer dst.Close()
	type chunk struct {
		at   time.Time
		data []byte
	}
	ch := make(chan chunk, 1024)
	go func() {
		for c := range ch {
			if d := time.Until(c.at); d > 0 {
				time.Sleep(d)
			}
			if _, err := dst.Write(c.data); err != nil {
				return
			}
		}
	}()
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			ch <- chunk{at: time.Now().Add(delay), data: append([]byte(nil), buf[:n]...)}
		}
		if err != nil {
			close(ch)
			return
		}
	}
}

// makeTorrentWithPieceLength builds a single-file .torrent with an explicit
// piece length. The default test torrents use 16 KiB pieces (one block), which
// cannot exercise the request queue (pipelining): a bigger piece has several
// 16 KiB blocks, so the queue depth matters.
func makeTorrentWithPieceLength(t *testing.T, dir, name string, size, pieceLength int) []byte {
	t.Helper()
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	var pieces []byte
	for offset := 0; offset < size; offset += pieceLength {
		end := min(offset+pieceLength, size)
		sum := sha1.Sum(payload[offset:end])
		pieces = append(pieces, sum[:]...)
	}
	return bencode(map[string]any{
		"info": map[string]any{
			"name":         name,
			"length":       size,
			"piece length": pieceLength,
			"pieces":       pieces,
		},
	})
}

// TestMeasurePipeliningLatency isolates the adaptive request queue (pipelining)
// behind a controlled RTT: the leecher dials a latency proxy in front of the
// seeder, so a shallow queue (1 block in flight) is limited by RTT while a deep
// one fills the link. This is the isolation the loopback harness cannot give.
func TestMeasurePipeliningLatency(t *testing.T) {
	if os.Getenv("GX_MEASURE_PIPELINE") != "1" {
		t.Skip("set GX_MEASURE_PIPELINE=1 to run the pipelining measurement")
	}
	const size = 4 << 20 // 4 MiB with 256 KiB pieces = 16 blocks each
	const oneWay = 25 * time.Millisecond

	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 44760, PortEnd: 44779, Encryption: 1})
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrentWithPieceLength(t, src, "lat.bin", size, 256<<10)
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })
	ln, stop := latencyProxy(fmt.Sprintf("127.0.0.1:%d", seeder.peerPort), oneWay)
	defer stop()

	profiles := []struct {
		name string
		def  int
		max  int
	}{
		{"shallow", 1, 1},    // one block in flight: RTT-bound
		{"default", 50, 250}, // the engine default
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			leecher := newTestDaemonWith(t, NetworkOptions{
				OutgoingInterface: "127.0.0.9",
				PortBegin:         44960, PortEnd: 44979, Encryption: 1,
				DefaultRequestsOut: p.def, MaxRequestsOut: p.max,
			})
			dst := filepath.Join(t.TempDir(), "dst")
			if _, _, err := leecher.add(addRequest{TorrentData: data, Destination: dst, SeedRatio: -1, SeedDays: -1}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "leecher running", func() bool {
				s := stateOf(leecher, hash)
				return s == "downloading" || s == "stalled"
			})
			leecher.mu.Lock()
			handle, _ := leecher.findLocked(hash)
			leecher.mu.Unlock()
			if err := handle.AddPeer(ln.Addr().String()); err != nil {
				t.Fatalf("add peer: %v", err)
			}
			start := time.Now()
			deadline := start.Add(measureDeadline)
			for time.Now().Before(deadline) {
				if info, _ := findInfo(leecher, hash); info.Progress == 100 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			elapsed := time.Since(start)
			info, _ := findInfo(leecher, hash)
			t.Logf("profile=%s def=%d max=%d rtt=%s downloaded=%d elapsed=%s (%.0f KiB/s) complete=%v",
				p.name, p.def, p.max, 2*oneWay, info.Downloaded, elapsed.Round(time.Millisecond),
				float64(info.Downloaded)/1024/elapsed.Seconds(), info.Progress == 100)
			if info.Progress != 100 {
				t.Fatalf("profile %s did not complete within %s", p.name, measureDeadline)
			}
		})
	}
}
