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
