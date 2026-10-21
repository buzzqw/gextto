package main

// connauto_test.go fixes the machine allowance and the runtime trimming of the
// automatic peer cap (connauto.go).

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
	"github.com/buzzqw/gextto/internal/queue"
)

func TestAutoConnectionsPerTorrentScalesWithTheMachine(t *testing.T) {
	const (
		gib = int64(1) << 30
	)
	cases := []struct {
		name     string
		cores    int
		total    int64
		avail    int64
		expected int
	}{
		{"four cores", 4, 16 * gib, 4 * gib, 55},
		{"one core", 1, 16 * gib, 4 * gib, 25},
		{"sixteen cores", 16, 32 * gib, 8 * gib, 175},
		{"clamped at the ceiling", 64, 64 * gib, 32 * gib, connAutoMaxPeers},
		{"small host", 4, 2 * gib, gib, 30},
		{"under memory pressure", 4, 16 * gib, 256 << 20, 27},
		{"no core information", 0, 16 * gib, 4 * gib, 25},
	}
	for _, tc := range cases {
		if got := autoConnectionsPerTorrent(tc.cores, tc.total, tc.avail); got != tc.expected {
			t.Errorf("%s: %d cores/%d GiB avail %d MiB -> %d, want %d",
				tc.name, tc.cores, tc.total>>30, tc.avail>>20, got, tc.expected)
		}
	}
}

// TestDaemonAutoConnectionsFollowProgress exercises the pass end to end on a
// real torrent: the machine allowance is written in place, an explicit
// per-torrent cap wins, and a torrent with peers and no progress is trimmed.
func TestDaemonAutoConnectionsFollowProgress(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "payload.bin", 100_000), Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	capOf := func() int {
		d.mu.Lock()
		defer d.mu.Unlock()
		tt, _ := d.findLocked(hash)
		if tt == nil {
			return -1
		}
		return tt.MaxConnections()
	}
	d.mu.Lock()
	first, _ := d.findLocked(hash)
	id := first.ID()
	d.mu.Unlock()

	d.mu.Lock()
	d.state.Config.ConnAuto = queue.BoolPtr(true)
	// Keep the background pass out of the way: this test drives the pass by hand.
	d.connAutoAt = time.Now().Add(time.Hour)
	d.mu.Unlock()

	now := time.Now()
	handle := func() *torrent.Torrent {
		d.mu.Lock()
		defer d.mu.Unlock()
		tt, _ := d.findLocked(hash)
		return tt
	}
	apply := func(stats torrent.Stats, base int, at time.Time) {
		tt := handle()
		d.mu.Lock()
		defer d.mu.Unlock()
		d.applyAutoConnectionsLocked(at, []*torrent.Torrent{tt}, map[string]torrent.Stats{id: stats}, base)
	}

	apply(torrent.Stats{}, 50, now)
	if got := capOf(); got != 50 {
		t.Fatalf("cap after the first pass = %d, want 50", got)
	}

	// The automatic pass never overwrites a limit the operator chose.
	own := int64(7)
	if err := d.setConnLimits(hash, &own, nil); err != nil {
		t.Fatal(err)
	}
	apply(torrent.Stats{}, 50, now.Add(time.Second))
	if got := capOf(); got != 7 {
		t.Fatalf("explicit cap overwritten: %d, want 7", got)
	}
	if err := d.setConnLimits(hash, int64Ptr(-1), nil); err != nil {
		t.Fatal(err)
	}

	// Peers but no progress for two minutes: half the allowance.
	d.mu.Lock()
	d.connAutoState[id].lastProgress = now.Add(-3 * time.Minute)
	d.mu.Unlock()
	stalled := torrent.Stats{}
	stalled.Peers.Total = 12
	apply(stalled, 50, now.Add(2*time.Second))
	if got := capOf(); got != 25 {
		t.Fatalf("cap on a silent torrent with 12 peers = %d, want 25", got)
	}

	// Progress resumes: back to the machine allowance.
	moving := torrent.Stats{}
	moving.Peers.Total = 12
	moving.Speed.Download = 1000
	apply(moving, 50, now.Add(3*time.Second))
	if got := capOf(); got != 50 {
		t.Fatalf("cap after progress resumed = %d, want 50", got)
	}
}

// TestDaemonAutoConnectionsAppliedByTheTick checks the wiring: turning the
// policy on makes the queue loop size the cap without a restart.
func TestDaemonAutoConnectionsAppliedByTheTick(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "payload.bin", 100_000), Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	raw, _ := json.Marshal(true)
	d.mu.Lock()
	d.connAutoAt = time.Time{}
	d.mu.Unlock()
	if _, err := d.setConfig(map[string]json.RawMessage{"conn_auto": raw}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "automatic cap", func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		tt, _ := d.findLocked(hash)
		return tt != nil && tt.MaxConnections() > 0
	})
	expected := autoConnectionsPerTorrent(runtime.NumCPU(), memoryTotal(), memoryAvailable())
	d.mu.Lock()
	tt, _ := d.findLocked(hash)
	got := tt.MaxConnections()
	d.mu.Unlock()
	if got != expected {
		t.Fatalf("cap = %d, want the machine allowance %d", got, expected)
	}
}

// int64Ptr is the test-side counterpart of queue.Int64Ptr.
func int64Ptr(v int64) *int64 { return &v }
