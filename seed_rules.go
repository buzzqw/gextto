package gextto

// seed_rules.go resolves how long a completed torrent keeps seeding. The limits
// come from three layers, the first that speaks wins for each limit:
//
//  1. the torrent's own limits (ratio, days; -1 inherits, 0 = never stop);
//  2. the first seed rule that matches the torrent's origin: the source it came
//     from (the indexer, e.g. a private tracker with its own seeding rules) and
//     its category (series, movie, comic, other);
//  3. the global limits (torrent_seed_ratio, torrent_seed_time_days/_time,
//     torrent_seed_idle_minutes).
//
// qBittorrent 5.2 attaches share limits to a category; Gextto's categories are
// the kind of media, while a private tracker's rules follow the tracker, so a
// rule matches the source, the category or both. Gextto enforces the result
// itself (EnforceSeedPolicy), so it holds with either engine.
//
// The inactivity limit stops a seed that uploaded nothing for N minutes while
// it was actively seeding: the same measure as qBittorrent's "inactive seeding
// time". The clock runs only in the seeding state, so a queued seed that never
// had a slot is not stopped for being idle, and it restarts with Gextto (a
// restart can only make a seed last longer, never shorter).

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// seedRulesKey is the setting holding the rules (a JSON list of SeedRule).
const seedRulesKey = "torrent_seed_rules"

// maxSeedRules bounds the list: a rule is matched for every seeding torrent at
// every tick.
const maxSeedRules = 100

// seedRuleCategories are the categories a rule can name ("" = any).
var seedRuleCategories = map[string]bool{"": true, "series": true, "movie": true, "comic": true, "other": true}

// SeedRule is one per-origin seeding policy. A nil limit leaves that limit to
// the global setting; 0 means "no limit" (it overrides a global limit); a
// positive value is the limit.
type SeedRule struct {
	Name string `json:"name"`
	// Source matches the release source (the indexer name as Gextto records
	// it, e.g. "mircrew indexerr", "jackett:LimeTorrents", "archive:ExtTo") by
	// a case-insensitive substring. Empty matches any source.
	Source string `json:"source"`
	// Category is series, movie, comic or other; empty matches any category.
	Category    string   `json:"category"`
	Ratio       *float64 `json:"ratio"`
	SeedDays    *float64 `json:"seed_days"`
	IdleMinutes *float64 `json:"idle_minutes"`
}

// seedOrigin is where a torrent came from, as recorded in torrent_meta.
type seedOrigin struct {
	Source   string
	Category string
}

// seedCategory maps torrent_meta's kind and tag to a rule category.
func seedCategory(kind, tag string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "series", "episode":
		return "series"
	case "movie":
		return "movie"
	case "comic":
		return "comic"
	}
	if strings.EqualFold(strings.TrimSpace(tag), "comic") {
		return "comic"
	}
	return "other"
}

// matches reports whether the rule applies to a torrent of this origin.
func (r SeedRule) matches(origin seedOrigin) bool {
	if r.Category != "" && r.Category != origin.Category {
		return false
	}
	needle := strings.ToLower(strings.TrimSpace(r.Source))
	if needle != "" && !strings.Contains(strings.ToLower(origin.Source), needle) {
		return false
	}
	return true
}

// validate refuses a rule that would be ambiguous or out of range.
func (r SeedRule) validate() error {
	if strings.TrimSpace(r.Source) == "" && r.Category == "" {
		return errors.New("a seed rule needs a source, a category or both")
	}
	if len(r.Name) > 128 || len(r.Source) > 256 {
		return errors.New("seed rule name or source too long")
	}
	if !seedRuleCategories[r.Category] {
		return fmt.Errorf("unknown seed rule category %q", r.Category)
	}
	for _, limit := range []struct {
		value *float64
		max   float64
		what  string
	}{
		{r.Ratio, 1000, "ratio"},
		{r.SeedDays, 3650, "seed days"},
		{r.IdleMinutes, 525600, "idle minutes"},
	} {
		if limit.value == nil {
			continue
		}
		if math.IsNaN(*limit.value) || *limit.value < 0 || *limit.value > limit.max {
			return fmt.Errorf("seed rule %s must be between 0 and %g", limit.what, limit.max)
		}
	}
	return nil
}

// normalizeSeedRules trims the rules, drops negative limits (the editor's
// "inherit") and validates them.
func normalizeSeedRules(rules []SeedRule) ([]SeedRule, error) {
	if len(rules) > maxSeedRules {
		return nil, fmt.Errorf("at most %d seed rules", maxSeedRules)
	}
	out := make([]SeedRule, 0, len(rules))
	for _, rule := range rules {
		rule.Name = strings.TrimSpace(rule.Name)
		rule.Source = strings.TrimSpace(rule.Source)
		rule.Category = strings.ToLower(strings.TrimSpace(rule.Category))
		for _, limit := range []**float64{&rule.Ratio, &rule.SeedDays, &rule.IdleMinutes} {
			if *limit != nil && **limit < 0 {
				*limit = nil
			}
		}
		if err := rule.validate(); err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, nil
}

// parseSeedRules reads the rules setting; an invalid value yields no rules (the
// writes are validated, so only a hand-edited database gets here).
func parseSeedRules(raw string) []SeedRule {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var rules []SeedRule
	if json.Unmarshal([]byte(raw), &rules) != nil {
		return nil
	}
	rules, err := normalizeSeedRules(rules)
	if err != nil {
		return nil
	}
	return rules
}

// seedRulesFrom returns the configured rules.
func seedRulesFrom(cfg *Config) []SeedRule {
	if cfg == nil {
		return nil
	}
	return parseSeedRules(cfg.Settings[seedRulesKey])
}

// validateSeedRulesSetting is the validateSettingValue check of the rules.
func validateSeedRulesSetting(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var rules []SeedRule
	if err := json.Unmarshal([]byte(value), &rules); err != nil {
		return errors.New("seed rules must be a JSON list")
	}
	_, err := normalizeSeedRules(rules)
	return err
}

// matchSeedRule returns the first rule matching the origin, nil when none does.
func matchSeedRule(rules []SeedRule, origin seedOrigin) *SeedRule {
	for i := range rules {
		if rules[i].matches(origin) {
			return &rules[i]
		}
	}
	return nil
}

// seedLimits are the effective limits of one torrent: nil means no such limit.
type seedLimits struct {
	Ratio       *float64
	Seconds     *int64
	IdleSeconds *int64
	// Rule names the matching rule (empty when none matched).
	Rule string
}

// none reports a torrent that seeds forever.
func (l seedLimits) none() bool {
	return l.Ratio == nil && l.Seconds == nil && l.IdleSeconds == nil
}

// resolveSeedLimits combines the torrent's own limits, the matching rule and
// the global ones: for the ratio and for the time the first that is set wins
// (torrent, then rule, then global). A zero means infinite. When either the
// ratio or the time that applies is infinite the seed never ends: every limit
// is off, inactivity included, and the file stays where it is seeding (a
// torrent is not moved at the end of a seed that never ends, an eD2k file is
// copied instead of moved).
func resolveSeedLimits(cfg *Config, rule *SeedRule, seedRatio float64, seedDays int64) seedLimits {
	limits := seedLimits{}
	if rule != nil {
		limits.Rule = rule.Name
		if limits.Rule == "" {
			limits.Rule = rule.Source
		}
	}
	positiveFloat := func(value float64) *float64 { return &value }
	positiveSeconds := func(value int64) *int64 { return &value }

	// The ratio that applies: torrent (>= 0 is set), rule, global.
	var ratio float64
	switch {
	case seedRatio >= 0:
		ratio = seedRatio
	case rule != nil && rule.Ratio != nil:
		ratio = *rule.Ratio
	case cfg != nil:
		ratio = cfg.Transfer.SeedRatio
	}
	// The time that applies, in seconds: torrent days, rule days, global days
	// or minutes.
	var seconds int64
	switch {
	case seedDays >= 0:
		seconds = tev_saturatingMulInt64(seedDays, 86_400)
	case rule != nil && rule.SeedDays != nil:
		seconds = int64(math.Round(*rule.SeedDays * 86_400))
	case cfg != nil && cfg.Transfer.SeedTimeDays > 0:
		seconds = tev_saturatingMulInt64(cfg.Transfer.SeedTimeDays, 86_400)
	case cfg != nil:
		seconds = tev_saturatingMulInt64(cfg.Transfer.SeedTimeMinutes, 60)
	}
	if ratio <= 0 || seconds <= 0 {
		return seedLimits{Rule: limits.Rule}
	}
	limits.Ratio = positiveFloat(ratio)
	limits.Seconds = positiveSeconds(seconds)

	switch {
	case rule != nil && rule.IdleMinutes != nil:
		if *rule.IdleMinutes > 0 {
			limits.IdleSeconds = positiveSeconds(int64(math.Round(*rule.IdleMinutes * 60)))
		}
	case cfg != nil && cfg.Transfer.SeedIdleMinutes > 0:
		limits.IdleSeconds = positiveSeconds(tev_saturatingMulInt64(cfg.Transfer.SeedIdleMinutes, 60))
	}
	return limits
}

// ratioReached reports whether the torrent's share ratio passed the limit.
func (l seedLimits) ratioReached(torrent *models.TorrentView) bool {
	if l.Ratio == nil {
		return false
	}
	download := tev_effectiveDownloadForRatio(torrent)
	return download > 0 && float64(torrent.AllTimeUpload)/download >= *l.Ratio
}

// timeReached reports whether the torrent seeded for the limit.
func (l seedLimits) timeReached(torrent *models.TorrentView) bool {
	return l.Seconds != nil && torrent.SeedingSeconds >= *l.Seconds
}

// seedPolicy resolves the limits of the torrents seen in one pass. It reads the
// origins from the database once, and only when rules exist.
type seedPolicy struct {
	cfg     *Config
	db      *Database
	rules   []SeedRule
	origins map[string]seedOrigin
}

// newSeedPolicy prepares the resolution for one pass over the session. The
// first one also reads back the seeds stopped for inactivity before a restart.
func newSeedPolicy(cfg *Config, db *Database) *seedPolicy {
	policy := &seedPolicy{cfg: cfg, db: db, rules: seedRulesFrom(cfg)}
	if db != nil {
		seedIdle.restoreOnce(db)
		if len(policy.rules) > 0 {
			policy.origins, _ = db.TorrentOrigins()
		}
	}
	return policy
}

// limitsFor returns the effective limits of one torrent.
func (p *seedPolicy) limitsFor(torrent *models.TorrentView) seedLimits {
	var rule *SeedRule
	if len(p.rules) > 0 {
		origin, ok := p.origins[strings.ToLower(torrent.Hash)]
		if !ok {
			origin = seedOrigin{Category: "other"}
		}
		rule = matchSeedRule(p.rules, origin)
	}
	return resolveSeedLimits(p.cfg, rule, torrent.SeedRatio, torrent.SeedDays)
}

// TorrentOrigins returns the source and category of every recorded torrent,
// keyed by lowercase hash.
func (d *Database) TorrentOrigins() (map[string]seedOrigin, error) {
	rows, err := d.db.Query("SELECT lower(hash), COALESCE(source,''), COALESCE(kind,''), COALESCE(tag,'') FROM torrent_meta")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	origins := map[string]seedOrigin{}
	for rows.Next() {
		var hash, source, kind, tag string
		if err := rows.Scan(&hash, &source, &kind, &tag); err != nil {
			return nil, err
		}
		origins[hash] = seedOrigin{Source: source, Category: seedCategory(kind, tag)}
	}
	return origins, rows.Err()
}

// seedIdleTracker measures, per torrent, how long it has been seeding without
// uploading anything, and remembers the seeds it stopped. That verdict is also
// kept in torrent_meta.seed_idle_stop: a seed paused for inactivity looks like
// any paused torrent, and without it a restart before the end-of-seed handling
// (archive, removal) finished would leave the torrent paused for good.
type seedIdleTracker struct {
	mu       sync.Mutex
	marks    map[string]seedIdleMark
	restored bool
}

type seedIdleMark struct {
	upload int64
	since  time.Time
	// reached keeps a seed stopped for inactivity recognised as such while it
	// stays stopped, so the end-of-seed handling (archive, removal) completes
	// on the following passes too.
	reached bool
	seen    bool
}

// restoredUpload marks a verdict read back from the database: the uploaded
// bytes are unknown and the first sighting adopts them.
const restoredUpload = -1

// seedIdle is the process-wide tracker used by EnforceSeedPolicy.
var seedIdle = &seedIdleTracker{marks: map[string]seedIdleMark{}}

// restoreOnce reads back, once per process, the seeds stopped for inactivity.
func (t *seedIdleTracker) restoreOnce(db *Database) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.restored {
		return
	}
	t.restored = true
	hashes, err := db.SeedIdleStops()
	if err != nil {
		return
	}
	now := time.Now()
	for _, hash := range hashes {
		t.marks[hash] = seedIdleMark{upload: restoredUpload, since: now, reached: true, seen: true}
	}
}

// observe records one sighting of a torrent and returns how long it has been
// idle while seeding (0 when it is not seeding or just uploaded), and whether a
// seed stopped for inactivity is seeding again (its verdict is then dropped).
func (t *seedIdleTracker) observe(hash string, upload int64, seeding bool, now time.Time) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := strings.ToLower(hash)
	mark, ok := t.marks[key]
	resumed := false
	switch {
	case ok && mark.upload == restoredUpload:
		mark.upload = upload
		if seeding {
			resumed = mark.reached
			mark = seedIdleMark{upload: upload, since: now}
		}
	case !ok, upload != mark.upload:
		resumed = ok && mark.reached
		mark = seedIdleMark{upload: upload, since: now}
	case !seeding:
		// Not seeding (paused or queued): the clock does not run. A seed
		// already stopped for inactivity keeps its verdict.
		if !mark.reached {
			mark.since = now
		}
	case mark.reached:
		// Seeding again after a stop for inactivity (resumed by hand): a new
		// period starts.
		resumed = true
		mark = seedIdleMark{upload: upload, since: now}
	}
	mark.seen = true
	t.marks[key] = mark
	if !seeding && !mark.reached {
		return 0, resumed
	}
	return now.Sub(mark.since), resumed
}

// markReached remembers that the torrent was stopped for inactivity.
func (t *seedIdleTracker) markReached(hash string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := strings.ToLower(hash)
	mark := t.marks[key]
	mark.reached = true
	mark.seen = true
	t.marks[key] = mark
}

// reached reports whether the torrent was stopped for inactivity.
func (t *seedIdleTracker) reached(hash string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.marks[strings.ToLower(hash)].reached
}

// sweep forgets the torrents not observed since the previous sweep (removed
// from the session). A stop verdict stays: a torrent missing from one listing
// (the engine restarting) keeps it.
func (t *seedIdleTracker) sweep() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, mark := range t.marks {
		if !mark.seen && !mark.reached {
			delete(t.marks, key)
			continue
		}
		mark.seen = false
		t.marks[key] = mark
	}
}

// idleReached reports whether a torrent passed its inactivity limit (or was
// already stopped for it), recording the observation.
func (p *seedPolicy) idleReached(torrent *models.TorrentView, limits seedLimits, now time.Time) bool {
	idle, resumed := seedIdle.observe(torrent.Hash, torrent.AllTimeUpload, torrent.State == "seeding", now)
	if resumed && p.db != nil {
		_ = p.db.SetSeedIdleStop(torrent.Hash, false)
	}
	if limits.IdleSeconds == nil {
		return false
	}
	if seedIdle.reached(torrent.Hash) {
		return true
	}
	return idle >= time.Duration(*limits.IdleSeconds)*time.Second
}

// markIdleStop records that the torrent was stopped for inactivity, in memory
// and in the database.
func (p *seedPolicy) markIdleStop(hash string) {
	seedIdle.markReached(hash)
	if p.db != nil {
		_ = p.db.SetSeedIdleStop(hash, true)
	}
}

// SetSeedIdleStop records or clears the inactivity stop of a torrent.
func (d *Database) SetSeedIdleStop(hash string, stopped bool) error {
	if !stopped {
		_, err := d.db.Exec("UPDATE torrent_meta SET seed_idle_stop=0 WHERE hash=?1", strings.ToLower(hash))
		return err
	}
	_, err := d.db.Exec("INSERT INTO torrent_meta(hash,seed_idle_stop,status,created_at,updated_at) VALUES (?1,1,'queued',datetime('now'),datetime('now')) ON CONFLICT(hash) DO UPDATE SET seed_idle_stop=1, updated_at=excluded.updated_at", strings.ToLower(hash))
	return err
}

// SeedIdleStops returns the torrents stopped for inactivity.
func (d *Database) SeedIdleStops() ([]string, error) {
	rows, err := d.db.Query("SELECT lower(hash) FROM torrent_meta WHERE seed_idle_stop=1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	return hashes, rows.Err()
}

// SeedRulesView handles GET /api/seed-rules.
func SeedRulesView(w http.ResponseWriter, r *http.Request, s *AppState) {
	rules := seedRulesFrom(LatestConfig(s))
	if rules == nil {
		rules = []SeedRule{}
	}
	jsonResponse(w, map[string]any{"ok": true, "items": rules})
}

// SaveSeedRules handles POST /api/seed-rules: it replaces the rules.
func SaveSeedRules(w http.ResponseWriter, r *http.Request, s *AppState) {
	rules := []SeedRule{}
	if err := decodeJSON(r, &rules); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	rules, err := normalizeSeedRules(rules)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload, err := json.Marshal(rules)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := saveConfigSetting(s.cfg.DataDir, seedRulesKey, string(payload)); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "items": rules})
}
