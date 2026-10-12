package gextto

import "testing"

// TestSeedLimitsForTorrent fixes the effective seed limits: the torrent's own
// when positive, the globals when negative (inherit), none when zero
// (unlimited). Both the "seed ∞" badge and "Pulisci completati" rely on it.
func TestSeedLimitsForTorrent(t *testing.T) {
	cfg := &Config{}
	cfg.Libtorrent.SeedRatio = 2.0
	cfg.Libtorrent.SeedTimeDays = 3

	if ratio, timeLimit := seedLimitsForTorrent(cfg, 5, 1); ratio == nil || *ratio != 5 || timeLimit == nil || *timeLimit != 86_400 {
		t.Fatalf("own limits: ratio=%v time=%v", ratio, timeLimit)
	}
	if ratio, timeLimit := seedLimitsForTorrent(cfg, -1, -1); ratio == nil || *ratio != 2 || timeLimit == nil || *timeLimit != 3*86_400 {
		t.Fatalf("inherit globals: ratio=%v time=%v", ratio, timeLimit)
	}
	if ratio, timeLimit := seedLimitsForTorrent(cfg, 0, 0); ratio != nil || timeLimit != nil {
		t.Fatalf("unlimited (zero) must mean no limit: ratio=%v time=%v", ratio, timeLimit)
	}
	// Inheriting from globals that are themselves unset means seed forever.
	empty := &Config{}
	if ratio, timeLimit := seedLimitsForTorrent(empty, -1, -1); ratio != nil || timeLimit != nil {
		t.Fatalf("no global limit: ratio=%v time=%v", ratio, timeLimit)
	}
	// Minutes fallback when no day limit is set.
	minutes := &Config{}
	minutes.Libtorrent.SeedTimeMinutes = 30
	if _, timeLimit := seedLimitsForTorrent(minutes, -1, -1); timeLimit == nil || *timeLimit != 1800 {
		t.Fatalf("minutes fallback: time=%v", timeLimit)
	}
}
