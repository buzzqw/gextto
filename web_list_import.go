package gextto

import (
	"net/http"
	"strings"
)

type listImportPreviewResponse struct {
	Items             []ListImportItem `json:"items"`
	Profiles          []QualityProfile `json:"profiles"`
	SeriesRoot        string           `json:"series_root"`
	GlobalArchiveRoot string           `json:"global_archive_root"`
	DryRun            bool             `json:"dry_run"`
}

type listImportApplyRequest struct {
	Source  string            `json:"source"`
	Items   []ListImportItem  `json:"items"`
	Options ListImportOptions `json:"options"`
}

func simklListImportItems(r *http.Request, cfg *Config) ([]ListImportItem, error) {
	client := (&SimklClient{}).FromSettings(cfg.Settings)
	if !client.Configured() || !client.Authenticated() {
		return nil, errSimklNotConfigured
	}
	value, err := client.Watchlist(r.Context())
	if err != nil {
		return nil, err
	}
	return collectListImportItems("simkl", value), nil
}

var errSimklNotConfigured = &listImportError{"Simkl non configurato"}

type listImportError struct{ message string }

func (e *listImportError) Error() string { return e.message }

// ListImportPreview fetches and plans a provider list without writing anything.
func ListImportPreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	if strings.TrimSpace(r.URL.Query().Get("source")) != "simkl" {
		jsonError(w, http.StatusBadRequest, "unsupported import source")
		return
	}
	cfg := latestConfig(s)
	items, err := simklListImportItems(r, cfg)
	if err != nil {
		status := http.StatusBadGateway
		if err == errSimklNotConfigured {
			status = http.StatusConflict
		}
		jsonError(w, status, err.Error())
		return
	}
	root := ""
	if cfg.ArchiveRoot != nil {
		root = strings.TrimSpace(*cfg.ArchiveRoot)
	}
	jsonResponse(w, listImportPreviewResponse{
		Items: planListImport(cfg, items), Profiles: qualityProfilesFromConfig(cfg),
		SeriesRoot: root, GlobalArchiveRoot: root, DryRun: s.dryRun(),
	})
}

// ListImportApply revalidates and transactionally applies selected rows.
func ListImportApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input listImportApplyRequest
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Source != "simkl" {
		jsonError(w, http.StatusBadRequest, "unsupported import source")
		return
	}
	if s.dryRun() && !input.Options.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not modify the library; use dry_run=true for a simulation")
		return
	}
	for i := range input.Items {
		input.Items[i].Source = "simkl"
		input.Items[i].Status, input.Items[i].MatchedName, input.Items[i].Reason = "", "", ""
	}
	report, err := ApplyListImport(latestConfig(s), input.Items, input.Options)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, report)
}
