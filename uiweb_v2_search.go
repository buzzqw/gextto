package gextto

// uiweb_v2_search.go implements the release-search part of the v2 "Esplora"
// page. The TMDB discover/trending panels keep a link to the classic UI for
// now; the manual release search (the main use of the page) is fully
// server-rendered and the "Aggiungi" action reuses POST /api/search/add.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
)

type v2SearchResult struct {
	Title       string
	Source      string
	Score       int64
	Seeders     int64
	SizeBytes   int64
	ReleaseJSON string
}

type v2SearchView struct {
	Query    string
	Results  []v2SearchResult
	Searched bool
}

// V2Search runs the manual release search server-side.
func V2Search(w http.ResponseWriter, r *http.Request, s *AppState) {
	query := strings.TrimSpace(r.FormValue("q"))
	view := v2SearchView{Query: query}
	if query != "" {
		view.Searched = true
		body, _ := json.Marshal(map[string]string{"query": query})
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/search", nil, body)
		if status < 400 {
			var payload struct {
				Results []models.Release `json:"results"`
			}
			if json.Unmarshal(raw, &payload) == nil {
				for _, release := range payload.Results {
					encoded, _ := json.Marshal(release)
					view.Results = append(view.Results, v2SearchResult{
						Title:       release.Title,
						Source:      v2SourceLabel(release.Source),
						Score:       release.Score,
						Seeders:     release.Seeders,
						SizeBytes:   release.SizeBytes,
						ReleaseJSON: string(encoded),
					})
				}
			}
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_search_results", view, dict, eng)
}

// V2SearchAdd adds a release to the acquisition queue.
func V2SearchAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	release := strings.TrimSpace(r.FormValue("release"))
	if release != "" && json.Valid([]byte(release)) {
		body, _ := json.Marshal(map[string]json.RawMessage{"release": json.RawMessage(release)})
		v2InternalJSON(s, http.MethodPost, "/api/search/add", nil, body)
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/v2?view=search", http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Redirect", "/v2?view=search")
	w.WriteHeader(http.StatusNoContent)
}
