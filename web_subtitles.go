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

	"github.com/buzzqw/gextto/internal/logging"
	subtitleprovider "github.com/buzzqw/gextto/internal/subtitles"
)

type subtitleSettingsInput struct {
	Enabled      any    `json:"enabled"`
	ProviderKind string `json:"provider_kind"`
	ProviderURL  string `json:"provider_url"`
	APIKey       string `json:"api_key"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Forced       string `json:"forced"`
	SDH          string `json:"sdh"`
	Collision    string `json:"collision_policy"`
}

func subtitleSettingsValues(cfg *Config, input subtitleSettingsInput) (map[string]string, error) {
	values := map[string]string{
		subtitleSettingEnabled:      strconv.FormatBool(settingTruthy(fmt.Sprint(input.Enabled))),
		subtitleSettingProviderKind: subtitleProviderKind(input.ProviderKind),
		subtitleSettingProviderURL:  strings.TrimSpace(input.ProviderURL),
		subtitleSettingUsername:     strings.TrimSpace(input.Username),
		subtitleSettingForced:       strings.ToLower(strings.TrimSpace(input.Forced)),
		subtitleSettingSDH:          strings.ToLower(strings.TrimSpace(input.SDH)),
		subtitleSettingCollision:    strings.ToLower(strings.TrimSpace(input.Collision)),
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
	if settingTruthy(values[subtitleSettingEnabled]) && values[subtitleSettingProviderKind] == "com" && apiKey == "" {
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
		"enabled": prefs.Enabled, "provider_kind": prefs.Kind, "provider_url": prefs.ProviderURL,
		"username": prefs.Username,
		"forced":   prefs.Forced, "sdh": prefs.SDH, "collision_policy": prefs.Collision,
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
	query := r.URL.Query()
	series := strings.TrimSpace(query.Get("series"))
	season, _ := strconv.ParseInt(strings.TrimSpace(query.Get("season")), 10, 64)
	episode, _ := strconv.ParseInt(strings.TrimSpace(query.Get("episode")), 10, 64)
	movieID, _ := strconv.ParseInt(strings.TrimSpace(query.Get("movie_id")), 10, 64)
	torrentHash := strings.TrimSpace(query.Get("torrent_hash"))
	var target subtitleTarget
	var err error
	if torrentHash != "" {
		if series != "" || season != 0 || episode != 0 || movieID != 0 {
			jsonError(w, http.StatusBadRequest, "choose either an archived title or an active torrent")
			return
		}
		rawIndex, present := query["file_index"]
		if !present || len(rawIndex) != 1 {
			jsonError(w, http.StatusBadRequest, "file_index is required for an active torrent")
			return
		}
		fileIndex, parseErr := strconv.Atoi(strings.TrimSpace(rawIndex[0]))
		if parseErr != nil || fileIndex < 0 {
			jsonError(w, http.StatusBadRequest, "file_index must be a non-negative integer")
			return
		}
		target, err = resolveSubtitleTorrentTarget(r.Context(), cfg, s.db, s.activeEngine(), torrentHash, fileIndex)
	} else {
		target, err = resolveSubtitleTarget(r.Context(), cfg, s.db, series, season, episode, movieID)
	}
	if err != nil {
		logging.Warn("Subtitle search could not identify the video", "error", err.Error())
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	languages := subtitleLanguagesFor(cfg, target)
	if torrentHash != "" && strings.TrimSpace(query.Get("languages")) == "" {
		// Browsing an active download starts with every language; the archived
		// title modal keeps its existing title preference.
		languages = nil
	}
	if raw := strings.TrimSpace(query.Get("languages")); raw != "" {
		languages, err = subtitleLanguagesFilter(raw)
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	logging.Info("Looking for subtitles for a video", "title", target.Label, "languages", subtitleSearchLogLanguages(languages))
	candidates, err := searchSubtitleCandidatesForLanguages(r.Context(), cfg, s.db, target, languages)
	if err != nil {
		logging.Warn("Subtitle search failed", "title", target.Label, "error", err.Error())
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "disabled") {
			status = http.StatusConflict
		}
		jsonError(w, status, err.Error())
		return
	}
	if len(candidates) == 0 {
		logging.Info("No subtitles found for a video", "title", target.Label, "languages", subtitleSearchLogLanguages(languages))
	} else {
		logging.Info(fmt.Sprintf("Found %d subtitles for a video", len(candidates)), "title", target.Label, "languages", subtitleSearchLogLanguages(languages))
	}
	jsonResponse(w, subtitleSearchResponse{Target: target, Candidates: candidates})
}

type subtitleApplyRequest struct {
	Series      string `json:"series"`
	Season      int64  `json:"season"`
	Episode     int64  `json:"episode"`
	MovieID     int64  `json:"movie_id"`
	TorrentHash string `json:"torrent_hash"`
	FileIndex   *int64 `json:"file_index"`
	FileID      int64  `json:"file_id"`
	Language    string `json:"language"`
	Forced      bool   `json:"forced"`
	SDH         bool   `json:"sdh"`
	Replace     bool   `json:"replace"`
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
	var target subtitleTarget
	var err error
	if torrentHash := strings.TrimSpace(input.TorrentHash); torrentHash != "" {
		// A subtitle can be applied while the video is still downloading: the
		// sidecar is written next to the file and follows it into the library.
		if input.Series != "" || input.Season != 0 || input.Episode != 0 || input.MovieID != 0 {
			jsonError(w, http.StatusBadRequest, "choose either an archived title or an active torrent")
			return
		}
		if input.FileIndex == nil || *input.FileIndex < 0 {
			jsonError(w, http.StatusBadRequest, "file_index is required for an active torrent")
			return
		}
		target, err = resolveSubtitleTorrentTarget(r.Context(), cfg, s.db, s.activeEngine(), torrentHash, int(*input.FileIndex))
	} else {
		target, err = resolveSubtitleTarget(r.Context(), cfg, s.db, input.Series, input.Season, input.Episode, input.MovieID)
	}
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	path, err := applySubtitle(r.Context(), cfg, s.db, target, input.FileID, input.Language, input.Forced, input.SDH, input.Replace)
	if errors.Is(err, errSubtitleCollision) {
		logging.Warn("Could not add the subtitle: a file with that name already exists", "title", target.Label)
		jsonError(w, http.StatusConflict, "subtitle already exists; enable replace to overwrite it")
		return
	}
	if err != nil {
		logging.Warn("Could not add the subtitle to a video", "title", target.Label, "error", err.Error())
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	logging.Info("Added a subtitle next to a video", "title", target.Label, "language", input.Language)
	jsonResponse(w, map[string]any{"ok": true, "path": path})
}

func subtitleSearchLogLanguages(languages []string) string {
	if len(languages) == 0 {
		return "all languages"
	}
	return strings.Join(languages, ", ")
}
