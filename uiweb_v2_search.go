package gextto

// uiweb_v2_search.go implements the release-search part of the v2 "Esplora"
// page. The TMDB discover/trending panels keep a link to the classic UI for
// now; the manual release search (the main use of the page) is fully
// server-rendered and the "Aggiungi" action reuses POST /api/search/add.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
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
	Links    string
}

var ed2kLinkPattern = regexp.MustCompile(`(?i)ed2k://\|file\|[^|\r\n]*\|\d+\|[0-9a-f]{32}\|/`)

// extractEd2kLinks finds every ed2k file link in pasted text, accepting one
// per line, several per line, or text around them. Identities are deduplicated
// by MD4, so pasting the same link twice adds it once.
func extractEd2kLinks(raw string) []string {
	matches := ed2kLinkPattern.FindAllString(raw, -1)
	out := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		match = strings.TrimSpace(match)
		file, err := ed2k.ParseFileLink(match)
		if err != nil {
			continue
		}
		hash := strings.ToLower(file.Hash)
		if seen[hash] {
			continue
		}
		seen[hash] = true
		out = append(out, file.String())
	}
	return out
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
	switch r.FormValue("view") {
	case "downloads":
		// The same form is embedded in Scarico: keep the user there after
		// adding a result instead of jumping to Esplora.
		redirect = "/?view=downloads"
	case "dashboard":
		redirect = "/?view=dashboard"
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
	redirect := "/?view=search"
	if r.FormValue("view") == "downloads" {
		redirect = "/?view=downloads"
	}
	view := v2SearchView{Redirect: redirect, Searched: true}
	links := extractEd2kLinks(raw)
	if len(links) == 0 {
		view.Error = "Nessun link eD2k valido trovato nel testo incollato."
		dict, eng := v2Dictionaries(s)
		v2Render(w, http.StatusOK, "v2_search_results", view, dict, eng)
		return
	}
	cfg := latestConfig(s)
	_, download := ed2kAvailability(cfg)
	now := time.Now().UTC()
	valid := make([]string, 0, len(links))
	for _, link := range links {
		file, err := ed2k.ParseFileLink(link)
		if err != nil {
			continue
		}
		release := ParseEd2kRelease(file.Name, link, "ed2k:link", file.Size, 0, now)
		if release == nil {
			continue
		}
		release.Score = cfg.ReleaseScore(release)
		encoded, _ := json.Marshal(release)
		view.Results = append(view.Results, v2SearchResult{
			Title: release.Title, Source: "eD2k/Kad", Network: "ed2k", Sources: 0,
			Ed2kLink: release.Ed2kLink, Addable: download, Score: release.Score,
			SizeBytes: release.SizeBytes, ReleaseJSON: string(encoded), ExplainJSON: string(encoded),
		})
		valid = append(valid, link)
	}
	view.Links = strings.Join(valid, "\n")
	view.Query = fmt.Sprintf("%d link eD2k", len(view.Results))
	if download {
		view.Notice = fmt.Sprintf("%d link eD2k validi: puoi aggiungerli singolarmente o tutti insieme.", len(view.Results))
	} else {
		view.Notice = fmt.Sprintf("%d link eD2k validi, ma il download eD2k non è disponibile in questa versione.", len(view.Results))
	}
	if len(view.Results) == 0 {
		view.Error = "Nessun link eD2k valido trovato nel testo incollato."
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_search_results", view, dict, eng)
}

// V2SearchAddAll queues every ed2k link in the pasted block, reusing the same
// /api/search/add path as the per-row button.
func V2SearchAddAll(w http.ResponseWriter, r *http.Request, s *AppState) {
	for _, link := range extractEd2kLinks(r.FormValue("ed2k_links")) {
		file, err := ed2k.ParseFileLink(link)
		if err != nil {
			continue
		}
		release := ParseEd2kRelease(file.Name, link, "ed2k:link", file.Size, 0, time.Now().UTC())
		if release == nil || release.Ed2kLink == "" {
			continue
		}
		encoded, _ := json.Marshal(release)
		body, _ := json.Marshal(map[string]json.RawMessage{"release": encoded})
		v2InternalJSON(s, http.MethodPost, "/api/search/add", nil, body)
	}
	redirect := v2SearchRedirectTarget(r.FormValue("redirect"), "/?view=search")
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Redirect", redirect)
	w.WriteHeader(http.StatusNoContent)
}

// v2SearchRedirectTarget keeps the post-add navigation inside a known view:
// the value comes from a hidden form field, so it must not be an open redirect.
func v2SearchRedirectTarget(value, fallback string) string {
	switch value {
	case "/?view=search", "/?view=dashboard", "/?view=downloads":
		return value
	}
	return fallback
}

// V2SearchAdd adds a release to the acquisition queue.
func V2SearchAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	release := strings.TrimSpace(r.FormValue("release"))
	if release != "" && json.Valid([]byte(release)) {
		body, _ := json.Marshal(map[string]json.RawMessage{"release": json.RawMessage(release)})
		v2InternalJSON(s, http.MethodPost, "/api/search/add", nil, body)
	}
	redirect := v2SearchRedirectTarget(r.FormValue("redirect"), "/?view=search")
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
