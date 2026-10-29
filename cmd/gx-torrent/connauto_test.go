package main

// connauto_test.go fixes the machine allowance and the runtime trimming of the
// automatic peer cap (connauto.go).

import (
	"bytes"
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
	"github.com/buzzqw/gextto/internal/logging"
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
		limits := connAutoLimits{perTorrent: base, budget: 1000}
		d.applyAutoConnectionsLocked(at, []*torrent.Torrent{tt}, map[string]torrent.Stats{id: stats}, limits)
	}
	downloading := func(peers int, completed int64) torrent.Stats {
		st := torrent.Stats{Status: torrent.Downloading}
		st.Peers.Total = peers
		st.Bytes.Completed = completed
		return st
	}

	apply(downloading(0, 0), 50, now)
	if got := capOf(); got != 50 {
		t.Fatalf("cap after the first pass = %d, want 50", got)
	}

	// The automatic pass never overwrites a limit the operator chose.
	own := int64(7)
	if err := d.setConnLimits(hash, &own, nil); err != nil {
		t.Fatal(err)
	}
	apply(downloading(0, 0), 50, now.Add(time.Second))
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
	// A trickle below the progress threshold does not count as progress.
	apply(downloading(12, 1000), 50, now.Add(2*time.Second))
	if got := capOf(); got != 25 {
		t.Fatalf("cap on a silent torrent with 12 peers = %d, want 25", got)
	}

	// Progress resumes: back to the machine allowance.
	apply(downloading(12, 1<<20), 50, now.Add(3*time.Second))
	if got := capOf(); got != 50 {
		t.Fatalf("cap after progress resumed = %d, want 50", got)
	}

	// A seed asks for half, and the floor once nobody downloads from it.
	seed := downloading(12, 1<<20)
	seed.Status = torrent.Seeding
	apply(seed, 50, now.Add(4*time.Second))
	if got := capOf(); got != 25 {
		t.Fatalf("cap on a seed = %d, want 25", got)
	}
	d.mu.Lock()
	d.connAutoState[id].lastProgress = now.Add(-3 * time.Minute)
	d.mu.Unlock()
	apply(seed, 50, now.Add(5*time.Second))
	if got := capOf(); got != connAutoMinPeers {
		t.Fatalf("cap on an idle seed = %d, want %d", got, connAutoMinPeers)
	}
}

func TestAutoConnectionsBudgetScalesWithTheMachine(t *testing.T) {
	const gib = int64(1) << 30
	cases := []struct {
		name         string
		cores        int
		total, avail int64
		expected     int
	}{
		{"four cores", 4, 16 * gib, 4 * gib, 300},
		{"one core", 1, 16 * gib, 4 * gib, 75},
		{"small host", 4, 2 * gib, gib, 150},
		{"under memory pressure", 4, 16 * gib, 256 << 20, 150},
		{"floor", 1, gib, 256 << 20, 4 * connAutoMinPeers},
	}
	for _, tc := range cases {
		if got := autoConnectionsBudget(tc.cores, tc.total, tc.avail); got != tc.expected {
			t.Errorf("%s: budget %d, want %d", tc.name, got, tc.expected)
		}
	}
}

// TestShareConnections fixes how the session budget is split: untouched when
// it covers every ask, by weight (downloads twice) when it does not, a small
// ask granted whole with its leftover shared again, never under the floor.
func TestShareConnections(t *testing.T) {
	targets := func(budget int, asks ...connAutoAsk) []int {
		shareConnections(asks, budget)
		out := make([]int, len(asks))
		for i, a := range asks {
			out[i] = a.target
		}
		return out
	}
	dl := func(ask int) connAutoAsk { return connAutoAsk{ask: ask, weight: 2} }
	seed := func(ask int) connAutoAsk { return connAutoAsk{ask: ask, weight: 1} }

	if got := targets(300, dl(55)); got[0] != 55 {
		t.Fatalf("a single torrent got %d, want its whole allowance", got[0])
	}
	if got := targets(300, dl(55), dl(55), seed(27)); got[0] != 55 || got[2] != 27 {
		t.Fatalf("a budget that covers the asks changed them: %v", got)
	}
	// Ten downloads and ten seeds on a 300 budget: weight 30, downloads get
	// 2/30, seeds 1/30 — but a seed asks 15 at most, so its leftover is
	// shared among the downloads.
	asks := []connAutoAsk{}
	for range 10 {
		asks = append(asks, dl(55))
	}
	for range 10 {
		asks = append(asks, seed(15))
	}
	got := targets(300, asks...)
	sum := 0
	for i, v := range got {
		sum += v
		if v < connAutoMinPeers {
			t.Fatalf("torrent %d under the floor: %v", i, got)
		}
	}
	if got[0] != 15 || got[10] != 15 {
		t.Fatalf("shares = %v, want 15 for downloads and seeds", got)
	}
	if sum > 300 {
		t.Fatalf("shares exceed the budget: %d > 300 (%v)", sum, got)
	}
	// Four downloads and twenty seeds: the seeds take their floor and the
	// downloads share what is left.
	asks = asks[:0]
	for range 4 {
		asks = append(asks, dl(55))
	}
	for range 20 {
		asks = append(asks, seed(27))
	}
	got = targets(300, asks...)
	if got[0] < 15 || got[0] > 55 || got[4] != 15 {
		t.Fatalf("shares = %v, want the floor for the seeds", got)
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
	expected := autoConnectionsPerTorrent(connAutoCores(), memoryTotal(), memoryAvailable())
	d.mu.Lock()
	tt, _ := d.findLocked(hash)
	got := tt.MaxConnections()
	d.mu.Unlock()
	// The torrent seeds: it asks for half the allowance.
	if expected = max(connAutoMinPeers, expected/2); got != expected {
		t.Fatalf("cap = %d, want half the machine allowance %d", got, expected)
	}
}

// int64Ptr is the test-side counterpart of queue.Int64Ptr.
func int64Ptr(v int64) *int64 { return &v }

// TestAutoConnectionsPassDueOnNewTorrent: the pass waits for its clock, except
// when the number of torrents changed (a new torrent must not run uncapped).
func TestAutoConnectionsPassDueOnNewTorrent(t *testing.T) {
	d := newTestDaemon(t)
	d.mu.Lock()
	d.state.Config.ConnAuto = queue.BoolPtr(true)
	d.mu.Unlock()
	now := time.Now()
	if d.autoConnectionsBase(now, 2).perTorrent == 0 {
		t.Fatal("first pass not due")
	}
	if d.autoConnectionsBase(now.Add(time.Second), 2).perTorrent != 0 {
		t.Fatal("pass due before its clock with the same torrents")
	}
	if d.autoConnectionsBase(now.Add(2*time.Second), 3).perTorrent == 0 {
		t.Fatal("a new torrent did not make the pass due")
	}
}

// TestLogAutoConnectionChangesGroups: one pass that moves many torrents to the
// same cap writes one line, a lone change keeps its own.
func TestLogAutoConnectionChangesGroups(t *testing.T) {
	var buf bytes.Buffer
	previous, flags := log.Writer(), log.Flags()
	previousLevel := daemonLogLevel
	log.SetOutput(&buf)
	log.SetFlags(0)
	daemonLogLevel = logging.LevelDebug
	t.Cleanup(func() { log.SetOutput(previous); log.SetFlags(flags); daemonLogLevel = previousLevel })
	changes := []connAutoChange{{name: "solo", from: 55, to: 27, reason: "seeding"}}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		changes = append(changes, connAutoChange{name: name, from: 37, to: 33, reason: "machine allowance, shared with 9 running torrents"})
	}
	logAutoConnectionChanges(changes)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q, want 2", lines)
	}
	if !strings.Contains(lines[0], "«solo»: 55 → 27") || !strings.Contains(lines[1], "peer cap 33 for 5 torrents") || !strings.Contains(lines[1], "+2") {
		t.Fatalf("lines = %q", lines)
	}
}
