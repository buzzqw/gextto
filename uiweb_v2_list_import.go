package gextto

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type v2ListImportView struct {
	Source            string
	Items             []ListImportItem
	Profiles          []v2ProfileChoice
	SeriesRoot        string
	GlobalArchiveRoot string
	DryRun            bool
	Scanned           bool
	Report            *ListImportReport
	Message           string
	Error             bool
}

func (v v2ListImportView) ItemsJSON() string {
	raw, _ := json.Marshal(v.Items)
	return string(raw)
}

func v2ListImportPageFrom(s *AppState) v2ListImportView {
	cfg := latestConfig(s)
	root := ""
	if cfg.ArchiveRoot != nil {
		root = strings.TrimSpace(*cfg.ArchiveRoot)
	}
	return v2ListImportView{
		Source:     "simkl",
		Profiles:   v2ProfileChoices(s, qualityProfilesFromConfig(cfg)),
		SeriesRoot: root, GlobalArchiveRoot: root, DryRun: s.dryRun(),
	}
}

func v2ListImportPreview(s *AppState, source string) v2ListImportView {
	view := v2ListImportPageFrom(s)
	if selected, ok := listImportProviderSource(source); ok {
		view.Source = selected
	}
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/list-import/preview?source="+url.QueryEscape(view.Source), nil, nil)
	if status >= 400 {
		view.Error, view.Message = true, v2JSONError(raw)
		return view
	}
	var payload listImportPreviewResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		view.Error, view.Message = true, "risposta di anteprima non valida"
		return view
	}
	view.Items, view.SeriesRoot, view.GlobalArchiveRoot, view.DryRun = payload.Items, payload.SeriesRoot, payload.GlobalArchiveRoot, payload.DryRun
	view.Scanned = true
	return view
}

func v2ListImportApply(s *AppState, r *http.Request) v2ListImportView {
	view := v2ListImportPageFrom(s)
	var scanned []ListImportItem
	if err := json.Unmarshal([]byte(r.FormValue("items")), &scanned); err != nil {
		view.Error, view.Message = true, "elenco di anteprima non valido"
		return view
	}
	selected := make([]ListImportItem, 0, len(scanned))
	for index, item := range scanned {
		if r.FormValue(fmt.Sprintf("accept__%d", index)) != "" {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		view.Error, view.Message = true, "nessun titolo selezionato"
		return view
	}
	profileID, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("quality_profile_id")))
	options := ListImportOptions{
		QualityProfileID: profileID,
		SeriesRoot:       strings.TrimSpace(r.FormValue("series_root")),
		SeasonSubfolders: r.FormValue("season_subfolders") == "true",
		DryRun:           r.FormValue("dry_run") == "true",
	}
	source, _ := listImportProviderSource(r.FormValue("source"))
	if source == "" {
		source = "simkl"
	}
	body, _ := json.Marshal(listImportApplyRequest{Source: source, Items: selected, Options: options})
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/list-import/apply", nil, body)
	if status >= 400 {
		view.Error, view.Message = true, v2JSONError(raw)
		return view
	}
	var report ListImportReport
	if err := json.Unmarshal(raw, &report); err != nil {
		view.Error, view.Message = true, "risposta di import non valida"
		return view
	}
	view.Report = &report
	view.Message = fmt.Sprintf("Serie aggiunte: %d. Film aggiunti: %d. Già presenti: %d.", report.SeriesAdded, report.MoviesAdded, report.AlreadyExists)
	view.Scanned = false
	return view
}

func V2ListImportPreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	v2Render(w, http.StatusOK, "v2_list_import_panel", v2ListImportPreview(s, r.FormValue("source")), s)
}

func V2ListImportApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	v2Render(w, http.StatusOK, "v2_list_import_panel", v2ListImportApply(s, r), s)
}
