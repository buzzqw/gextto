package gextto

import "testing"

// TestSeedLimitsForTorrent fixes the effective seed limits: the torrent's own
// when positive, the globals when negative (inherit), none when zero
// (unlimited). Both the "seed ∞" badge and "Pulisci completati" rely on it.
func TestSeedLimitsForTorrent(t *testing.T) {
	cfg := &Config{}
	cfg.Transfer.SeedRatio = 2.0
	cfg.Transfer.SeedTimeDays = 3

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
	minutes.Transfer.SeedTimeMinutes = 30
	if _, timeLimit := seedLimitsForTorrent(minutes, -1, -1); timeLimit == nil || *timeLimit != 1800 {
		t.Fatalf("minutes fallback: time=%v", timeLimit)
	}
}

// TestSeedEndReason fixes the words used to say why a seed ended, shared by the
// pause line and the trash line: the end-of-seed message must name the limit
// that was reached.
func TestSeedEndReason(t *testing.T) {
	cases := []struct {
		name                          string
		ratio, timeLimit, idleReached bool
		want                          string
	}{
		{"ratio and time", true, true, false, "ratio and time reached"},
		{"ratio", true, false, false, "ratio reached"},
		{"time", false, true, false, "seed time reached"},
		{"idle", false, false, true, "no upload while seeding"},
		{"time wins over idle", false, true, true, "seed time reached"},
	}
	for _, tc := range cases {
		if got := seedEndReason(tc.ratio, tc.timeLimit, tc.idleReached); got != tc.want {
			t.Errorf("%s: seedEndReason = %q, want %q", tc.name, got, tc.want)
		}
	}
}
