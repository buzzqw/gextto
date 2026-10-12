package main

import (
	"os"
	"testing"
	"time"
)

// TestManualRealTorrent downloads a real, well-seeded public torrent to
// validate the engine end to end (in particular the ranged mmap flush under
// real concurrent piece writes). Opt-in: it never runs in CI.
//
//	GX_REAL_TORRENT=/path/to/file.torrent GX_MMAP=1 \
//	  go test -count=1 -run TestManualRealTorrent -v ./cmd/gx-torrent/
//
// A well-seeded example is the current Debian netinst torrent from
// https://cdimage.debian.org/debian-cd/current/amd64/bt-cd/.
func TestManualRealTorrent(t *testing.T) {
	path := os.Getenv("GX_REAL_TORRENT")
	if path == "" {
		t.Skip("set GX_REAL_TORRENT to a .torrent file to run")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d := newTestDaemonWith(t, NetworkOptions{
		PortBegin: 43900, PortEnd: 43999, Encryption: 1, DHT: true, UTP: true,
	})
	dst := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: dst, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("added %s, downloading...", hash)
	deadline := time.Now().Add(15 * time.Minute)
	last := -1
	for time.Now().Before(deadline) {
		info, ok := findInfo(d, hash)
		if !ok {
			t.Fatal("torrent disappeared")
		}
		if int(info.Progress) != last {
			last = int(info.Progress)
			t.Logf("progress %.1f%% down=%d up=%d", info.Progress, info.Downloaded, info.Uploaded)
		}
		if info.Progress >= 100 {
			t.Logf("completed: %s (%.1f%%, down=%d bytes)", hash, info.Progress, info.Downloaded)
			return
		}
		time.Sleep(time.Second)
	}
	info, _ := findInfo(d, hash)
	t.Fatalf("timeout: %.1f%% down=%d", info.Progress, info.Downloaded)
}
