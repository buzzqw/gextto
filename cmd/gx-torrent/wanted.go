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

// wantedIndexerInterval throttles the per-title indexer search: the feeds grow
// the archive continuously, so the indexers are asked for a monitored title
// every so often, not at every cycle.
const wantedIndexerInterval = 30 * time.Minute

// wantedFeedName is the archive feed name under which a wanted title's indexer
// results are stored. Archiving them means a later search reads them from the
// archive (fast) instead of asking the indexer again.
func wantedFeedName(name string) string {
	return "wanted:" + strings.TrimSpace(name)
}

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

// wantedIncludes are the include terms of a wanted entry: language and
// quality, formatted for display.
func (w wantedEntry) includes() []string {
	out := splitList(w.Language)
	if q := strings.TrimSpace(w.Quality); q != "" && q != "any" && q != "*" {
		out = append(out, q)
	}
	return out
}

func extractResolutionNumber(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	for _, res := range []struct {
		name string
		num  int
	}{
		{"2160p", 2160},
		{"4k", 2160},
		{"1080p", 1080},
		{"1080i", 1080},
		{"720p", 720},
		{"576p", 576},
		{"480p", 480},
		{"360p", 360},
	} {
		if strings.Contains(s, res.name) {
			return res.num
		}
	}
	return 0
}

func extractTitleResolution(lowerTitle string) int {
	for _, res := range []struct {
		name string
		num  int
	}{
		{"2160p", 2160},
		{"4k", 2160},
		{"uhd", 2160},
		{"1080p", 1080},
		{"1080i", 1080},
		{"720p", 720},
		{"576p", 576},
		{"480p", 480},
		{"360p", 360},
	} {
		if strings.Contains(lowerTitle, res.name) {
			return res.num
		}
	}
	return 0
}

func wantedQualityMatch(qualityReq string, lowerTitle string) bool {
	qualityReq = strings.ToLower(strings.TrimSpace(qualityReq))
	if qualityReq == "" || qualityReq == "any" || qualityReq == "*" {
		return true
	}
	titleRes := extractTitleResolution(lowerTitle)

	if strings.HasSuffix(qualityReq, "+") {
		minStr := strings.TrimSuffix(qualityReq, "+")
		minRes := extractResolutionNumber(minStr)
		if minRes > 0 {
			if titleRes > 0 {
				return titleRes >= minRes
			}
			return strings.Contains(lowerTitle, minStr)
		}
	} else if startText, endText, found := strings.Cut(qualityReq, "-"); found {
		minRes := extractResolutionNumber(startText)
		maxRes := extractResolutionNumber(endText)
		if minRes > 0 && maxRes > 0 {
			if titleRes > 0 {
				return titleRes >= minRes && titleRes <= maxRes
			}
			return strings.Contains(lowerTitle, startText) || strings.Contains(lowerTitle, endText)
		}
	} else {
		reqRes := extractResolutionNumber(qualityReq)
		if reqRes > 0 && titleRes > 0 {
			return titleRes == reqRes
		}
	}
	return strings.Contains(lowerTitle, qualityReq)
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

// wantedIncludeMatch reports whether the title contains every language term
// and satisfies the quality requirement.
func wantedIncludeMatch(w wantedEntry, title string) bool {
	lower := strings.ToLower(title)
	for _, term := range splitList(w.Language) {
		if term = strings.ToLower(strings.TrimSpace(term)); term != "" && !strings.Contains(lower, term) {
			return false
		}
	}
	if !wantedQualityMatch(w.Quality, lower) {
		return false
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

// wantedPollResult is the summary outcome of a wanted titles search.
type wantedPollResult struct {
	OK      bool     `json:"ok"`
	Checked int      `json:"checked"`
	Added   int      `json:"added"`
	Titles  []string `json:"titles,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

// pollWanted drives the wanted titles each feed cycle.
func (d *Daemon) pollWanted(ctx context.Context) {
	d.pollWantedLocked(ctx, "")
}

// pollWantedTitles triggers a wanted search on demand (all or a single title).
func (d *Daemon) pollWantedTitles(ctx context.Context, onlyName string) wantedPollResult {
	if !d.feedPollMu.TryLock() {
		return wantedPollResult{OK: false, Errors: []string{"a check is already running"}}
	}
	defer d.feedPollMu.Unlock()
	return d.pollWantedLocked(ctx, onlyName)
}

// pollWantedLocked searches the indexers and archive for wanted titles and
// executes the gap search for missing episodes. The caller must hold feedPollMu.
func (d *Daemon) pollWantedLocked(ctx context.Context, onlyName string) wantedPollResult {
	wanteds := d.wantedList()
	if len(wanteds) == 0 {
		return wantedPollResult{OK: true}
	}
	now := time.Now()
	var (
		checked     int
		addedCount  int
		addedTitles []string
		errorsList  []string
	)

	for _, w := range wanteds {
		if onlyName != "" && !strings.EqualFold(w.Name, onlyName) {
			continue
		}
		if w.Paused {
			continue
		}
		if ctx.Err() != nil {
			errorsList = append(errorsList, "timeout during search")
			break
		}
		checked++
		var items []rss.Item
		if len(d.indexers()) > 0 && (onlyName != "" || d.wantedIndexerDue(w.Name, now)) {
			found, err := d.searchFeedSuggest(ctx, w.Name)
			if err != nil {
				errorsList = append(errorsList, w.Name+": "+err.Error())
			} else {
				items = append(items, found...)
				// The indexer results are archived too: the next search reads them
				// from the archive (fast) instead of asking the indexer again.
				if len(found) > 0 {
					if _, aerr := d.archive().record(wantedFeedName(w.Name), found, now); aerr != nil {
						logf("wanted %q: cannot archive the indexer results: %v", w.Name, aerr)
					}
				}
			}
			d.markWantedIndexer(w.Name, now)
		}
		items = append(items, d.archiveWantedItems(w.Name, wantedArchiveScan)...)
		if len(items) > 0 {
			n, titles := d.applyWanted(now, wantedFeedName(w.Name), items)
			addedCount += n
			addedTitles = append(addedTitles, titles...)
		}

		if w.kind() != "movie" {
			if onlyName != "" || d.wantedGapDue(w.Name, now) {
				n, titles := d.wantedGapSearch(ctx, now, w)
				addedCount += n
				addedTitles = append(addedTitles, titles...)
				d.markWantedGap(w.Name, now)
			}
		}
	}
	d.mu.Lock()
	if d.dirty {
		d.saveLocked()
	}
	d.mu.Unlock()

	return wantedPollResult{
		OK:      true,
		Checked: checked,
		Added:   addedCount,
		Titles:  addedTitles,
		Errors:  errorsList,
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
func (d *Daemon) applyWanted(now time.Time, feedName string, items []rss.Item) (int, []string) {
	wanteds := d.wantedList()
	if len(wanteds) == 0 {
		return 0, nil
	}
	added := 0
	var addedTitles []string
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
				addedTitles = append(addedTitles, item.Title)
				logf("wanted %q: added %s S%02dE%02d", w.Name, item.Title, season, episode)
			}
		}
	}
	return added, addedTitles
}

// wantedGapSearch searches the indexers for the missing episodes of one wanted
// series, so an episode seen before but not downloaded is fetched even if it
// does not show up again in the regular poll.
func (d *Daemon) wantedGapSearch(ctx context.Context, now time.Time, w wantedEntry) (int, []string) {
	missing := d.wantedMissing(w)
	if len(missing) == 0 {
		return 0, nil
	}
	if len(missing) > wantedGapMaxPerCycle {
		missing = missing[:wantedGapMaxPerCycle]
	}
	total := 0
	var addedTitles []string
	for _, ep := range missing {
		if ctx.Err() != nil {
			return total, addedTitles
		}
		query := fmt.Sprintf("%s S%02dE%02d", w.Name, ep.season, ep.episode)
		items, err := d.searchFeedSuggest(ctx, query)
		if err != nil {
			continue
		}
		if len(items) > 0 {
			if _, aerr := d.archive().record(wantedFeedName(w.Name), items, now); aerr != nil {
				logf("wanted %q: cannot archive the gap-search results: %v", w.Name, aerr)
			}
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
				addedTitles = append(addedTitles, item.Title)
				break
			}
		}
	}
	if total > 0 {
		logf("wanted %q: gap search added %d missing episode(s)", w.Name, total)
	}
	return total, addedTitles
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

// wantedIndexerDue reports whether the per-title indexer search is due. It is
// separate from the gap search: the title search finds new releases (which the
// feed cycle then turns into downloads), the gap search fills the episodes seen
// but not yet taken.
func (d *Daemon) wantedIndexerDue(name string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	last := d.state.WantedSearchLast[strings.TrimSpace(name)]
	if last <= 0 {
		return true
	}
	return now.Sub(time.Unix(last, 0)) >= wantedIndexerInterval
}

func (d *Daemon) markWantedIndexer(name string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.WantedSearchLast == nil {
		d.state.WantedSearchLast = map[string]int64{}
	}
	d.state.WantedSearchLast[strings.TrimSpace(name)] = now.Unix()
	d.dirty = true
}
