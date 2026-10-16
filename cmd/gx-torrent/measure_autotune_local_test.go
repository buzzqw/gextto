package main

// measure_autotune_local_test.go is the controlled half of the auto-tune
// benchmark: a local swarm (several seeders on loopback) with the leecher's
// per-torrent connection cap swept, so the parameter is the only variable. On a
// public swarm the peers one happens to reach dominate, so it cannot rank the
// parameters; here it can. Opt-in, outside CI:
//
//	GX_MEASURE_AUTOTUNE_LOCAL=1 \
//	  GX_MEASURE_AUTOTUNE_LOCAL_SEEDERS=8 \
//	  GX_MEASURE_AUTOTUNE_LOCAL_SIZE_MB=64 \
//	  go test ./cmd/gx-torrent/ -run TestMeasureAutotuneLocal -v -count=1 -timeout 20m

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMeasureAutotuneLocal(t *testing.T) {
	if os.Getenv("GX_MEASURE_AUTOTUNE_LOCAL") != "1" {
		t.Skip("set GX_MEASURE_AUTOTUNE_LOCAL=1 to run the controlled auto-tune sweep")
	}
	size := 64 << 20
	if raw := os.Getenv("GX_MEASURE_AUTOTUNE_LOCAL_SIZE_MB"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			size = n << 20
		}
	}
	seeders := measureCount("GX_MEASURE_AUTOTUNE_LOCAL_SEEDERS", 8)
	caps := []int{1, 2, 4, 8, 0}
	if raw := os.Getenv("GX_MEASURE_AUTOTUNE_CAPS"); raw != "" {
		caps = nil
		for _, part := range strings.Split(raw, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n >= 0 {
				caps = append(caps, n)
			}
		}
	}

	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "measure.bin", size)

	// Several seeders each hold the whole corpus, so a capped leecher has more
	// peers available than any cap we test.
	hash := ""
	var seederNodes []measureNode
	for i := 0; i < seeders; i++ {
		net := NetworkOptions{
			ListenInterface: "127.0.0.1",
			PortBegin:       uint16(48000 + i*20), PortEnd: uint16(48019 + i*20),
			Encryption: 1,
		}
		s := newTestDaemonWith(t, net)
		h, _, err := s.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
		if err != nil {
			t.Fatalf("seeder %d: %v", i, err)
		}
		hash = h
		waitFor(t, fmt.Sprintf("seeder %d seeding", i), func() bool { return stateOf(s, h) == "seeding" })
		seederNodes = append(seederNodes, measureNode{daemon: s, ip: "127.0.0.1", port: s.peerPort})
	}

	baseRSS, _ := procSelfSample()
	for _, cap := range caps {
		cap := cap
		name := "illimitato"
		if cap > 0 {
			name = "cap " + strconv.Itoa(cap)
		}
		t.Run(name, func(t *testing.T) {
			l := newTestDaemonWith(t, NetworkOptions{
				OutgoingInterface: "127.0.0.9", ListenInterface: "127.0.0.9",
				PortBegin: 48900, PortEnd: 48999, Encryption: 1,
			})
			if cap > 0 {
				l.state.Config.MaxConnectionsPerTorrent = cap
			}
			dst := filepath.Join(t.TempDir(), "dst")
			if _, _, err := l.add(addRequest{TorrentData: data, Destination: dst, SeedRatio: -1, SeedDays: -1}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "leecher running", func() bool {
				state := stateOf(l, hash)
				return state == "downloading" || state == "stalled"
			})
			// Dial every seeder from this leecher while its outgoing interface is
			// the process-wide one.
			for _, sn := range seederNodes {
				l.mu.Lock()
				handle, _ := l.findLocked(hash)
				l.mu.Unlock()
				if err := handle.AddPeer(sn.addr()); err != nil {
					t.Fatalf("dial %s: %v", sn.addr(), err)
				}
			}
			start := time.Now()
			peakPeers := 0
			for time.Since(start) < measureDeadline {
				info, _ := findInfo(l, hash)
				if info.NumPeers > peakPeers {
					peakPeers = info.NumPeers
				}
				if info.Progress == 100 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			elapsed := time.Since(start)
			rssKB, _ := procSelfSample()
			rateMiB := float64(size) / elapsed.Seconds() / (1 << 20)
			t.Logf("autotune-local %s: %d MiB in %s (%.0f MiB/s) · peer max %d · RSS delta %.0f MB",
				name, size>>20, elapsed.Round(time.Millisecond), rateMiB, peakPeers, float64(rssKB-baseRSS)/1024)
		})
	}
}
