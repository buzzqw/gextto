package main

// measure_autotune_test.go is the auto-tune benchmark battery: it downloads a
// public torrent with different per-torrent connection caps (the value the guided
// tuning picks between 50 and 400) and reports throughput, peers, RSS and CPU, so
// we can tell whether the auto-tuned bound helps or cripples the transfer.
// Opt-in, outside CI:
//
//	GX_MEASURE_AUTOTUNE=/path/to/public.torrent \
//	  GX_MEASURE_AUTOTUNE_SECONDS=45 \
//	  go test ./cmd/gx-torrent/ -run TestMeasureAutotune -v -count=1 -timeout 30m
//
// A public swarm is noisy: the numbers are indicative, and an arm that never saw
// a peer is reported as such instead of being read as a result.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func procSelfSample() (rssKB int64, cpuTicks int64) {
	if data, err := os.ReadFile("/proc/self/statm"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 2 {
			if pages, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				rssKB = pages * 4
			}
		}
	}
	if data, err := os.ReadFile("/proc/self/stat"); err == nil {
		// The comm can contain spaces; cut to the first ')' so the field indices
		// after it are stable (utime = 14, stime = 15, 1-based).
		if close := strings.LastIndexByte(string(data), ')'); close >= 0 {
			fields := strings.Fields(string(data)[close+1:])
			if len(fields) >= 13 {
				utime, _ := strconv.ParseInt(fields[11], 10, 64)
				stime, _ := strconv.ParseInt(fields[12], 10, 64)
				cpuTicks = utime + stime
			}
		}
	}
	return rssKB, cpuTicks
}

func TestMeasureAutotune(t *testing.T) {
	torrentPath := os.Getenv("GX_MEASURE_AUTOTUNE")
	if torrentPath == "" {
		t.Skip("set GX_MEASURE_AUTOTUNE=<public torrent file> to run the auto-tune benchmark")
	}
	window := 45 * time.Second
	if raw := os.Getenv("GX_MEASURE_AUTOTUNE_SECONDS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			window = time.Duration(n) * time.Second
		}
	}
	data, err := os.ReadFile(torrentPath)
	if err != nil {
		t.Fatal(err)
	}

	// The caps to sweep: the auto-tune bound is 50..400, so we test below, at
	// and above it, plus unlimited (0) as the control. GX_MEASURE_AUTOTUNE_CAPS
	// overrides the list ("20,50,100,200,400,800,0").
	caps := []int{50, 400, 0}
	if raw := os.Getenv("GX_MEASURE_AUTOTUNE_CAPS"); raw != "" {
		caps = nil
		for _, part := range strings.Split(raw, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n >= 0 {
				caps = append(caps, n)
			}
		}
	}
	repeat := 1
	if raw := os.Getenv("GX_MEASURE_AUTOTUNE_REPEAT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			repeat = n
		}
	}

	type arm struct {
		name        string
		connections int
		portBase    int
	}
	var arms []arm
	port := 45900
	for pass := 1; pass <= repeat; pass++ {
		for _, cap := range caps {
			name := "illimitato"
			if cap > 0 {
				name = "cap " + strconv.Itoa(cap)
			} else {
				cap = 0
			}
			if repeat > 1 {
				name += " · giro " + strconv.Itoa(pass)
			}
			arms = append(arms, arm{name: name, connections: cap, portBase: port})
			port += 20
			if port > 47000 {
				port = 45900
			}
		}
	}

	baseRSS, _ := procSelfSample()
	for _, arm := range arms {
		arm := arm
		t.Run(arm.name, func(t *testing.T) {
			startCPU := int64(0)
			_, startCPU = procSelfSample()
			d := newTestDaemonWith(t, NetworkOptions{
				PortBegin: uint16(arm.portBase), PortEnd: uint16(arm.portBase + 19),
				Encryption: 1, DHT: true, UTP: true, PEX: true,
			})
			if arm.connections > 0 {
				d.state.Config.MaxConnectionsPerTorrent = arm.connections
			}
			dst := filepath.Join(t.TempDir(), "dl")
			hash, _, err := d.add(addRequest{TorrentData: data, Destination: dst, SeedRatio: -1, SeedDays: -1})
			if err != nil {
				t.Fatal(err)
			}

			var firstBytes, lastBytes int64
			var sumRate, samples, peakRate int64
			peakPeers := 0
			deadline := time.Now().Add(window)
			for time.Now().Before(deadline) {
				if info, ok := findInfo(d, hash); ok {
					if firstBytes == 0 && info.TotalDone > 0 {
						firstBytes = info.TotalDone
					}
					lastBytes = info.TotalDone
					if info.DownloadRate > peakRate {
						peakRate = info.DownloadRate
					}
					if info.DownloadRate > 0 {
						sumRate += info.DownloadRate
						samples++
					}
					if info.NumPeers > peakPeers {
						peakPeers = info.NumPeers
					}
				}
				time.Sleep(2 * time.Second)
			}
			rssKB, endCPU := procSelfSample()
			avgRate := int64(0)
			if samples > 0 {
				avgRate = sumRate / samples
			}
			t.Logf("autotune %s: scaricato %d B in %v · media %.0f KiB/s · picco %.0f KiB/s · peer max %d · RSS %.0f MB (delta %.0f MB) · CPU %d ms",
				arm.name, lastBytes-firstBytes, window, float64(avgRate)/1024, float64(peakRate)/1024, peakPeers,
				float64(rssKB)/1024, float64(rssKB-baseRSS)/1024, (endCPU-startCPU)*10)
			if peakPeers == 0 {
				t.Logf("autotune %s: NESSUN peer — la corsa non dice nulla (rete/DHT/swarm)", arm.name)
			}
		})
	}
}
