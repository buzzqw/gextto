package main

// wanted.go implements the standalone "wanted" list: the titles the operator is
// looking for. The list does not create feeds or rules: each cycle the engine
// searches the configured indexers for every title and evaluates the items of
// the RSS feeds, so the advanced rules stay free for the feeds. A per-title
// episode ledger in the state records what appeared in feeds/indexers and what
// was downloaded, and the difference is searched again (gap search) so a missing
// episode is fetched even when it does not show up in the regular poll.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
)

const wantedSettingKey = "wanted"

// wantedGapInterval throttles the missing-episode search per title.
const wantedGapInterval = 30 * time.Minute

// wantedGapMaxPerCycle bounds how many missing episodes are searched per title
// per cycle, so a large gap does not flood the indexers.
const wantedGapMaxPerCycle = 3

// wantedEntry is one title the operator wants.
type wantedEntry struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"` // "series" | "movie"
	Seasons       string `json:"seasons,omitempty"`
	Language      string `json:"language,omitempty"`
	Quality       string `json:"quality,omitempty"`
	Exclude       string `json:"exclude,omitempty"`
	EpisodeFilter string `json:"episode_filter,omitempty"`
	Category      string `json:"category,omitempty"`
	SavePath      string `json:"savepath,omitempty"`
	Paused        bool   `json:"paused,omitempty"`
}

// wantedEpisode is one missing episode of a wanted series.
type wantedEpisode struct {
	season  int
	episode int
}

func (d *Daemon) wantedList() []wantedEntry {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(wantedSettingKey, ""))
	if raw == "" {
		return nil
	}
	var list []wantedEntry
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		logf("cannot parse the wanted setting: %v", err)
		return nil
	}
	return list
}

func (d *Daemon) saveWanted(list []wantedEntry) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	d.opts.Settings.Set(wantedSettingKey, string(raw))
	return d.opts.Settings.Save()
}

// wantedIncludes are the include substrings of a wanted entry: language and
// quality, both matched as case-insensitive substrings.
func (w wantedEntry) includes() []string {
	out := append(splitList(w.Language), splitList(w.Quality)...)
	return out
}

// criteria is the match a wanted entry applies to a release title. The language
// and quality terms are checked separately (AND), because the rule engine's
// Include is an OR: wantedIncludeMatch enforces them.
func (w wantedEntry) criteria() rss.Criteria {
	return rss.Criteria{
		Exclude:        splitList(w.Exclude),
		Regex:          titleMatchRegex(w.Name),
		EpisodeFilter:  w.episodeFilter(),
		RequireEpisode: w.kind() != "movie",
	}
}

// wantedIncludeMatch reports whether the title contains every language/quality
// term of the wanted entry (an AND, unlike the rule engine's Include).
func wantedIncludeMatch(w wantedEntry, title string) bool {
	lower := strings.ToLower(title)
	for _, term := range w.includes() {
		if term = strings.ToLower(strings.TrimSpace(term)); term != "" && !strings.Contains(lower, term) {
			return false
		}
	}
	return true
}

func (w wantedEntry) kind() string {
	if strings.EqualFold(strings.TrimSpace(w.Kind), "movie") {
		return "movie"
	}
	return "series"
}

// matches reports whether a release title belongs to the wanted entry.
func (w wantedEntry) matches(title string, now time.Time) bool {
	return wantedIncludeMatch(w, title) && w.criteria().Match(rss.Item{Title: title}, now)
}

// episodeFilter is the rule episode filter: the explicit one, or the seasons
// spec turned into a filter.
func (w wantedEntry) episodeFilter() string {
	if strings.TrimSpace(w.EpisodeFilter) != "" {
		return strings.TrimSpace(w.EpisodeFilter)
	}
	return seasonsToEpisodeFilter(w.Seasons)
}

// seasonsToEpisodeFilter turns "1+", "2-3" or "1" into an episode filter.
func seasonsToEpisodeFilter(seasons string) string {
	raw := strings.TrimSpace(seasons)
	if raw == "" {
		return ""
	}
	if strings.HasSuffix(raw, "+") {
		if n, err := strconv.Atoi(strings.TrimSuffix(raw, "+")); err == nil && n > 0 {
			return strconv.Itoa(n) + "x01-"
		}
		return ""
	}
	if left, right, ok := strings.Cut(raw, "-"); ok {
		a, errA := strconv.Atoi(strings.TrimSpace(left))
		b, errB := strconv.Atoi(strings.TrimSpace(right))
		if errA == nil && errB == nil && a > 0 && b >= a {
			return strconv.Itoa(a) + "x01-" + strconv.Itoa(b) + "x99"
		}
		return ""
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return strconv.Itoa(n) + "x*"
	}
	return ""
}

// seasonAllowed reports whether a season is inside the wanted seasons spec.
func seasonAllowed(seasons string, season int) bool {
	raw := strings.TrimSpace(seasons)
	if raw == "" {
		return true
	}
	if strings.HasSuffix(raw, "+") {
		n, err := strconv.Atoi(strings.TrimSuffix(raw, "+"))
		return err == nil && season >= n
	}
	if left, right, ok := strings.Cut(raw, "-"); ok {
		a, errA := strconv.Atoi(strings.TrimSpace(left))
		b, errB := strconv.Atoi(strings.TrimSpace(right))
		return errA == nil && errB == nil && season >= a && season <= b
	}
	n, err := strconv.Atoi(raw)
	return err == nil && season == n
}

// pollWanted drives the wanted titles each feed cycle: it searches the indexers
// for each title and hands the results to applyWanted (which also runs over the
// RSS feed items), then searches the indexers for the episodes seen but not
// downloaded (gap search). Every step is bounded and throttled.
func (d *Daemon) pollWanted(ctx context.Context) {
	wanteds := d.wantedList()
	if len(wanteds) == 0 {
		return
	}
	now := time.Now()

	// Indexer results for each title (the RSS feed items are handled by
	// applyWanted from pollFeeds).
	if len(d.indexers()) > 0 {
		for _, w := range wanteds {
			if w.Paused {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			items, _ := d.searchFeedSuggest(ctx, w.Name)
			if len(items) > 0 {
				d.applyWanted(now, "wanted:"+strings.TrimSpace(w.Name), items)
			}
		}
	}

	// Missing episodes that the feeds did not bring.
	for _, w := range wanteds {
		if w.Paused || w.kind() == "movie" {
			continue
		}
		if !d.wantedGapDue(w.Name, now) {
			continue
		}
		d.wantedGapSearch(ctx, now, w)
		d.markWantedGap(w.Name, now)
	}
}

// wantedKey is the ledger key of a title+season.
func wantedKey(name string, season int) string {
	return strings.TrimSpace(name) + "\x00" + strconv.Itoa(season)
}

// recordWantedSeen notes that an episode (or the movie) appeared in the feeds.
func (d *Daemon) recordWantedSeen(w wantedEntry, season, episode int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.WantedSeen == nil {
		d.state.WantedSeen = map[string]smartEpisodes{}
	}
	key := wantedKey(w.Name, season)
	if d.state.WantedSeen[key] == nil {
		d.state.WantedSeen[key] = smartEpisodes{}
	}
	d.state.WantedSeen[key][strconv.Itoa(episode)] = time.Now().Unix()
	d.dirty = true
}

// recordWantedTaken notes that an episode (or the movie) was downloaded.
func (d *Daemon) recordWantedTaken(w wantedEntry, season, episode int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.WantedTaken == nil {
		d.state.WantedTaken = map[string]smartEpisodes{}
	}
	key := wantedKey(w.Name, season)
	if d.state.WantedTaken[key] == nil {
		d.state.WantedTaken[key] = smartEpisodes{}
	}
	d.state.WantedTaken[key][strconv.Itoa(episode)] = time.Now().Unix()
	d.dirty = true
}

func (d *Daemon) wantedTaken(name string, season, episode int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	eps := d.state.WantedTaken[wantedKey(name, season)]
	if eps == nil {
		return false
	}
	_, ok := eps[strconv.Itoa(episode)]
	return ok
}

// wantedMissing lists the episodes seen but not downloaded, inside the wanted
// seasons, ordered by season then episode.
func (d *Daemon) wantedMissing(w wantedEntry) []wantedEpisode {
	d.mu.Lock()
	seen := map[string]smartEpisodes{}
	for key, eps := range d.state.WantedSeen {
		seen[key] = eps
	}
	taken := map[string]smartEpisodes{}
	for key, eps := range d.state.WantedTaken {
		taken[key] = eps
	}
	d.mu.Unlock()

	prefix := strings.TrimSpace(w.Name) + "\x00"
	var missing []wantedEpisode
	for key, eps := range seen {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		season, err := strconv.Atoi(strings.TrimPrefix(key, prefix))
		if err != nil || !seasonAllowed(w.Seasons, season) {
			continue
		}
		takenSeason := taken[key]
		for token := range eps {
			episode, err := strconv.Atoi(strings.SplitN(token, "-", 2)[0])
			if err != nil {
				continue
			}
			if _, ok := takenSeason[token]; ok {
				continue
			}
			missing = append(missing, wantedEpisode{season: season, episode: episode})
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].season != missing[j].season {
			return missing[i].season < missing[j].season
		}
		return missing[i].episode < missing[j].episode
	})
	return missing
}

// wantedItemEpisode extracts the season/episode of an item for a wanted title,
// or (-1, 0) for a movie.
func wantedItemEpisode(w wantedEntry, title string) (int, int) {
	if w.kind() == "movie" {
		return -1, 0
	}
	_, season, episode, ok := rss.FindEpisode(title)
	if !ok {
		return -1, 0
	}
	return season, episode
}

// applyWanted evaluates the items of one feed against the wanted list: it records
// what appeared and downloads the episodes (or movie) not taken yet. It runs
// after the rules loop, so items already added there are only recorded, not
// added again.
func (d *Daemon) applyWanted(now time.Time, feedName string, items []rss.Item) {
	wanteds := d.wantedList()
	if len(wanteds) == 0 {
		return
	}
	added := 0
	for _, item := range items {
		if added >= feedMaxAddsPerPoll {
			break
		}
		for _, w := range wanteds {
			if w.Paused || !w.matches(item.Title, now) {
				continue
			}
			season, episode := wantedItemEpisode(w, item.Title)
			if w.kind() != "movie" && season < 0 {
				continue
			}
			d.recordWantedSeen(w, season, episode)
			if d.feedSeen(feedName, item.GUID) || d.wantedTaken(w.Name, season, episode) {
				continue
			}
			action := rss.Action{SavePath: strings.TrimSpace(w.SavePath), Category: strings.TrimSpace(w.Category)}
			newly, err := d.addFeedItem(item, action)
			if err != nil {
				logf("wanted %q: cannot add %q: %v", w.Name, item.Title, err)
				continue
			}
			if newly {
				d.markFeedSeen(feedName, item.GUID)
				d.recordWantedTaken(w, season, episode)
				added++
				logf("wanted %q: added %s S%02dE%02d", w.Name, item.Title, season, episode)
			}
		}
	}
}

// wantedGapSearch searches the indexers for the missing episodes of one wanted
// series, so an episode seen before but not downloaded is fetched even if it
// does not show up again in the regular poll.
func (d *Daemon) wantedGapSearch(ctx context.Context, now time.Time, w wantedEntry) {
	missing := d.wantedMissing(w)
	if len(missing) == 0 {
		return
	}
	if len(missing) > wantedGapMaxPerCycle {
		missing = missing[:wantedGapMaxPerCycle]
	}
	total := 0
	for _, ep := range missing {
		if ctx.Err() != nil {
			return
		}
		query := fmt.Sprintf("%s S%02dE%02d", w.Name, ep.season, ep.episode)
		items, err := d.searchFeedSuggest(ctx, query)
		if err != nil {
			continue
		}
		crit := w.criteria()
		crit.EpisodeFilter = fmt.Sprintf("%dx%02d", ep.season, ep.episode)
		for _, item := range items {
			if !wantedIncludeMatch(w, item.Title) || !crit.Match(rss.Item{Title: item.Title}, now) {
				continue
			}
			if d.wantedTaken(w.Name, ep.season, ep.episode) {
				break
			}
			action := rss.Action{SavePath: strings.TrimSpace(w.SavePath), Category: strings.TrimSpace(w.Category)}
			if newly, err := d.addFeedItem(item, action); err == nil && newly {
				d.recordWantedTaken(w, ep.season, ep.episode)
				total++
				break
			}
		}
	}
	if total > 0 {
		logf("wanted %q: gap search added %d missing episode(s)", w.Name, total)
	}
}

func (d *Daemon) wantedGapDue(name string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	last := d.state.WantedGapLast[strings.TrimSpace(name)]
	if last <= 0 {
		return true
	}
	return now.Sub(time.Unix(last, 0)) >= wantedGapInterval
}

func (d *Daemon) markWantedGap(name string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.WantedGapLast == nil {
		d.state.WantedGapLast = map[string]int64{}
	}
	d.state.WantedGapLast[strings.TrimSpace(name)] = now.Unix()
	d.dirty = true
}
