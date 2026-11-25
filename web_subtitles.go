package gextto

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	subtitleprovider "github.com/buzzqw/gextto/internal/subtitles"
)

type subtitleSettingsInput struct {
	Enabled     any    `json:"enabled"`
	ProviderURL string `json:"provider_url"`
	APIKey      string `json:"api_key"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Forced      string `json:"forced"`
	SDH         string `json:"sdh"`
	Collision   string `json:"collision_policy"`
}

func subtitleSettingsValues(cfg *Config, input subtitleSettingsInput) (map[string]string, error) {
	values := map[string]string{
		subtitleSettingEnabled:     strconv.FormatBool(settingTruthy(fmt.Sprint(input.Enabled))),
		subtitleSettingProviderURL: strings.TrimSpace(input.ProviderURL),
		subtitleSettingUsername:    strings.TrimSpace(input.Username),
		subtitleSettingForced:      strings.ToLower(strings.TrimSpace(input.Forced)),
		subtitleSettingSDH:         strings.ToLower(strings.TrimSpace(input.SDH)),
		subtitleSettingCollision:   strings.ToLower(strings.TrimSpace(input.Collision)),
	}
	if values[subtitleSettingProviderURL] == "" {
		values[subtitleSettingProviderURL] = subtitleprovider.DefaultBaseURL
	}
	if values[subtitleSettingForced] == "" {
		values[subtitleSettingForced] = "any"
	}
	if values[subtitleSettingSDH] == "" {
		values[subtitleSettingSDH] = "any"
	}
	if values[subtitleSettingCollision] == "" {
		values[subtitleSettingCollision] = "keep"
	}
	if parsed, err := url.Parse(values[subtitleSettingProviderURL]); err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("subtitle provider URL must be http:// or https://")
	}
	if values[subtitleSettingForced] != "any" && values[subtitleSettingForced] != "yes" && values[subtitleSettingForced] != "no" {
		return nil, errors.New("forced must be any, yes or no")
	}
	if values[subtitleSettingSDH] != "any" && values[subtitleSettingSDH] != "yes" && values[subtitleSettingSDH] != "no" {
		return nil, errors.New("sdh must be any, yes or no")
	}
	if values[subtitleSettingCollision] != "keep" && values[subtitleSettingCollision] != "replace" {
		return nil, errors.New("collision policy must be keep or replace")
	}
	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(cfg.Settings[subtitleSettingAPIKey])
	}
	username := strings.TrimSpace(input.Username)
	if username == "" {
		username = strings.TrimSpace(cfg.Settings[subtitleSettingUsername])
	}
	values[subtitleSettingUsername] = username
	password := input.Password
	if password == "" {
		password = cfg.Settings[subtitleSettingPassword]
	}
	if settingTruthy(values[subtitleSettingEnabled]) && apiKey == "" {
		return nil, errors.New("subtitle provider API key is required when subtitles are enabled")
	}
	if (values[subtitleSettingUsername] == "") != (strings.TrimSpace(password) == "") {
		return nil, errors.New("subtitle provider username and password must be supplied together")
	}
	values[subtitleSettingAPIKey] = apiKey
	values[subtitleSettingPassword] = password
	return values, nil
}

func saveSubtitleSettings(cfg *Config, input subtitleSettingsInput) error {
	values, err := subtitleSettingsValues(cfg, input)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	conn, err := OpenConfigDBWritable(filepath.Join(cfg.DataDir, "gextto_config.db"))
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Exec("CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		return err
	}
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := tx.Exec("INSERT INTO settings(key,value) VALUES (?1,?2) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, values[key]); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	TouchConfigGeneration()
	return nil
}

func SubtitleSettingsSave(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.dryRun() {
		jsonError(w, http.StatusConflict, "dry-run does not save settings")
		return
	}
	var input subtitleSettingsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := saveSubtitleSettings(latestConfig(s), input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

func SubtitleSettingsView(w http.ResponseWriter, r *http.Request, s *AppState) {
	prefs := subtitlePreferencesFrom(latestConfig(s))
	jsonResponse(w, map[string]any{
		"enabled": prefs.Enabled, "provider_url": prefs.ProviderURL,
		"username": prefs.Username,
		"forced": prefs.Forced, "sdh": prefs.SDH, "collision_policy": prefs.Collision,
		"api_key_configured": prefs.APIKey != "", "password_configured": prefs.Password != "",
	})
}

func SubtitleProviderTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	client, _, err := subtitleProviderClient(latestConfig(s))
	if err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	if _, err := client.Search(r.Context(), subtitleprovider.SearchQuery{Language: "en", Kind: "movie", Title: "test"}); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

type subtitleSearchResponse struct {
	Target     subtitleTarget      `json:"target"`
	Candidates []subtitleCandidate `json:"candidates"`
}

func SubtitleSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	series := strings.TrimSpace(r.URL.Query().Get("series"))
	season, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("season")), 10, 64)
	episode, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("episode")), 10, 64)
	movieID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("movie_id")), 10, 64)
	target, err := resolveSubtitleTarget(r.Context(), cfg, s.db, series, season, episode, movieID)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	candidates, err := searchSubtitleCandidates(r.Context(), cfg, s.db, target)
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "disabled") {
			status = http.StatusConflict
		}
		jsonError(w, status, err.Error())
		return
	}
	jsonResponse(w, subtitleSearchResponse{Target: target, Candidates: candidates})
}

type subtitleApplyRequest struct {
	Series   string `json:"series"`
	Season   int64  `json:"season"`
	Episode  int64  `json:"episode"`
	MovieID  int64  `json:"movie_id"`
	FileID   int64  `json:"file_id"`
	Language string `json:"language"`
	Forced   bool   `json:"forced"`
	SDH      bool   `json:"sdh"`
	Replace  bool   `json:"replace"`
}

func SubtitleApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.dryRun() {
		jsonError(w, http.StatusConflict, "dry-run does not write subtitles")
		return
	}
	var input subtitleApplyRequest
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.FileID <= 0 || subtitleProviderLanguage(input.Language) == "" {
		jsonError(w, http.StatusBadRequest, "file_id and language are required")
		return
	}
	cfg := latestConfig(s)
	target, err := resolveSubtitleTarget(r.Context(), cfg, s.db, input.Series, input.Season, input.Episode, input.MovieID)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	path, err := applySubtitle(r.Context(), cfg, s.db, target, input.FileID, input.Language, input.Forced, input.SDH, input.Replace)
	if errors.Is(err, errSubtitleCollision) {
		jsonError(w, http.StatusConflict, "subtitle already exists; enable replace to overwrite it")
		return
	}
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "path": path})
}
