package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// The tests in this file cover the remaining behaviours ported into the Go implementation
// implementation that had no Go counterpart yet, keeping the same scenarios.

func TestSeedLimitsReachedMatchesPolicy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Libtorrent.SeedRatio = 0.5
	cfg.Libtorrent.SeedTimeDays = 1

	// Uses the global ratio (per-torrent -1) and reaches it.
	torrent := &models.TorrentView{
		SeedRatio:       -1,
		SeedDays:        -1,
		AllTimeDownload: 1000,
		AllTimeUpload:   600,
	}
	ratio, timeReached := tev_seedLimitsReached(&cfg, torrent)
	if !ratio || timeReached {
		t.Fatalf("ratio=%v time=%v, want ratio reached only", ratio, timeReached)
	}

	// Per-torrent infinite seeding (ratio 0) is never "reached".
	infinite := torrent
	infinite.SeedRatio = 0
	if ratio, _ := tev_seedLimitsReached(&cfg, infinite); ratio {
		t.Fatal("per-torrent infinite seed must not count as ratio reached")
	}

	// Seed time reached.
	timed := torrent
	timed.SeedingSeconds = 90_000
	if _, timeReached := tev_seedLimitsReached(&cfg, timed); !timeReached {
		t.Fatal("seed time should be reached after one day")
	}
}

func TestInfiniteSeedResumeSkipsQueuePaused(t *testing.T) {
	base := models.TorrentView{SeedRatio: 0, Progress: 100, State: "paused", AutoManaged: false}
	if !tev_needsInfiniteSeedResume(&base) {
		t.Fatal("paused complete torrent with infinite seed should resume")
	}
	cases := []models.TorrentView{
		{SeedRatio: 0, Progress: 100, State: "paused", AutoManaged: true},                // still managed
		{SeedRatio: 0, Progress: 50, State: "paused", AutoManaged: false},                // incomplete
		{SeedRatio: 1, SeedDays: -1, Progress: 100, State: "paused", AutoManaged: false}, // finite rule
		{SeedRatio: 0, Progress: 100, State: "seeding", AutoManaged: false},
	}
	for _, torrent := range cases {
		if tev_needsInfiniteSeedResume(&torrent) {
			t.Fatalf("unexpected resume for %+v", torrent)
		}
	}
}

func TestStorageMoveBackoffBounded(t *testing.T) {
	previous := tev_storageMoveBackoff(1)
	for attempt := uint8(2); attempt <= 6; attempt++ {
		current := tev_storageMoveBackoff(attempt)
		if current < previous {
			t.Fatalf("backoff decreased at attempt %d: %s < %s", attempt, current, previous)
		}
		previous = current
	}
	if tev_storageMoveBackoff(200) != 3600*time.Second {
		t.Fatalf("backoff not capped: %s", tev_storageMoveBackoff(200))
	}
}

func TestStorageMoveRetryScheduleKeepsPostSeedIntent(t *testing.T) {
	retries := map[string]StorageMoveRetry{}
	now := time.Now()
	tev_scheduleStorageMoveRetry(retries, "ABC123", "/dest", true, now)
	entry, ok := retries["abc123"]
	if !ok {
		t.Fatal("retry not recorded under the lowercase hash")
	}
	if entry.destination != "/dest" || !entry.postSeed || entry.attempts != 1 {
		t.Fatalf("schedule = %+v", entry)
	}
	if !entry.nextAttempt.After(now) {
		t.Fatalf("next attempt %v not after %v", entry.nextAttempt, now)
	}
}

func TestClearEmptyDestinationRemovesOnlyEmptyDirs(t *testing.T) {
	destination := t.TempDir()
	empty := filepath.Join(destination, "Show")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	tev_clearEmptyDestination(destination, "Show")
	if _, err := os.Stat(empty); err == nil {
		t.Fatal("empty destination directory was not removed")
	}

	full := filepath.Join(destination, "Full")
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(full, "file.mkv"), validMKV(16))
	tev_clearEmptyDestination(destination, "Full")
	if _, err := os.Stat(full); err != nil {
		t.Fatal("non-empty destination directory was removed")
	}
}

func TestStallTimerRequiresRealProgress(t *testing.T) {
	now := time.Now()
	last := now.Add(-10 * time.Minute)
	lastDone := int64(100)

	// No progress for longer than the timeout: stalled.
	if !tev_stallExpired(&last, &lastDone, now, 100, 5*time.Minute) {
		t.Fatal("stalled download not detected")
	}
	// Progress resets the timer.
	last = now.Add(-10 * time.Minute)
	lastDone = 100
	if tev_stallExpired(&last, &lastDone, now, 200, 5*time.Minute) {
		t.Fatal("progress should reset the stall timer")
	}
}

func TestStallRetryNoticeBackoff(t *testing.T) {
	now := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	entry := StallWatch{}
	want := []time.Duration{time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 24 * time.Hour}
	for index, delay := range want {
		if !tev_stallRetryNoticeDue(&entry, now) {
			t.Fatalf("notice %d should be due", index)
		}
		tev_scheduleNextStallRetryNotice(&entry, now)
		if got := entry.nextRetryNoticeAt.Sub(now); got != delay {
			t.Fatalf("notice %d delay = %s, want %s", index, got, delay)
		}
		if tev_stallRetryNoticeDue(&entry, now.Add(delay-time.Second)) {
			t.Fatalf("notice %d was due before its delay elapsed", index)
		}
		now = entry.nextRetryNoticeAt
	}
}

func TestMetadataRetryWarningIsRateLimited(t *testing.T) {
	now := time.Now()
	if !tev_metadataRetryWarningDue(time.Time{}, now) {
		t.Fatal("first metadata retry must be reported")
	}
	if tev_metadataRetryWarningDue(now.Add(-30*time.Minute), now) {
		t.Fatal("metadata retry warning should be rate limited")
	}
	if !tev_metadataRetryWarningDue(now.Add(-time.Hour), now) {
		t.Fatal("metadata retry warning should reappear after the interval")
	}
}

func TestParseSeasonEpisodeFromArchiveFilenames(t *testing.T) {
	cases := []struct {
		name    string
		season  int64
		episode int64
		ok      bool
	}{
		{"Show.S01E02.1080p.WEB-DL.mkv", 1, 2, true},
		{"Show.1x03.mkv", 1, 3, true},
		{"Show - S2E10 - Title.mkv", 2, 10, true},
		{"Show.mkv", 0, 0, false},
	}
	for _, tc := range cases {
		season, episode, ok := gh5_parseSeasonEpisode(tc.name)
		if ok != tc.ok || (ok && (season != tc.season || episode != tc.episode)) {
			t.Errorf("parse(%q) = %d,%d,%v want %d,%d,%v", tc.name, season, episode, ok, tc.season, tc.episode, tc.ok)
		}
	}
}

func TestLibtorrentVersionComparison(t *testing.T) {
	if newer := gh4_versionIsNewer("2.0.11", "2.0.9"); !newer {
		t.Fatal("2.0.11 should be newer than 2.0.9")
	}
	if newer := gh4_versionIsNewer("2.0.9", "2.0.11"); newer {
		t.Fatal("2.0.9 is not newer than 2.0.11")
	}
	if newer := gh4_versionIsNewer("2.0.11", "2.0.11"); newer {
		t.Fatal("equal versions are not newer")
	}
	if newer := gh4_versionIsNewer("2.1", "2.0.11"); !newer {
		t.Fatal("2.1 should be newer than 2.0.11")
	}
	parts := gh4_versionParts("v2.0.11.0")
	if len(parts) != 4 || parts[0] != 2 || parts[3] != 0 {
		t.Fatalf("version parts = %v", parts)
	}
}

func TestSortCalendarItemsByAirDate(t *testing.T) {
	item := func(date string) any {
		return map[string]any{"episode": map[string]any{"air_date": date}}
	}
	items := []any{item("2026-02-01"), item(""), item("2026-01-15"), item("2026-03-10")}
	gh0_sortCalendarItems(items)
	dates := make([]string, len(items))
	for index, value := range items {
		dates[index] = value.(map[string]any)["episode"].(map[string]any)["air_date"].(string)
	}
	want := []string{"2026-01-15", "2026-02-01", "2026-03-10", ""}
	for index := range want {
		if dates[index] != want[index] {
			t.Fatalf("sorted dates = %v, want %v", dates, want)
		}
	}
}

func TestExtraSettingsLinesParseTypes(t *testing.T) {
	lines := extraSettingsLines("max_uploads=12\nenable_dht=true\nbind=\"x\"\n# comment\nnoequals\nempty=\npeer=off")
	want := []string{"i:max_uploads=12", "b:enable_dht=1", "s:bind=\"x\"", "b:peer=0"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("line %d = %q, want %q", index, lines[index], want[index])
		}
	}
}

func TestBackupDueIntervalAndDailyTime(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{"backup_schedule_hours": "24"}
	if !bwm_backupDue(&cfg, nil) {
		t.Fatal("no previous backup should be due")
	}
	recent := time.Now().Add(-time.Hour)
	if bwm_backupDue(&cfg, &recent) {
		t.Fatal("backup within the interval must not be due")
	}
	old := time.Now().Add(-25 * time.Hour)
	if !bwm_backupDue(&cfg, &old) {
		t.Fatal("backup past the interval should be due")
	}

	// A daily time in the past is due once per day.
	cfg.Settings = map[string]string{"backup_schedule_at": time.Now().Add(-2 * time.Minute).Format("15:04")}
	if !bwm_backupDue(&cfg, nil) {
		t.Fatal("daily schedule in the past should be due")
	}
	now := time.Now()
	if bwm_backupDue(&cfg, &now) {
		t.Fatal("daily schedule already run today must not be due again")
	}
}

func TestFeedSourceNamesAndBreakdown(t *testing.T) {
	cases := map[string]string{
		"https://ext.to/rss":               "ExtTo",
		"https://www.torrentgalaxy.to/rss": "TGx",
		"https://www.torrentleech.org/rss": "TorrentLeech",
		"https://knaben.eu/rss":            "Knaben",
		"https://example.com/feed":         "example.com",
	}
	for input, want := range cases {
		if got := feedSourceName(input); got != want {
			t.Errorf("feedSourceName(%q) = %q, want %q", input, got, want)
		}
	}
	label := feedLabel("https://example.com/rss/")
	if label != "example.com/rss" {
		t.Fatalf("feedLabel = %q", label)
	}

	stats := []logging.SourceStatEntry{
		{Kind: "feed", Name: "ExtTo", Stats: logging.SourceStat{OK: 2, Results: 100}},
		{Kind: "feed", Name: "Knaben", Stats: logging.SourceStat{OK: 1, Fail: 1, Results: 5}},
		{Kind: "feed", Name: "Down", Stats: logging.SourceStat{Fail: 1}},
		{Kind: "indexer", Name: "Ignored", Stats: logging.SourceStat{OK: 1}},
	}
	breakdown := sourceBreakdown(stats)
	for _, fragment := range []string{"ExtTo: 100", "Knaben: 5 (1 error)", "Down: error"} {
		if !containsSubstring(breakdown, fragment) {
			t.Fatalf("breakdown %q missing %q", breakdown, fragment)
		}
	}
	if containsSubstring(breakdown, "Ignored") {
		t.Fatal("non-feed sources must be excluded from the feed breakdown")
	}
}

func TestLatestConfigCacheRefreshesAfterSetting(t *testing.T) {
	state := newTestAppState(t)
	first := LatestConfig(state)
	if first == nil {
		t.Fatal("no config")
	}
	// A new setting must invalidate the cache.
	if err := SaveSetting(state.cfg.DataDir, "notify_test_setting", "1"); err != nil {
		t.Fatalf("save setting: %v", err)
	}
	TouchConfigGeneration()
	second := LatestConfig(state)
	if second == nil {
		t.Fatal("no config after refresh")
	}
	if second.Settings["notify_test_setting"] != "1" {
		t.Fatalf("refreshed config does not carry the new setting: %v", second.Settings["notify_test_setting"])
	}
}

func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestPerTorrentSeedLimitsLowercaseHashes(t *testing.T) {
	dir := t.TempDir()
	client := &LibtorrentClient{stateDir: dir}
	// A file written with uppercase keys must be read back lowercase.
	if err := os.WriteFile(client.seedLimitsPath(), []byte(`{"ABC123":{"ratio":1.5,"days":2}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	limits, err := client.seedLimits()
	if err != nil {
		t.Fatalf("seedLimits: %v", err)
	}
	if _, ok := limits["abc123"]; !ok {
		t.Fatalf("limits = %v, want lowercase key", limits)
	}
	// Saving normalises the hash to lowercase too.
	if err := client.saveSeedLimit("DEADBEEF", 0.5, -1); err != nil {
		t.Fatalf("saveSeedLimit: %v", err)
	}
	raw, err := os.ReadFile(client.seedLimitsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "deadbeef") || strings.Contains(string(raw), "DEADBEEF") {
		t.Fatalf("seed limits not stored lowercase: %s", raw)
	}
}

func TestMagnetFeedBuildsRSSWithEscapedMagnets(t *testing.T) {
	entries := [][3]string{{
		"Title <A> & B",
		"magnet:?xt=urn:btih:abc&dn=x&tr=udp://tracker:80",
		"src",
	}}
	feed := gh7_build_magnet_feed(entries)
	if !strings.Contains(feed, "<rss version=\"2.0\">") {
		t.Fatalf("not an RSS feed: %s", feed)
	}
	if !strings.Contains(feed, "Title &lt;A&gt; &amp; B") {
		t.Fatalf("title not escaped: %s", feed)
	}
	if !strings.Contains(feed, "magnet:?xt=urn:btih:abc&amp;dn=x&amp;tr=udp://tracker:80") {
		t.Fatalf("magnet not escaped in the feed: %s", feed)
	}
	if !strings.Contains(feed, "[src]") {
		t.Fatal("source label missing from the feed description")
	}
}

func TestRamdiskOrphanSweepIsConservative(t *testing.T) {
	now := time.Now()
	if ramdiskOrphanIsStale(now.Add(-30*time.Second), now) {
		t.Fatal("a freshly written partial file must not be treated as an orphan")
	}
	if ramdiskOrphanIsStale(now.Add(-30*time.Minute), now) {
		t.Fatal("a recent file must not be treated as an orphan")
	}
	if !ramdiskOrphanIsStale(now.Add(-2*time.Hour), now) {
		t.Fatal("an old unreferenced entry should be sweepable")
	}
}
