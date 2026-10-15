package rss

// rules.go adds the advanced, ordered rule engine (the feature set that makes
// the manager comparable to qBittorrent, Deluge/YaRSS2 and BiglyBT): PASS/FAIL
// rules evaluated in order (first match wins), numeric filters (size, seeders,
// peers, age), episode/season matching and per-rule actions.

import (
	"regexp"
	"strings"
	"time"
)

// Criteria is what a rule matches on. Empty/zero fields are not enforced.
type Criteria struct {
	Include []string `json:"include,omitempty"` // at least one term
	// Require lists terms that must all appear (Include is one of them).
	Require  []string `json:"require,omitempty"`
	Exclude  []string `json:"exclude,omitempty"` // no term
	Regex    string   `json:"regex,omitempty"`   // title must match
	NotRegex string   `json:"not_regex,omitempty"`

	MinSize int64 `json:"min_size,omitempty"` // bytes
	MaxSize int64 `json:"max_size,omitempty"`
	// Seeders: 0 means "not enforced"; a negative Min rejects untracked swarms.
	MinSeeders int `json:"min_seeders,omitempty"`
	MaxSeeders int `json:"max_seeders,omitempty"`
	MinPeers   int `json:"min_peers,omitempty"`
	MaxAgeDays int `json:"max_age_days,omitempty"`

	RequireEpisode bool `json:"require_episode,omitempty"`
	SmartEpisode   bool `json:"smart_episode,omitempty"` // daemon keeps the state
	// EpisodeFilter restricts the rule to some episodes (qBittorrent-style
	// syntax: "1x01", "1x01-05", "1x01-", "1x??"). Empty = every episode.
	EpisodeFilter string `json:"episode_filter,omitempty"`
}

// Action is what happens when a PASS rule matches.
type Action struct {
	SavePath   string   `json:"savepath,omitempty"`
	Category   string   `json:"category,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Paused     bool     `json:"paused,omitempty"`
	Sequential bool     `json:"sequential,omitempty"`
	FirstLast  bool     `json:"first_last,omitempty"`
	Top        bool     `json:"top,omitempty"`
	// Once disables the rule after it adds an item, so following a movie does
	// not queue every release the indexer returns.
	Once bool `json:"once,omitempty"`
}

// Rule is one entry of the ordered rule set.
type Rule struct {
	Name string `json:"name,omitempty"`
	// Feeds scopes the rule to some feeds by name; empty = every feed.
	Feeds []string `json:"feeds,omitempty"`
	// Enabled is optional: nil (the common case) means enabled, a pointer to
	// false disables the rule without deleting it.
	Enabled *bool `json:"enabled,omitempty"`
	// IgnoreDays is a cooldown: after the rule matches, it does not match again
	// for this many days of wall-clock time (like qBittorrent's "ignore
	// subsequent matches"). 0 = off.
	IgnoreDays int      `json:"ignore_days,omitempty"`
	Fail       bool     `json:"fail,omitempty"` // FAIL: matching items are skipped
	Match      Criteria `json:"match"`
	Action     Action   `json:"action,omitempty"`
}

// RuleEnabled reports whether a rule is active (nil Enabled means yes).
func RuleEnabled(rule Rule) bool {
	return rule.Enabled == nil || *rule.Enabled
}

// FindEpisode extracts the series, season and episode from a title like
// "Show.Name.S02E07.1080p" or "Show 2x07". It returns ok=false when the title
// carries no episode marker.
func FindEpisode(title string) (series string, season, episode int, ok bool) {
	if loc := episodeRegex.FindStringSubmatchIndex(title); loc != nil {
		season = atoiOr(title[loc[2]:loc[3]], 0)
		episode = atoiOr(title[loc[4]:loc[5]], 0)
		return normalizeSeries(title[:loc[0]]), season, episode, true
	}
	if loc := episodeXRegex.FindStringSubmatchIndex(title); loc != nil {
		season = atoiOr(title[loc[2]:loc[3]], 0)
		episode = atoiOr(title[loc[4]:loc[5]], 0)
		return normalizeSeries(title[:loc[0]]), season, episode, true
	}
	return "", 0, 0, false
}

var (
	episodeRegex  = regexp.MustCompile(`(?i)[.\s_\-]?s(\d{1,2})[.\s_\-]?e(\d{1,3})`)
	episodeXRegex = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)
	// seasonPackRegex finds a season marker without an episode: S04 (with an
	// optional disc suffix S03D01), Season 4, Stagione 4. It is the pack
	// counterpart of FindEpisode, so a title like "Reacher S04 COMPLETE" or
	// "Reacher (2022) S04 1080p WEBRip" yields its season.
	seasonPackSRegex       = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])s(\d{1,2})(?:d\d{1,2})?(?:[\s._\-\]\)\.]|$)`)
	seasonPackWordRegex    = regexp.MustCompile(`(?i)\bseason[\s._\-]*(\d{1,2})\b`)
	seasonPackStagioneRegex = regexp.MustCompile(`(?i)\bstagione[\s._\-]*(\d{1,2})\b`)
)

// FindSeason extracts the season number from a season-pack title like
// "Show.S04.1080p", "Show Season 2 Complete" or "Show - S04 (2026) [COMPLETE]".
// It returns ok=false when the title carries no season marker. Unlike
// FindEpisode it also matches titles that carry an episode marker (the caller
// checks FindEpisode first when it needs packs only).
func FindSeason(title string) (season int, ok bool) {
	if loc := seasonPackSRegex.FindStringSubmatch(title); loc != nil {
		if season = atoiOr(loc[1], 0); season > 0 {
			return season, true
		}
	}
	if loc := seasonPackWordRegex.FindStringSubmatch(title); loc != nil {
		if season = atoiOr(loc[1], 0); season > 0 {
			return season, true
		}
	}
	if loc := seasonPackStagioneRegex.FindStringSubmatch(title); loc != nil {
		if season = atoiOr(loc[1], 0); season > 0 {
			return season, true
		}
	}
	return 0, false
}

// normalizeSeries reduces the part before the episode marker to a stable key.
func normalizeSeries(value string) string {
	value = strings.ToLower(value)
	value = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

// Match reports whether the item satisfies the criteria at time now.
func (c Criteria) Match(item Item, now time.Time) bool {
	title := item.Title

	// Terms are found with HasTerm (separators normalized, word starts).
	if countTerms(c.Include) > 0 && !hasAnyTerm(title, c.Include) {
		return false
	}
	for _, term := range c.Require {
		if strings.TrimSpace(term) != "" && !HasTerm(title, term) {
			return false
		}
	}
	if hasAnyTerm(title, c.Exclude) {
		return false
	}
	if c.Regex != "" {
		re, err := regexp.Compile(c.Regex)
		if err != nil || !re.MatchString(title) {
			return false
		}
	}
	if c.NotRegex != "" {
		if re, err := regexp.Compile(c.NotRegex); err == nil && re.MatchString(title) {
			return false
		}
	}

	// Size: enforced only when the item reports one (unknown sizes pass, so a
	// feed that omits it is not dropped wholesale).
	if item.Size > 0 {
		if c.MinSize > 0 && item.Size < c.MinSize {
			return false
		}
		if c.MaxSize > 0 && item.Size > c.MaxSize {
			return false
		}
	}
	if c.MinSeeders != 0 && item.Seeders < c.MinSeeders {
		return false
	}
	if c.MaxSeeders > 0 && item.Seeders > c.MaxSeeders {
		return false
	}
	if c.MinPeers > 0 && item.Leechers < c.MinPeers {
		return false
	}
	if c.MaxAgeDays > 0 && !item.Published.IsZero() {
		if now.Sub(item.Published) > time.Duration(c.MaxAgeDays)*24*time.Hour {
			return false
		}
	}
	if c.RequireEpisode {
		if _, _, _, ok := FindEpisode(title); !ok {
			return false
		}
	}
	if c.EpisodeFilter != "" {
		// A malformed filter never matches: a typo must not download the whole
		// season. The rule editor refuses invalid filters before saving.
		filter, err := ParseEpisodeFilter(c.EpisodeFilter)
		if err != nil || !filter.Match(title) {
			return false
		}
	}
	return true
}

// RejectReason names the first criterion that turns item away, or "" when the
// item satisfies every criterion. It mirrors Criteria.Match check by check, so
// the rule preview (and the feed items view) can tell why an archived release
// stays out instead of just hiding it. Stable keys: "include", "require",
// "exclude", "regex", "not_regex", "min_size", "max_size", "min_seeders",
// "max_seeders", "min_peers", "max_age_days", "require_episode",
// "episode_filter".
func (c Criteria) RejectReason(item Item, now time.Time) string {
	title := item.Title

	if countTerms(c.Include) > 0 && !hasAnyTerm(title, c.Include) {
		return "include"
	}
	for _, term := range c.Require {
		if strings.TrimSpace(term) != "" && !HasTerm(title, term) {
			return "require"
		}
	}
	if hasAnyTerm(title, c.Exclude) {
		return "exclude"
	}
	if c.Regex != "" {
		re, err := regexp.Compile(c.Regex)
		if err != nil || !re.MatchString(title) {
			return "regex"
		}
	}
	if c.NotRegex != "" {
		if re, err := regexp.Compile(c.NotRegex); err == nil && re.MatchString(title) {
			return "not_regex"
		}
	}
	if item.Size > 0 {
		if c.MinSize > 0 && item.Size < c.MinSize {
			return "min_size"
		}
		if c.MaxSize > 0 && item.Size > c.MaxSize {
			return "max_size"
		}
	}
	if c.MinSeeders != 0 && item.Seeders < c.MinSeeders {
		return "min_seeders"
	}
	if c.MaxSeeders > 0 && item.Seeders > c.MaxSeeders {
		return "max_seeders"
	}
	if c.MinPeers > 0 && item.Leechers < c.MinPeers {
		return "min_peers"
	}
	if c.MaxAgeDays > 0 && !item.Published.IsZero() {
		if now.Sub(item.Published) > time.Duration(c.MaxAgeDays)*24*time.Hour {
			return "max_age_days"
		}
	}
	if c.RequireEpisode {
		if _, _, _, ok := FindEpisode(title); !ok {
			return "require_episode"
		}
	}
	if c.EpisodeFilter != "" {
		filter, err := ParseEpisodeFilter(c.EpisodeFilter)
		if err != nil || !filter.Match(title) {
			return "episode_filter"
		}
	}
	return ""
}

// Evaluate walks the rules in order and returns the first that matches the item
// for feedName: pass=true means "add it" (using the rule's Action), pass=false
// means the rule explicitly skips it. matched is false when no rule applies.
func Evaluate(rules []Rule, feedName string, item Item, now time.Time) (rule Rule, pass, matched bool) {
	return EvaluateWithLastMatch(rules, feedName, item, now, nil)
}

// EvaluateWithLastMatch is Evaluate with the per-rule last-match lookup used by
// the ignore-days window: a rule with IgnoreDays > 0 only matches an item
// published at or after lastMatch(rule)+IgnoreDays. lastMatch may be nil.
func EvaluateWithLastMatch(rules []Rule, feedName string, item Item, now time.Time, lastMatch func(ruleName string) time.Time) (rule Rule, pass, matched bool) {
	for _, candidate := range rules {
		if !RuleEnabled(candidate) {
			continue
		}
		if !ruleAppliesToFeed(candidate, feedName) {
			continue
		}
		if !ruleWithinIgnoreWindow(candidate, now, lastMatch) {
			continue
		}
		if candidate.Match.Match(item, now) {
			return candidate, !candidate.Fail, true
		}
	}
	return Rule{}, false, false
}

// ruleWithinIgnoreWindow reports whether a rule may match at time now. With
// IgnoreDays > 0 the rule is a wall-clock cooldown: it does not match again
// until that many days have passed since its last match (like qBittorrent's
// "ignore subsequent matches for N days"). An unknown last match means "match".
func ruleWithinIgnoreWindow(rule Rule, now time.Time, lastMatch func(string) time.Time) bool {
	if rule.IgnoreDays <= 0 || lastMatch == nil {
		return true
	}
	last := lastMatch(rule.Name)
	if last.IsZero() {
		return true
	}
	return !now.Before(last.Add(time.Duration(rule.IgnoreDays) * 24 * time.Hour))
}

// RuleAppliesToFeed reports whether rule watches feedName (a rule with no
// Feeds watches every feed); a disabled rule still applies.
func RuleAppliesToFeed(rule Rule, feedName string) bool { return ruleAppliesToFeed(rule, feedName) }

func ruleAppliesToFeed(rule Rule, feedName string) bool {
	if len(rule.Feeds) == 0 {
		return true
	}
	for _, feed := range rule.Feeds {
		if strings.EqualFold(strings.TrimSpace(feed), feedName) {
			return true
		}
	}
	return false
}

// FeedCoveredByRules reports whether at least one rule targets feedName (a rule
// with no Feeds applies to every feed). When no rule covers a feed, the feed's
// own include/exclude filter still governs it: adding a rule for one feed must
// not silently stop the other feeds. A disabled rule still covers its feeds:
// turning off the only rule (or a "download once" rule disabling itself) must
// stop the downloads, not fall back to the feed filter, which takes everything
// when it is empty.
func FeedCoveredByRules(rules []Rule, feedName string) bool {
	for _, rule := range rules {
		if ruleAppliesToFeed(rule, feedName) {
			return true
		}
	}
	return false
}
