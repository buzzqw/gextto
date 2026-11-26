package gextto

// config_transfer.go exports and imports the operator's configuration as a
// versioned, human-readable document. Secrets are left out unless the operator
// explicitly asks for them, and an import applies only the sections present in
// the file: saving a setup on another machine must not erase the rest.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/utils"
)

const (
	configExportFormat  = "gextto-config"
	configExportVersion = 1
	configImportMaxBody = 8 << 20
)

// configTransferSecretKeys are never written to an export by default, and
// never read back from one. They are listed by name in the document so the
// operator can see what they must configure again.
var configTransferSecretKeys = map[string]bool{
	"auth_password": true, "auth_api_key": true, "qbittorrent_password": true,
	"backup_ftp_password": true, "tmdb_api_key": true, "tvdb_api_key": true,
	"telegram_bot_token": true, "notify_webhook_secret": true, "notify_webhook_token": true,
	"notify_discord_url": true, "notify_slack_url": true, "notify_gotify_token": true,
	"notify_ntfy_token": true, "notify_pushover_token": true, "notify_apprise_key": true,
	"email_password": true, "jellyfin_api_key": true, "plex_token": true,
	"simkl_client_id": true, "simkl_access_token": true, "gxtorrent_proxy": true,
	"flaresolverr_url": true, subtitleSettingAPIKey: true, subtitleSettingPassword: true,
}

// configTransferSections are stored as JSON strings in the settings table but
// belong to their own section in the document, where they stay readable.
var configTransferSections = map[string]bool{
	"quality_profiles": true, "torrent_seed_rules": true, "source_filters": true,
	"watched_folders": true, "event_hooks": true, "tag_dir_rules": true,
}

type configExport struct {
	Format        string                     `json:"format"`
	SchemaVersion int                        `json:"schema_version"`
	CreatedAt     string                     `json:"created_at"`
	AppVersion    string                     `json:"app_version"`
	Secrets       string                     `json:"secrets"`
	ExcludedKeys  []string                   `json:"excluded_keys,omitempty"`
	Sections      map[string]json.RawMessage `json:"sections"`
}

func buildConfigExport(cfg *Config, includeSecrets bool) configExport {
	settings := map[string]string{}
	excluded := []string{}
	for key, value := range cfg.Settings {
		if configTransferSections[key] {
			continue
		}
		if !includeSecrets && configTransferSecretKeys[key] {
			excluded = append(excluded, key)
			continue
		}
		settings[key] = value
	}
	sort.Strings(excluded)
	sections := map[string]json.RawMessage{}
	if raw, err := json.MarshalIndent(settings, "", "  "); err == nil {
		sections["settings"] = raw
	}
	for key := range configTransferSections {
		if value, ok := cfg.Settings[key]; ok && strings.TrimSpace(value) != "" {
			sections[key] = json.RawMessage(value)
		}
	}
	if raw, err := json.MarshalIndent(cfg.Series, "", "  "); err == nil {
		sections["series"] = raw
	}
	if raw, err := json.MarshalIndent(cfg.Movies, "", "  "); err == nil {
		sections["movies"] = raw
	}
	indexers := append([]IndexerConfig(nil), cfg.Indexers...)
	if !includeSecrets {
		for i := range indexers {
			indexers[i].APIKey = ""
		}
		excluded = append(excluded, "indexers.api_key")
		sort.Strings(excluded)
	}
	if raw, err := json.MarshalIndent(indexers, "", "  "); err == nil {
		sections["indexers"] = raw
	}
	secrets := "excluded"
	if includeSecrets {
		secrets = "included"
	}
	return configExport{
		Format: configExportFormat, SchemaVersion: configExportVersion,
		CreatedAt: time.Now().UTC().Format(time.RFC3339), AppVersion: constants.AppVersion(),
		Secrets: secrets, ExcludedKeys: excluded, Sections: sections,
	}
}

// configImportPlan is the validated, explicit set of writes an import will do.
type configImportPlan struct {
	settings map[string]string
	warnings []string
}

func parseConfigImport(raw []byte, cfg *Config, current []IndexerConfig) (*configImportPlan, error) {
	var document configExport
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("the file is not a Gextto configuration: %w", err)
	}
	if document.Format != configExportFormat {
		return nil, errors.New("unknown configuration format")
	}
	if document.SchemaVersion != configExportVersion {
		return nil, fmt.Errorf("unsupported configuration version %d", document.SchemaVersion)
	}
	plan := &configImportPlan{settings: map[string]string{}}
	if section, ok := document.Sections["settings"]; ok {
		var values map[string]string
		if err := json.Unmarshal(section, &values); err != nil {
			return nil, errors.New("the settings section is not a map of values")
		}
		for key, value := range values {
			if configTransferSecretKeys[key] {
				plan.warnings = append(plan.warnings, "secret "+key+" was ignored")
				continue
			}
			if !settingKeyAllowed(key) && !configTransferSections[key] {
				plan.warnings = append(plan.warnings, "unknown setting "+key+" was ignored")
				continue
			}
			if err := validateSettingValue(key, value, cfg); err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			plan.settings[key] = value
		}
	}
	for key, value := range document.Sections {
		if key == "settings" || key == "series" || key == "movies" || key == "indexers" {
			continue
		}
		if !json.Valid(value) {
			return nil, fmt.Errorf("section %s is not valid JSON", key)
		}
		if err := validateSettingValue(key, string(value), cfg); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		plan.settings[key] = string(value)
	}
	if section, ok := document.Sections["series"]; ok {
		var series []SeriesConfig
		if err := json.Unmarshal(section, &series); err != nil || len(series) > 500 {
			return nil, errors.New("the series section is invalid or too large")
		}
		for _, item := range series {
			if strings.TrimSpace(item.Name) == "" {
				return nil, errors.New("a series has no name")
			}
		}
		plan.settings["__series"] = string(section)
	}
	if section, ok := document.Sections["movies"]; ok {
		var movies []MovieConfig
		if err := json.Unmarshal(section, &movies); err != nil || len(movies) > 500 {
			return nil, errors.New("the movies section is invalid or too large")
		}
		for _, item := range movies {
			if strings.TrimSpace(item.Name) == "" {
				return nil, errors.New("a movie has no name")
			}
		}
		plan.settings["__movies"] = string(section)
	}
	if section, ok := document.Sections["indexers"]; ok {
		var indexers []IndexerConfig
		if err := json.Unmarshal(section, &indexers); err != nil {
			return nil, errors.New("the indexers section is invalid")
		}
		if document.Secrets != "included" {
			for i := range indexers {
				for _, existing := range current {
					if strings.EqualFold(existing.Name, indexers[i].Name) && strings.EqualFold(existing.URL, indexers[i].URL) && existing.APIKey != "" {
						indexers[i].APIKey = existing.APIKey
						break
					}
				}
			}
		}
		if raw, err := json.Marshal(indexers); err == nil {
			plan.settings["indexers"] = string(raw)
		}
	}
	sort.Strings(plan.warnings)
	return plan, nil
}

func applyConfigImportPlan(s *AppState, plan *configImportPlan) error {
	for key, value := range plan.settings {
		switch key {
		case "__series", "__movies":
			var series []SeriesConfig
			var movies []MovieConfig
			raw := []byte(value)
			if key == "__series" {
				if err := json.Unmarshal(raw, &series); err != nil {
					return err
				}
				movies = latestConfig(s).Movies
			} else {
				if err := json.Unmarshal(raw, &movies); err != nil {
					return err
				}
				series = latestConfig(s).Series
			}
			if err := SaveLibrary(s.cfg.DataDir, series, movies); err != nil {
				return err
			}
		default:
			if err := saveConfigSetting(s.cfg.DataDir, key, value); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	if hooks, ok := plan.settings["event_hooks"]; ok {
		var parsed []EventHook
		if json.Unmarshal([]byte(hooks), &parsed) == nil && s.notifier != nil {
			s.notifier.ReloadHooks(parsed)
		}
	}
	return nil
}

func readConfigImportBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, configImportMaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > configImportMaxBody {
		return nil, errors.New("the configuration file is too large")
	}
	return body, nil
}

// ConfigExport implements GET /api/config/export. Secret values need an
// explicit include_secrets=1 and are never sent by the UI by default.
func ConfigExport(w http.ResponseWriter, r *http.Request, s *AppState) {
	document := buildConfigExport(latestConfig(s), r.URL.Query().Get("include_secrets") == "1")
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=gextto-config-v1.json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

// ConfigImportPreview implements POST /api/config/import/preview: it validates
// everything and writes nothing.
func ConfigImportPreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	body, err := readConfigImportBody(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, utils.RedactURLSecrets(err.Error()))
		return
	}
	cfg := latestConfig(s)
	plan, err := parseConfigImport(body, cfg, cfg.Indexers)
	if err != nil {
		jsonError(w, http.StatusBadRequest, utils.RedactURLSecrets(err.Error()))
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "sections": len(plan.settings), "warnings": plan.warnings})
}

// ConfigImport implements POST /api/config/import. The same document must first
// pass the preview; confirm is an explicit second step.
func ConfigImport(w http.ResponseWriter, r *http.Request, s *AppState) {
	body, err := readConfigImportBody(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, utils.RedactURLSecrets(err.Error()))
		return
	}
	cfg := latestConfig(s)
	plan, err := parseConfigImport(body, cfg, cfg.Indexers)
	if err != nil {
		jsonError(w, http.StatusBadRequest, utils.RedactURLSecrets(err.Error()))
		return
	}
	if r.URL.Query().Get("confirm") != "1" {
		jsonError(w, http.StatusBadRequest, "the import needs confirm=1 after a preview")
		return
	}
	if err := applyConfigImportPlan(s, plan); err != nil {
		jsonError(w, http.StatusInternalServerError, utils.RedactURLSecrets(err.Error()))
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "sections": len(plan.settings), "warnings": plan.warnings})
}
