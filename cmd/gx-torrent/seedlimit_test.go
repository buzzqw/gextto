package main

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// TestSeedLimitReached covers the global seeding limits and the per-torrent
// override (including the "global" -1 and "infinite" 0 sentinels).
func TestSeedLimitReached(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	meta := &torrentMeta{SeedRatio: -1, SeedDays: -1}
	stats := torrent.Stats{}
	stats.Bytes.Total = 1000
	stats.Bytes.Uploaded = 500
	stats.SeededFor = 30 * time.Minute

	// No global limit: never reached.
	if _, reached := d.seedLimitReached(meta, stats); reached {
		t.Fatal("no limits configured, but reached")
	}

	// Global ratio 0.4: reached at 0.5, with the removal action.
	d.opts.Settings.Set(maxRatioEnabledKey, "true")
	d.opts.Settings.Set(maxRatioKey, "0.4")
	if remove, reached := d.seedLimitReached(meta, stats); !reached || remove {
		t.Fatalf("ratio 0.4: reached=%v remove=%v", reached, remove)
	}
	d.opts.Settings.Set(maxRatioActKey, "1")
	if remove, reached := d.seedLimitReached(meta, stats); !reached || !remove {
		t.Fatalf("ratio 0.4 remove action: reached=%v remove=%v", reached, remove)
	}
	d.opts.Settings.Set(maxRatioActKey, "0")
	d.opts.Settings.Set(maxRatioKey, "2")
	if _, reached := d.seedLimitReached(meta, stats); reached {
		t.Fatal("ratio 2 must not be reached at 0.5")
	}

	// A per-torrent infinite ratio (0) wins over the global.
	meta.SeedRatio = 0
	d.opts.Settings.Set(maxRatioKey, "0.1")
	if _, reached := d.seedLimitReached(meta, stats); reached {
		t.Fatal("a per-torrent infinite ratio must win")
	}

	// Global seeding time: 20 minutes, 30 minutes seeded.
	meta.SeedRatio = -1
	d.opts.Settings.Set(maxRatioEnabledKey, "false")
	d.opts.Settings.Set(maxSeedingTimeEnabledKey, "true")
	d.opts.Settings.Set(maxSeedingTimeKey, "20")
	if _, reached := d.seedLimitReached(meta, stats); !reached {
		t.Fatal("20 minutes must be reached at 30")
	}
	// Per-torrent days win over the global minutes.
	meta.SeedDays = 2
	if _, reached := d.seedLimitReached(meta, stats); reached {
		t.Fatal("2 days must not be reached at 30 minutes")
	}
}

// TestSeedIdleReached covers the inactive-seeding limit: it counts only the
// time spent seeding without uploads and spares a torrent set to seed forever.
func TestSeedIdleReached(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	meta := &torrentMeta{SeedRatio: -1, SeedDays: -1}
	rt := &runtimeInfo{}
	stats := torrent.Stats{Status: torrent.Seeding}
	stats.Bytes.Uploaded = 100
	start := time.Unix(1_700_000_000, 0)

	d.opts.Settings.Set(maxInactiveSeedingTimeEnabledKey, "true")
	d.opts.Settings.Set(maxInactiveSeedingTimeKey, "30")
	if d.seedIdleReached(rt, meta, stats, start) {
		t.Fatal("the first sighting starts the clock")
	}
	if d.seedIdleReached(rt, meta, stats, start.Add(29*time.Minute)) {
		t.Fatal("29 idle minutes must not reach a 30-minute limit")
	}
	if !d.seedIdleReached(rt, meta, stats, start.Add(31*time.Minute)) {
		t.Fatal("31 idle minutes must reach a 30-minute limit")
	}

	// An upload restarts the clock.
	stats.Bytes.Uploaded = 200
	if d.seedIdleReached(rt, meta, stats, start.Add(40*time.Minute)) {
		t.Fatal("an upload must restart the clock")
	}

	// Stopped or queued time does not count.
	stopped := stats
	stopped.Status = torrent.Stopped
	d.seedIdleReached(rt, meta, stopped, start.Add(60*time.Minute))
	if d.seedIdleReached(rt, meta, stats, start.Add(80*time.Minute)) {
		t.Fatal("time spent stopped was counted")
	}

	// A torrent set to seed forever is never stopped for inactivity.
	forever := &torrentMeta{SeedRatio: 0, SeedDays: -1}
	if d.seedIdleReached(rt, forever, stats, start.Add(200*time.Minute)) {
		t.Fatal("an infinite seed must not stop for inactivity")
	}

	// Disabled: never reached.
	d.opts.Settings.Set(maxInactiveSeedingTimeEnabledKey, "false")
	if d.seedIdleReached(rt, meta, stats, start.Add(300*time.Minute)) {
		t.Fatal("the limit is off")
	}
}
