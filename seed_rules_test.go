package gextto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func floatPtr(v float64) *float64 { return &v }

func TestSeedRuleMatchesSourceAndCategory(t *testing.T) {
	rules := []SeedRule{
		{Name: "mircrew", Source: "MIRCrew"},
		{Name: "movies", Category: "movie"},
		{Name: "ext series", Source: "extto", Category: "series"},
	}
	cases := []struct {
		origin seedOrigin
		want   string
	}{
		{seedOrigin{Source: "mircrew indexerr", Category: "series"}, "mircrew"},
		{seedOrigin{Source: "jackett:LimeTorrents", Category: "movie"}, "movies"},
		{seedOrigin{Source: "archive:ExtTo", Category: "series"}, "ext series"},
		{seedOrigin{Source: "ExtTo - TheBlackKing", Category: "series"}, "ext series"},
		{seedOrigin{Source: "ExtTo", Category: "other"}, ""},
		{seedOrigin{Source: "", Category: "comic"}, ""},
	}
	for _, c := range cases {
		got := ""
		if rule := matchSeedRule(rules, c.origin); rule != nil {
			got = rule.Name
		}
		if got != c.want {
			t.Errorf("%+v matched %q, want %q", c.origin, got, c.want)
		}
	}
}

func TestSeedCategoryFromKindAndTag(t *testing.T) {
	for _, c := range []struct{ kind, tag, want string }{
		{"series", "", "series"},
		{"movie", "", "movie"},
		{"", "Comic", "comic"},
		{"", "Manuale", "other"},
		{"", "", "other"},
	} {
		if got := seedCategory(c.kind, c.tag); got != c.want {
			t.Errorf("seedCategory(%q,%q)=%q, want %q", c.kind, c.tag, got, c.want)
		}
	}
}

func TestResolveSeedLimitsPrecedence(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Transfer.SeedRatio = 1
	cfg.Transfer.SeedTimeDays = 7
	cfg.Transfer.SeedIdleMinutes = 120
	rule := &SeedRule{Name: "private", Source: "x", Ratio: floatPtr(2), SeedDays: floatPtr(3)}

	// The rule wins over the global limits; idle falls back to the global one.
	limits := resolveSeedLimits(&cfg, rule, -1, -1)
	if limits.Ratio == nil || *limits.Ratio != 2 || limits.Seconds == nil || *limits.Seconds != 3*86_400 {
		t.Fatalf("rule limits not applied: %+v", limits)
	}
	if limits.IdleSeconds == nil || *limits.IdleSeconds != 120*60 || limits.Rule != "private" {
		t.Fatalf("idle or rule name wrong: %+v", limits)
	}

	// The torrent's own limits win over the rule.
	limits = resolveSeedLimits(&cfg, rule, 5, 10)
	if *limits.Ratio != 5 || *limits.Seconds != 10*86_400 {
		t.Fatalf("per-torrent limits must win: %+v", limits)
	}

	// A rule's 0 removes a global limit; a nil field inherits it.
	noRatio := &SeedRule{Source: "x", Ratio: floatPtr(0), IdleMinutes: floatPtr(0)}
	limits = resolveSeedLimits(&cfg, noRatio, -1, -1)
	if limits.Ratio != nil || limits.IdleSeconds != nil || limits.Seconds == nil || *limits.Seconds != 7*86_400 {
		t.Fatalf("rule 0 must remove the limit and nil inherit: %+v", limits)
	}

	// An explicit per-torrent zero seeds forever, rules and idle included.
	if limits := resolveSeedLimits(&cfg, rule, 0, -1); !limits.none() {
		t.Fatalf("per-torrent infinite seed must clear every limit: %+v", limits)
	}

	// Without rules nor globals the torrent seeds forever.
	empty := DefaultConfig()
	if limits := resolveSeedLimits(&empty, nil, -1, -1); !limits.none() {
		t.Fatalf("no limits expected: %+v", limits)
	}
}

func TestSeedRulesValidation(t *testing.T) {
	bad := [][]SeedRule{
		{{Name: "anything"}},                   // no source nor category
		{{Source: "x", Category: "music"}},     // unknown category
		{{Source: "x", Ratio: floatPtr(5000)}}, // out of range
		make([]SeedRule, maxSeedRules+1),       // too many
	}
	for i, rules := range bad {
		for j := range rules {
			if rules[j].Source == "" && rules[j].Category == "" && rules[j].Name == "" {
				rules[j].Source = "x"
			}
		}
		if _, err := normalizeSeedRules(rules); err == nil {
			t.Errorf("case %d: expected a validation error", i)
		}
	}
	// A negative limit from the editor is "inherit".
	rules, err := normalizeSeedRules([]SeedRule{{Source: " mircrew ", Category: "Series", Ratio: floatPtr(-1)}})
	if err != nil || rules[0].Ratio != nil || rules[0].Source != "mircrew" || rules[0].Category != "series" {
		t.Fatalf("normalize: %+v %v", rules, err)
	}
	if err := validateSettingValue(seedRulesKey, `[{"category":"nope"}]`, nil); err == nil {
		t.Fatal("validateSettingValue must refuse an invalid rule list")
	}
	if err := validateSettingValue(seedRulesKey, `[{"source":"mircrew","ratio":1.5}]`, nil); err != nil {
		t.Fatalf("valid rules refused: %v", err)
	}
}

func TestSeedIdleTrackerCountsOnlyActiveSeeding(t *testing.T) {
	tracker := &seedIdleTracker{marks: map[string]seedIdleMark{}}
	start := time.Unix(1_700_000_000, 0)
	idleAt := func(upload int64, seeding bool, at time.Duration) time.Duration {
		idle, _ := tracker.observe("A", upload, seeding, start.Add(at))
		return idle
	}
	if idle := idleAt(100, true, 0); idle != 0 {
		t.Fatalf("first sighting idle=%v", idle)
	}
	if idle := idleAt(100, true, 30*time.Minute); idle != 30*time.Minute {
		t.Fatalf("idle=%v, want 30m", idle)
	}
	// An upload resets the clock.
	if idle := idleAt(150, true, 40*time.Minute); idle != 0 {
		t.Fatalf("upload must reset, idle=%v", idle)
	}
	// Queued or paused time does not count.
	idleAt(150, false, 90*time.Minute)
	if idle := idleAt(150, true, 100*time.Minute); idle != 10*time.Minute {
		t.Fatalf("paused time counted: idle=%v", idle)
	}
	// A seed stopped for inactivity keeps its verdict while stopped, also
	// across sweeps.
	tracker.markReached("A")
	tracker.sweep()
	tracker.sweep()
	if idle := idleAt(150, false, 200*time.Minute); idle == 0 || !tracker.reached("A") {
		t.Fatalf("stopped-for-idle verdict lost: idle=%v", idle)
	}
	// Resumed by hand: a new period starts and the verdict is dropped.
	idle, resumed := tracker.observe("A", 150, true, start.Add(210*time.Minute))
	if idle != 0 || !resumed || tracker.reached("A") {
		t.Fatalf("resume must start a new period: idle=%v resumed=%v reached=%v", idle, resumed, tracker.reached("A"))
	}
	// Torrents no longer listed are forgotten.
	tracker.sweep()
	tracker.sweep()
	if len(tracker.marks) != 0 {
		t.Fatalf("sweep kept %d marks", len(tracker.marks))
	}
}

func TestEnforceSeedPolicyStopsIdleSeed(t *testing.T) {
	saved := seedIdle
	seedIdle = &seedIdleTracker{marks: map[string]seedIdleMark{}, restored: true}
	defer func() { seedIdle = saved }()

	cfg := DefaultConfig()
	cfg.Transfer.SeedIdleMinutes = 30
	view := models.TorrentView{
		Hash: seedTestHash, Name: "Idle.S01E01.mkv", State: "seeding", Progress: 100,
		TotalSize: 10, TotalDone: 10, SeedRatio: -1, SeedDays: -1, AllTimeUpload: 5,
	}
	// First sighting starts the clock, then 31 minutes without upload.
	seedIdle.observe(view.Hash, view.AllTimeUpload, true, time.Now().Add(-31*time.Minute))
	policy := newSeedPolicy(&cfg, nil)
	limits := policy.limitsFor(&view)
	if !policy.idleReached(&view, limits, time.Now()) {
		t.Fatal("a seed idle for 31 minutes must reach a 30-minute limit")
	}
	// With uploads the limit is not reached.
	busy := view
	busy.AllTimeUpload = 50
	if policy.idleReached(&busy, limits, time.Now()) {
		t.Fatal("an uploading seed must not be idle")
	}
}

// TestSeedIdleStopSurvivesRestart: a seed paused for inactivity is still known
// as such after a restart (it looks like any paused torrent otherwise), so its
// end-of-seed handling completes; resuming it by hand clears the verdict.
func TestSeedIdleStopSurvivesRestart(t *testing.T) {
	saved := seedIdle
	defer func() { seedIdle = saved }()
	state := newTestAppState(t)
	cfg := DefaultConfig()
	cfg.Transfer.SeedIdleMinutes = 30
	paused := models.TorrentView{
		Hash: seedTestHash, State: "paused", Progress: 100, TotalSize: 10, TotalDone: 10,
		SeedRatio: -1, SeedDays: -1, AllTimeUpload: 5,
	}

	seedIdle = &seedIdleTracker{marks: map[string]seedIdleMark{}}
	newSeedPolicy(&cfg, state.db).markIdleStop(paused.Hash)

	// Restart: a fresh tracker reads the verdict back.
	seedIdle = &seedIdleTracker{marks: map[string]seedIdleMark{}}
	policy := newSeedPolicy(&cfg, state.db)
	if !policy.idleReached(&paused, policy.limitsFor(&paused), time.Now()) {
		t.Fatal("the inactivity stop was lost across the restart")
	}

	// Resumed by hand: seeding again, the verdict is dropped everywhere.
	seeding := paused
	seeding.State = "seeding"
	if policy.idleReached(&seeding, policy.limitsFor(&seeding), time.Now()) {
		t.Fatal("a resumed seed must start a new idle period")
	}
	if hashes, err := state.db.SeedIdleStops(); err != nil || len(hashes) != 0 {
		t.Fatalf("verdict still stored: %v %v", hashes, err)
	}
}

func TestSeedRulesAPIRoundTrip(t *testing.T) {
	raw := `[{"name":"MIRCrew","source":"mircrew","category":"","ratio":1,"seed_days":3,"idle_minutes":null}]`
	var rules []SeedRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Settings[seedRulesKey] = raw
	got := seedRulesFrom(&cfg)
	if len(got) != 1 || got[0].IdleMinutes != nil || *got[0].SeedDays != 3 {
		t.Fatalf("rules not read back: %+v", got)
	}
}

// BenchmarkSeedPolicyPass measures what one EnforceSeedPolicy pass adds with
// ten rules: reading them from the settings and resolving 100 torrents (the
// origins come from one query, given here).
func BenchmarkSeedPolicyPass(b *testing.B) {
	cfg := DefaultConfig()
	rules := make([]SeedRule, 10)
	for i := range rules {
		rules[i] = SeedRule{Name: "r", Source: "indexer" + string(rune('a'+i)), Ratio: floatPtr(1)}
	}
	raw, _ := json.Marshal(rules)
	cfg.Settings[seedRulesKey] = string(raw)
	views := make([]models.TorrentView, 100)
	origins := map[string]seedOrigin{}
	for i := range views {
		views[i] = models.TorrentView{Hash: string(rune('A'+i%26)) + "x", SeedRatio: -1, SeedDays: -1}
		origins[views[i].Hash] = seedOrigin{Source: "indexerj", Category: "series"}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		policy := &seedPolicy{cfg: &cfg, rules: seedRulesFrom(&cfg), origins: origins}
		for j := range views {
			_ = policy.limitsFor(&views[j])
		}
	}
}
