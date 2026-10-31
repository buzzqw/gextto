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
	"regexp"
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
	return wantedFeedPrefix + strings.TrimSpace(name)
}

// wantedFeedPrefix marks the archive "feeds" that hold a wanted title's
// indexer results: internal buckets, not feeds the operator configured.
const wantedFeedPrefix = "wanted:"

// wantedGapMaxPerCycle bounds how many missing episodes are searched per title
// per cycle, so a large gap does not flood the indexers.
const wantedGapMaxPerCycle = 3

// wantedEntry is one title the operator wants.
type wantedEntry struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"` // "series" | "movie"
	Seasons       string `json:"seasons,omitempty"`
	Language      string `json:"language,omitempty"`
	Subtitles     string `json:"subtitles,omitempty"`
	Quality       string `json:"quality,omitempty"`
	Profile       string `json:"profile,omitempty"`
	Exclude       string `json:"exclude,omitempty"`
	EpisodeFilter string `json:"episode_filter,omitempty"`
	Category      string `json:"category,omitempty"`
	SavePath      string `json:"savepath,omitempty"`
	Paused        bool   `json:"paused,omitempty"`
	// profile is the resolved quality profile (not persisted): wantedList fills
	// it from the profile name so the match and upgrade rules are pure methods.
	profile qualityProfile
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
		logfError("cannot parse the wanted setting: %v", err)
		return nil
	}
	for i := range list {
		if profile, ok := d.qualityProfileByName(list[i].Profile); ok {
			list[i].profile = profile
		}
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

// wantedIncludes are the include terms of a wanted entry: language, subtitles
// and quality, formatted for display.
func (w wantedEntry) includes() []string {
	out := splitList(w.Language)
	out = append(out, splitList(w.Subtitles)...)
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

// wantedIncludeMatch reports whether the title contains every language and
// subtitle term and satisfies the quality requirement.
func wantedIncludeMatch(w wantedEntry, title string) bool {
	// rss.HasTerm: "sub ita" finds "SUB.ITA", "ita" does not find "Capitan".
	for _, term := range splitList(w.Language) {
		if strings.TrimSpace(term) != "" && !rss.HasTerm(title, term) {
			return false
		}
	}
	for _, term := range splitList(w.Subtitles) {
		if strings.TrimSpace(term) != "" && !rss.HasTerm(title, term) {
			return false
		}
	}
	if !w.qualityMatch(title) {
		return false
	}
	return true
}

// qualityMatch applies the quality profile when the title has one, otherwise
// the plain Quality field.
func (w wantedEntry) qualityMatch(title string) bool {
	if w.Profile != "" && w.profile.Name != "" {
		return w.profile.allows(title)
	}
	return wantedQualityMatch(w.Quality, strings.ToLower(title))
}

func (w wantedEntry) kind() string {
	if strings.EqualFold(strings.TrimSpace(w.Kind), "movie") {
		return "movie"
	}
	return "series"
}

// wantedPackSeason returns the season of a season-pack title: a season marker
// without an episode marker ("Reacher S04 COMPLETE", "Reacher (2022) S04
// 1080p"). Single episodes are not packs (FindEpisode wins).
func wantedPackSeason(title string) (int, bool) {
	if _, _, _, ok := rss.FindEpisode(title); ok {
		return 0, false
	}
	return rss.FindSeason(title)
}

// matchesPack reports whether a season-pack title belongs to the wanted
// series: the name matches, the season is wanted and the language, subtitle,
// quality and exclude terms hold. Episode 0 marks the whole season.
func (w wantedEntry) matchesPack(title string) bool {
	season, ok := wantedPackSeason(title)
	if !ok || w.kind() == "movie" {
		return false
	}
	if !seasonAllowed(w.Seasons, season) && strings.TrimSpace(w.EpisodeFilter) == "" {
		return false
	}
	if pattern := titleMatchRegex(w.Name); pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil || !re.MatchString(title) {
			return false
		}
	} else {
		return false
	}
	for _, term := range splitList(w.Exclude) {
		if strings.TrimSpace(term) != "" && rss.HasTerm(title, term) {
			return false
		}
	}
	if filter, err := rss.ParseEpisodeFilter(w.episodeFilter()); err != nil || !filter.MatchesSeason(season) {
		return false
	}
	return wantedIncludeMatch(w, title)
}

// matches reports whether a release title belongs to the wanted entry.
func (w wantedEntry) matches(title string, now time.Time) bool {
	if w.matchesPack(title) {
		return true
	}
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
	if onlyName == "" {
		d.pruneWantedTimers(wanteds)
	}
	if len(wanteds) == 0 {
		d.mu.Lock()
		if d.dirty {
			d.saveLocked()
		}
		d.mu.Unlock()
		return wantedPollResult{OK: true}
	}
	now := time.Now()
	var (
		checked       int
		addedCount    int
		addedTitles   []string
		errorsList    []string
		metadataTries int
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
		if metadataTries < metadataPerCycle && d.refreshWantedMetadata(ctx, w, onlyName != "") {
			metadataTries++
		}
		var items []rss.Item
		// A /regex/ title is searched by its literal words (none with "|").
		searchText := titleSearchText(w.Name)
		if searchText != "" && len(d.indexers()) > 0 && (onlyName != "" || d.wantedIndexerDue(w.Name, now)) {
			found, err := d.searchFeedSuggest(ctx, searchText)
			if err != nil {
				errorsList = append(errorsList, w.Name+": "+err.Error())
			} else {
				items = append(items, found...)
				// The indexer results are archived too: the next search reads them
				// from the archive (fast) instead of asking the indexer again.
				if len(found) > 0 {
					if _, aerr := d.archive().record(wantedFeedName(w.Name), found, now); aerr != nil {
						logfError("wanted %q: cannot archive the indexer results: %v", w.Name, aerr)
					}
				}
			}
			d.markWantedIndexer(w.Name, now)
		}
		items = append(items, d.archiveWantedItems(searchText, wantedArchiveScan)...)
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

// recordWantedTakenQuality stores the resolution of the release actually taken
// for an episode, so a later better release can be recognized as an upgrade.
func (d *Daemon) recordWantedTakenQuality(w wantedEntry, season, episode int, title string) {
	_, rank := rss.QualityRank(title)
	if rank <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.WantedTakenQuality == nil {
		d.state.WantedTakenQuality = map[string]smartEpisodes{}
	}
	key := wantedKey(w.Name, season)
	if d.state.WantedTakenQuality[key] == nil {
		d.state.WantedTakenQuality[key] = smartEpisodes{}
	}
	d.state.WantedTakenQuality[key][strconv.Itoa(episode)] = int64(rank)
	d.dirty = true
}

// recordWantedTakenTorrent remembers the torrent that covers an episode, so an
// upgrade can drop the older torrent from the session (its files are kept).
func (d *Daemon) recordWantedTakenTorrent(w wantedEntry, season, episode int, hash string) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.WantedTakenTorrent == nil {
		d.state.WantedTakenTorrent = map[string]map[string]string{}
	}
	key := wantedKey(w.Name, season)
	if d.state.WantedTakenTorrent[key] == nil {
		d.state.WantedTakenTorrent[key] = map[string]string{}
	}
	d.state.WantedTakenTorrent[key][strconv.Itoa(episode)] = hash
	d.dirty = true
}

func (d *Daemon) wantedTakenRank(w wantedEntry, season, episode int) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	eps := d.state.WantedTakenQuality[wantedKey(w.Name, season)]
	if eps == nil {
		return 0
	}
	return int(eps[strconv.Itoa(episode)])
}

func (d *Daemon) wantedTakenTorrentHash(w wantedEntry, season, episode int) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state.WantedTakenTorrent[wantedKey(w.Name, season)][strconv.Itoa(episode)]
}

// wantedCanUpgrade reports whether a release is a better copy of an episode
// already taken: only with a profile that has a cutoff, only above the taken
// quality and never beyond the cutoff.
func (d *Daemon) wantedCanUpgrade(w wantedEntry, season, episode int, title string) bool {
	if w.Profile == "" || w.profile.Name == "" || w.profile.Cutoff == "" {
		return false
	}
	taken := d.wantedTakenRank(w, season, episode)
	if taken <= 0 {
		return false
	}
	_, candidate := rss.QualityRank(title)
	if candidate <= taken {
		return false
	}
	if !w.profile.allows(title) {
		return false
	}
	cutoff := w.profile.cutoffRank()
	if cutoff > 0 && (taken >= cutoff || candidate > cutoff) {
		return false
	}
	return true
}

// dropUpgradedTorrent removes the previous torrent of an upgraded episode from
// the session without deleting its files: a failed new download never destroys
// what was already there.
func (d *Daemon) dropUpgradedTorrent(w wantedEntry, season, episode int, newHash string) {
	old := d.wantedTakenTorrentHash(w, season, episode)
	if old == "" || old == strings.ToLower(newHash) {
		return
	}
	if err := d.remove(old, false); err != nil {
		logfError("wanted %q: cannot drop the previous torrent %s after the upgrade: %v", w.Name, old, err)
	}
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

// wantedPackTaken reports whether the whole season was taken as a season pack
// (episode 0) or is already in the session as a pack.
func (d *Daemon) wantedPackTaken(w wantedEntry, season int, inSession map[wantedEpisode]bool) bool {
	if d.wantedTaken(w.Name, season, 0) {
		return true
	}
	return inSession[wantedEpisode{season: season, episode: 0}]
}

// setWantedTaken marks an episode as downloaded (have) or not (missing), from
// the episode manager of the wanted page.
func (d *Daemon) setWantedTaken(name string, season, episode int, have bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.WantedTaken == nil {
		d.state.WantedTaken = map[string]smartEpisodes{}
	}
	key := wantedKey(name, season)
	if d.state.WantedTaken[key] == nil {
		d.state.WantedTaken[key] = smartEpisodes{}
	}
	token := strconv.Itoa(episode)
	if have {
		d.state.WantedTaken[key][token] = time.Now().Unix()
	} else {
		delete(d.state.WantedTaken[key], token)
		delete(d.state.WantedTakenQuality[key], token)
		delete(d.state.WantedTakenTorrent[key], token)
	}
	d.dirty = true
}

// addWantedSeen records an episode as known, so it counts as missing (and gets
// searched) even if it never appeared in a feed. From the episode manager.
func (d *Daemon) addWantedSeen(name string, season, episode int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.WantedSeen == nil {
		d.state.WantedSeen = map[string]smartEpisodes{}
	}
	key := wantedKey(name, season)
	if d.state.WantedSeen[key] == nil {
		d.state.WantedSeen[key] = smartEpisodes{}
	}
	d.state.WantedSeen[key][strconv.Itoa(episode)] = time.Now().Unix()
	d.dirty = true
}

// wantedTakenEpisodes lists the episodes downloaded for a wanted title, inside
// its seasons, ordered by season then episode. It is what the operator "has",
// the counterpart of wantedMissing.
func (d *Daemon) wantedTakenEpisodes(w wantedEntry) []wantedEpisode {
	d.mu.Lock()
	taken := map[string]smartEpisodes{}
	for key, eps := range d.state.WantedTaken {
		taken[key] = eps
	}
	d.mu.Unlock()

	prefix := strings.TrimSpace(w.Name) + "\x00"
	seen := map[string]bool{}
	var out []wantedEpisode
	for key, eps := range taken {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		season, err := strconv.Atoi(strings.TrimPrefix(key, prefix))
		if err != nil || !seasonAllowed(w.Seasons, season) {
			continue
		}
		for token := range eps {
			episode, err := strconv.Atoi(strings.SplitN(token, "-", 2)[0])
			if err != nil {
				continue
			}
			ep := fmt.Sprintf("%d-%d", season, episode)
			if seen[ep] {
				continue
			}
			seen[ep] = true
			out = append(out, wantedEpisode{season: season, episode: episode})
		}
	}
	if w.kind() != "movie" {
		for ep := range d.wantedSessionEpisodes(w) {
			if key := fmt.Sprintf("%d-%d", ep.season, ep.episode); !seen[key] {
				seen[key] = true
				out = append(out, ep)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].season != out[j].season {
			return out[i].season < out[j].season
		}
		return out[i].episode < out[j].episode
	})
	return out
}

// wantedSessionMedia maps every episode of a wanted series already in the
// session — whatever brought it there (a rule, a manual add, another release)
// — to the torrent that holds it, so the page can tell a downloaded episode
// from one still downloading and link to it. A movie in the session is the
// {-1, 0} entry. A season pack, with no episode number, counts as the whole
// season (episode 0).
func (d *Daemon) wantedSessionMedia(w wantedEntry) map[wantedEpisode]torrentInfo {
	pattern := titleMatchRegex(w.Name)
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	out := map[wantedEpisode]torrentInfo{}
	for _, view := range d.snapshotViews() {
		if !re.MatchString(view.Name) {
			continue
		}
		_, season, episode, ok := rss.FindEpisode(view.Name)
		if w.kind() == "movie" {
			if !ok {
				ep := wantedEpisode{season: -1}
				out[ep] = preferMedia(out[ep], view)
			}
			continue
		}
		if ok && seasonAllowed(w.Seasons, season) {
			ep := wantedEpisode{season: season, episode: episode}
			out[ep] = preferMedia(out[ep], view)
			continue
		}
		if !ok {
			if packSeason, ok := wantedPackSeason(view.Name); ok && seasonAllowed(w.Seasons, packSeason) {
				ep := wantedEpisode{season: packSeason, episode: 0}
				out[ep] = preferMedia(out[ep], view)
			}
		}
	}
	return out
}

// preferMedia keeps the more advanced torrent when two copies of the same
// episode are in the session (a repack, a second release): a fully downloaded
// one wins over a still-downloading one.
func preferMedia(a, b torrentInfo) torrentInfo {
	if a.Hash == "" || (mediaDownloaded(b) && !mediaDownloaded(a)) {
		return b
	}
	return a
}

// mediaDownloaded reports whether a session torrent is fully downloaded (it is
// seeding or its progress is complete), as opposed to still downloading.
func mediaDownloaded(v torrentInfo) bool {
	return v.Progress >= 100 || v.State == "seeding"
}

// wantedSessionEpisodes is the set of episode keys wantedSessionMedia holds;
// callers that only ask "is it in the session?" use this.
func (d *Daemon) wantedSessionEpisodes(w wantedEntry) map[wantedEpisode]bool {
	media := d.wantedSessionMedia(w)
	if media == nil {
		return nil
	}
	out := make(map[wantedEpisode]bool, len(media))
	for ep := range media {
		out[ep] = true
	}
	return out
}

// Episode states shown on the wanted page. They answer, at a glance, what the
// operator has: downloaded (in the session and complete, or taken by the
// engine), downloading (in the session but still running) or missing (seen in
// the feeds/indexers but not taken).
const (
	wantedEpisodeDownloaded  = "downloaded"
	wantedEpisodeDownloading = "downloading"
	wantedEpisodeMissing     = "missing"
)

// wantedSeasonStat is the per-season count of a wanted title's episodes.
type wantedSeasonStat struct {
	Season      int
	Downloaded  int
	Downloading int
	Missing     int
}

// wantedStats is the episode picture of one wanted title: how many episodes are
// downloaded, still downloading and known but not taken, with the per-season
// breakdown. It is what the page shows, so "have" and "missing" are never
// guessed from a partial season (a season we only saw three episodes of is not
// presented as complete).
type wantedStats struct {
	Downloaded  int
	Downloading int
	Missing     int
	Seasons     []wantedSeasonStat
}

// wantedStatsFor computes the episode picture of a wanted title. A movie has a
// single release, not episodes: it counts one downloaded or one downloading.
func (d *Daemon) wantedStatsFor(w wantedEntry) wantedStats {
	if w.kind() == "movie" {
		var s wantedStats
		if media, ok := d.wantedSessionMedia(w)[wantedEpisode{season: -1}]; ok {
			if mediaDownloaded(media) {
				s.Downloaded = 1
			} else {
				s.Downloading = 1
			}
		} else if len(d.wantedLedgerEpisodes(w)) > 0 {
			s.Downloaded = 1
		}
		return s
	}
	states := map[wantedEpisode]string{}
	for _, ep := range d.wantedLedgerEpisodes(w) {
		if seasonAllowed(w.Seasons, ep.season) {
			states[ep] = wantedEpisodeDownloaded
		}
	}
	for ep, view := range d.wantedSessionMedia(w) {
		if !seasonAllowed(w.Seasons, ep.season) {
			continue
		}
		if mediaDownloaded(view) {
			states[ep] = wantedEpisodeDownloaded
		} else if states[ep] != wantedEpisodeDownloaded {
			states[ep] = wantedEpisodeDownloading
		}
	}
	for _, ep := range d.wantedMissing(w) {
		if _, ok := states[ep]; !ok {
			states[ep] = wantedEpisodeMissing
		}
	}
	return summarizeWanted(states)
}

// summarizeWanted turns the per-episode states into the counts the page shows.
func summarizeWanted(states map[wantedEpisode]string) wantedStats {
	var s wantedStats
	bySeason := map[int]*wantedSeasonStat{}
	for ep, state := range states {
		switch state {
		case wantedEpisodeDownloaded:
			s.Downloaded++
		case wantedEpisodeDownloading:
			s.Downloading++
		case wantedEpisodeMissing:
			s.Missing++
		}
		if ep.season < 0 {
			continue
		}
		row := bySeason[ep.season]
		if row == nil {
			row = &wantedSeasonStat{Season: ep.season}
			bySeason[ep.season] = row
		}
		switch state {
		case wantedEpisodeDownloaded:
			row.Downloaded++
		case wantedEpisodeDownloading:
			row.Downloading++
		case wantedEpisodeMissing:
			row.Missing++
		}
	}
	for _, row := range bySeason {
		s.Seasons = append(s.Seasons, *row)
	}
	sort.Slice(s.Seasons, func(i, j int) bool { return s.Seasons[i].Season < s.Seasons[j].Season })
	return s
}

// wantedTitleState is the single most useful state of a wanted title, shown as
// its badge: a paused title, one downloading, one with known missing episodes,
// one up to date with what was seen, or one still waiting for a first result.
func wantedTitleState(w wantedEntry, s wantedStats) string {
	switch {
	case w.Paused:
		return "paused"
	case s.Downloading > 0:
		return "downloading"
	case s.Missing > 0:
		return "missing"
	case s.Downloaded > 0:
		return "uptodate"
	default:
		return "waiting"
	}
}

// formatEpisodeRanges compacts a sorted episode list into ranges, e.g.
// "S01E01–E05, S02E01". At most max episodes are listed; any more becomes a
// trailing "+N" so a long-running series does not flood the row.
func formatEpisodeRanges(eps []wantedEpisode, max int) string {
	var parts []string
	shown := 0
	for i := 0; i < len(eps) && shown < max; {
		start := eps[i]
		j := i
		for j+1 < len(eps) && eps[j+1].season == start.season && eps[j+1].episode == eps[j].episode+1 {
			j++
		}
		run := j - i + 1
		if shown+run > max {
			// Only part of this run fits.
			fit := max - shown
			if fit == 1 {
				parts = append(parts, fmt.Sprintf("S%02dE%02d", start.season, start.episode))
			} else {
				parts = append(parts, fmt.Sprintf("S%02dE%02d–E%02d", start.season, start.episode, start.episode+fit-1))
			}
			shown += fit
			break
		}
		if run == 1 {
			parts = append(parts, fmt.Sprintf("S%02dE%02d", start.season, start.episode))
		} else {
			parts = append(parts, fmt.Sprintf("S%02dE%02d–E%02d", start.season, start.episode, eps[j].episode))
		}
		shown += run
		i = j + 1
	}
	out := strings.Join(parts, ", ")
	if shown < len(eps) {
		out += " +" + strconv.Itoa(len(eps)-shown)
	}
	return out
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
	inSession := d.wantedSessionEpisodes(w)
	var missing []wantedEpisode
	for key, eps := range seen {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		season, err := strconv.Atoi(strings.TrimPrefix(key, prefix))
		if err != nil || !seasonAllowed(w.Seasons, season) {
			continue
		}
		// A season pack covers the whole season: nothing of it is missing.
		if _, ok := taken[key]["0"]; ok || inSession[wantedEpisode{season: season, episode: 0}] {
			continue
		}
		takenSeason := taken[key]
		for token := range eps {
			episode, err := strconv.Atoi(strings.SplitN(token, "-", 2)[0])
			if err != nil || episode <= 0 {
				continue
			}
			if _, ok := takenSeason[token]; ok {
				continue
			}
			if inSession[wantedEpisode{season: season, episode: episode}] {
				continue
			}
			missing = append(missing, wantedEpisode{season: season, episode: episode})
		}
	}
	known := make(map[wantedEpisode]bool, len(missing))
	for _, ep := range missing {
		known[ep] = true
	}
	for _, ep := range d.wantedMetadataEpisodes(w, time.Now()) {
		if known[ep] || d.wantedTaken(w.Name, ep.season, ep.episode) || inSession[ep] {
			continue
		}
		if _, ok := taken[wantedKey(w.Name, ep.season)]["0"]; ok {
			continue
		}
		known[ep] = true
		missing = append(missing, ep)
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
// or (-1, 0) for a movie. A season pack (season marker without an episode)
// is the whole season, marked as episode 0.
func wantedItemEpisode(w wantedEntry, title string) (int, int) {
	if w.kind() == "movie" {
		return -1, 0
	}
	_, season, episode, ok := rss.FindEpisode(title)
	if ok {
		return season, episode
	}
	if packSeason, ok := wantedPackSeason(title); ok {
		return packSeason, 0
	}
	return -1, 0
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
	// Best quality first: a profile with several accepted qualities takes the
	// best release in the batch, not whichever appeared first.
	sort.SliceStable(items, func(i, j int) bool {
		_, ri := rss.QualityRank(items[i].Title)
		_, rj := rss.QualityRank(items[j].Title)
		return ri > rj
	})
	// What is already in the session, per title, read once for this batch.
	inSession := map[string]map[wantedEpisode]bool{}
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
			if d.feedSeen(feedName, item.GUID) || d.archive().alreadyAdded(feedName, item) {
				continue
			}
			taken := d.wantedTaken(w.Name, season, episode)
			upgrade := false
			if taken {
				if !d.wantedCanUpgrade(w, season, episode, item.Title) {
					continue
				}
				upgrade = true
			}
			if _, ok := inSession[w.Name]; !ok {
				inSession[w.Name] = d.wantedSessionEpisodes(w)
			}
			if inSession[w.Name][wantedEpisode{season: season, episode: episode}] {
				// Already downloaded by something else: had, not taken again.
				if !taken {
					d.recordWantedTaken(w, season, episode)
				}
				continue
			}
			action := rss.Action{SavePath: strings.TrimSpace(w.SavePath), Category: strings.TrimSpace(w.Category)}
			newly, hash, err := d.addFeedItemHash(item, action)
			if err != nil {
				logfError("wanted %q: cannot add %q: %v", w.Name, item.Title, err)
				continue
			}
			if newly {
				if upgrade {
					d.dropUpgradedTorrent(w, season, episode, hash)
				}
				d.markFeedSeen(feedName, item.GUID)
				d.recordWantedTaken(w, season, episode)
				d.recordWantedTakenQuality(w, season, episode, item.Title)
				d.recordWantedTakenTorrent(w, season, episode, hash)
				added++
				addedTitles = append(addedTitles, item.Title)
				if upgrade {
					logf("wanted %q: upgraded %s S%02dE%02d to a better release", w.Name, item.Title, season, episode)
				} else if episode == 0 && season > 0 {
					logf("wanted %q: added %s S%02d pack", w.Name, item.Title, season)
				} else {
					logf("wanted %q: added %s S%02dE%02d", w.Name, item.Title, season, episode)
				}
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
	inSession := d.wantedSessionEpisodes(w)
	for _, ep := range missing {
		if inSession[ep] {
			d.recordWantedTaken(w, ep.season, ep.episode)
			continue
		}
		if ctx.Err() != nil {
			return total, addedTitles
		}
		text := titleSearchText(w.Name)
		if text == "" {
			break
		}
		query := fmt.Sprintf("%s S%02dE%02d", text, ep.season, ep.episode)
		items, err := d.searchFeedSuggest(ctx, query)
		if err != nil {
			continue
		}
		if len(items) > 0 {
			if _, aerr := d.archive().record(wantedFeedName(w.Name), items, now); aerr != nil {
				logfError("wanted %q: cannot archive the gap-search results: %v", w.Name, aerr)
			}
		}
		crit := w.criteria()
		crit.EpisodeFilter = fmt.Sprintf("%dx%02d", ep.season, ep.episode)
		for _, item := range items {
			if !wantedIncludeMatch(w, item.Title) || !crit.Match(rss.Item{Title: item.Title, Freeleech: item.Freeleech, FreeleechKnown: item.FreeleechKnown}, now) {
				continue
			}
			if d.wantedTaken(w.Name, ep.season, ep.episode) {
				break
			}
			action := rss.Action{SavePath: strings.TrimSpace(w.SavePath), Category: strings.TrimSpace(w.Category)}
			if newly, hash, err := d.addFeedItemHash(item, action); err == nil && newly {
				d.recordWantedTaken(w, ep.season, ep.episode)
				d.recordWantedTakenQuality(w, ep.season, ep.episode, item.Title)
				d.recordWantedTakenTorrent(w, ep.season, ep.episode, hash)
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

// wantedNextGap is when the missing-episode search for this title is due. A
// title never searched is due now, so the page can say "soon" instead of
// showing a misleading past time.
func (d *Daemon) wantedNextGap(name string, now time.Time) time.Time {
	d.mu.Lock()
	last := d.state.WantedGapLast[strings.TrimSpace(name)]
	d.mu.Unlock()
	if last <= 0 {
		return now
	}
	next := time.Unix(last, 0).Add(wantedGapInterval)
	if next.Before(now) {
		return now
	}
	return next
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

// pruneWantedTimers drops the per-title search timestamps (indexer and gap
// search) of titles no longer in the wanted list, so removed titles do not
// stay in the state file forever. The seen/taken episode ledgers are kept on
// purpose: re-adding a title must not download its episodes again.
func (d *Daemon) pruneWantedTimers(list []wantedEntry) {
	keep := make(map[string]bool, len(list))
	for _, w := range list {
		keep[strings.TrimSpace(w.Name)] = true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, m := range []map[string]int64{d.state.WantedSearchLast, d.state.WantedGapLast} {
		for name := range m {
			if !keep[name] {
				delete(m, name)
				d.dirty = true
			}
		}
	}
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
