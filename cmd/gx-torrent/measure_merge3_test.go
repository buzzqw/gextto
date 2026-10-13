package main

// measure_merge3_test.go measures the swarm merge Phase 3 (block-granularity
// reuse, docs/evoluzione.md §16.1) on real data: a public ISO and two v2
// torrents of it with different piece lengths, prepared by
// scripts/phase3-env.sh. Opt-in, outside CI:
//
//	scripts/phase3-env.sh ~/gx-phase3
//	GX_MEASURE_MERGE3=~/gx-phase3/debian go test ./cmd/gx-torrent/ -run MeasureMergePhase3 -v -timeout 30m
//
// A seeder holds the destination torrent with the full ISO. The leecher holds
// the source torrent over a copy of the ISO with one source piece in four
// corrupted, so it verifies only part of it; then the destination torrent is
// added to the leecher, merges what it can from the source and downloads the
// rest from the seeder. The figure that matters is how many bytes came from
// the network.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMeasureMergePhase3(t *testing.T) {
	dir := os.Getenv("GX_MEASURE_MERGE3")
	if dir == "" {
		t.Skip("set GX_MEASURE_MERGE3=<dir prepared by scripts/phase3-env.sh>")
	}
	srcTorrent, err := os.ReadFile(filepath.Join(dir, "src.torrent"))
	if err != nil {
		t.Fatal(err)
	}
	dstTorrent, err := os.ReadFile(filepath.Join(dir, "dst.torrent"))
	if err != nil {
		t.Fatal(err)
	}
	waitLong := func(what string, limit time.Duration, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(limit)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	// GX_MEASURE_MERGE3_PEER=host:port uses an external seeder instead (e.g.
	// libtorrent, scripts/phase3-seed.py), to check interoperability.
	peerAddr := os.Getenv("GX_MEASURE_MERGE3_PEER")
	if peerAddr == "" {
		seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 45760, PortEnd: 45779, Encryption: 1})
		hash, _, err := seeder.add(addRequest{TorrentData: dstTorrent, Destination: filepath.Join(dir, "seed"), SeedRatio: -1, SeedDays: -1})
		if err != nil {
			t.Fatal(err)
		}
		waitLong("seeder seeding", 10*time.Minute, func() bool { return stateOf(seeder, hash) == "seeding" })
		peerAddr = fmt.Sprintf("127.0.0.1:%d", seeder.peerPort)
	}

	leecher := newTestDaemonWith(t, NetworkOptions{OutgoingInterface: "127.0.0.9", PortBegin: 45960, PortEnd: 45979, Encryption: 1})
	srcHash, _, err := leecher.add(addRequest{TorrentData: srcTorrent, Destination: filepath.Join(dir, "src"), SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitLong("source verified", 10*time.Minute, func() bool {
		s := stateOf(leecher, srcHash)
		return s == "downloading" || s == "stalled" || s == "seeding"
	})
	srcInfo, _ := findInfo(leecher, srcHash)
	t.Logf("source: %.1f%% verified (%d of %d bytes)", srcInfo.Progress, srcInfo.TotalDone, srcInfo.TotalSize)

	before := leecher.session.Stats()
	out := filepath.Join(t.TempDir(), "dst")
	start := time.Now()
	dstHash, _, err := leecher.add(addRequest{TorrentData: dstTorrent, Destination: out, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitLong("destination running", 10*time.Minute, func() bool {
		s := stateOf(leecher, dstHash)
		return s == "downloading" || s == "stalled" || s == "seeding"
	})
	mergeTime := time.Since(start)
	afterMerge, _ := findInfo(leecher, dstHash)
	leecher.mu.Lock()
	handle, _ := leecher.findLocked(dstHash)
	leecher.mu.Unlock()
	if err := handle.AddPeer(peerAddr); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	waitLong("destination complete", 20*time.Minute, func() bool {
		info, _ := findInfo(leecher, dstHash)
		return info.Progress == 100
	})
	elapsed := time.Since(start)
	info, _ := findInfo(leecher, dstHash)
	after := leecher.session.Stats()
	t.Logf("destination: size=%d whole-pieces-after-merge=%d (%.1f%%) merge+check=%s",
		info.TotalSize, afterMerge.TotalDone, afterMerge.Progress, mergeTime.Round(time.Millisecond))
	t.Logf("network downloaded=%d (%.1f%% of the file) total=%s merged_bytes+=%d merged_pieces+=%d partial_pieces+=%d",
		info.Downloaded, 100*float64(info.Downloaded)/float64(info.TotalSize), elapsed.Round(time.Millisecond),
		after.MergedBytes-before.MergedBytes, after.MergedPieces-before.MergedPieces, after.MergedPartialPieces-before.MergedPartialPieces)
}
