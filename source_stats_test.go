package gextto

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func insertSourceDay(t *testing.T, db *Database, kind, provider, day string, attempts, successes, failures, results, accepted int64) {
	t.Helper()
	if _, err := db.db.Exec(`
		INSERT INTO source_daily(kind, provider, day, attempts, successes, failures, results, accepted)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`,
		kind, provider, day, attempts, successes, failures, results, accepted); err != nil {
		t.Fatalf("insert source day: %v", err)
	}
}

func TestSourceStatsWindowsAndHealth(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	sixDaysAgo := now.AddDate(0, 0, -6).Format("2006-01-02")
	twentyNineDaysAgo := now.AddDate(0, 0, -29).Format("2006-01-02")
	fortyDaysAgo := now.AddDate(0, 0, -40).Format("2006-01-02")

	insertSourceDay(t, db, "feed", "example.com", today, 2, 1, 1, 10, 1)
	insertSourceDay(t, db, "feed", "example.com", sixDaysAgo, 1, 1, 0, 5, 0)
	insertSourceDay(t, db, "feed", "example.com", twentyNineDaysAgo, 1, 0, 1, 0, 0)
	insertSourceDay(t, db, "feed", "example.com", fortyDaysAgo, 9, 0, 9, 0, 0)

	if err := db.RecordSourceAttempt("feed", "example.com", true, 7, ""); err != nil {
		t.Fatalf("RecordSourceAttempt: %v", err)
	}
	if err := db.RecordSourceAccepted("feed", "example.com"); err != nil {
		t.Fatalf("RecordSourceAccepted: %v", err)
	}
	stats, err := db.SourceStats("feed")
	if err != nil {
		t.Fatalf("SourceStats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("stats = %+v, want one provider", stats)
	}
	got := stats[0]
	if got.Attempts1 != 3 || got.Successes1 != 2 || got.Failures1 != 1 || got.Results1 != 17 || got.Accepted1 != 2 {
		t.Fatalf("1d window = %+v", got)
	}
	if got.Attempts7 != 4 || got.Successes7 != 3 || got.Failures7 != 1 || got.Results7 != 22 {
		t.Fatalf("7d window = %+v", got)
	}
	if got.Attempts30 != 5 || got.Successes30 != 3 || got.Failures30 != 2 || got.Results30 != 22 {
		t.Fatalf("30d window = %+v", got)
	}
	if got.LastSuccessAt == "" || got.LastResults != 7 {
		t.Fatalf("health = %+v", got)
	}

	health, err := db.SourceHealth("feed", "example.com")
	if err != nil {
		t.Fatalf("SourceHealth: %v", err)
	}
	if health.FirstAttemptAt == "" || health.LastSuccessAt == "" {
		t.Fatalf("health = %+v", health)
	}

	if err := db.MarkSourceAutoDisabled("feed", "example.com", "7", now); err != nil {
		t.Fatalf("MarkSourceAutoDisabled: %v", err)
	}
	health, _ = db.SourceHealth("feed", "example.com")
	if health.DisabledAt == "" || health.DisabledReason != "7" {
		t.Fatalf("disabled health = %+v", health)
	}
	// A later success clears the marker: the operator re-enabled the feed.
	if err := db.RecordSourceAttempt("feed", "example.com", true, 3, ""); err != nil {
		t.Fatalf("RecordSourceAttempt success: %v", err)
	}
	health, _ = db.SourceHealth("feed", "example.com")
	if health.DisabledAt != "" || health.DisabledReason != "" {
		t.Fatalf("success did not clear the disable marker: %+v", health)
	}

	if err := db.RecordSourceAttempt("feed", "other.example", true, 1, ""); err != nil {
		t.Fatalf("RecordSourceAttempt other: %v", err)
	}
	if ok, err := db.AnySourceSuccessToday("feed", "example.com"); err != nil || !ok {
		t.Fatalf("AnySourceSuccessToday = %v, %v", ok, err)
	}
	if ok, err := db.AnySourceSuccessToday("indexer", ""); err != nil || ok {
		t.Fatalf("AnySourceSuccessToday for an empty kind = %v, %v", ok, err)
	}
}

func TestFeedProviderForSourceAndAcceptedCount(t *testing.T) {
	cfg := &Config{FeedURLs: []string{
		"https://one.example/feed.xml",
		"https://torrentgalaxy.example/get-posts/user:someone/",
	}}
	for _, test := range []struct {
		source string
		want   string
		ok     bool
	}{
		{"https://one.example/feed.xml", "one.example", true},
		{"one.example", "one.example", true},
		{"TGx — someone", "TGx", true},
		{"a regular indexer", "", false},
	} {
		got, ok := feedProviderForSource(cfg, test.source)
		if got != test.want || ok != test.ok {
			t.Errorf("feedProviderForSource(%q) = %q, %v, want %q, %v", test.source, got, ok, test.want, test.ok)
		}
	}
	db := newTestDB(t)
	recordFeedAccepted(db, cfg, &models.Release{Source: "one.example"})
	stats, err := db.SourceStats("feed")
	if err != nil {
		t.Fatalf("SourceStats: %v", err)
	}
	if len(stats) != 1 || stats[0].Accepted1 != 1 {
		t.Fatalf("accepted stats = %+v", stats)
	}
}

func TestFeedAutoDisableDaysSetting(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{"", 7}, {"7", 7}, {"0", 0}, {"30", 30}, {"-1", 0}, {"bad", 7},
	} {
		cfg := &Config{Settings: map[string]string{"feed_auto_disable_days": test.value}}
		if got := feedAutoDisableDays(cfg); got != test.want {
			t.Errorf("feedAutoDisableDays(%q) = %d, want %d", test.value, got, test.want)
		}
	}
	if got := feedAutoDisableDays(nil); got != 7 {
		t.Errorf("feedAutoDisableDays(nil) = %d, want 7", got)
	}
}

func TestDisableFeedInConfigPreservesOtherLines(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		"https://one.example/feed.xml",
		"# https://old.example/feed.xml",
		"https://two.example/feed.xml",
	}
	encoded, _ := json.Marshal(lines)
	cfg := &Config{DataDir: dir, Settings: map[string]string{"url": string(encoded)}}
	changed, err := disableFeedInConfig(cfg, "https://one.example/feed.xml")
	if err != nil || !changed {
		t.Fatalf("disableFeedInConfig = %v, %v", changed, err)
	}
	conn, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
	if err != nil {
		t.Fatalf("OpenConfigDB: %v", err)
	}
	defer conn.Close()
	var stored string
	if err := conn.QueryRow("SELECT value FROM settings WHERE key='url'").Scan(&stored); err != nil {
		t.Fatalf("read setting: %v", err)
	}
	var got []string
	if err := json.Unmarshal([]byte(stored), &got); err != nil {
		t.Fatalf("decode setting: %v", err)
	}
	if len(got) != 3 || got[0] != "# https://one.example/feed.xml" ||
		got[1] != "# https://old.example/feed.xml" || got[2] != "https://two.example/feed.xml" {
		t.Fatalf("lines = %#v", got)
	}
	// Already disabled: no second change.
	if changed, err := disableFeedInConfig(cfg, "https://one.example/feed.xml"); err != nil || changed {
		t.Fatalf("second disable = %v, %v", changed, err)
	}
}

func TestMaybeAutoDisableFeedNeedsAnotherSuccess(t *testing.T) {
	db := newTestDB(t)
	dir := t.TempDir()
	rawURL := "https://one.example/feed.xml"
	provider := feedSourceName(rawURL)
	encoded, _ := json.Marshal([]string{rawURL, "https://two.example/feed.xml"})
	cfg := &Config{DataDir: dir, Settings: map[string]string{
		"url":                    string(encoded),
		"feed_auto_disable_days": "7",
	}}
	old := time.Now().UTC().AddDate(0, 0, -8)
	if _, err := db.db.Exec(`INSERT INTO source_health(kind, provider, first_attempt_at, last_attempt_at, last_results)
		VALUES ('feed', ?1, ?2, ?2, 0)`, provider, sqliteTimestamp(old)); err != nil {
		t.Fatalf("insert health: %v", err)
	}
	engine := &Engine{db: db}

	// No other feed succeeded: a global outage must not disable the list.
	engine.maybeAutoDisableFeed(cfg, rawURL, provider)
	health, _ := db.SourceHealth("feed", provider)
	if health.DisabledAt != "" {
		t.Fatal("feed disabled without another successful feed")
	}

	// Another feed succeeding today makes this one a single-source failure.
	if err := db.RecordSourceAttempt("feed", "two.example", true, 1, ""); err != nil {
		t.Fatalf("RecordSourceAttempt other: %v", err)
	}
	engine.maybeAutoDisableFeed(cfg, rawURL, provider)
	health, _ = db.SourceHealth("feed", provider)
	if health.DisabledAt == "" || health.DisabledReason != "7" {
		t.Fatalf("feed not disabled: %+v", health)
	}
	conn, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
	if err != nil {
		t.Fatalf("OpenConfigDB: %v", err)
	}
	defer conn.Close()
	var stored string
	if err := conn.QueryRow("SELECT value FROM settings WHERE key='url'").Scan(&stored); err != nil {
		t.Fatalf("read setting: %v", err)
	}
	var lines []string
	_ = json.Unmarshal([]byte(stored), &lines)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "# ") || lines[1] != "https://two.example/feed.xml" {
		t.Fatalf("lines = %#v", lines)
	}
}
