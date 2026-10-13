package main

// measure_merge_later_test.go measures the Swarm Merging later-sibling trigger
// on real data: the same public-ISO environment as measure_merge3_test.go
// (scripts/phase3-env.sh). The destination is added *before* the source and with
// no peer, so at allocation there is nothing to reuse; when the source is added
// afterwards it publishes its data and wakes the destination, which then merges
// it. Opt-in, outside CI:
//
//	scripts/phase3-env.sh ~/gx-phase3
//	GX_MEASURE_MERGE3=~/gx-phase3/debian go test ./cmd/gx-torrent/ \
//	    -run MeasureMergeLaterSibling -v -timeout 40m
//
// The figure is how many bytes the destination takes from the network: with the
// later trigger it stays at the Phase 3 level (~25% on the prepared data);
// without it the destination downloads the whole file (~100%).

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitUpTo polls check for up to limit and reports whether it became true.
func waitUpTo(limit time.Duration, check func() bool) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func TestMeasureMergeLaterSibling(t *testing.T) {
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
	running := func(d *Daemon, hash string) bool {
		s := stateOf(d, hash)
		return s == "downloading" || s == "stalled" || s == "seeding"
	}

	// External seeder with the full destination payload.
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 45760, PortEnd: 45779, Encryption: 1})
	seedHash, _, err := seeder.add(addRequest{TorrentData: dstTorrent, Destination: filepath.Join(dir, "seed"), SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitLong("seeder seeding", 10*time.Minute, func() bool { return stateOf(seeder, seedHash) == "seeding" })
	peerAddr := fmt.Sprintf("127.0.0.1:%d", seeder.peerPort)

	leecher := newTestDaemonWith(t, NetworkOptions{OutgoingInterface: "127.0.0.9", PortBegin: 45960, PortEnd: 45979, Encryption: 1})

	// Destination first, with no sibling and no peer: it can only download.
	out := filepath.Join(t.TempDir(), "dst")
	dstHash, _, err := leecher.add(addRequest{TorrentData: dstTorrent, Destination: out, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitLong("destination running", 10*time.Minute, func() bool { return running(leecher, dstHash) })
	before := leecher.session.Stats()

	// Then the source: it verifies, publishes and wakes the destination.
	srcHash, _, err := leecher.add(addRequest{TorrentData: srcTorrent, Destination: filepath.Join(dir, "src"), SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitLong("source verified", 10*time.Minute, func() bool { return running(leecher, srcHash) })
	srcInfo, _ := findInfo(leecher, srcHash)
	t.Logf("source: %.1f%% verified", srcInfo.Progress)

	// The destination must now merge the sibling that appeared after it.
	// Bounded wait (not a failure): without the later trigger nothing merges
	// and the destination would take the whole file from the network, which is
	// exactly the figure this measurement contrasts.
	merged := waitUpTo(30*time.Second, func() bool {
		return leecher.session.Stats().MergedBytes > before.MergedBytes
	})
	t.Logf("later merge triggered: %v", merged)
	afterMerge, _ := findInfo(leecher, dstHash)
	leecher.mu.Lock()
	handle, _ := leecher.findLocked(dstHash)
	leecher.mu.Unlock()
	if err := handle.AddPeer(peerAddr); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	networkBefore := afterMerge.Downloaded
	start := time.Now()
	waitLong("destination complete", 20*time.Minute, func() bool {
		info, _ := findInfo(leecher, dstHash)
		return info.Progress == 100
	})
	elapsed := time.Since(start)
	info, _ := findInfo(leecher, dstHash)
	after := leecher.session.Stats()

	t.Logf("after trigger: merged whole pieces=%d (%.1f%%), merged_bytes+=%d merged_pieces+=%d partial_pieces+=%d",
		afterMerge.TotalDone, afterMerge.Progress,
		after.MergedBytes-before.MergedBytes, after.MergedPieces-before.MergedPieces, after.MergedPartialPieces-before.MergedPartialPieces)
	network := info.Downloaded - networkBefore
	t.Logf("network downloaded=%d (%.1f%% of the file) total=%s",
		network, 100*float64(network)/float64(info.TotalSize), elapsed.Round(time.Millisecond))
}
