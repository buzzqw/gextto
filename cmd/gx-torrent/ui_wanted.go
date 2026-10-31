package main

// ui_wanted.go is the dedicated "wanted" page (/ui/wanted): the titles the
// operator is looking for. The list is edited here; the engine (see wanted.go)
// searches the configured indexers and the RSS feeds for each title, on the
// periodic cycle or immediately (the page's "Search all wanted" / per-row
// "Search" buttons). The "search in feeds" step probes the indexers and the
// configured RSS feeds for candidate series, filtered by the language/quality
// typed so far.
//
// A wanted title does not generate a feed or a rule: it is the engine that
// queries the indexers and the feeds for it.

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/webui/assets"
)

// wantedEpisodeListMax bounds how many episodes the Have/Missing list of a
// wanted row shows before a trailing "+N".
const wantedEpisodeListMax = 24

// handleUIWanted adds, edits, pauses/resumes or removes one wanted title.
func (d *Daemon) handleUIWanted(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		d.handleUIWantedGet(w, r)
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	list := d.wantedList()
	switch strings.TrimSpace(r.FormValue("op")) {
	case "remove":
		index, err := parseFormIndex(r.FormValue("index"))
		if err != nil || index < 0 || index >= len(list) {
			d.renderWanted(w, r, "The wanted title to remove does not exist anymore.")
			return
		}
		list = append(list[:index], list[index+1:]...)
	case "toggle-pause", "pause":
		index, err := parseFormIndex(r.FormValue("index"))
		if err != nil || index < 0 || index >= len(list) {
			d.renderWanted(w, r, "The wanted title to pause or resume does not exist anymore.")
			return
		}
		list[index].Paused = !list[index].Paused
	default:
		entry := wantedEntry{
			Name:          strings.TrimSpace(r.FormValue("name")),
			Kind:          strings.TrimSpace(r.FormValue("kind")),
			Seasons:       strings.TrimSpace(r.FormValue("seasons")),
			Language:      strings.TrimSpace(r.FormValue("language")),
			Subtitles:     strings.TrimSpace(r.FormValue("subtitles")),
			Quality:       strings.TrimSpace(r.FormValue("quality")),
			Exclude:       strings.TrimSpace(r.FormValue("exclude")),
			EpisodeFilter: strings.TrimSpace(r.FormValue("episode_filter")),
			Category:      strings.TrimSpace(r.FormValue("category")),
			SavePath:      strings.TrimSpace(r.FormValue("savepath")),
			Paused:        formBool(r, "paused"),
		}
		if entry.Name == "" {
			d.renderWanted(w, r, "A wanted title needs a name.")
			return
		}
		if pattern, ok := titleRegexPattern(entry.Name); ok {
			if _, err := regexp.Compile("(?i)" + pattern); err != nil {
				d.renderWanted(w, r, "The title regular expression is not valid.")
				return
			}
		}
		if _, err := rss.ParseEpisodeFilter(entry.episodeFilter()); err != nil {
			d.renderWanted(w, r, err.Error())
			return
		}
		if index, err := parseFormIndex(r.FormValue("index")); err == nil && index >= 0 {
			if index >= len(list) {
				d.renderWanted(w, r, "The wanted title to edit does not exist anymore.")
				return
			}
			list[index] = entry
		} else {
			replaced := false
			for i := range list {
				if strings.EqualFold(list[i].Name, entry.Name) {
					list[i] = entry
					replaced = true
					break
				}
			}
			if !replaced {
				list = append(list, entry)
			}
		}
	}
	if err := d.saveWanted(list); err != nil {
		d.renderWanted(w, r, "Cannot save the wanted list: "+err.Error())
		return
	}
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/wanted", http.StatusSeeOther)
}

func (d *Daemon) handleUIWantedGet(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	d.renderWanted(w, r, "")
}

// wantedEpisodeView is one episode of a wanted title for the episode grid: its
// state (downloaded, downloading or missing) and, when the episode is in the
// session, the torrent that holds it, so the page can link to it.
type wantedEpisodeView struct {
	Season   int     `json:"season"`
	Episode  int     `json:"episode"`
	Have     bool    `json:"have"`
	State    string  `json:"state"`
	Hash     string  `json:"hash,omitempty"`
	Name     string  `json:"name,omitempty"`
	Progress float64 `json:"progress,omitempty"`
}

// wantedEpisodeViews lists the known episodes of a wanted title, ordered by
// season then episode, with what the operator has: downloaded (taken by the
// engine, or in the session and complete), downloading (in the session, still
// running) or missing (seen in the feeds/indexers, not taken).
func (d *Daemon) wantedEpisodeViews(w wantedEntry) []wantedEpisodeView {
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
	media := d.wantedSessionMedia(w)

	prefix := strings.TrimSpace(w.Name) + "\x00"
	states := map[[2]int]string{}
	mediaByKey := map[[2]int]torrentInfo{}
	var keys [][2]int
	record := func(k [2]int, state string) {
		if _, ok := states[k]; !ok {
			keys = append(keys, k)
		}
		states[k] = strongerEpisodeState(states[k], state)
	}
	add := func(byKey map[string]smartEpisodes, state string) {
		for key, eps := range byKey {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			season, err := strconv.Atoi(strings.TrimPrefix(key, prefix))
			if err != nil {
				continue
			}
			for token := range eps {
				episode, err := strconv.Atoi(strings.SplitN(token, "-", 2)[0])
				if err != nil {
					continue
				}
				record([2]int{season, episode}, state)
			}
		}
	}
	add(seen, wantedEpisodeMissing)
	add(taken, wantedEpisodeDownloaded)
	for ep, view := range media {
		state := wantedEpisodeDownloading
		if mediaDownloaded(view) {
			state = wantedEpisodeDownloaded
		}
		k := [2]int{ep.season, ep.episode}
		record(k, state)
		mediaByKey[k] = view
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	out := make([]wantedEpisodeView, 0, len(keys))
	for _, k := range keys {
		state := states[k]
		view := wantedEpisodeView{
			Season:  k[0],
			Episode: k[1],
			Have:    state == wantedEpisodeDownloaded,
			State:   state,
		}
		if media, ok := mediaByKey[k]; ok {
			view.Hash = media.Hash
			view.Name = media.Name
			view.Progress = media.Progress
		}
		out = append(out, view)
	}
	return out
}

// strongerEpisodeState keeps the most advanced of two states of the same
// episode: downloaded beats downloading beats missing.
func strongerEpisodeState(a, b string) string {
	if a == "" {
		return b
	}
	rank := func(s string) int {
		switch s {
		case wantedEpisodeDownloaded:
			return 2
		case wantedEpisodeDownloading:
			return 1
		}
		return 0
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// handleUIWantedEpisodes reads and edits the episode ledger of one wanted title:
// the episode manager marks what is present and what is missing. A GET returns
// the episodes; a POST (op=have|missing|add) changes one.
func (d *Daemon) handleUIWantedEpisodes(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	list := d.wantedList()
	if r.Method == http.MethodGet {
		index, err := parseFormIndex(r.URL.Query().Get("index"))
		if err != nil || index < 0 || index >= len(list) {
			writeError(w, http.StatusNotFound, errNotFound)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{
			"name":     list[index].Name,
			"kind":     list[index].kind(),
			"episodes": d.wantedEpisodeViews(list[index]),
		})
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	index, err := parseFormIndex(r.FormValue("index"))
	if err != nil || index < 0 || index >= len(list) {
		writeError(w, http.StatusNotFound, errNotFound)
		return
	}
	entry := list[index]
	if entry.kind() == "movie" {
		writeError(w, http.StatusBadRequest, errors.New("a movie has no episodes"))
		return
	}
	season, errSeason := strconv.Atoi(strings.TrimSpace(r.FormValue("season")))
	episode, errEpisode := strconv.Atoi(strings.TrimSpace(r.FormValue("episode")))
	if errSeason != nil || errEpisode != nil || season < 0 || episode <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("season and episode are required"))
		return
	}
	switch strings.TrimSpace(r.FormValue("op")) {
	case "have":
		d.setWantedTaken(entry.Name, season, episode, true)
	case "missing", "add":
		// Known and not downloaded: it counts as missing and is searched again.
		d.addWantedSeen(entry.Name, season, episode)
		d.setWantedTaken(entry.Name, season, episode, false)
	default:
		writeError(w, http.StatusBadRequest, errors.New("unknown episode operation"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "episodes": d.wantedEpisodeViews(entry)})
}

func (d *Daemon) renderWanted(w http.ResponseWriter, r *http.Request, errKey string) {
	page := assets.WantedPage()
	wanteds := d.wantedList()
	missingCount, downloadingCount, completeCount, pausedCount := 0, 0, 0, 0
	for _, w := range wanteds {
		stats := d.wantedStatsFor(w)
		switch wantedTitleState(w, stats) {
		case "paused":
			pausedCount++
		case "downloading":
			downloadingCount++
		case "missing":
			missingCount++
		case "uptodate":
			completeCount++
		}
	}
	page = strings.Replace(page, "{{WANTEDCOUNT}}", strconv.Itoa(len(wanteds)), 1)
	page = strings.Replace(page, "{{WANTEDMISSING}}", strconv.Itoa(missingCount), 1)
	page = strings.Replace(page, "{{WANTEDDOWNLOADING}}", strconv.Itoa(downloadingCount), 1)
	page = strings.Replace(page, "{{WANTEDCOMPLETE}}", strconv.Itoa(completeCount), 1)
	page = strings.Replace(page, "{{WANTEDPAUSED}}", strconv.Itoa(pausedCount), 1)
	page = strings.Replace(page, "{{WANTEDROWS}}", d.uiWantedRowsHTML(), 1)

	errorHTML := ""
	if errKey != "" {
		errorHTML = `<div class="notice err" role="alert">` + html.EscapeString(errKey) + `</div>`
	}
	page = strings.Replace(page, "<!--ERROR-->", errorHTML, 1)

	lang := d.uiLang(r)
	if lang != "" && lang != "en" {
		page = strings.Replace(page, `lang="en"`, `lang="`+lang+`"`, 1)
	}
	page = uiTranslateHTML(page, uiDictionary(lang))
	page = uiInjectClientDictionary(page, lang)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func (d *Daemon) handleUIWantedPoll(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	res := d.pollWantedTitles(ctx, name)
	writeJSON(w, http.StatusOK, res)
}

// wantedSearchResult is the answer of the wanted-title search step: the series
// names found, plus where they came from. The counts let the page say whether
// the indexers were queried (and whether they answered) instead of leaving the
// operator guessing why only feed names showed up.
type wantedSearchResult struct {
	Names        []string `json:"names"`
	Indexers     int      `json:"indexers"`
	IndexerHits  int      `json:"indexer_hits"`
	FeedHits     int      `json:"feed_hits"`
	ArchiveHits  int      `json:"archive_hits"`
	IndexerError string   `json:"indexer_error,omitempty"`
}

// wantedArchiveScan bounds how many archive entries the wanted step reads.
const wantedArchiveScan = 500

// archiveWantedItems returns the archived releases whose title matches the
// query, as feed items. The archive is where the feeds keep what they find, so
// a wanted title searches it too, next to the indexers.
func (d *Daemon) archiveWantedItems(query string, limit int) []rss.Item {
	archive := d.archive()
	if archive == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	page, err := archive.search(parseArchiveQuery(query, "", false), "seeds", 1, wantedArchiveScan)
	if err != nil {
		return nil
	}
	out := make([]rss.Item, 0, len(page.Items))
	for _, entry := range page.Items {
		if len(out) >= limit {
			break
		}
		out = append(out, archiveItemToRSS(entry))
	}
	return out
}

// archiveItemToRSS maps an archived entry to the feed item shape used by the
// rules and wanted pipeline.
func archiveItemToRSS(entry archiveEntry) rss.Item {
	item := rss.Item{
		Title:    entry.Title,
		Size:     entry.Size,
		Seeders:  entry.Seeders,
		Leechers: entry.Leechers,
		Source:   entry.Feed,
		GUID:     entry.Download,
	}
	if strings.HasPrefix(entry.Download, "magnet:") {
		item.Magnet = entry.Download
	} else {
		item.Link = entry.Download
	}
	if entry.Published > 0 {
		item.Published = time.Unix(entry.Published, 0)
	}
	return item
}

// handleUIWantedSearch searches for candidate series. By default it reads the
// archive (+ the configured RSS feeds), which is fast; with live=1 it also
// queries the configured indexers (slow) and archives the results.
func (d *Daemon) handleUIWantedSearch(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	// A /regex/ title is searched by its literal words.
	query := titleSearchText(r.URL.Query().Get("q"))
	langFilters := splitList(r.URL.Query().Get("language"))
	qualityFilter := strings.TrimSpace(r.URL.Query().Get("quality"))
	if len([]rune(query)) < 2 {
		writeJSON(w, http.StatusOK, wantedSearchResult{Names: []string{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), indexerSearchTimeout)
	defer cancel()

	// The archive is the search index: the feeds and the periodic indexer polls
	// keep it full, so a search answers at once. The indexers are queried live
	// only when explicitly asked (live=1), because that is the slow path.
	live := strings.TrimSpace(r.URL.Query().Get("live")) == "1"

	result := wantedSearchResult{Names: []string{}}
	var items []rss.Item
	indexers := d.indexers()
	result.Indexers = len(indexers)
	if live && result.Indexers > 0 {
		found, errs := searchIndexers(ctx, indexers, query)
		// Every failing indexer is reported, also when the others answered:
		// a silent failure made a search look complete when it was not.
		var failed []string
		for _, err := range errs {
			failed = append(failed, err.Error())
		}
		result.IndexerError = strings.Join(failed, "; ")
		result.IndexerHits = len(found)
		items = append(items, found...)
		// The live results are archived too, so the next search reads them from
		// the archive instead of asking the indexer again.
		if len(found) > 0 {
			if _, aerr := d.archive().record("search:"+strings.ToLower(query), found, time.Now()); aerr != nil {
				logfError("wanted search %q: cannot archive the indexer results: %v", query, aerr)
			}
		}
	}
	// A feed hands over its whole current content, not a search: only the items
	// that match the query count as hits, otherwise the counter looks full
	// while nothing relevant was found.
	terms := strings.Fields(strings.ToLower(query))
	for _, item := range d.wantedRSSItems(ctx) {
		if titleHasAllTerms(strings.ToLower(item.Title), terms) {
			result.FeedHits++
			items = append(items, item)
		}
	}
	archiveItems := d.archiveWantedItems(query, wantedArchiveScan)
	result.ArchiveHits = len(archiveItems)
	items = append(items, archiveItems...)
	result.Names = filterSuggestions(query, items, langFilters, qualityFilter, 20)
	// Season packs ("Show S01", "Show Season 2") are series too, without an
	// episode marker: their names join the list.
	result.Names = mergeNames(result.Names, baseNameSuggestions(query, items, langFilters, qualityFilter, true), 20)
	if len(result.Names) == 0 {
		// Nothing looks like a series (a movie, a special): suggest the clean
		// names of the matching releases, never a raw release title.
		result.Names = baseNameSuggestions(query, items, langFilters, qualityFilter, false)
		if len(result.Names) > 20 {
			result.Names = result.Names[:20]
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// mergeNames appends the names of extra that are not in base yet (case
// insensitively), up to limit.
func mergeNames(base, extra []string, limit int) []string {
	seen := map[string]bool{}
	for _, name := range base {
		seen[strings.ToLower(name)] = true
	}
	for _, name := range extra {
		if len(base) >= limit {
			break
		}
		if !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			base = append(base, name)
		}
	}
	return base
}

// wantedRSSItems fetches the items of every configured RSS feed (not the search
// feeds, whose results come from the indexers), for the wanted search step.
func (d *Daemon) wantedRSSItems(ctx context.Context) []rss.Item {
	var out []rss.Item
	client := outboundClient(feedRequestTimeout)
	for _, feed := range d.feeds() {
		if strings.TrimSpace(feed.URL) == "" || feed.Paused {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		items, err := d.fetchFeedItems(ctx, client, feed.URL, feed.Name)
		if err != nil {
			continue
		}
		out = append(out, items...)
	}
	return out
}

// titleHasAllTerms reports whether the lower-cased title contains every term.
func titleHasAllTerms(lowerTitle string, terms []string) bool {
	for _, term := range terms {
		if term != "" && !strings.Contains(lowerTitle, term) {
			return false
		}
	}
	return true
}

// filterSuggestions reduces release titles to unique series names whose title
// contains every word of the query and every include term, most frequent first.
// The query matters because the RSS feeds hand over their whole current content,
// not a search: without it the suggestions list unrelated series of the feeds.
func filterSuggestions(query string, items []rss.Item, includeLang []string, quality string, limit int) []string {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	counts := map[string]int{}
	var names []string
	for _, item := range items {
		title := strings.ToLower(item.Title)
		if !titleHasAllTerms(title, terms) {
			continue
		}
		ok := true
		for _, term := range includeLang {
			if term = strings.ToLower(strings.TrimSpace(term)); term != "" && !strings.Contains(title, term) {
				ok = false
				break
			}
		}
		if !ok || !wantedQualityMatch(quality, title) {
			continue
		}
		series, _, _, found := rss.FindEpisode(item.Title)
		if !found || len([]rune(series)) < 2 {
			continue
		}
		if _, seen := counts[series]; !seen {
			names = append(names, series)
		}
		counts[series]++
	}
	sort.SliceStable(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > limit {
		names = names[:limit]
	}
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = titleCase(name)
	}
	return out
}

// releaseSeasonMarker matches a season marker without an episode (S01, a disc
// S03D01, Season 2, Stagione 3, a complete pack); releaseNameCut matches where
// the name of a release ends: a year, a resolution, a source/codec/format tag
// or the start of a bracketed group.
var (
	releaseSeasonMarker = regexp.MustCompile(`(?i)(^|[\s._\-(\[])(s\d{1,2}(d\d{1,2})?(\b|[\s._\-])|season[\s._]*\d|stagione[\s._]*\d|complete\b|completa\b)`)
	releaseNameCut      = regexp.MustCompile(`(?i)(^|[\s._\-(\[])((19|20)\d{2}\b|\d{3,4}p\b|\d+bit\b|4k\w*|uhd\b|imax\b|hdr\b|web[\s._\-]?(dl|rip)?\b|blu[\s._\-]?ray|bd\w*\b|hdts\b|hdtv|dvdrip|x26[45]|h[\s._]?26[45]|hevc|remux|ita\b|eng\b|multi\b)|[\[(*]`)
	releaseNameSymbols  = regexp.MustCompile(`[^\p{L}\p{N}' ]+`)
)

// releaseBaseName reduces a release title to the name it is about and says
// whether it is a season pack ("The.Mandalorian.S01.2160p" -> "the mandalorian",
// true; "The Mandalorian and Grogu 2026 1080p" -> "the mandalorian and grogu",
// false). A title in two languages ("Мандалорец / The Mandalorian") keeps the
// part that matches the query terms.
func releaseBaseName(title string, terms ...string) (name string, seasonPack bool) {
	title = html.UnescapeString(html.UnescapeString(title))
	if parts := strings.Split(title, " / "); len(parts) > 1 {
		title = parts[len(parts)-1]
		for _, part := range parts {
			if titleHasAllTerms(strings.ToLower(part), terms) {
				title = part
				break
			}
		}
	}
	cut := len(title)
	if loc := releaseSeasonMarker.FindStringIndex(title); loc != nil {
		cut, seasonPack = loc[0], true
	}
	if loc := releaseNameCut.FindStringIndex(title[:cut]); loc != nil && loc[0] > 0 {
		cut = loc[0]
	}
	name = strings.ToLower(title[:cut])
	name = strings.ReplaceAll(name, "&", " and ")
	name = releaseNameSymbols.ReplaceAllString(strings.NewReplacer(".", " ", "_", " ").Replace(name), " ")
	return strings.Join(strings.Fields(name), " "), seasonPack
}

// baseNameSuggestions returns the clean names of the matching releases, most
// frequent first, with the same query/language/quality filters as
// filterSuggestions. With packsOnly it keeps only season packs (series without
// an episode marker); otherwise every release counts (movies, specials).
func baseNameSuggestions(query string, items []rss.Item, includeLang []string, quality string, packsOnly bool) []string {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	counts := map[string]int{}
	var names []string
	for _, item := range items {
		lower := strings.ToLower(item.Title)
		if !titleHasAllTerms(lower, terms) {
			continue
		}
		ok := true
		for _, term := range includeLang {
			if term = strings.ToLower(strings.TrimSpace(term)); term != "" && !strings.Contains(lower, term) {
				ok = false
				break
			}
		}
		if !ok || !wantedQualityMatch(quality, lower) {
			continue
		}
		if _, _, _, episode := rss.FindEpisode(item.Title); episode {
			continue // already a series name in filterSuggestions
		}
		name, pack := releaseBaseName(item.Title, terms...)
		if (packsOnly && !pack) || len([]rune(name)) < 2 || !titleHasAllTerms(name, terms) {
			continue
		}
		if _, seen := counts[name]; !seen {
			names = append(names, name)
		}
		counts[name]++
	}
	sort.SliceStable(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = titleCase(name)
	}
	return out
}

// wantedSessionHash returns the torrent of a wanted title currently in the
// session, when there is one, so the row's primary action can open it.
func (d *Daemon) wantedSessionHash(w wantedEntry) string {
	for _, media := range d.wantedSessionMedia(w) {
		if media.Hash != "" {
			return media.Hash
		}
	}
	return ""
}

// uiWantedRowsHTML renders the wanted titles as rows, each with Search,
// Edit, Pause/Resume and Remove actions, and the missing episodes found so far.
func (d *Daemon) uiWantedRowsHTML() string {
	list := d.wantedList()
	if len(list) == 0 {
		return `<li class="empty"><svg class="i" style="width:40px;height:40px;color:var(--subtle)"><use href="/assets/icons.svg#i-tv"/></svg><h3>Nothing wanted yet</h3><p>Add a series or a movie: gx-torrent watches the feeds and the indexers for it and downloads what is missing.</p><button type="button" class="primary sm" onclick="document.getElementById('add-wanted').click()" title="Add a series or a movie to the wanted list">Add title</button></li>`
	}
	var b strings.Builder
	now := time.Now()
	for index, w := range list {
		idx := strconv.Itoa(index)
		stats := d.wantedStatsFor(w)
		state := wantedTitleState(w, stats)
		badgeClass, badgeLabel := wantedBadge(state)
		icon, kindLabel := "i-tv", "Series"
		if w.kind() == "movie" {
			icon, kindLabel = "i-film", "Movie"
		}
		cls := "item"
		if w.Paused {
			cls += " off"
		}
		pauseText, pauseTitle := "Pause monitoring", "Pause monitoring this title"
		if w.Paused {
			pauseText, pauseTitle = "Resume monitoring", "Resume monitoring this title"
		}
		lastSearch := time.Time{}
		if _, last := d.wantedProgress(w); !last.IsZero() {
			lastSearch = last
		}
		nextGap := d.wantedNextGap(w.Name, now)
		hash := d.wantedSessionHash(w)
		b.WriteString(`<li class="` + cls + `" data-wanted-row="` + idx + `" data-wanted-state="` + state + `" data-wanted-name="` + html.EscapeString(w.Name) + `" data-wanted-missing="` + strconv.Itoa(stats.Missing) + `" data-wanted-downloading="` + strconv.Itoa(stats.Downloading) + `" data-wanted-last="` + strconv.FormatInt(lastSearch.Unix(), 10) + `" data-wanted-gap="` + strconv.FormatInt(nextGap.Unix(), 10) + `" data-wanted-index="` + idx + `"><div class="kind-icon" aria-hidden="true"><svg class="i"><use href="/assets/icons.svg#` + icon + `"/></svg></div><div class="item-main">`)
		b.WriteString(`<div class="item-title"><span class="name">` + html.EscapeString(w.Name) + `</span><span class="chip">` + kindLabel + `</span><span class="badge ` + badgeClass + `">` + badgeLabel + `</span></div>`)

		var chips strings.Builder
		if w.kind() != "movie" && strings.TrimSpace(w.Seasons) != "" {
			chips.WriteString(uiChip("", "Seasons", w.Seasons))
		}
		if strings.TrimSpace(w.Language) != "" {
			chips.WriteString(uiChip("in", "Language", w.Language))
		}
		if strings.TrimSpace(w.Subtitles) != "" {
			chips.WriteString(uiChip("in", "Subtitles", w.Subtitles))
		}
		if q := strings.TrimSpace(w.Quality); q != "" {
			chips.WriteString(uiChip("in", "Quality", q))
		}
		if strings.TrimSpace(w.Exclude) != "" {
			chips.WriteString(uiChip("out", "Exclude", w.Exclude))
		}
		if strings.TrimSpace(w.EpisodeFilter) != "" {
			chips.WriteString(uiChip("", "Episodes", w.EpisodeFilter))
		}
		if strings.TrimSpace(w.Category) != "" {
			chips.WriteString(uiChip("act", "Category", w.Category))
		}
		if strings.TrimSpace(w.SavePath) != "" {
			chips.WriteString(uiChip("act", "Save to", w.SavePath))
		}
		if chips.Len() > 0 {
			b.WriteString(`<div class="chips">` + chips.String() + `</div>`)
		}

		// What the operator has, honestly: how many episodes are downloaded,
		// how many are still downloading and how many were found but not taken.
		// A season we only partly saw is never claimed complete.
		b.WriteString(`<div class="item-status">`)
		b.WriteString(d.uiWantedStatusHTML(w, stats))
		if !lastSearch.IsZero() {
			b.WriteString(` · <span class="muted" title="When the indexers were last searched for this title"><span>Last search</span> ` + lastSearch.Format("02/01 15:04") + `</span>`)
		}
		if !w.Paused {
			b.WriteString(` · <span class="muted" title="When the missing episodes are searched next"><span>Next search</span> ` + uiWantedWait(int64(time.Until(nextGap).Seconds())) + `</span>`)
		}
		b.WriteString(`</div>`)
		if w.kind() != "movie" {
			// The episode grid is loaded on demand into this container.
			b.WriteString(`<div class="epgrid" data-wanted-grid="` + idx + `" data-name="` + html.EscapeString(w.Name) + `" hidden></div>`)
		}
		b.WriteString(`<div class="item-status" data-wanted-status="` + idx + `" aria-live="polite"></div></div><div class="item-actions">`)

		// One primary action, chosen from the state; everything else is in the
		// "More" menu so the row never becomes a wall of buttons.
		episodesButton := `<button type="button" class="sm" data-wanted-episodes data-index="` + idx + `" data-name="` + html.EscapeString(w.Name) + `" aria-expanded="false" title="Show or hide the episodes of this title"><svg class="i"><use href="/assets/icons.svg#i-list"/></svg>Episodes</button>`
		releasesButton := `<button type="button" class="sm" data-wanted-releases data-index="` + idx + `" data-name="` + html.EscapeString(w.Name) + `" title="See the releases found and why they were not taken">Why not?</button>`
		searchButton := `<button type="button" class="sm" data-wanted-poll data-index="` + idx + `" data-name="` + html.EscapeString(w.Name) + `" title="Search this title now"><svg class="i"><use href="/assets/icons.svg#i-search"/></svg>Search now</button>`
		missingButton := `<button type="button" class="sm primary" data-wanted-poll data-index="` + idx + `" data-name="` + html.EscapeString(w.Name) + `" title="Search the missing episodes now"><svg class="i"><use href="/assets/icons.svg#i-search"/></svg>Search missing</button>`
		switch {
		case w.Paused:
			b.WriteString(`<form method="post" action="/ui/wanted"><input type="hidden" name="op" value="toggle-pause"><input type="hidden" name="index" value="` + idx + `"><button type="submit" class="sm primary" title="Resume monitoring this title">Resume monitoring</button></form>`)
		case stats.Missing > 0:
			b.WriteString(missingButton)
		case stats.Downloading > 0 && hash != "":
			b.WriteString(`<a class="btn sm primary" href="/?open=` + html.EscapeString(hash) + `" title="Open the torrent in the session">Open</a>`)
		case stats.Downloading > 0:
			b.WriteString(searchButton)
		case stats.Downloaded > 0 && w.kind() != "movie":
			b.WriteString(episodesButton)
		case stats.Downloaded > 0:
			b.WriteString(releasesButton)
		default:
			b.WriteString(searchButton)
		}
		attrs := `data-wanted-edit data-index="` + idx + `"` +
			` data-name="` + html.EscapeString(w.Name) + `"` +
			` data-kind="` + w.kind() + `"` +
			` data-seasons="` + html.EscapeString(w.Seasons) + `"` +
			` data-language="` + html.EscapeString(w.Language) + `"` +
			` data-subtitles="` + html.EscapeString(w.Subtitles) + `"` +
			` data-quality="` + html.EscapeString(w.Quality) + `"` +
			` data-exclude="` + html.EscapeString(w.Exclude) + `"` +
			` data-episode="` + html.EscapeString(w.EpisodeFilter) + `"` +
			` data-category="` + html.EscapeString(w.Category) + `"` +
			` data-savepath="` + html.EscapeString(w.SavePath) + `"` +
			` data-paused="` + strconv.FormatBool(w.Paused) + `"`
		b.WriteString(`<details class="row-more"><summary class="sm" aria-label="More actions for this title" title="More actions: releases, episodes, edit, pause, remove">More</summary><div class="row-more-menu">`)
		b.WriteString(releasesButton)
		if w.kind() != "movie" {
			b.WriteString(episodesButton)
		}
		b.WriteString(searchButton)
		b.WriteString(`<button type="button" class="sm" ` + attrs + ` title="Edit this title">Edit</button>`)
		b.WriteString(`<form method="post" action="/ui/wanted"><input type="hidden" name="op" value="toggle-pause"><input type="hidden" name="index" value="` + idx + `"><button type="submit" class="sm ghost" title="` + pauseTitle + `">` + pauseText + `</button></form>`)
		b.WriteString(`<form method="post" action="/ui/wanted" data-confirm="Stop monitoring this title?"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + idx + `"><button type="submit" class="ghost icon sm danger" title="Remove this title from the list" aria-label="Remove this title from the list"><svg class="i"><use href="/assets/icons.svg#i-trash"/></svg></button></form>`)
		b.WriteString(`</div></details></div></li>`)
	}
	return b.String()
}

// uiWantedWait formats the time until the next search; a title never searched
// is "soon" rather than "0s".
func uiWantedWait(seconds int64) string {
	if seconds <= 0 {
		return "soon"
	}
	return uiDuration(seconds)
}

// wantedBadge maps a title state to its badge class and label (a catalog key).
func wantedBadge(state string) (class, label string) {
	switch state {
	case "paused":
		return "b-paused", "Not monitored"
	case "downloading":
		return "b-downloading", "Downloading"
	case "missing":
		return "b-missing", "Episodes missing"
	case "uptodate":
		return "b-seeding", "Complete"
	default:
		return "b-paused", "No episodes yet"
	}
}

// uiWantedStatusHTML renders the "what I have" line of one wanted title: the
// counts of downloaded, downloading and known-missing episodes, with a chip per
// season. For a movie it shows the single release and links to its torrent.
func (d *Daemon) uiWantedStatusHTML(w wantedEntry, stats wantedStats) string {
	if w.kind() == "movie" {
		return d.uiWantedMovieStatusHTML(w, stats)
	}
	var parts []string
	if stats.Downloaded > 0 {
		parts = append(parts, `<span class="ok" title="Episodes downloaded for this title">✓</span> <b>`+strconv.Itoa(stats.Downloaded)+`</b> <span>Downloaded</span>`)
	}
	if stats.Downloading > 0 {
		parts = append(parts, `<span class="warn" title="Episodes still downloading">◐</span> <b>`+strconv.Itoa(stats.Downloading)+`</b> <span>Downloading</span>`)
	}
	if stats.Missing > 0 {
		parts = append(parts, `<span class="warn" title="Episodes found in the feeds or indexers and not downloaded yet">○</span> <b>`+strconv.Itoa(stats.Missing)+`</b> <span>Found, not downloaded</span>`)
	}
	if len(parts) == 0 {
		return `<span class="muted">No episodes found yet</span>`
	}
	status := d.uiWantedProgressHTML(stats) + strings.Join(parts, ` · `)
	if len(stats.Seasons) > 0 {
		var chips []string
		for _, season := range stats.Seasons {
			chips = append(chips, uiWantedSeasonChip(season))
		}
		status += `<div class="seasons" title="Episodes per season">` + strings.Join(chips, " ") + `</div>`
	}
	return status
}

// uiWantedProgressHTML is the progress of the episodes known so far: the bar
// is downloaded / (downloaded + downloading + found but not downloaded). It is
// deliberately not a claim about the whole series, which gx-torrent does not
// know: the tooltip says "of the episodes found so far".
func (d *Daemon) uiWantedProgressHTML(stats wantedStats) string {
	known := stats.Downloaded + stats.Downloading + stats.Missing
	if known == 0 {
		return ""
	}
	pct := stats.Downloaded * 100 / known
	return `<div class="wprogress" role="progressbar" aria-valuenow="` + strconv.Itoa(pct) + `" aria-valuemin="0" aria-valuemax="100" aria-label="Downloaded episodes out of the ones found so far" title="Downloaded episodes out of the ones found so far"><i style="width:` + strconv.Itoa(pct) + `%"></i></div>`
}

// uiWantedSeasonChip renders one season with its episode counts (only the
// non-zero ones), so a partly-seen season is not mistaken for a complete one.
func uiWantedSeasonChip(s wantedSeasonStat) string {
	var parts []string
	if s.Downloaded > 0 {
		parts = append(parts, `<span class="ok">✓`+strconv.Itoa(s.Downloaded)+`</span>`)
	}
	if s.Downloading > 0 {
		parts = append(parts, `<span class="warn">◐`+strconv.Itoa(s.Downloading)+`</span>`)
	}
	if s.Missing > 0 {
		parts = append(parts, `<span class="warn">○`+strconv.Itoa(s.Missing)+`</span>`)
	}
	return `<span class="chip seasonchip">` + fmt.Sprintf("S%02d", s.Season) + ` ` + strings.Join(parts, " ") + `</span>`
}

// uiWantedMovieStatusHTML shows the state of a wanted movie and links to its
// torrent in the session when there is one.
func (d *Daemon) uiWantedMovieStatusHTML(w wantedEntry, stats wantedStats) string {
	media, _ := d.wantedSessionMedia(w)[wantedEpisode{season: -1}]
	if media.Hash != "" {
		link := `<a class="eplink" href="/?open=` + media.Hash + `" title="Opens the torrent in the session">` + html.EscapeString(media.Name) + `</a>`
		if mediaDownloaded(media) {
			return `<span class="ok">✓</span> <span>Downloaded</span> · ` + link
		}
		return `<span class="warn">◐</span> <span>Downloading</span> <b>` + fmt.Sprintf("%.0f%%", media.Progress) + `</b> · ` + link
	}
	if stats.Downloaded > 0 {
		return `<span class="ok">✓</span> <span>Downloaded</span> · <span class="muted">Downloaded earlier, not in the session</span>`
	}
	return `<span class="muted">No release found yet</span>`
}

// wantedProgress returns how many episodes (or releases) were taken for a
// wanted title and when its indexers were last searched.
func (d *Daemon) wantedProgress(w wantedEntry) (taken int, lastSearch time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	prefix := strings.TrimSpace(w.Name) + "\x00"
	for key, eps := range d.state.WantedTaken {
		if strings.HasPrefix(key, prefix) {
			taken += len(eps)
		}
	}
	if last := d.state.WantedSearchLast[strings.TrimSpace(w.Name)]; last > 0 {
		lastSearch = time.Unix(last, 0)
	}
	return taken, lastSearch
}
