package main

// feed.go is the standalone RSS engine: it follows the configured feeds
// (Jackett, Prowlarr, MIRCrew and any RSS listing), applies include/exclude
// rules and adds the matching torrents. It runs only in standalone mode: in
// managed mode Gextto owns acquisition, so the loop never starts.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
)

const (
	feedsSettingKey     = "feeds"
	rulesSettingKey     = "rules"
	feedIntervalKey     = "feed-interval-secs"
	defaultFeedInterval = 15 * time.Minute
	feedSeenRetention   = 30 * 24 * time.Hour
	feedRequestTimeout  = 30 * time.Second
	// indexerSearchTimeout gives a Torznab indexer more room than a plain RSS
	// fetch: MirCrew, for instance, may search its forum before answering.
	indexerSearchTimeout = 45 * time.Second
	feedMaxAddsPerPoll   = 25
)

// feedConfig is one configured feed with its rules. A feed is either an RSS
// URL or a Torznab search query ("search"); the search results are processed by
// the same rules as a feed.
type feedConfig struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Search   string `json:"search,omitempty"`  // Torznab query, across all indexers
	Folder   string `json:"folder,omitempty"`  // optional group shown in the page
	Include  string `json:"include,omitempty"` // comma-separated
	Exclude  string `json:"exclude,omitempty"`
	Category string `json:"category,omitempty"`
	SavePath string `json:"savepath,omitempty"`
	Paused   bool   `json:"paused,omitempty"`
}

func (c feedConfig) rule() rss.TitleFilter {
	return rss.TitleFilter{Include: splitList(c.Include), Exclude: splitList(c.Exclude)}
}

// feedStatus is the last outcome of one feed, for the page.
type feedStatus struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Items     int    `json:"items"`
	Added     int    `json:"added"`
	LastError string `json:"last_error,omitempty"`
	LastCheck int64  `json:"last_check,omitempty"`
}

func splitList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

// feeds returns the configured feeds (empty when none/invalid).
func (d *Daemon) feeds() []feedConfig {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(feedsSettingKey, ""))
	if raw == "" {
		return nil
	}
	var list []feedConfig
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		logf("cannot parse the feeds setting: %v", err)
		return nil
	}
	return list
}

func (d *Daemon) feedInterval() time.Duration {
	if d.opts.Settings == nil {
		return defaultFeedInterval
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(feedIntervalKey, ""))
	if raw == "" {
		return defaultFeedInterval
	}
	secs, err := time.ParseDuration(raw + "s")
	if err != nil || secs < 60*time.Second {
		return defaultFeedInterval
	}
	return secs
}

// runFeeds polls the feeds on an interval until stop is closed. It runs for the
// whole standalone lifetime so feeds added at runtime (the RSS page, the
// wizard) are picked up on the next round; with no feed configured pollFeeds is
// a cheap no-op. In managed mode Gextto owns acquisition, so the loop never
// starts.
func (d *Daemon) runFeeds(stop <-chan struct{}) {
	if d.opts.Mode != ModeStandalone {
		return
	}
	// A short initial delay lets the session settle before the first poll; a
	// feed added meanwhile (the wizard or the RSS page) skips the wait.
	select {
	case <-stop:
		return
	case <-d.feedWake:
	case <-time.After(30 * time.Second):
	}
	for {
		d.pollFeeds()
		select {
		case <-stop:
			return
		case <-d.feedWake:
			// The feeds were just edited: poll again now instead of waiting a
			// whole interval.
		case <-time.After(d.feedInterval()):
		}
	}
}

// pokeFeeds asks the RSS loop for an immediate poll, so a feed saved at runtime
// (handleUIRssConfig) is checked at once instead of after the whole interval.
func (d *Daemon) pokeFeeds() {
	if d.feedWake == nil {
		return
	}
	select {
	case d.feedWake <- struct{}{}:
	default:
	}
}

// pollFeeds fetches every feed once, applies its rules and adds the matching
// torrents that were not added before. Only one poll runs at a time: the loop
// and the manual button must not fetch the same feed together.
func (d *Daemon) pollFeeds() {
	if !d.feedPollMu.TryLock() {
		return
	}
	defer d.feedPollMu.Unlock()
	feeds := d.feeds()
	if len(feeds) == 0 {
		return
	}
	client := &http.Client{Timeout: feedRequestTimeout}
	var rulesOnce []string
	onceHit := map[string]bool{}
	for _, feed := range feeds {
		search := strings.TrimSpace(feed.Search)
		if strings.TrimSpace(feed.URL) == "" && search == "" {
			continue
		}
		source := feed.URL
		if search != "" {
			source = "search:" + search
		}
		name := feed.Name
		if name == "" {
			name = source
		}
		timeout := feedRequestTimeout
		if search != "" {
			timeout = indexerSearchTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		var items []rss.Item
		var err error
		if search != "" {
			items, err = d.searchFeed(ctx, name, search)
		} else {
			items, err = rss.Fetch(ctx, client, feed.URL, name)
		}
		cancel()
		status := feedStatus{Name: name, URL: source, LastCheck: time.Now().Unix()}
		// Notify only when a feed enters the error state: a feed that keeps
		// failing at every poll must not send the same webhook over and over.
		wasError := d.feedErrorState(name)
		addErrorNotified := false
		if err != nil {
			status.LastError = err.Error()
			logf("feed %s: %v", name, err)
			if !wasError {
				d.notify("feed_error", map[string]any{"feed": name, "error": err.Error()})
			}
			d.setFeedStatus(status)
			continue
		}
		status.Items = len(items)
		added := 0
		var addedTitles []string
		rules := d.rules()
		for _, item := range items {
			if added >= feedMaxAddsPerPoll {
				break
			}
			rule, smart, ok := filterFeedItem(rules, feed, name, item, time.Now(), d.ruleLastMatch)
			if !ok {
				continue
			}
			if rule.Action.Once && onceHit[rule.Name] {
				continue
			}
			if d.feedSeen(name, item.GUID) {
				continue
			}
			smartKey := ""
			var smartTokens []string
			if smart {
				series, season, episode, found := rss.FindEpisode(item.Title)
				if !found {
					continue
				}
				smartKey = fmt.Sprintf("%s\x00%d", series, season)
				smartTokens = d.smartEpisodeVerdict(smartKey, item.Title, episode)
				if len(smartTokens) == 0 {
					continue
				}
			}
			newlyAdded, err := d.addFeedItem(item, rule.Action)
			if err != nil {
				status.LastError = err.Error()
				logf("feed %s: cannot add %q: %v", name, item.Title, err)
				if !wasError && !addErrorNotified {
					d.notify("feed_error", map[string]any{"feed": name, "title": item.Title, "error": err.Error()})
					addErrorNotified = true
				}
				continue
			}
			d.markFeedSeen(name, item.GUID)
			if smartKey != "" {
				d.markSmartEpisode(smartKey, smartTokens)
			}
			if rule.IgnoreDays > 0 {
				d.markRuleMatch(rule.Name, time.Now())
			}
			if newlyAdded {
				added++
				addedTitles = append(addedTitles, item.Title)
				if rule.Action.Once {
					onceHit[rule.Name] = true
					rulesOnce = append(rulesOnce, rule.Name)
				}
			}
		}
		// Wanted titles: record what appeared and take the missing episodes (or
		// the movie) from the same items, after the rules path.
		d.applyWanted(time.Now(), name, items)
		status.Added = added
		if added > 0 {
			logf("feed %s: added %d torrent(s)", name, added)
			d.notify("feed_match", map[string]any{"feed": name, "added": added, "titles": addedTitles})
		}
		d.setFeedStatus(status)
	}
	if len(rulesOnce) > 0 {
		d.disableRules(rulesOnce)
	}
	// One atomic write at the end of the cycle: markFeedSeen/markSmartEpisode only
	// set dirty, so a poll that adds many items does not rewrite state.json once
	// per item (and re-scan the whole FeedSeen/FeedSmart maps each time).
	d.mu.Lock()
	if d.dirty {
		d.saveLocked()
	}
	d.mu.Unlock()

	// Wanted titles: search the indexers and take the missing episodes/movies,
	// plus the gap search for what appeared but was not downloaded.
	gapCtx, cancelGap := context.WithTimeout(context.Background(), 5*time.Minute)
	d.pollWanted(gapCtx)
	cancelGap()
}

// filterFeedItem decides whether an item is added and with which action. When at
// least one rule covers the feed the rules are ordered and the first match wins
// (PASS adds, FAIL skips); otherwise the feed's own include/exclude filter
// applies. It returns the matched rule so the caller can record its last-match
// time.
func filterFeedItem(rules []rss.Rule, feed feedConfig, feedName string, item rss.Item, now time.Time, lastMatch func(string) time.Time) (rss.Rule, bool, bool) {
	if item.Download() == "" || item.Title == "" {
		return rss.Rule{}, false, false
	}
	if rss.FeedCoveredByRules(rules, feedName) {
		rule, pass, matched := rss.EvaluateWithLastMatch(rules, feedName, item, now, lastMatch)
		if !matched || !pass {
			return rss.Rule{}, false, false
		}
		return rule, rule.Match.SmartEpisode, true
	}
	if !feed.rule().Match(item.Title) {
		return rss.Rule{}, false, false
	}
	return rss.Rule{Action: rss.Action{
		SavePath: feed.SavePath,
		Category: feed.Category,
		Paused:   feed.Paused,
	}}, false, true
}

// addFeedItem adds one feed item with the rule's action: a magnet directly, a
// .torrent URL downloaded first. It reports whether the torrent was newly added
// (false when it was already in the session).
func (d *Daemon) addFeedItem(item rss.Item, action rss.Action) (bool, error) {
	req := addRequest{
		Destination: strings.TrimSpace(action.SavePath),
		Paused:      action.Paused,
		Category:    strings.TrimSpace(action.Category),
		Tags:        action.Tags,
		Sequential:  action.Sequential,
		FirstLast:   action.FirstLast,
		QueueTop:    action.Top,
	}
	if item.Magnet != "" {
		req.Magnet = item.Magnet
	} else {
		data, err := d.fetchTorrentURL(item.Link)
		if err != nil {
			return false, err
		}
		req.TorrentData = data
	}
	_, existing, err := d.add(req)
	return !existing, err
}

// rules returns the configured ordered rules (empty when none/invalid).
func (d *Daemon) rules() []rss.Rule {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(rulesSettingKey, ""))
	if raw == "" {
		return nil
	}
	var list []rss.Rule
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		logf("cannot parse the rules setting: %v", err)
		return nil
	}
	return list
}

// smartEpisodeSeen reports whether an episode token of a series+season was
// already taken (smart episode). Keeping the whole set, not just the highest
// number, means an episode that arrives after a newer one is still accepted.
func (d *Daemon) smartEpisodeSeen(key, token string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	eps, ok := d.state.FeedSmart[key]
	if !ok {
		return false
	}
	_, seen := eps[token]
	return seen
}

// smartEpisodeVerdict decides whether a smart-episode item must be added and
// returns the tokens to record on success (nil = skip). It mirrors qBittorrent's
// smart episode filter: the first release of an episode is taken; a later REPACK
// or PROPER of an already-taken episode is taken once per combination, so a
// repack is not downloaded again and again.
func (d *Daemon) smartEpisodeVerdict(key, title string, episode int) []string {
	base := strconv.Itoa(episode)
	upper := strings.ToUpper(title)
	isRepack := strings.Contains(upper, "REPACK")
	isProper := strings.Contains(upper, "PROPER")
	token := base
	if isRepack {
		token += "-REPACK"
	}
	if isProper {
		token += "-PROPER"
	}
	if !d.smartEpisodeSeen(key, base) {
		// A brand-new episode that arrives as a repack/proper: record the base
		// (so the plain release is not taken afterwards) and every lower token,
		// so a later single REPACK or PROPER of the same episode is not taken a
		// second time.
		tokens := []string{base}
		if isRepack {
			tokens = append(tokens, base+"-REPACK")
		}
		if isProper {
			tokens = append(tokens, base+"-PROPER")
		}
		if isRepack && isProper {
			tokens = append(tokens, token)
		}
		return tokens
	}
	// The episode is already taken: only a new repack/proper combination is.
	if !isRepack && !isProper {
		return nil
	}
	if d.smartEpisodeSeen(key, token) {
		return nil
	}
	tokens := []string{token}
	if isRepack && isProper {
		tokens = append(tokens, base+"-REPACK", base+"-PROPER")
	}
	return tokens
}

// markSmartEpisode records the episode tokens as taken and prunes entries older
// than the retention window.
func (d *Daemon) markSmartEpisode(key string, tokens []string) {
	if len(tokens) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.FeedSmart == nil {
		d.state.FeedSmart = map[string]smartEpisodes{}
	}
	eps := d.state.FeedSmart[key]
	if eps == nil {
		eps = smartEpisodes{}
		d.state.FeedSmart[key] = eps
	}
	now := time.Now()
	for _, token := range tokens {
		eps[token] = now.Unix()
	}
	for token, at := range eps {
		if at > 0 && now.Sub(time.Unix(at, 0)) > feedSeenRetention {
			delete(eps, token)
		}
	}
	// Only mark dirty: pollFeeds does one atomic save at the end of the cycle,
	// instead of rewriting state.json for every item added.
	d.dirty = true
}

// ruleLastMatch returns the wall-clock time a rule last accepted an item, or the
// zero time when the rule never matched (used by the ignore-days cooldown).
func (d *Daemon) ruleLastMatch(name string) time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	at, ok := d.state.RuleLastMatch[name]
	if !ok || at <= 0 {
		return time.Time{}
	}
	return time.Unix(at, 0)
}

// markRuleMatch records the wall-clock time a rule last accepted an item, so the
// ignore-days cooldown is measured from the match, not from item dates.
func (d *Daemon) markRuleMatch(name string, at time.Time) {
	if strings.TrimSpace(name) == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.RuleLastMatch == nil {
		d.state.RuleLastMatch = map[string]int64{}
	}
	d.state.RuleLastMatch[name] = at.Unix()
	d.dirty = true
}

// disableRules turns off the named rules (Once rules, after they added an item)
// and saves once for all of them. The rule stays in the list so the operator can
// re-enable it.
func (d *Daemon) disableRules(names []string) {
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	rules := d.rules()
	changed := false
	var disabled []string
	for i := range rules {
		if !wanted[rules[i].Name] {
			continue
		}
		if rules[i].Enabled == nil || *rules[i].Enabled {
			off := false
			rules[i].Enabled = &off
			changed = true
			disabled = append(disabled, rules[i].Name)
		}
	}
	if !changed {
		return
	}
	if err := d.saveRules(rules); err != nil {
		logf("cannot disable once rules %v: %v", disabled, err)
		return
	}
	for _, name := range disabled {
		logf("rule %q disabled after the first download (download once)", name)
	}
}

// feedSeenKey is the persisted key of one item.
func feedSeenKey(feed, guid string) string { return feed + "\x00" + guid }

// feedSeen reports whether an item was already added.
func (d *Daemon) feedSeen(feed, guid string) bool {
	if guid == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.state.FeedSeen[feedSeenKey(feed, guid)]
	return ok
}

// markFeedSeen records an item as added and prunes old entries.
func (d *Daemon) markFeedSeen(feed, guid string) {
	if guid == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.FeedSeen == nil {
		d.state.FeedSeen = map[string]int64{}
	}
	now := time.Now()
	d.state.FeedSeen[feedSeenKey(feed, guid)] = now.Unix()
	for key, at := range d.state.FeedSeen {
		if now.Sub(time.Unix(at, 0)) > feedSeenRetention {
			delete(d.state.FeedSeen, key)
		}
	}
	// Only mark dirty: pollFeeds does one atomic save at the end of the cycle,
	// instead of rewriting state.json for every item added.
	d.dirty = true
}

func (d *Daemon) setFeedStatus(status feedStatus) {
	d.feedMu.Lock()
	defer d.feedMu.Unlock()
	if d.feedStatuses == nil {
		d.feedStatuses = map[string]feedStatus{}
	}
	d.feedStatuses[status.Name] = status
}

// feedErrorState reports whether the last poll of this feed ended with an error.
// It is used to notify only when a feed enters (or was in) the error state,
// instead of on every failing poll.
func (d *Daemon) feedErrorState(name string) bool {
	d.feedMu.Lock()
	defer d.feedMu.Unlock()
	status, ok := d.feedStatuses[name]
	return ok && status.LastError != ""
}

// feedStatusesSnapshot returns the feeds' last outcomes, ordered by name.
func (d *Daemon) feedStatusesSnapshot() []feedStatus {
	d.feedMu.Lock()
	defer d.feedMu.Unlock()
	out := make([]feedStatus, 0, len(d.feedStatuses))
	for _, status := range d.feedStatuses {
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
