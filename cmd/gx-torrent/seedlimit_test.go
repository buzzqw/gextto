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
	if _, reached := d.seedLimitReachedForTest(meta, stats); reached {
		t.Fatal("no limits configured, but reached")
	}

	// Global ratio 0.4: reached at 0.5, with the removal action.
	d.opts.Settings.Set(maxRatioEnabledKey, "true")
	d.opts.Settings.Set(maxRatioKey, "0.4")
	if remove, reached := d.seedLimitReachedForTest(meta, stats); !reached || remove {
		t.Fatalf("ratio 0.4: reached=%v remove=%v", reached, remove)
	}
	d.opts.Settings.Set(maxRatioActKey, "1")
	if remove, reached := d.seedLimitReachedForTest(meta, stats); !reached || !remove {
		t.Fatalf("ratio 0.4 remove action: reached=%v remove=%v", reached, remove)
	}
	d.opts.Settings.Set(maxRatioActKey, "0")
	d.opts.Settings.Set(maxRatioKey, "2")
	if _, reached := d.seedLimitReachedForTest(meta, stats); reached {
		t.Fatal("ratio 2 must not be reached at 0.5")
	}

	// A per-torrent infinite ratio (0) wins over the global.
	meta.SeedRatio = 0
	d.opts.Settings.Set(maxRatioKey, "0.1")
	if _, reached := d.seedLimitReachedForTest(meta, stats); reached {
		t.Fatal("a per-torrent infinite ratio must win")
	}

	// Global seeding time: 20 minutes, 30 minutes seeded.
	meta.SeedRatio = -1
	d.opts.Settings.Set(maxRatioEnabledKey, "false")
	d.opts.Settings.Set(maxSeedingTimeEnabledKey, "true")
	d.opts.Settings.Set(maxSeedingTimeKey, "20")
	if _, reached := d.seedLimitReachedForTest(meta, stats); !reached {
		t.Fatal("20 minutes must be reached at 30")
	}
	// Per-torrent days win over the global minutes.
	meta.SeedDays = 2
	if _, reached := d.seedLimitReachedForTest(meta, stats); reached {
		t.Fatal("2 days must not be reached at 30 minutes")
	}
}

// TestSeedTimeMinutesOverride pins the exact qBittorrent minute limit, which
// must not be rounded up to a whole day by the seed policy.
func TestSeedTimeMinutesOverride(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	stats := torrent.Stats{}
	stats.Bytes.Total = 1000
	stats.SeededFor = 30 * time.Minute

	meta := &torrentMeta{SeedRatio: -1, SeedDays: -1, SeedMinutes: int64Ptr(45)}
	if set, reached := d.seedTimeState(meta, stats); !set || reached {
		t.Fatalf("45 minutes at 30: set=%v reached=%v", set, reached)
	}
	meta.SeedMinutes = int64Ptr(20)
	if set, reached := d.seedTimeState(meta, stats); !set || !reached {
		t.Fatalf("20 minutes at 30: set=%v reached=%v", set, reached)
	}
	meta.SeedMinutes = int64Ptr(-1)
	if set, reached := d.seedTimeState(meta, stats); set || reached {
		t.Fatalf("infinite minutes must disable the time limit: set=%v reached=%v", set, reached)
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

	// A per-torrent inactive limit wins and works even when the global one is
	// off; -1 disables it for that torrent only.
	perTorrent := &torrentMeta{SeedRatio: -1, SeedDays: -1, InactiveMinutes: int64Ptr(10)}
	rt2 := &runtimeInfo{}
	d.seedIdleReached(rt2, perTorrent, stats, start)
	if !d.seedIdleReached(rt2, perTorrent, stats, start.Add(11*time.Minute)) {
		t.Fatal("a 10-minute per-torrent limit must be reached")
	}
	perTorrent.InactiveMinutes = int64Ptr(-1)
	rt3 := &runtimeInfo{}
	d.seedIdleReached(rt3, perTorrent, stats, start)
	if d.seedIdleReached(rt3, perTorrent, stats, start.Add(300*time.Minute)) {
		t.Fatal("a per-torrent disabled limit must never be reached")
	}
}

// seedLimitReachedForTest keeps the old two-value shape of the ratio and time
// limits for the table test above.
func (d *Daemon) seedLimitReachedForTest(meta *torrentMeta, stats torrent.Stats) (bool, bool) {
	remove := d.settingInt(maxRatioActKey, 0) == 1
	_, ratioHit := d.seedRatioState(meta, stats)
	_, timeHit := d.seedTimeState(meta, stats)
	return remove, ratioHit || timeHit
}

// The all-limits rule is the declared default: a torrent that reached its
// ratio keeps seeding until its time limit is served, with no switch to set.
func TestSeedLimitStopAllIsTheDefault(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(maxRatioEnabledKey, "true")
	d.opts.Settings.Set(maxRatioKey, "2")
	d.opts.Settings.Set(maxSeedingTimeEnabledKey, "true")
	d.opts.Settings.Set(maxSeedingTimeKey, "60")
	meta := &torrentMeta{SeedRatio: -1, SeedDays: -1}
	rt := &runtimeInfo{}
	stats := torrent.Stats{Status: torrent.Seeding}
	stats.Bytes.Total = 100
	stats.Bytes.Uploaded = 500
	stats.SeededFor = 30 * time.Minute
	now := time.Unix(1_700_000_000, 0)
	d.seedIdleReached(rt, meta, stats, now)

	if _, _, reached := d.seedLimitStop(rt, meta, stats, now.Add(time.Minute)); reached {
		t.Fatal("ratio alone must not end a seed with the default rule")
	}
	stats.SeededFor = 90 * time.Minute
	if _, _, reached := d.seedLimitStop(rt, meta, stats, now.Add(2*time.Minute)); !reached {
		t.Fatal("both limits reached, but the seed went on")
	}
}

// TestSeedLimitStopAll: with share-limits-all the seed ends only when every
// configured limit has been reached, not at the first one, and a limit left off
// is not part of the condition.
func TestSeedLimitStopAll(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(shareLimitsAllKey, "true")
	d.opts.Settings.Set(maxRatioEnabledKey, "true")
	d.opts.Settings.Set(maxRatioKey, "2")
	d.opts.Settings.Set(maxSeedingTimeEnabledKey, "true")
	d.opts.Settings.Set(maxSeedingTimeKey, "60")
	meta := &torrentMeta{SeedRatio: -1, SeedDays: -1}
	rt := &runtimeInfo{}
	stats := torrent.Stats{Status: torrent.Seeding}
	stats.Bytes.Total = 100
	stats.Bytes.Uploaded = 500 // ratio 5 >= 2
	stats.SeededFor = 30 * time.Minute
	now := time.Unix(1_700_000_000, 0)
	d.seedIdleReached(rt, meta, stats, now) // start the idle clock

	// Ratio reached, time not: the seed goes on.
	if _, _, reached := d.seedLimitStop(rt, meta, stats, now.Add(time.Minute)); reached {
		t.Fatal("ratio alone must not end an all-limits seed")
	}
	// Time reached too: stop.
	stats.SeededFor = 90 * time.Minute
	if _, _, reached := d.seedLimitStop(rt, meta, stats, now.Add(2*time.Minute)); !reached {
		t.Fatal("both limits reached, but the seed went on")
	}
	// With only the ratio configured, the ratio alone is enough.
	d.opts.Settings.Set(maxSeedingTimeEnabledKey, "false")
	if _, _, reached := d.seedLimitStop(rt, meta, stats, now.Add(3*time.Minute)); !reached {
		t.Fatal("the only configured limit was reached, but the seed went on")
	}
	// Nothing configured: never stop.
	d.opts.Settings.Set(maxRatioEnabledKey, "false")
	if _, _, reached := d.seedLimitStop(rt, meta, stats, now.Add(4*time.Minute)); reached {
		t.Fatal("no limit configured, but the seed stopped")
	}
}
