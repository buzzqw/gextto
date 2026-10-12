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

func (d *Daemon) renderWanted(w http.ResponseWriter, r *http.Request, errKey string) {
	page := assets.WantedPage()
	wanteds := d.wantedList()
	activeCount := 0
	pausedCount := 0
	for _, w := range wanteds {
		if w.Paused {
			pausedCount++
		} else {
			activeCount++
		}
	}
	page = strings.Replace(page, "{{WANTEDCOUNT}}", strconv.Itoa(len(wanteds)), 1)
	page = strings.Replace(page, "{{WANTEDACTIVE}}", strconv.Itoa(activeCount), 1)
	page = strings.Replace(page, "{{WANTEDPAUSED}}", strconv.Itoa(pausedCount), 1)
	page = strings.Replace(page, "{{WANTEDROWS}}", d.uiWantedRowsHTML(), 1)

	errorHTML := ""
	if errKey != "" {
		errorHTML = `<p class="err">` + html.EscapeString(errKey) + `</p>`
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
	query := strings.TrimSpace(r.URL.Query().Get("q"))
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
				logf("wanted search %q: cannot archive the indexer results: %v", query, aerr)
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
	client := &http.Client{Timeout: feedRequestTimeout}
	for _, feed := range d.feeds() {
		if strings.TrimSpace(feed.URL) == "" || feed.Paused {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		items, err := rss.Fetch(ctx, client, feed.URL, feed.Name)
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

// uiWantedRowsHTML renders the wanted titles as table rows, each with Search,
// Edit, Pause/Resume and Remove actions, and the missing episodes found so far.
func (d *Daemon) uiWantedRowsHTML() string {
	list := d.wantedList()
	if len(list) == 0 {
		return `<tr><td colspan="7" class="muted">None</td></tr>`
	}
	var b strings.Builder
	for index, w := range list {
		missingLabel := "—"
		if w.kind() != "movie" {
			missing := d.wantedMissing(w)
			if len(missing) == 0 {
				missingLabel = "✓"
			} else {
				parts := make([]string, 0, len(missing))
				for i, ep := range missing {
					if i == 8 {
						parts = append(parts, "…")
						break
					}
					parts = append(parts, fmt.Sprintf("S%02dE%02d", ep.season, ep.episode))
				}
				missingLabel = strings.Join(parts, " ")
			}
		}
		stateClass := "flag on"
		stateText := "active"
		pauseBtnText := "Pause"
		pauseBtnTitle := "Pause monitoring this wanted title"
		if w.Paused {
			stateClass = "flag"
			stateText = "paused"
			pauseBtnText = "Resume"
			pauseBtnTitle = "Resume monitoring this wanted title"
		}
		idxStr := strconv.Itoa(index)
		b.WriteString("<tr><td>" + html.EscapeString(w.Name) + "</td>")
		b.WriteString("<td>" + html.EscapeString(w.kind()) + "</td>")
		b.WriteString("<td>" + html.EscapeString(orDefault(w.Seasons, "—")) + "</td>")
		b.WriteString("<td>" + html.EscapeString(strings.Join(w.includes(), ", ")) + "</td>")
		b.WriteString("<td>" + html.EscapeString(missingLabel) + "</td>")
		b.WriteString(`<td><span class="` + stateClass + `">` + stateText + `</span></td>`)
		b.WriteString(`<td class="actions">`)
		b.WriteString(`<button type="button" class="sm" data-wanted-poll data-index="` + idxStr + `" data-name="` + html.EscapeString(w.Name) + `" title="Search this title now">Search</button> `)
		attrs := `data-wanted-edit data-index="` + idxStr + `"` +
			` data-name="` + html.EscapeString(w.Name) + `"` +
			` data-kind="` + w.kind() + `"` +
			` data-seasons="` + html.EscapeString(w.Seasons) + `"` +
			` data-language="` + html.EscapeString(w.Language) + `"` +
			` data-quality="` + html.EscapeString(w.Quality) + `"` +
			` data-exclude="` + html.EscapeString(w.Exclude) + `"` +
			` data-episode="` + html.EscapeString(w.EpisodeFilter) + `"` +
			` data-category="` + html.EscapeString(w.Category) + `"` +
			` data-savepath="` + html.EscapeString(w.SavePath) + `"` +
			` data-paused="` + strconv.FormatBool(w.Paused) + `"`
		b.WriteString(`<button type="button" class="sm" ` + attrs + ` title="Edit wanted title">Edit</button> `)
		b.WriteString(`<form method="post" action="/ui/wanted"><input type="hidden" name="op" value="toggle-pause"><input type="hidden" name="index" value="` + idxStr + `"><button type="submit" class="secondary" title="` + pauseBtnTitle + `">` + pauseBtnText + `</button></form> `)
		b.WriteString(`<form method="post" action="/ui/wanted"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + idxStr + `"><button type="submit" class="danger" title="Remove this wanted title">Remove</button></form> `)
		b.WriteString(`<span class="muted" data-wanted-status="` + idxStr + `" aria-live="polite" style="margin-left:6px"></span>`)
		b.WriteString("</td></tr>")
	}
	return b.String()
}
