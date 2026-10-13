package main

// measure_merge_resume_test.go measures the Swarm Merging resume path on real
// data: the same public-ISO environment as measure_merge3_test.go
// (scripts/phase3-env.sh) but the destination is paused and the leecher's
// session is restarted, so the destination reloads from its resume data and the
// merge must run again to keep the block-level reuse. Opt-in, outside CI:
//
//	scripts/phase3-env.sh ~/gx-phase3
//	GX_MEASURE_MERGE3=~/gx-phase3/debian go test ./cmd/gx-torrent/ \
//	    -run MeasureMergeResumePath -v -timeout 40m
//
// The figure that matters is how many bytes the destination still takes from
// the network after the restart: with the resume-path merge it stays at the
// Phase 3 level (~25% on the prepared data); without it the destination
// re-downloads every block of the not-yet-complete pieces (~100%).

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMeasureMergeResumePath(t *testing.T) {
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

	// Leecher with a stable data dir, so a second daemon can reload its session.
	data := t.TempDir()
	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     NetworkOptions{OutgoingInterface: "127.0.0.9", PortBegin: 45960, PortEnd: 45979, Encryption: 1},
		Tick:        50 * time.Millisecond,
		ProbeWindow: time.Minute,
	}
	boot := func() (*Daemon, func()) {
		d, err := newDaemon(opts)
		if err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() { d.run(stop); close(done) }()
		return d, func() { close(stop); <-done; d.close() }
	}
	dstOut := filepath.Join(t.TempDir(), "dst")

	// First run: source verified, destination merged (no peer yet), then paused
	// so its resume data (bitfield included) is written.
	d1, stop1 := boot()
	srcHash, _, err := d1.add(addRequest{TorrentData: srcTorrent, Destination: filepath.Join(dir, "src"), SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitLong("source verified", 10*time.Minute, func() bool { return running(d1, srcHash) })
	srcInfo, _ := findInfo(d1, srcHash)
	t.Logf("source: %.1f%% verified", srcInfo.Progress)

	dstHash, _, err := d1.add(addRequest{TorrentData: dstTorrent, Destination: dstOut, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitLong("destination merged", 10*time.Minute, func() bool { return running(d1, dstHash) })
	firstMerge, _ := findInfo(d1, dstHash)
	if err := d1.pause(dstHash); err != nil {
		t.Fatalf("pause destination: %v", err)
	}
	stop1()

	// Second run: the destination reloads from its resume bitfield; the source
	// verifies again. Only now is the seeder added as a peer.
	d2, stop2 := boot()
	defer stop2()
	waitLong("source ready after restart", 10*time.Minute, func() bool { return running(d2, srcHash) })
	before := d2.session.Stats()
	if err := d2.resume(dstHash); err != nil {
		t.Fatalf("resume destination: %v", err)
	}
	waitLong("destination running after restart", 10*time.Minute, func() bool { return running(d2, dstHash) })
	afterMerge, _ := findInfo(d2, dstHash)
	d2.mu.Lock()
	handle, _ := d2.findLocked(dstHash)
	d2.mu.Unlock()
	if err := handle.AddPeer(peerAddr); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	start := time.Now()
	waitLong("destination complete", 20*time.Minute, func() bool {
		info, _ := findInfo(d2, dstHash)
		return info.Progress == 100
	})
	elapsed := time.Since(start)
	info, _ := findInfo(d2, dstHash)
	after := d2.session.Stats()

	t.Logf("first run before restart: merged whole pieces=%d (%.1f%% of the file) with no peer",
		firstMerge.TotalDone, firstMerge.Progress)
	t.Logf("after restart: merged whole pieces=%d (%.1f%%), merged_bytes+=%d merged_pieces+=%d partial_pieces+=%d",
		afterMerge.TotalDone, afterMerge.Progress,
		after.MergedBytes-before.MergedBytes, after.MergedPieces-before.MergedPieces, after.MergedPartialPieces-before.MergedPartialPieces)
	network := info.Downloaded - afterMerge.Downloaded
	t.Logf("network downloaded after restart=%d (%.1f%% of the file) total=%s",
		network, 100*float64(network)/float64(info.TotalSize), elapsed.Round(time.Millisecond))
}
