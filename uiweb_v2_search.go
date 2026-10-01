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
	ExplainJSON string
}

type v2SearchView struct {
	Query    string
	Results  []v2SearchResult
	Searched bool
	Error    string
	Redirect string
}

type v2ExplainView struct {
	Trace DecisionTrace
	Error string
}

// V2Search runs the manual release search server-side.
func V2Search(w http.ResponseWriter, r *http.Request, s *AppState) {
	query := strings.TrimSpace(r.FormValue("q"))
	view := v2SearchViewFrom(s, query, "/v2?view=search")
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_search_results", view, dict, eng)
}

func v2SearchViewFrom(s *AppState, query, redirect string) v2SearchView {
	view := v2SearchView{Query: query, Redirect: redirect}
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
						ExplainJSON: string(encoded),
					})
				}
			}
		} else {
			view.Error = v2JSONError(raw)
		}
	}
	return view
}

// V2SearchAdd adds a release to the acquisition queue.
func V2SearchAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	release := strings.TrimSpace(r.FormValue("release"))
	if release != "" && json.Valid([]byte(release)) {
		body, _ := json.Marshal(map[string]json.RawMessage{"release": json.RawMessage(release)})
		v2InternalJSON(s, http.MethodPost, "/api/search/add", nil, body)
	}
	redirect := strings.TrimSpace(r.FormValue("redirect"))
	if !strings.HasPrefix(redirect, "/v2") {
		redirect = "/v2?view=search"
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Redirect", redirect)
	w.WriteHeader(http.StatusNoContent)
}

// V2SearchExplain renders the decision trace for an archive release without
// sending the user back to the classic UI.
func V2SearchExplain(w http.ResponseWriter, r *http.Request, s *AppState) {
	release := strings.TrimSpace(r.FormValue("release"))
	view := v2ExplainView{}
	if !json.Valid([]byte(release)) {
		view.Error = "release non valida"
	} else {
		body, _ := json.Marshal(map[string]json.RawMessage{"release": json.RawMessage(release)})
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/search/explain", nil, body)
		if status >= 400 {
			view.Error = v2JSONError(raw)
		} else {
			var payload struct {
				Trace DecisionTrace `json:"trace"`
			}
			if err := json.Unmarshal(raw, &payload); err != nil {
				view.Error = "risposta spiegazione non valida"
			} else {
				view.Trace = payload.Trace
			}
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_explain_modal", view, dict, eng)
}
