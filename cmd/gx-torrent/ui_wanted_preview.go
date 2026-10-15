package main

// ui_wanted_preview.go answers "what would this wanted title take, and why
// not the rest?" (GET /ui/wanted-preview). It reads the archive the feeds and
// the indexers fill, and gives every release of the title a verdict: taken
// already, had (in the session from elsewhere), would be taken now, or the
// first filter that turns it away (season, episode filter, language, quality,
// excluded term). The add/edit dialog shows it live while the filters are
// typed, the list shows it per title: nothing is decided out of sight.

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
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
func wantedVerdict(w wantedEntry, re *regexp.Regexp, title string, now time.Time, taken, inSession map[wantedEpisode]bool) (verdict string, ep wantedEpisode) {
	if re != nil && !re.MatchString(title) {
		return verdictOther, ep
	}
	ep = wantedEpisode{season: -1}
	if w.kind() != "movie" {
		_, season, episode, ok := rss.FindEpisode(title)
		if !ok {
			return verdictNoEp, ep
		}
		ep = wantedEpisode{season: season, episode: episode}
		if !seasonAllowed(w.Seasons, season) && strings.TrimSpace(w.EpisodeFilter) == "" {
			return verdictSeason, ep
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
	if !wantedQualityMatch(w.Quality, strings.ToLower(title)) {
		return verdictQuality, ep
	}
	for _, term := range splitList(w.Exclude) {
		if rss.HasTerm(title, term) {
			return verdictExcluded, ep
		}
	}
	if !w.criteria().Match(rss.Item{Title: title}, now) {
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
		EpisodeFilter: q.Get("episode_filter"),
	}
	if raw := strings.TrimSpace(q.Get("index")); raw != "" {
		list := d.wantedList()
		if index, err := parseFormIndex(raw); err == nil && index >= 0 && index < len(list) {
			entry = list[index]
		}
	}
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
	re, err := regexp.Compile(pattern)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the title regular expression is not valid"))
		return
	}
	var items []rss.Item
	if text != "" {
		items = d.archiveWantedItems(text, wantedArchiveScan)
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
			rel.Episode = fmt.Sprintf("S%02dE%02d", ep.season, ep.episode)
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
		takeSummary = formatEpisodeRanges(takeEps, wantedEpisodeListMax)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"releases": releases, "total": total, "counts": byVerdict,
		"take": len(willTake), "take_episodes": takeSummary,
		"had": len(taken) + len(inSession), "movie": entry.kind() == "movie",
	})
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
