package gextto

import (
	"encoding/json"
	"net/http"
	"strings"
)

// v2DashboardFeedRow is the flattened form used by the classic dashboard:
// one row per release, with a direct queue action.
type v2DashboardFeedRow struct {
	Name   string
	Kind   string
	Title  string
	Source string
	Magnet string
}

type v2DashboardFeedView struct {
	Rows  []v2DashboardFeedRow
	Error string
}

func V2DashboardSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2SearchViewFrom(s, strings.TrimSpace(r.FormValue("q")), "/v2?view=dashboard")
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_dashboard_search_results", view, dict, eng)
}

func V2DashboardFeed(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2DashboardFeedView{}
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/feed/status", nil, nil)
	if status >= 400 {
		view.Error = v2JSONError(raw)
	} else {
		var payload struct {
			Items []struct {
				Name    string `json:"name"`
				Kind    string `json:"kind"`
				Matches []struct {
					Title  string `json:"title"`
					Source string `json:"source"`
					Magnet string `json:"magnet"`
				} `json:"matches"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			view.Error = "risposta feed non valida"
		} else {
			for _, item := range payload.Items {
				for _, match := range item.Matches {
					view.Rows = append(view.Rows, v2DashboardFeedRow{
						Name: item.Name, Kind: item.Kind, Title: match.Title,
						Source: match.Source, Magnet: match.Magnet,
					})
					if len(view.Rows) >= 40 {
						break
					}
				}
				if len(view.Rows) >= 40 {
					break
				}
			}
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_dashboard_feed", view, dict, eng)
}

func V2DashboardFeedAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	body, _ := json.Marshal(ArchiveAddInput{
		Title:  strings.TrimSpace(r.FormValue("title")),
		Magnet: strings.TrimSpace(r.FormValue("magnet")),
		Source: strings.TrimSpace(r.FormValue("source")),
	})
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/archive/add", nil, body)
	result := map[string]any{"Error": status >= 400}
	if status >= 400 {
		result["Message"] = v2JSONError(raw)
	} else {
		result["Message"] = "Accodato"
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_dashboard_feed_result", result, dict, eng)
}
