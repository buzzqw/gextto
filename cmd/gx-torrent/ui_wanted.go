package main

// ui_wanted.go is the "wanted" section of the RSS page: the titles the operator
// is looking for. The list is edited here; the engine (see wanted.go) searches
// the configured indexers and the RSS feeds for each title. The "search in
// feeds" step probes the indexers and the configured RSS feeds for candidate
// series, filtered by the language/quality typed so far.
//
// A wanted title does not generate a feed or a rule: it is the engine that
// queries the indexers and the feeds for it.

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/rss"
)

// handleUIWanted adds, edits or removes one wanted title.
func (d *Daemon) handleUIWanted(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	list := d.wantedList()
	switch strings.TrimSpace(r.FormValue("op")) {
	case "remove":
		index, err := parseFormIndex(r.FormValue("index"))
		if err != nil || index < 0 || index >= len(list) {
			d.renderRSS(w, r, "The wanted title to remove does not exist anymore.")
			return
		}
		list = append(list[:index], list[index+1:]...)
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
			d.renderRSS(w, r, "A wanted title needs a name.")
			return
		}
		if _, err := rss.ParseEpisodeFilter(entry.episodeFilter()); err != nil {
			d.renderRSS(w, r, err.Error())
			return
		}
		if index, err := parseFormIndex(r.FormValue("index")); err == nil && index >= 0 {
			if index >= len(list) {
				d.renderRSS(w, r, "The wanted title to edit does not exist anymore.")
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
		d.renderRSS(w, r, "Cannot save the wanted list: "+err.Error())
		return
	}
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/rss", http.StatusSeeOther)
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
	IndexerError string   `json:"indexer_error,omitempty"`
}

// handleUIWantedSearch searches the indexers and the configured RSS feeds for
// candidate series, applying the language/quality filters typed so far.
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

	result := wantedSearchResult{Names: []string{}}
	var items []rss.Item
	result.Indexers = len(d.indexers())
	if result.Indexers > 0 {
		found, err := d.searchFeedSuggest(ctx, query)
		if err != nil {
			result.IndexerError = err.Error()
		}
		result.IndexerHits = len(found)
		items = append(items, found...)
	}
	feedItems := d.wantedRSSItems(ctx)
	result.FeedHits = len(feedItems)
	items = append(items, feedItems...)
	result.Names = filterSuggestions(query, items, langFilters, qualityFilter, 20)
	writeJSON(w, http.StatusOK, result)
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

// uiWantedRowsHTML renders the wanted titles as table rows, each with Edit and
// Remove, and the missing episodes found so far.
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
		state := "active"
		if w.Paused {
			state = "paused"
		}
		b.WriteString("<tr><td>" + html.EscapeString(w.Name) + "</td>")
		b.WriteString("<td>" + html.EscapeString(w.kind()) + "</td>")
		b.WriteString("<td>" + html.EscapeString(orDefault(w.Seasons, "—")) + "</td>")
		b.WriteString("<td>" + html.EscapeString(strings.Join(w.includes(), ", ")) + "</td>")
		b.WriteString("<td>" + html.EscapeString(missingLabel) + "</td>")
		b.WriteString("<td>" + state + "</td>")
		b.WriteString(`<td class="actions">`)
		attrs := `data-wanted-edit data-index="` + strconv.Itoa(index) + `"` +
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
		b.WriteString(`<button type="button" class="sm" ` + attrs + `>Edit</button> `)
		b.WriteString(`<form method="post" action="/ui/wanted"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + strconv.Itoa(index) + `"><button type="submit" class="danger">Remove</button></form>`)
		b.WriteString("</td></tr>")
	}
	return b.String()
}
