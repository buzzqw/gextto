package gextto

// uiweb_v2_import.go is the manual-import page: it previews the video files of
// a path, matches them to a configured series or movie and performs the move on
// confirmation. The same operation is exposed as JSON under /api/import for
// scripts.

import (
	"encoding/json"
	"net/http"
	"strings"
)

// v2ImportView is the page model of /?view=import.
type v2ImportView struct {
	Path     string
	Message  string
	Error    string
	Series   []string
	Movies   []v2ImportMovie
	Ready    bool
	Result   []ManualImportCandidate
	ResultAt string
}

type v2ImportMovie struct {
	Name string
	Year string
}

func v2ImportViewFrom(s *AppState) v2ImportView {
	view := v2ImportView{Ready: true}
	cfg := latestConfig(s)
	if cfg == nil {
		view.Error = "configuration unavailable"
		return view
	}
	for _, series := range cfg.Series {
		view.Series = append(view.Series, series.Name)
	}
	for _, movie := range cfg.Movies {
		view.Movies = append(view.Movies, v2ImportMovie{Name: movie.Name, Year: movie.Year})
	}
	return view
}

// V2ImportPreview renders the candidate list for the page (GET /import/preview).
func V2ImportPreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2ImportViewFrom(s)
	view.Path = strings.TrimSpace(r.FormValue("path"))
	if view.Path == "" {
		view.Error = "choose a file or a folder"
	} else {
		candidates, err := ManualImportPreview(latestConfig(s), view.Path)
		if err != nil {
			view.Error = err.Error()
		} else {
			view.Result = candidates
			view.ResultAt = view.Path
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_import_candidates", view, dict, eng)
}

// V2ImportApply performs one confirmed candidate (POST /import/apply).
func V2ImportApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	ctx, clean := s.BackgroundContext()
	defer clean()
	view := v2ImportViewFrom(s)
	req := ManualImportRequest{
		Path:    strings.TrimSpace(r.FormValue("path")),
		Kind:    strings.TrimSpace(r.FormValue("kind")),
		Series:  strings.TrimSpace(r.FormValue("series")),
		Movie:   strings.TrimSpace(r.FormValue("movie")),
		Year:    v2ParseInt(r.FormValue("year"), 0),
		Season:  v2ParseInt(r.FormValue("season"), 0),
		Episode: v2ParseInt(r.FormValue("episode"), 0),
		Replace: r.FormValue("replace") != "",
	}
	final, err := ManualImport(ctx, latestConfig(s), s.db, req)
	if err != nil {
		view.Error = err.Error()
	} else {
		view.Message = final
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_import_done", view, dict, eng)
}

// ManualImportPreviewAPI is GET /api/import/preview?path=…
func ManualImportPreviewAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	candidates, err := ManualImportPreview(latestConfig(s), path)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": path, "candidates": candidates})
}

// ManualImportAPI is POST /api/import with the fields of ManualImportRequest.
func ManualImportAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	var req ManualImportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx, clean := s.BackgroundContext()
	defer clean()
	final, err := ManualImport(ctx, latestConfig(s), s.db, req)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": final})
}
