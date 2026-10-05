package gextto

// uiweb_v2_detail.go implements the v2 series and movie detail pages. They reuse
// the existing Go view-models (uiSeriesDetailFrom / uiMovieDetailFrom) and only
// add v2 templates plus the two write paths the classic client performed by
// hand: saving a series (read-modify-write of the library array) and showing the
// per-episode sources.

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/buzzqw/gextto/internal/utils"
)

// v2ContentDetail returns the detail body when the view selects one item.
func v2ContentDetail(s *AppState, r *http.Request, view string) (string, any, bool) {
	switch view {
	case "series":
		if detail, ok := uiSeriesDetailFrom(s, r); ok {
			return "v2_series_detail", detail, true
		}
	case "movies":
		if detail, ok := uiMovieDetailFrom(s, r); ok {
			return "v2_movie_detail", detail, true
		}
	}
	return "", nil, false
}

// V2SeriesSave mirrors the classic series editor: it reads the whole library and
// replaces the edited series, then saves the array exactly like /api/config/series.
func V2SeriesSave(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := strings.TrimSpace(r.FormValue("name"))
	redirect := "/?view=series"
	if name != "" {
		redirect = "/?view=series&series=" + url.QueryEscape(name)
	}
	if name != "" {
		if raw, status := v2InternalJSON(s, http.MethodGet, "/api/config/library", nil, nil); status < 400 {
			var library struct {
				Series []map[string]any `json:"series"`
				Movies []map[string]any `json:"movies"`
			}
			if json.Unmarshal(raw, &library) == nil {
				updated := make([]map[string]any, 0, len(library.Series))
				for _, item := range library.Series {
					if v2String(item["name"]) == name {
						item["seasons"] = r.FormValue("seasons")
						item["quality"] = r.FormValue("quality")
						item["language"] = r.FormValue("language")
						item["subtitle"] = r.FormValue("subtitle")
						item["tvdb_id"] = strings.TrimSpace(r.FormValue("tvdb_id"))
						item["archive_path"] = r.FormValue("archive_path")
						item["exclude"] = r.FormValue("exclude")
						aliases := []string{}
						for _, part := range strings.Split(r.FormValue("aliases"), ",") {
							if trimmed := strings.TrimSpace(part); trimmed != "" {
								aliases = append(aliases, trimmed)
							}
						}
						item["aliases"] = aliases
					}
					updated = append(updated, item)
				}
				if body, err := json.Marshal(updated); err == nil {
					v2InternalJSON(s, http.MethodPost, "/api/config/series", nil, body)
				}
			}
		}
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Redirect", redirect)
	w.WriteHeader(http.StatusNoContent)
}

type v2SourceRow struct {
	Title     string
	Source    string
	Origin    string
	Score     int64
	SizeBytes int64
	Magnet    template.URL
	Body      string
	Key       string
}

type v2SourcesView struct {
	Label     string
	Hint      string
	OnlineURL string
	Redirect  string
	Results   []v2SourceRow
}

// v2EpisodeResultsView builds the modal view for one episode, either from the
// sources already collected (live=false) or from a fresh online search
// (live=true).
func v2EpisodeResultsView(s *AppState, series, season, episode string, live bool) v2SourcesView {
	view := v2SourcesView{Label: "S" + season + "E" + episode}
	if series == "" || season == "" || episode == "" {
		return view
	}
	view.Redirect = "/?view=series&series=" + url.QueryEscape(series)
	base := "/api/episodes/" + url.PathEscape(series) + "/" + url.PathEscape(season) + "/" + url.PathEscape(episode)
	path := base + "/sources"
	method := http.MethodGet
	var body []byte
	if live {
		path = base + "/search"
		method = http.MethodPost
		body = []byte("{}")
	}
	raw, status := v2InternalJSON(s, method, path, nil, body)
	if status >= 400 {
		return view
	}
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return view
	}
	for _, entry := range payload.Results {
		release, _ := entry["release"].(map[string]any)
		if release == nil {
			release = entry
		}
		title := v2String(release["title"])
		source := v2String(release["source"])
		link := v2String(release["magnet"])
		if link == "" {
			link = v2String(release["torrent_url"])
		}
		body, _ := json.Marshal(map[string]any{
			"items": []map[string]string{{"title": title, "magnet": link, "source": source}},
		})
		row := v2SourceRow{
			Title:     title,
			Source:    v2SourceLabel(source),
			Origin:    v2String(entry["origin"]),
			Score:     int64(v2Float(entry["score"])),
			SizeBytes: int64(v2Float(release["size_bytes"])),
			Magnet:    uiMagnetURL(link),
			Key:       v2SourceKey(link, title),
		}
		if strings.TrimSpace(link) != "" {
			row.Body = string(body)
		}
		view.Results = append(view.Results, row)
	}
	return view
}

// v2SourceKey returns a stable identity for a release so the same release
// coming from different origins (feed, archive, indexer) is listed once.
func v2SourceKey(link, title string) string {
	if value, ok := utils.MagnetHash(link); ok {
		return value
	}
	if strings.TrimSpace(link) != "" {
		return link
	}
	return "title:" + strings.ToLower(strings.TrimSpace(title))
}

// v2SourcePreferred reports whether `candidate` is a better copy of the same
// release than `current` (richer metadata wins, then the origin).
func v2SourcePreferred(candidate, current v2SourceRow) bool {
	rank := func(origin string) int {
		switch {
		case strings.EqualFold(origin, "Indexer / web"):
			return 3
		case strings.EqualFold(origin, "Archivio"):
			return 2
		case strings.EqualFold(origin, "Feed RSS"):
			return 1
		default:
			return 0
		}
	}
	if rank(candidate.Origin) != rank(current.Origin) {
		return rank(candidate.Origin) > rank(current.Origin)
	}
	return candidate.SizeBytes > current.SizeBytes
}

// v2DedupSources keeps one row per release, preserving the original order.
func v2DedupSources(rows []v2SourceRow) []v2SourceRow {
	index := map[string]int{}
	out := make([]v2SourceRow, 0, len(rows))
	for _, row := range rows {
		if at, ok := index[row.Key]; ok {
			if v2SourcePreferred(row, out[at]) {
				out[at] = row
			}
			continue
		}
		index[row.Key] = len(out)
		out = append(out, row)
	}
	return out
}

// V2SeriesSources renders the per-episode sources modal using only the local
// archive. It never triggers an online search (MirCrew / RSS feeds); use the
// search button for that.
func V2SeriesSources(w http.ResponseWriter, r *http.Request, s *AppState) {
	all := v2EpisodeResultsView(s, r.FormValue("series"), r.FormValue("season"), r.FormValue("episode"), false)
	view := v2SourcesView{Label: all.Label, Hint: "Nessuna release in archivio per questa puntata. Usa 🔍 per cercare online.", Redirect: all.Redirect}
	for _, row := range all.Results {
		if strings.EqualFold(row.Origin, "Archivio") {
			view.Results = append(view.Results, row)
		}
	}
	view.Results = v2DedupSources(view.Results)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_sources_modal", view, dict, eng)
}

// V2SeriesEpisodeSearch opens the manual episode search modal: the local
// archive results are shown immediately, then the (slow) feed/indexer results
// are loaded asynchronously into #v2-sources-online.
func V2SeriesEpisodeSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := r.FormValue("series")
	season := r.FormValue("season")
	episode := r.FormValue("episode")
	stored := v2EpisodeResultsView(s, series, season, episode, false)
	view := v2SourcesView{Label: stored.Label, Redirect: stored.Redirect}
	for _, row := range stored.Results {
		if strings.EqualFold(row.Origin, "Archivio") {
			view.Results = append(view.Results, row)
		}
	}
	view.Results = v2DedupSources(view.Results)
	view.OnlineURL = "/series/episode-search-online?series=" + url.QueryEscape(series) +
		"&season=" + url.QueryEscape(season) + "&episode=" + url.QueryEscape(episode)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_episode_search_modal", view, dict, eng)
}

// V2SeriesEpisodeSearchOnline runs the online part of the search (feeds and
// indexers) and returns just those rows for the async modal section.
func V2SeriesEpisodeSearchOnline(w http.ResponseWriter, r *http.Request, s *AppState) {
	all := v2EpisodeResultsView(s, r.FormValue("series"), r.FormValue("season"), r.FormValue("episode"), true)
	view := v2SourcesView{Label: all.Label, Hint: "Nessun risultato online per questa puntata.", Redirect: all.Redirect}
	// A release already shown in the archive section must not reappear here.
	archived := map[string]bool{}
	for _, row := range all.Results {
		if strings.EqualFold(row.Origin, "Archivio") {
			archived[row.Key] = true
		}
	}
	for _, row := range all.Results {
		if strings.EqualFold(row.Origin, "Archivio") || archived[row.Key] {
			continue
		}
		view.Results = append(view.Results, row)
	}
	view.Results = v2DedupSources(view.Results)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_sources_rows", view, dict, eng)
}
