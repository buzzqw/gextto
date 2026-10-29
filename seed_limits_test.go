package gextto

import (
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

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

// TestTorrentRowShowsInfiniteSeedWhileDownloading: the "seed ∞" badge follows
// the seed limits the torrent is set to, not its current state, so a torrent
// set to ratio 0 and days 0 shows it while it still downloads.
func TestTorrentRowShowsInfiniteSeedWhileDownloading(t *testing.T) {
	state := newTestAppState(t)
	hash := strings.Repeat("d", 40)
	state.setActiveEngine(fakeProblemsEngine{list: []models.TorrentView{{
		Hash: hash, Name: "movie", State: "downloading", Progress: 10,
		TotalSize: 1000, TotalDone: 100, SeedRatio: 0, SeedDays: 0,
	}}})
	rows := uiTorrentRows(state)
	if len(rows) != 1 || !rows[0].SeedInfinite {
		t.Fatalf("a torrent with ratio 0 and days 0 must show seed ∞: %+v", rows)
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

// TestSeedRemovalReason makes sure a torrent removed while paused, before any
// limit was reached, does not claim a limit that never fired.
func TestSeedRemovalReason(t *testing.T) {
	if got := seedRemovalReason(false, false, false); got != "seed stopped" {
		t.Fatalf("paused torrent without limits: reason = %q, want seed stopped", got)
	}
	if got := seedRemovalReason(true, false, false); got != "ratio reached" {
		t.Fatalf("ratio limit: reason = %q", got)
	}
	if got := seedRemovalReason(false, false, true); got != "no upload while seeding" {
		t.Fatalf("idle limit: reason = %q", got)
	}
}
