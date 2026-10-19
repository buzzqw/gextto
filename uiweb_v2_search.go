package gextto

// uiweb_v2_search.go implements the release-search part of the v2 "Esplora"
// page. The TMDB discover/trending panels keep a link to the classic UI for
// now; the manual release search (the main use of the page) is fully
// server-rendered and the "Aggiungi" action reuses POST /api/search/add.

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/models"
)

type v2SearchResult struct {
	Title       string
	Source      string
	Network     string
	Sources     int64
	Ed2kLink    string
	Addable     bool
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
	Notice   string
	Redirect string
}

type v2ExplainView struct {
	Trace DecisionTrace
	Error string
}

// V2Search runs the manual release search server-side.
func V2Search(w http.ResponseWriter, r *http.Request, s *AppState) {
	query := strings.TrimSpace(r.FormValue("q"))
	kad := r.FormValue("kad") != ""
	redirect := "/?view=search"
	if r.FormValue("view") == "downloads" {
		// The same form is embedded in Scarico: keep the user there after
		// adding a result instead of jumping to Esplora.
		redirect = "/?view=downloads"
	}
	view := v2SearchViewFrom(s, query, redirect, kad)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_search_results", view, dict, eng)
}

func v2SearchViewFrom(s *AppState, query, redirect string, kad bool) v2SearchView {
	view := v2SearchView{Query: query, Redirect: redirect}
	if query != "" {
		view.Searched = true
		body, _ := json.Marshal(map[string]any{"query": query, "kad": kad})
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/search", nil, body)
		if status < 400 {
			var payload struct {
				Results []models.Release `json:"results"`
			}
			if json.Unmarshal(raw, &payload) == nil {
				_, ed2kDownload := ed2kAvailability(latestConfig(s))
				for _, release := range payload.Results {
					encoded, _ := json.Marshal(release)
					ed2kResult := release.Network == "ed2k" || release.Ed2kLink != ""
					view.Results = append(view.Results, v2SearchResult{
						Title:       release.Title,
						Source:      v2SearchSource(release),
						Network:     release.Network,
						Sources:     release.Sources,
						Ed2kLink:    release.Ed2kLink,
						Addable:     !ed2kResult || ed2kDownload,
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

// v2SearchSource labels a search result. eD2k results carry a network name
// instead of a host: v2SourceLabel would misparse "ed2k:kad" as a hostname.
func v2SearchSource(release models.Release) string {
	if release.Network == "ed2k" || release.Ed2kLink != "" {
		return "eD2k/Kad"
	}
	return v2SourceLabel(release.Source)
}

// V2SearchLink validates a pasted ed2k:// link and shows it as a search
// result. Until gx-ed2k can download, the row is visible but not addable: the
// link is checked, the user sees name, size and hash, and Gextto does not
// queue something it cannot finish.
func V2SearchLink(w http.ResponseWriter, r *http.Request, s *AppState) {
	raw := strings.TrimSpace(r.FormValue("ed2k_link"))
	view := v2SearchView{Redirect: "/?view=search", Searched: true}
	file, err := ed2k.ParseFileLink(raw)
	if err != nil {
		view.Error = "Link eD2k non valido: " + err.Error()
		dict, eng := v2Dictionaries(s)
		v2Render(w, http.StatusOK, "v2_search_results", view, dict, eng)
		return
	}
	release := ParseEd2kRelease(file.Name, file.String(), "ed2k:link", file.Size, 0, time.Now().UTC())
	if release == nil {
		view.Error = "Link eD2k non valido."
		dict, eng := v2Dictionaries(s)
		v2Render(w, http.StatusOK, "v2_search_results", view, dict, eng)
		return
	}
	cfg := latestConfig(s)
	release.Score = cfg.ReleaseScore(release)
	encoded, _ := json.Marshal(release)
	view.Query = file.Name
	view.Notice = "Link eD2k valido: il download non è disponibile in questa versione."
	view.Results = []v2SearchResult{{
		Title: release.Title, Source: "eD2k/Kad", Network: "ed2k", Sources: 0,
		Ed2kLink: release.Ed2kLink, Addable: false, Score: release.Score,
		SizeBytes: release.SizeBytes, ReleaseJSON: string(encoded), ExplainJSON: string(encoded),
	}}
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
	redirect := "/?view=search"
	if r.FormValue("redirect") == "/?view=dashboard" {
		redirect = "/?view=dashboard"
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
