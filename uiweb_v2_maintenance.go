package gextto

// uiweb_v2_maintenance.go adds two maintenance widgets to v2: the trash panel
// (list + delete single/all) and the source check (on demand), both rendered on
// the server and forwarded to the existing APIs.

import (
	"encoding/json"
	"net/http"
	"strings"
)

type v2TrashItem struct {
	Name      string
	Path      string
	SizeBytes int64
	IsDir     bool
}

type v2TrashView struct {
	Items []v2TrashItem
	Total int64
}

func v2TrashViewFrom(s *AppState) v2TrashView {
	view := v2TrashView{}
	if raw, status := v2InternalJSON(s, http.MethodGet, "/api/trash", nil, nil); status < 400 {
		var payload struct {
			TotalBytes int64 `json:"total_bytes"`
			Items      []struct {
				Name      string `json:"name"`
				Path      string `json:"path"`
				SizeBytes int64  `json:"size_bytes"`
				IsDir     bool   `json:"is_dir"`
			} `json:"items"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			view.Total = payload.TotalBytes
			for _, item := range payload.Items {
				view.Items = append(view.Items, v2TrashItem{Name: item.Name, Path: item.Path, SizeBytes: item.SizeBytes, IsDir: item.IsDir})
			}
		}
	}
	return view
}

// V2TrashList renders the trash entries on demand.
func V2TrashList(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_trash_list", v2TrashViewFrom(s), dict, eng)
}

// V2TrashDelete deletes one entry or empties the trash.
func V2TrashDelete(w http.ResponseWriter, r *http.Request, s *AppState) {
	var body []byte
	if r.FormValue("all") != "" {
		body = []byte(`{"all":true}`)
	} else if name := strings.TrimSpace(r.FormValue("name")); name != "" {
		body, _ = json.Marshal(map[string][]string{"names": {name}})
	}
	if len(body) > 0 {
		v2InternalJSON(s, http.MethodPost, "/api/trash/delete", nil, body)
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=maintenance", http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Redirect", "/?view=maintenance")
	w.WriteHeader(http.StatusNoContent)
}

// V2SourcesCheck runs the source health check on demand and returns the table.
func V2SourcesCheck(w http.ResponseWriter, r *http.Request, s *AppState) {
	spec, ok := v2SpecFor(s, "health-sources")
	if !ok {
		http.Error(w, "spec non trovata", http.StatusInternalServerError)
		return
	}
	request := r.Clone(r.Context())
	query := request.URL.Query()
	query.Set("load", "1")
	request.URL.RawQuery = query.Encode()
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_table_body", v2TableDataFrom(s, request, "health-sources", spec), dict, eng)
}
