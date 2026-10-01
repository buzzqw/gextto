package gextto

// uiweb_v2_detail.go implements the v2 series and movie detail pages. They reuse
// the existing Go view-models (uiSeriesDetailFrom / uiMovieDetailFrom) and only
// add v2 templates plus the two write paths the classic client performed by
// hand: saving a series (read-modify-write of the library array) and showing the
// per-episode sources.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
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
	redirect := "/v2?view=series"
	if name != "" {
		redirect = "/v2?view=series&series=" + url.QueryEscape(name)
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
	Score     int64
	SizeBytes int64
	Magnet    string
}

type v2SourcesView struct {
	Label   string
	Results []v2SourceRow
}

// V2SeriesSources renders the per-episode sources modal (read-only).
func V2SeriesSources(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := r.FormValue("series")
	season := r.FormValue("season")
	episode := r.FormValue("episode")
	view := v2SourcesView{Label: "S" + season + "E" + episode}
	if series != "" && season != "" && episode != "" {
		path := "/api/episodes/" + url.PathEscape(series) + "/" + url.PathEscape(season) + "/" + url.PathEscape(episode) + "/sources"
		if raw, status := v2InternalJSON(s, http.MethodGet, path, nil, nil); status < 400 {
			var payload struct {
				Results []map[string]any `json:"results"`
			}
			if json.Unmarshal(raw, &payload) == nil {
				for _, entry := range payload.Results {
					view.Results = append(view.Results, v2SourceRow{
						Title:     v2String(entry["title"]),
						Source:    v2SourceLabel(v2String(entry["source"])),
						Score:     int64(v2Float(entry["score"])),
						SizeBytes: int64(v2Float(entry["size_bytes"])),
						Magnet:    v2SafeHref(v2String(entry["magnet"])),
					})
				}
			}
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_sources_modal", view, dict, eng)
}
