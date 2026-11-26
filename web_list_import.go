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

func listImportProviderSource(value string) (string, bool) {
	source := strings.ToLower(strings.TrimSpace(value))
	if source == "simkl" || source == "mdblist" {
		return source, true
	}
	return "", false
}

func providerListImportItems(r *http.Request, cfg *Config, source string) ([]ListImportItem, error) {
	if source == "mdblist" {
		client := (&MDbListClient{}).FromSettings(cfg.Settings)
		if !client.Configured() {
			return nil, errMDbListNotConfigured
		}
		value, err := client.Items(r.Context())
		if err != nil {
			return nil, err
		}
		return collectListImportItems(source, normalizeMDbListItems(value)), nil
	}
	client := (&SimklClient{}).FromSettings(cfg.Settings)
	if !client.Configured() || !client.Authenticated() {
		return nil, errSimklNotConfigured
	}
	value, err := client.Watchlist(r.Context())
	if err != nil {
		return nil, err
	}
	return collectListImportItems(source, value), nil
}

func simklListImportItems(r *http.Request, cfg *Config) ([]ListImportItem, error) {
	return providerListImportItems(r, cfg, "simkl")
}

var errSimklNotConfigured = &listImportError{"Simkl non configurato"}
var errMDbListNotConfigured = &listImportError{"MDbList non configurata"}

type listImportError struct{ message string }

func (e *listImportError) Error() string { return e.message }

// ListImportPreview fetches and plans a provider list without writing anything.
func ListImportPreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	source, ok := listImportProviderSource(r.URL.Query().Get("source"))
	if !ok {
		jsonError(w, http.StatusBadRequest, "unsupported import source")
		return
	}
	cfg := latestConfig(s)
	items, err := providerListImportItems(r, cfg, source)
	if err != nil {
		status := http.StatusBadGateway
		if err == errSimklNotConfigured || err == errMDbListNotConfigured {
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
	source, ok := listImportProviderSource(input.Source)
	if !ok {
		jsonError(w, http.StatusBadRequest, "unsupported import source")
		return
	}
	if s.dryRun() && !input.Options.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not modify the library; use dry_run=true for a simulation")
		return
	}
	for i := range input.Items {
		input.Items[i].Source = source
		input.Items[i].Status, input.Items[i].MatchedName, input.Items[i].Reason = "", "", ""
	}
	report, err := ApplyListImport(latestConfig(s), input.Items, input.Options)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, report)
}
