package main

// ui_wanted_preview.go answers "what would this wanted title take, and why
// not the rest?" (GET /ui/wanted-preview). It reads the archive the feeds and
// the indexers fill, and gives every release of the title a verdict: taken
// already, had (in the session from elsewhere), would be taken now, or the
// first filter that turns it away (season, episode filter, language, quality,
// excluded term). The add/edit dialog shows it live while the filters are
// typed, the list shows it per title: nothing is decided out of sight.

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
)

// wantedPreviewMax bounds the releases listed (the counts cover all of them).
const wantedPreviewMax = 60

// Verdicts of a release for a wanted title. The labels are catalog keys.
const (
	verdictTaken    = "Taken"
	verdictHad      = "Already in the client"
	verdictTake     = "Would be taken"
	verdictSeason   = "Season not wanted"
	verdictEpisode  = "Not in the episode filter"
	verdictNoEp     = "No episode number"
	verdictLanguage = "Language does not match"
	verdictSubtitle = "Subtitles do not match"
	verdictQuality  = "Quality does not match"
	verdictExcluded = "Excluded term"
	verdictOther    = "Another title"
)

// wantedVerdict is the first reason a release is not taken for w, or one of
// the taking verdicts. have holds the episodes already taken or in session.
// A season pack (episode 0) is a whole season: it takes the season when wanted.
func wantedVerdict(w wantedEntry, re *regexp.Regexp, title string, now time.Time, taken, inSession map[wantedEpisode]bool) (verdict string, ep wantedEpisode) {
	if re != nil && !re.MatchString(title) {
		return verdictOther, ep
	}
	ep = wantedEpisode{season: -1}
	if w.kind() != "movie" {
		if _, season, episode, ok := rss.FindEpisode(title); ok {
			ep = wantedEpisode{season: season, episode: episode}
			if !seasonAllowed(w.Seasons, season) && strings.TrimSpace(w.EpisodeFilter) == "" {
				return verdictSeason, ep
			}
		} else if packSeason, ok := wantedPackSeason(title); ok {
			ep = wantedEpisode{season: packSeason, episode: 0}
			if !seasonAllowed(w.Seasons, packSeason) && strings.TrimSpace(w.EpisodeFilter) == "" {
				return verdictSeason, ep
			}
			if filter, err := rss.ParseEpisodeFilter(w.episodeFilter()); err != nil || !filter.MatchesSeason(packSeason) {
				return verdictEpisode, ep
			}
		} else {
			return verdictNoEp, ep
		}
	} else if _, _, _, ok := rss.FindEpisode(title); ok {
		return verdictOther, ep
	}
	for _, term := range splitList(w.Language) {
		if strings.TrimSpace(term) != "" && !rss.HasTerm(title, term) {
			return verdictLanguage, ep
		}
	}
	for _, term := range splitList(w.Subtitles) {
		if strings.TrimSpace(term) != "" && !rss.HasTerm(title, term) {
			return verdictSubtitle, ep
		}
	}
	if !w.qualityMatch(title) {
		return verdictQuality, ep
	}
	for _, term := range splitList(w.Exclude) {
		if rss.HasTerm(title, term) {
			return verdictExcluded, ep
		}
	}
	if ep.season > 0 && ep.episode == 0 {
		// Season pack: the season and name checks above are the verdict;
		// the episode-based criteria would reject the pack for having no
		// episode number.
	} else if !w.criteria().Match(rss.Item{Title: title}, now) {
		return verdictEpisode, ep
	}
	switch {
	case taken[ep]:
		return verdictTaken, ep
	case inSession[ep]:
		return verdictHad, ep
	}
	return verdictTake, ep
}

// handleUIWantedPreview previews a wanted title from the dialog's fields (or
// a saved one, by index).
func (d *Daemon) handleUIWantedPreview(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	q := r.URL.Query()
	entry := wantedEntry{
		Name: strings.TrimSpace(q.Get("name")), Kind: q.Get("kind"), Seasons: q.Get("seasons"),
		Language: q.Get("language"), Subtitles: q.Get("subtitles"), Quality: q.Get("quality"), Exclude: q.Get("exclude"),
		EpisodeFilter: q.Get("episode_filter"), Category: q.Get("category"), SavePath: q.Get("savepath"),
	}
	if raw := strings.TrimSpace(q.Get("index")); raw != "" {
		list := d.wantedList()
		if index, err := parseFormIndex(raw); err == nil && index >= 0 && index < len(list) {
			entry = list[index]
		}
	}
	// The dialog has one quality control, the profile: resolve it here, or the
	// preview would ignore it and list releases the save would refuse.
	if name := strings.TrimSpace(q.Get("profile")); name != "" && entry.Profile == "" {
		entry.Profile = name
	}
	if entry.Profile != "" && entry.profile.Name == "" {
		if profile, ok := d.qualityProfileByName(entry.Profile); ok {
			entry.profile = profile
		}
	}
	// live asks the indexers now (the interactive search): the archive is read
	// anyway and the two sources are merged, so a release found both in the
	// archive and by the indexers appears once.
	live := q.Get("live") == "1"
	// season+episode scope the whole preview to one episode: the per-episode
	// interactive search, which asks the indexers for "Title SxxEyy" (or reads
	// the archive) and lists only that episode.
	season, episode := -1, -1
	if raw := strings.TrimSpace(q.Get("season")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			season = v
		}
	}
	if raw := strings.TrimSpace(q.Get("episode")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			episode = v
		}
	}
	oneEpisode := season >= 0 && episode >= 0
	type release struct {
		Title   string `json:"title"`
		Size    int64  `json:"size,omitempty"`
		Seeders int    `json:"seeders,omitempty"`
		Episode string `json:"episode,omitempty"`
		Verdict string `json:"verdict"`
		Take    bool   `json:"take,omitempty"`
		Magnet  string `json:"download,omitempty"`
	}
	out := map[string]any{"releases": []release{}, "total": 0}
	text := titleSearchText(entry.Name)
	pattern := titleMatchRegex(entry.Name)
	if len([]rune(strings.TrimSpace(entry.Name))) < 2 || pattern == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if oneEpisode {
		// The per-episode search looks for the episode itself, not the title: an
		// indexer asked for "Title" alone would return the whole series.
		if text == "" {
			text = strings.Trim(strings.TrimSpace(entry.Name), "/")
		}
		text = fmt.Sprintf("%s S%02dE%02d", text, season, episode)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the title regular expression is not valid"))
		return
	}
	var items []rss.Item
	if text != "" {
		items = d.archiveWantedItems(text, wantedArchiveScan)
	}
	// The interactive search: ask the indexers now, so a release that is not in
	// the archive yet can be seen (with its verdict) and taken by hand.
	indexerError := ""
	indexers := len(d.indexers())
	if live && text != "" && indexers > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), indexerSearchTimeout)
		found, err := d.searchFeed(ctx, wantedFeedName(entry.Name), text)
		cancel()
		if err != nil {
			indexerError = err.Error()
		} else {
			items = mergeFeedItems(items, found)
		}
	}
	taken := map[wantedEpisode]bool{}
	for _, ep := range d.wantedLedgerEpisodes(entry) {
		taken[ep] = true
	}
	inSession := d.wantedSessionEpisodes(entry)
	now := time.Now()
	counts := map[string]int{}
	var releases []release
	willTake := map[wantedEpisode]bool{}
	for _, item := range items {
		if oneEpisode {
			// Only the asked episode: the same list is used by the automatic
			// verdict, which would otherwise fill up with the other episodes.
			_, itemSeason, itemEpisode, ok := rss.FindEpisode(item.Title)
			if !ok || itemSeason != season || itemEpisode != episode {
				continue
			}
		}
		verdict, ep := wantedVerdict(entry, re, item.Title, now, taken, inSession)
		if verdict == verdictOther {
			continue
		}
		counts[verdict]++
		take := verdict == verdictTake
		if take {
			willTake[ep] = true
		}
		rel := release{Title: item.Title, Size: item.Size, Seeders: item.Seeders, Verdict: verdict, Take: take, Magnet: item.Download()}
		if ep.season >= 0 {
			if ep.episode == 0 {
				rel.Episode = fmt.Sprintf("S%02d pack", ep.season)
			} else {
				rel.Episode = fmt.Sprintf("S%02dE%02d", ep.season, ep.episode)
			}
		}
		releases = append(releases, rel)
	}
	// What would be taken first, then the rest by verdict and title.
	rank := func(v string) int {
		switch v {
		case verdictTake:
			return 0
		case verdictTaken, verdictHad:
			return 1
		}
		return 2
	}
	sort.SliceStable(releases, func(i, j int) bool {
		if a, b := rank(releases[i].Verdict), rank(releases[j].Verdict); a != b {
			return a < b
		}
		return releases[i].Title < releases[j].Title
	})
	total := len(releases)
	if len(releases) > wantedPreviewMax {
		releases = releases[:wantedPreviewMax]
	}
	var takeEps []wantedEpisode
	for ep := range willTake {
		takeEps = append(takeEps, ep)
	}
	sort.Slice(takeEps, func(i, j int) bool {
		if takeEps[i].season != takeEps[j].season {
			return takeEps[i].season < takeEps[j].season
		}
		return takeEps[i].episode < takeEps[j].episode
	})
	type count struct {
		Verdict string `json:"verdict"`
		Count   int    `json:"count"`
	}
	var byVerdict []count
	for v, n := range counts {
		byVerdict = append(byVerdict, count{Verdict: v, Count: n})
	}
	sort.Slice(byVerdict, func(i, j int) bool { return byVerdict[i].Count > byVerdict[j].Count })
	takeSummary := ""
	if entry.kind() != "movie" {
		var takePacks []int
		var takeRegular []wantedEpisode
		for _, ep := range takeEps {
			if ep.episode == 0 && ep.season > 0 {
				takePacks = append(takePacks, ep.season)
			} else {
				takeRegular = append(takeRegular, ep)
			}
		}
		parts := []string{}
		for _, season := range takePacks {
			parts = append(parts, fmt.Sprintf("S%02d pack", season))
		}
		if ranges := formatEpisodeRanges(takeRegular, wantedEpisodeListMax); ranges != "" {
			parts = append(parts, ranges)
		}
		takeSummary = strings.Join(parts, ", ")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"releases": releases, "total": total, "counts": byVerdict,
		"take": len(willTake), "take_episodes": takeSummary,
		"had": len(taken) + len(inSession), "movie": entry.kind() == "movie",
		// The action a release taken from here gets, and how the live search
		// went (the page adds a release with the title's own category and path).
		"category": entry.Category, "savepath": entry.SavePath,
		"live": live, "indexers": indexers, "indexer_error": indexerError,
		"season": season, "episode": episode,
	})
}

// mergeFeedItems merges the indexer results into the archive ones for the
// preview: one entry per release. The key is the lower-cased title, because the
// same release read from the archive and from the indexers does not carry the
// same identity (the archive keeps an info-hash or a detail URL, the indexer a
// magnet), while the title is what the operator recognises.
func mergeFeedItems(base, extra []rss.Item) []rss.Item {
	seen := map[string]bool{}
	for _, item := range base {
		seen[strings.ToLower(item.Title)] = true
	}
	out := base
	for _, item := range extra {
		key := strings.ToLower(item.Title)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

// wantedLedgerEpisodes are the episodes the wanted engine itself recorded as
// taken (wantedTakenEpisodes without the session ones).
func (d *Daemon) wantedLedgerEpisodes(w wantedEntry) []wantedEpisode {
	d.mu.Lock()
	defer d.mu.Unlock()
	prefix := strings.TrimSpace(w.Name) + "\x00"
	var out []wantedEpisode
	for key, eps := range d.state.WantedTaken {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		var season int
		if _, err := fmt.Sscanf(strings.TrimPrefix(key, prefix), "%d", &season); err != nil {
			continue
		}
		for token := range eps {
			var episode int
			if _, err := fmt.Sscanf(strings.SplitN(token, "-", 2)[0], "%d", &episode); err == nil {
				out = append(out, wantedEpisode{season: season, episode: episode})
			}
		}
	}
	return out
}
