package gextto

// uiweb_v2_library_layout.go gives the monitored **Series** and **Movies** lists
// a second way to be read: one poster per title instead of a table row, with the
// essential data on the card and everything else one click away — the poster
// view Sonarr and Radarr offer.
//
// The layout is chosen with a query parameter (?layout=cards) and remembered in
// a cookie, so it survives the navigation between views: the links of the shell
// do not carry the parameter, the cookie does. No JavaScript is involved, and
// with no cookie the list stays the default.
//
// The cards are built from the same JSON items as the table (so filter and sort
// stay identical) and their data reuses the table's own cell formatters: a card
// shows what the row shows, no second set of labels to translate.

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// v2LayoutCookie remembers the chosen layout between pages.
	v2LayoutCookie = "gx_layout"
	// v2LayoutCookieAge is how long the choice lasts.
	v2LayoutCookieAge = 365 * 24 * 3600
	// v2PosterCacheTTL keeps a resolved poster (or the fact that there is none):
	// posters change rarely, the lookup is the slow part. The cache is the
	// in-memory response cache, so after a restart it is paid once again.
	v2PosterCacheTTL = 12 * time.Hour
	// v2PosterWorkers bounds the metadata lookups running at once on a page.
	v2PosterWorkers = 6
	// v2PosterMax bounds how many posters one page resolves.
	v2PosterMax = 60
	// v2PosterTimeout is the deadline of one lookup: a page must not wait on a
	// metadata service.
	v2PosterTimeout = 5 * time.Second
)

// v2CardItem is one title shown as a poster card. Meta holds the title's cells
// already formatted by the table (badges, quality, progress…): the card shows
// the same words as the list.
type v2CardItem struct {
	Name   string
	Link   string
	Poster string
	// Meta holds the title's cells, in the order of v2CardMetaKeys: each is
	// formatted by the table, so no second set of labels has to be translated.
	Meta []v2CardMeta
}

// v2CardMeta is one field beside the title on a poster card: the value, already
// formatted by the table (a badge, quality, progress…), and the label of the
// column it comes from. The label is only a tooltip, so a compact "ita" on the
// card still reads as "Lingua" on hover and to assistive technology, and the
// card keeps saying what the list says.
type v2CardMeta struct {
	Value template.HTML
	Label string
}

// v2Layout answers which layout to render: the parameter wins, then the
// remembered cookie, then the list (the historical view).
func v2Layout(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("layout"))) {
	case "cards":
		return "cards"
	case "list":
		return "list"
	}
	if cookie, err := r.Cookie(v2LayoutCookie); err == nil && strings.EqualFold(strings.TrimSpace(cookie.Value), "cards") {
		return "cards"
	}
	return "list"
}

// v2RememberLayout stores the layout chosen with the parameter, so the pages
// reached later (whose links do not carry it) keep it.
func v2RememberLayout(w http.ResponseWriter, r *http.Request) {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("layout")))
	if value != "cards" && value != "list" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: v2LayoutCookie, Value: value, Path: "/",
		MaxAge: v2LayoutCookieAge, SameSite: http.SameSiteLaxMode,
		HttpOnly: true, Secure: requestIsHTTPS(r),
	})
}

// v2ViewHasLayouts reports whether a view is one of the lists that can be read
// as posters.
func v2ViewHasLayouts(base string) bool { return base == "series" || base == "movies" }

// v2LayoutURL is the link that switches a view to one layout, keeping the
// rest of the query (the filters) intact.
func v2LayoutURL(r *http.Request, view, layout string) string {
	values := r.URL.Query()
	values.Set("view", view)
	values.Set("layout", layout)
	return "/?" + values.Encode()
}

// v2CardMetaField selects one column for a poster card: by key when the column
// maps to a single item field, by format for the computed ones (progress,
// completion, status) whose column carries no key.
type v2CardMetaField struct {
	Key    string
	Format string
}

// v2CardMetaFields are the columns a card shows beside the title, in order.
func v2CardMetaFields(base string) []v2CardMetaField {
	if base == "movies" {
		return []v2CardMetaField{
			{Key: "year"}, {Key: "quality"}, {Key: "language"}, {Key: "enabled"},
		}
	}
	return []v2CardMetaField{
		{Key: "seasons"}, {Key: "quality"}, {Key: "language"},
		{Format: "episodes"}, {Format: "completion"}, {Format: "series_status"},
	}
}

// v2LibraryCardsFrom builds the cards of one library list out of the same items
// and columns the table used, in the same order (so the filter and the sort
// apply to both). A movie uses the poster_path the metadata refresh stored,
// falling back to a lookup when it has none; a series is resolved through the
// metadata provider. Both lookups are cached and run a few at a time.
func v2LibraryCardsFrom(ctx context.Context, s *AppState, base string, items []map[string]any, columns []uiColumn) []v2CardItem {
	cfg := latestConfig(s)
	posters := map[string]string{}
	if base == "series" {
		posters = v2SeriesPosters(ctx, cfg, seriesNames(items))
	} else {
		posters = v2MoviePosters(ctx, cfg, items)
	}
	cards := make([]v2CardItem, 0, len(items))
	for _, item := range items {
		name := v2String(item["name"])
		card := v2CardItem{Name: name}
		switch base {
		case "movies":
			card.Link = "/?view=movies&movie=" + url.QueryEscape(v2String(item["id"]))
			card.Poster = metadataImageURL(v2String(item["poster_path"]), "w342")
			if card.Poster == "" {
				card.Poster = posters[strings.ToLower(v2String(item["id"]))]
			}
		default:
			card.Link = "/?view=series&series=" + url.QueryEscape(name)
			card.Poster = posters[strings.ToLower(name)]
		}
		for _, field := range v2CardMetaFields(base) {
			if column, ok := v2CardColumn(columns, field); ok {
				if html := v2FormatCell(item, column); strings.TrimSpace(string(html)) != "" && string(html) != "—" {
					card.Meta = append(card.Meta, v2CardMeta{Value: html, Label: column.Label})
				}
			}
		}
		cards = append(cards, card)
	}
	return cards
}

// v2CardColumn finds the column a card field names: by key when the field has
// one, otherwise by format, so the computed columns (progress, completion,
// status) — which carry no key — still reach the card.
func v2CardColumn(columns []uiColumn, field v2CardMetaField) (uiColumn, bool) {
	for _, column := range columns {
		if field.Key != "" && column.Key == field.Key {
			return column, true
		}
		if field.Format != "" && column.Format == field.Format {
			return column, true
		}
	}
	return uiColumn{}, false
}

// seriesNames are the names of the library items, in order, without repeats.
func seriesNames(items []map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	for _, item := range items {
		name := strings.TrimSpace(v2String(item["name"]))
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// v2MoviePosters resolves the posters the movie metadata does not hold yet (the
// refresh fills them over time), keyed by movie id: the same cache, the same
// pacing and the same "learn that there is none" as the series.
func v2MoviePosters(ctx context.Context, cfg *Config, items []map[string]any) map[string]string {
	out := map[string]string{}
	if cfg == nil || cfg.TmdbAPIKey == nil || strings.TrimSpace(*cfg.TmdbAPIKey) == "" {
		return out
	}
	// A movie may have no TMDB id yet (the library is often added by name): in
	// that case the poster is looked up by name and year.
	type wanted struct {
		id, tmdb, name, year string
	}
	var missing []wanted
	for _, item := range items {
		if strings.TrimSpace(v2String(item["poster_path"])) != "" {
			continue
		}
		id := v2String(item["id"])
		if id == "" {
			continue
		}
		key := "movie_poster:" + id
		if cached, ok := CacheGet(key, v2PosterCacheTTL); ok {
			if url := v2AnyString(cached["url"]); url != "" {
				out[strings.ToLower(id)] = url
			}
			continue
		}
		missing = append(missing, wanted{
			id: id, tmdb: strings.TrimSpace(v2String(item["tmdb_id"])),
			name: v2String(item["name"]), year: strings.TrimSpace(v2String(item["year"])),
		})
		if len(missing) >= v2PosterMax {
			break
		}
	}
	if len(missing) == 0 {
		return out
	}
	client := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan wanted)
	for i := 0; i < v2PosterWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				lookupCtx, cancel := context.WithTimeout(ctx, v2PosterTimeout)
				url := ""
				if job.tmdb != "" {
					if details, err := client.MovieDetails(lookupCtx, job.tmdb); err == nil && details != nil && details.PosterPath != nil {
						url = metadataImageURL(*details.PosterPath, "w342")
					}
				} else if job.name != "" {
					var year *int64
					if parsed, err := strconv.ParseInt(job.year, 10, 64); err == nil && parsed > 0 {
						year = &parsed
					}
					if found, err := client.SearchMovie(lookupCtx, job.name, year); err == nil && found != nil && found.PosterPath != nil {
						url = metadataImageURL(*found.PosterPath, "w342")
					}
				}
				cancel()
				CachePut("movie_poster:"+job.id, map[string]any{"url": url})
				if url == "" {
					continue
				}
				mu.Lock()
				out[strings.ToLower(job.id)] = url
				mu.Unlock()
			}
		}()
	}
	for _, job := range missing {
		if ctx.Err() != nil {
			break
		}
		jobs <- job
	}
	close(jobs)
	wg.Wait()
	return out
}

// v2SeriesPosters resolves the posters of a list of series, from the shared
// cache when possible. A cold cache costs one metadata lookup per series, run
// few at a time (the lookups are network calls), and what is learned — including
// "no poster" — is kept for v2PosterCacheTTL, so the next page is free.
func v2SeriesPosters(ctx context.Context, cfg *Config, names []string) map[string]string {
	out := make(map[string]string, len(names))
	if len(names) == 0 {
		return out
	}
	if !metadataConfigured(cfg) {
		return out
	}
	if len(names) > v2PosterMax {
		names = names[:v2PosterMax]
	}
	missing := make([]string, 0, len(names))
	for _, name := range names {
		key := "series_poster:" + strings.ToLower(name)
		if cached, ok := CacheGet(key, v2PosterCacheTTL); ok {
			if url := v2AnyString(cached["url"]); url != "" {
				out[strings.ToLower(name)] = url
			}
			continue
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 {
		return out
	}
	meta := seriesMetadataFor(cfg)
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan string)
	for i := 0; i < v2PosterWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				lookupCtx, cancel := context.WithTimeout(ctx, v2PosterTimeout)
				path, err := meta.PosterForSeries(lookupCtx, name)
				cancel()
				url := ""
				if err == nil && path != nil {
					url = metadataImageURL(*path, "w342")
				}
				CachePut("series_poster:"+strings.ToLower(name), map[string]any{"url": url})
				if url == "" {
					continue
				}
				mu.Lock()
				out[strings.ToLower(name)] = url
				mu.Unlock()
			}
		}()
	}
	for _, name := range missing {
		if ctx.Err() != nil {
			break
		}
		jobs <- name
	}
	close(jobs)
	wg.Wait()
	return out
}
