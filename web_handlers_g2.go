package gextto

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/rules"
	"github.com/buzzqw/gextto/internal/utils"
)

// gh2_findSeries implements `find_series`: exact match on the configured name or one
// of its aliases (the comparison is case-sensitive).
func gh2_findSeries(cfg *Config, name string) *SeriesConfig {
	for index := range cfg.Series {
		series := &cfg.Series[index]
		if series.Name == name {
			return series
		}
		for _, alias := range series.Aliases {
			if alias == name {
				return series
			}
		}
	}
	return nil
}

// gh2_setupComplete implements `setup_complete`: the setup marker file exists.
func gh2_setupComplete(cfg *Config) bool {
	info, err := os.Stat(filepath.Join(cfg.DataDir, ".gextto-setup.json"))
	return err == nil && !info.IsDir()
}

// gh2_settingsValue implements `settings_value`.
func gh2_settingsValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		if typed {
			return "true", true
		}
		return "false", true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case json.Number:
		return typed.String(), true
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return "", false
		}
		return gh2_settingsValue(decoded)
	default:
		return "", false
	}
}

// gh2_truthy ports the `matches!(value, "yes" | "true" | "1")` setting check.
func gh2_truthy(value string) bool {
	return value == "yes" || value == "true" || value == "1"
}

// gh2_isJSONNull reports whether a flattened setting value is absent or JSON
// null (JSON skips `value.is_null()`).
func gh2_isJSONNull(value any) bool {
	if value == nil {
		return true
	}
	if raw, ok := value.(json.RawMessage); ok {
		return string(bytes.TrimSpace(raw)) == "null"
	}
	return false
}

// gh2_storedDownloadTags implements `stored_download_tags`.
func gh2_storedDownloadTags(cfg *Config) []string {
	raw, ok := cfg.Settings["download_tags"]
	if !ok {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil || tags == nil {
		return []string{}
	}
	return tags
}

// gh2_protectedTorrentPaths implements `protected_torrent_paths`.
func gh2_protectedTorrentPaths(torrents *LibtorrentClient) map[string]struct{} {
	protected := map[string]struct{}{}
	for _, torrent := range torrents.List() {
		files, ok, err := torrents.Files(torrent.Hash)
		if err != nil || !ok {
			continue
		}
		for _, file := range files {
			protected[filepath.Join(torrent.SavePath, file.Path)] = struct{}{}
		}
	}
	return protected
}

// gh2_decodeOptionalJSON decodes an optional JSON body: an empty body leaves
// target untouched ( `Option<Json<T>>` = None), a malformed body is an
// error.
func gh2_decodeOptionalJSON(r *http.Request, target any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, target)
}

// gh2_torrentAction implements `torrent_action`.
func gh2_torrentAction(result bool, err error) (int, any) {
	if err != nil {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
	}
	if result {
		return http.StatusOK, map[string]any{"ok": true}
	}
	return http.StatusNotFound, map[string]any{"ok": false, "error": "torrent unavailable in dry-run"}
}

// gh2_archiveReleasesForQuery implements `archive_releases_for_query`.
func gh2_archiveReleasesForQuery(s *AppState, cfg *Config, query string) []models.Release {
	results := []models.Release{}
	rows, err := s.archive.Search(query)
	if err != nil {
		return results
	}
	for _, row := range rows {
		release := ParseRelease(row[0], row[1], "archive:"+row[2])
		if release == nil {
			continue
		}
		if reason := cfg.AllReleaseDeniedReason(release); reason != "" {
			if cfg.ReleaseIsMonitored(release) {
				rules.LogRejection(release, reason)
			}
		} else {
			results = append(results, *release)
		}
	}
	return results
}

// gh2_episodeSource is one found episode release together with its origin.
type gh2_episodeSource struct {
	Release models.Release `json:"release"`
	Origin  string         `json:"origin"`
}

// gh2_episodeResult is the search result surfaced by series_search_missing.
type gh2_episodeResult struct {
	Release models.Release `json:"release"`
	Origin  string         `json:"origin"`
	Season  int64          `json:"season"`
	Episode int64          `json:"episode"`
}

// gh2_releaseMatchesSeriesEpisode implements `release_matches_series_episode`.
func gh2_releaseMatchesSeriesEpisode(release *models.Release, series *SeriesConfig, season, episode int64) bool {
	if release.Kind != "series" {
		return false
	}
	if release.Season == nil || *release.Season != season {
		return false
	}
	if len(release.EpisodeRange) == 0 {
		if release.Episode == nil || *release.Episode != episode {
			return false
		}
	} else if !containsInt64(release.EpisodeRange, episode) && !containsInt64(release.EpisodeRange, 0) {
		return false
	}
	if release.Series == nil {
		return false
	}
	name := *release.Series
	if SeriesNamesMatch(series.Name, name) {
		return true
	}
	for _, alias := range series.Aliases {
		if SeriesNamesMatch(alias, name) {
			return true
		}
	}
	return false
}

// gh2_storedSeriesEpisodeSources implements `stored_series_episode_sources`.
func gh2_storedSeriesEpisodeSources(s *AppState, series *SeriesConfig, season, episode int64) []gh2_episodeSource {
	query := fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode)
	results := []gh2_episodeSource{}
	if rows, err := s.db.SeriesFeedForEpisode(season, episode, 200); err == nil {
		for _, row := range rows {
			release := ParseRelease(row[0], row[1], row[2])
			if release == nil {
				continue
			}
			if gh2_releaseMatchesSeriesEpisode(release, series, season, episode) {
				results = append(results, gh2_episodeSource{Release: *release, Origin: "Feed RSS"})
			}
		}
	}
	if rows, err := s.archive.Search(query); err == nil {
		for _, row := range rows {
			release := ParseRelease(row[0], row[1], row[2])
			if release == nil {
				continue
			}
			if gh2_releaseMatchesSeriesEpisode(release, series, season, episode) {
				results = append(results, gh2_episodeSource{Release: *release, Origin: "Archivio"})
			}
		}
	}
	return results
}

// gh2_finalizeEpisodeSearchResults implements `finalize_episode_search_results`.
func gh2_finalizeEpisodeSearchResults(results []gh2_episodeSource, cfg *Config, series *SeriesConfig) []gh2_episodeSource {
	filtered := make([]gh2_episodeSource, 0, len(results))
	for _, result := range results {
		if cfg.ReleaseAllowed(&result.Release) && cfg.SeriesReleaseAllowed(series, &result.Release.Quality, result.Release.Title) {
			filtered = append(filtered, result)
		}
	}
	seen := map[string]struct{}{}
	deduped := make([]gh2_episodeSource, 0, len(filtered))
	for _, result := range filtered {
		hash, ok := utils.MagnetHash(result.Release.Magnet)
		if !ok {
			continue
		}
		key := hash + ":" + result.Origin
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, result)
	}
	sort.SliceStable(deduped, func(i, j int) bool {
		return cfg.ReleaseScore(&deduped[i].Release) > cfg.ReleaseScore(&deduped[j].Release)
	})
	return deduped
}

// AddDownloadTag implements `add_download_tag`.
func AddDownloadTag(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input DownloadTagInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	tag := strings.TrimSpace(input.Tag)
	if tag == "" || len(tag) > 128 {
		jsonError(w, http.StatusBadRequest, "tag non valido")
		return
	}
	cfg := latestConfig(s)
	tags := gh2_storedDownloadTags(cfg)
	exists := false
	for _, existing := range tags {
		if strings.EqualFold(existing, tag) {
			exists = true
			break
		}
	}
	if !exists {
		tags = append(tags, tag)
		sort.Slice(tags, func(i, j int) bool {
			return strings.ToLower(tags[i]) < strings.ToLower(tags[j])
		})
		payload, err := json.Marshal(tags)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := SaveSetting(s.cfg.DataDir, "download_tags", string(payload)); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	jsonResponse(w, map[string]any{"ok": true, "items": tags})
}

// BackupSettings implements `backup_settings`.
func BackupSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	value := func(key, fallback string) string {
		if current, ok := cfg.Settings[key]; ok {
			return current
		}
		return fallback
	}
	password := cfg.Settings["backup_ftp_password"]
	_, passwordPresent := cfg.Settings["backup_ftp_password"]
	jsonResponse(w, map[string]any{
		"ok":                      true,
		"retention":               value("backup_retention", "5"),
		"schedule_hours":          value("backup_schedule_hours", "0"),
		"schedule_at":             value("backup_schedule_at", ""),
		"send_telegram":           gh2_truthy(cfg.Settings["backup_send_telegram"]),
		"ftp_host":                value("backup_ftp_host", ""),
		"ftp_user":                value("backup_ftp_user", ""),
		"ftp_path":                value("backup_ftp_path", ""),
		"cloud_dir":               value("backup_cloud_dir", ""),
		"ftp_password_configured": passwordPresent && password != "",
	})
}

// CleanDuplicates implements `clean_duplicates`.
func CleanDuplicates(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input DuplicatesInput
	if err := gh2_decodeOptionalJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	execute := input.Execute
	cfg := latestConfig(s)
	protected := gh2_protectedTorrentPaths(s.torrents)
	preferred := cfg.DefaultLanguage()
	candidates := []DuplicateCandidate{}
	removed := 0
	for index := range cfg.Series {
		series := &cfg.Series[index]
		if strings.TrimSpace(series.ArchivePath) == "" {
			continue
		}
		directory := series.ArchivePath
		if execute {
			count, err := CleanupInferiorDuplicatesInDir(cfg, series.Name, directory, protected)
			if err != nil {
				logging.Warn("duplicate cleanup failed", "series", series.Name, "error", err)
			} else {
				removed += count
			}
		} else {
			found, err := FindInferiorDuplicatesInDir(series.Name, directory, protected, preferred)
			if err != nil {
				logging.Warn("duplicate scan failed", "series", series.Name, "error", err)
			} else {
				candidates = append(candidates, found...)
			}
		}
	}
	if execute {
		jsonResponse(w, map[string]any{"ok": true, "execute": true, "removed": removed})
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "execute": false, "count": len(candidates), "items": candidates})
}

// ComicDownloadRemove implements `comic_download_remove`.
func ComicDownloadRemove(w http.ResponseWriter, r *http.Request, s *AppState) {
	id := pathParam(r, "id")
	var input RemoveCompletedInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if CancelHTTPDownload(id, input.DeleteFiles) {
		jsonResponse(w, map[string]any{"ok": true})
		return
	}
	jsonError(w, http.StatusNotFound, "download non trovato")
}

// Comics implements `comics`.
func Comics(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.comics.ListMonitored(false)
	if err != nil {
		items = []ComicMonitored{}
	}
	jsonResponse(w, items)
}

// DbPrune implements `db_prune`.
func DbPrune(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input PruneInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	retain := int64(50)
	if input.RetainCycles != nil {
		retain = *input.RetainCycles
	}
	retain = clampInt64(retain, 1, 10000)
	errorAge := int64(7)
	if input.ErrorAgeDays != nil {
		errorAge = *input.ErrorAgeDays
	}
	if errorAge < 1 {
		errorAge = 1
	}
	seenDays := int64(0)
	if input.SeenRetentionDays != nil {
		seenDays = *input.SeenRetentionDays
	}
	if input.Preview {
		preview, err := s.db.PrunePreview(retain, errorAge, seenDays)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":           true,
			"preview":      true,
			"report":       preview,
			"seen_removed": preview.SeenToRemove,
		})
		return
	}
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not modify the database")
		return
	}
	report, err := s.db.Cleanup(retain, errorAge)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	seenRemoved, err := s.db.PruneSeenOlderThan(seenDays)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "report": report, "seen_removed": seenRemoved})
}

// DeleteSetting implements `delete_setting`.
func DeleteSettingHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	key := pathParam(r, "key")
	if !strings.HasPrefix(key, "score_group_") || len(key) <= len("score_group_") {
		jsonError(w, http.StatusBadRequest, "only custom score groups can be deleted here")
		return
	}
	removed, err := DeleteSetting(s.cfg.DataDir, key)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "removed": removed, "key": key})
}

// FlaresolverrTest implements `flaresolverr_test`.
func FlaresolverrTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	configured := ""
	if cfg.FlaresolverrURL != nil && strings.TrimSpace(*cfg.FlaresolverrURL) != "" {
		configured = *cfg.FlaresolverrURL
	}
	if configured == "" {
		jsonError(w, http.StatusBadRequest, "URL FlareSolverr non configurato")
		return
	}
	endpoint := strings.TrimRight(configured, "/") + "/v1"
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, _, err := HTTPPostJSON(ctx, endpoint, nil, []byte(`{"cmd": "sessions.list"}`))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	status, _ := value["status"].(string)
	sessions, ok := value["sessions"]
	if !ok {
		sessions = []any{}
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":       status == "ok",
		"status":   value["status"],
		"sessions": sessions,
	})
}

// LastCycle implements `last_cycle`.
func LastCycle(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{"ok": true, "cycle": s.last_cycle.Snapshot()})
}

// ManualSearch implements `manual_search`.
func ManualSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	if !gh2_setupComplete(s.cfg) {
		jsonError(w, http.StatusConflict, "complete the initial setup first")
		return
	}
	var input SearchInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, "query must contain 1-256 characters")
		return
	}
	s.cycle_lock.Lock()
	defer s.cycle_lock.Unlock()
	cfg := latestConfig(s)
	const manualSearchBudget = 90 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), manualSearchBudget)
	defer cancel()
	results := s.engine.SearchQueryManual(ctx, cfg, query)
	if ctx.Err() == context.DeadlineExceeded {
		logging.Warn("manual search budget expired; returning archive results", "query", query, "timeout_secs", int64(manualSearchBudget.Seconds()))
		results = nil
	}
	results = append(results, gh2_archiveReleasesForQuery(s, cfg, query)...)
	seen := map[string]struct{}{}
	deduped := make([]models.Release, 0, len(results))
	for _, release := range results {
		hash, ok := utils.MagnetHash(release.Magnet)
		if !ok {
			continue
		}
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		deduped = append(deduped, release)
	}
	results = deduped
	sort.SliceStable(results, func(i, j int) bool {
		return cfg.ReleaseScore(&results[i]) > cfg.ReleaseScore(&results[j])
	})
	jsonResponse(w, map[string]any{"ok": true, "query": query, "results": results})
}

// MovieList implements `movie_list`.
func MovieList(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonResponse(w, map[string]any{"ok": true, "items": cfg.Movies})
}

// PinTorrent implements `pin_torrent`.
func PinTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TorrentHashInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash := strings.ToLower(input.Hash)
	result, err := s.torrents.SetPin(hash, true)
	if err == nil {
		_ = SaveSetting(s.cfg.DataDir, "libtorrent_pinned_hash", hash)
	}
	status, value := gh2_torrentAction(result, err)
	jsonStatus(w, status, value)
}

// RedownloadEpisode implements `redownload_episode`.
func RedownloadEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := pathParam(r, "series")
	season, seasonOK := pathInt(r, "season")
	episode, episodeOK := pathInt(r, "episode")
	if !seasonOK || !episodeOK {
		jsonError(w, http.StatusBadRequest, "invalid path parameter")
		return
	}
	if err := s.db.ResetEpisode(series, season, episode, true); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":         true,
		"redownload": true,
		"message":    "episodio reso nuovamente disponibile al prossimo ciclo",
	})
}

// RenameAll implements `rename_all`.
func RenameAll(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RenameInput
	if err := gh2_decodeOptionalJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	force := input.Force
	sourceOnly := input.SourceOnly
	cfg := latestConfig(s)
	if !cfg.RenameEpisodes {
		jsonError(w, http.StatusConflict, "rename is disabled")
		return
	}
	if s.rename_progress.Running {
		jsonError(w, http.StatusConflict, "a rename is already running")
		return
	}
	s.rename_progress.Running = true
	s.rename_progress.Current = 0
	s.rename_progress.Total = 0
	s.rename_progress.Series = ""
	s.rename_progress.Message = "starting"
	s.rename_progress.Errors = 0
	names := []string{}
	for index := range cfg.Series {
		if cfg.Series[index].Enabled {
			names = append(names, cfg.Series[index].Name)
		}
	}
	total := len(names)
	s.rename_progress.Total = total
	s.rename_progress.Message = "running"
	go func() {
		errors := 0
		for index, name := range names {
			s.rename_progress.Current = index
			s.rename_progress.Series = name
			status, _ := seriesRenameApply(s, name, true, force, sourceOnly)
			if status != http.StatusOK {
				errors++
			}
		}
		s.rename_progress.Running = false
		s.rename_progress.Current = total
		s.rename_progress.Series = ""
		s.rename_progress.Errors = errors
		if errors == 0 {
			s.rename_progress.Message = "completed"
		} else {
			s.rename_progress.Message = fmt.Sprintf("completed with %d error(s)", errors)
		}
		logging.Info("background rename-all finished", "total", total, "errors", errors)
	}()
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true, "total": total})
}

// SaveBackupSettings implements `save_backup_settings`.
func SaveBackupSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SettingsPatch
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowed := map[string]struct{}{
		"backup_retention":      {},
		"backup_schedule_hours": {},
		"backup_schedule_at":    {},
		"backup_send_telegram":  {},
		"backup_ftp_host":       {},
		"backup_ftp_user":       {},
		"backup_ftp_password":   {},
		"backup_ftp_path":       {},
		"backup_cloud_dir":      {},
	}
	keys := make([]string, 0, len(input.Values))
	for key := range input.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	saved := []string{}
	for _, key := range keys {
		raw := input.Values[key]
		if _, ok := allowed[key]; !ok || gh2_isJSONNull(raw) {
			continue
		}
		value, ok := gh2_settingsValue(raw)
		if !ok {
			jsonError(w, http.StatusBadRequest, "invalid backup setting value")
			return
		}
		if len(value) > 4096 {
			jsonError(w, http.StatusBadRequest, "backup setting too long")
			return
		}
		if err := SaveSetting(s.cfg.DataDir, key, value); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		saved = append(saved, key)
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "saved": saved})
}

// SaveTraktSettings implements `save_trakt_settings`.
func SaveTraktSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SettingsPatch
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowed := map[string]struct{}{
		"trakt_client_id":        {},
		"trakt_client_secret":    {},
		"trakt_watchlist_sync":   {},
		"trakt_scrobble_enabled": {},
		"trakt_calendar_days":    {},
	}
	keys := make([]string, 0, len(input.Values))
	for key := range input.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	saved := []string{}
	for _, key := range keys {
		raw := input.Values[key]
		if _, ok := allowed[key]; !ok || gh2_isJSONNull(raw) {
			continue
		}
		value, ok := gh2_settingsValue(raw)
		if !ok {
			jsonError(w, http.StatusBadRequest, "invalid setting value")
			return
		}
		if len(value) > 4096 {
			jsonError(w, http.StatusBadRequest, "setting too long")
			return
		}
		if err := SaveSetting(s.cfg.DataDir, key, value); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		saved = append(saved, key)
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "saved": saved})
}

// SendMagnet implements `send_magnet`.
func SendMagnet(w http.ResponseWriter, r *http.Request, s *AppState) {
	if !gh2_setupComplete(s.cfg) {
		jsonError(w, http.StatusConflict, "complete the initial setup first")
		return
	}
	var input AddTorrentInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	startPaused := input.StartPaused
	noRename := input.NoRename
	options := AddOptions{
		Paused:         startPaused,
		Sequential:     input.Sequential,
		SeedMode:       input.SeedMode,
		QueueTop:       input.QueueTop,
		FirstLast:      input.FirstLast,
		StopAtMetadata: input.StopAtMetadata,
	}
	target := strings.TrimSpace(input.Magnet)
	if strings.HasPrefix(target, "magnet:") {
		var preferred *string
		if input.SavePath != nil {
			trimmed := strings.TrimSpace(*input.SavePath)
			if trimmed != "" {
				preferred = &trimmed
			}
		}
		added, err := s.torrents.AddWithOptions(target, cfg, preferred, options)
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !added {
			jsonError(w, http.StatusConflict, "duplicate")
			return
		}
		if noRename {
			if hash, ok := utils.MagnetHash(target); ok {
				_ = s.db.SetTorrentNoRename(hash, true)
			}
		}
		jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true, "kind": "magnet", "start_paused": startPaused})
		return
	}
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		jsonError(w, http.StatusBadRequest, "magnet or http(s) URL required")
		return
	}
	if cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not download torrent files")
		return
	}
	response, err := HTTPGet(r.Context(), target, nil)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		jsonError(w, http.StatusBadGateway, fmt.Sprintf("remote torrent returned %s", response.Status))
		return
	}
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if len(payload) > 5_000_000 {
		jsonError(w, http.StatusBadRequest, "torrent file is too large")
		return
	}
	dir := filepath.Join(cfg.DataDir, "incomplete")
	if cfg.LibtorrentTempDir != nil {
		dir = *cfg.LibtorrentTempDir
	}
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, fmt.Sprintf("upload-%s.torrent", backupUUID()))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hash, err := s.torrents.AddTorrentFileWithOptions(path, cfg, &cfg.LibtorrentDir, options)
	if err != nil {
		_ = os.Remove(path)
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if hash == nil {
		jsonError(w, http.StatusConflict, "torrent client unavailable")
		return
	}
	_ = os.Remove(path)
	if noRename {
		_ = s.db.SetTorrentNoRename(*hash, true)
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true, "kind": "torrent", "hash": *hash})
}

// SeriesSearchMissing implements `series_search_missing`.
func SeriesSearchMissing(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	cfg := latestConfig(s)
	series := gh2_findSeries(cfg, name)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	allGaps, err := s.db.UnarchivedEpisodesForSeries(series.Name, series.IgnoredSeasons)
	if err != nil {
		allGaps = nil
	}
	gaps := make([][2]int64, 0, len(allGaps))
	for _, gap := range allGaps {
		if containsInt64(series.IgnoredSeasons, gap[0]) {
			continue
		}
		if !SeasonAllowedForScan(series.Seasons, gap[0]) {
			continue
		}
		gaps = append(gaps, gap)
		if len(gaps) == 15 {
			break
		}
	}
	var liveReleases []models.Release
	if len(gaps) > 0 {
		liveReleases = s.engine.SearchQueryManual(r.Context(), cfg, series.Name)
	}
	results := []gh2_episodeResult{}
	for _, gap := range gaps {
		season, episode := gap[0], gap[1]
		episodeResults := gh2_storedSeriesEpisodeSources(s, series, season, episode)
		for index := range liveReleases {
			if gh2_releaseMatchesSeriesEpisode(&liveReleases[index], series, season, episode) {
				episodeResults = append(episodeResults, gh2_episodeSource{Release: liveReleases[index], Origin: "Indexer / web"})
			}
		}
		for _, result := range gh2_finalizeEpisodeSearchResults(episodeResults, cfg, series) {
			results = append(results, gh2_episodeResult{
				Release: result.Release,
				Origin:  result.Origin,
				Season:  season,
				Episode: episode,
			})
		}
	}
	seen := map[string]struct{}{}
	deduped := make([]gh2_episodeResult, 0, len(results))
	for _, result := range results {
		hash, ok := utils.MagnetHash(result.Release.Magnet)
		if !ok {
			continue
		}
		key := hash + ":" + result.Origin
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, result)
	}
	results = deduped
	sort.SliceStable(results, func(i, j int) bool {
		return cfg.ReleaseScore(&results[i].Release) > cfg.ReleaseScore(&results[j].Release)
	})
	for _, gap := range gaps {
		_ = s.db.MarkGapSearched(series.Name, gap[0], gap[1])
	}
	episodes := make([]map[string]any, 0, len(gaps))
	for _, gap := range gaps {
		episodes = append(episodes, map[string]any{"season": gap[0], "episode": gap[1]})
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":       true,
		"series":   series.Name,
		"searched": len(gaps),
		"episodes": episodes,
		"results":  results,
	})
}

// SetSequential implements `set_sequential`.
func SetSequential(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SequentialInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	value := "false"
	if input.Enabled {
		value = "true"
	}
	_ = SaveSetting(s.cfg.DataDir, "libtorrent_sequential", value)
	result, err := s.torrents.SetSequential(input.Enabled)
	status, response := gh2_torrentAction(result, err)
	jsonStatus(w, status, response)
}

// SetTorrentTag implements `set_torrent_tag`.
func SetTorrentTag(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TorrentTagInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.SetTorrentTag(input.Hash, strings.TrimSpace(input.Tag)); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// SimklCalendar implements `simkl_calendar`.
func SimklCalendar(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := (&SimklClient{}).FromSettings(cfg.Settings)
	if !client.Configured() || !client.Authenticated() {
		jsonError(w, http.StatusConflict, "Simkl non configurato")
		return
	}
	value, err := client.Calendar(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, value)
}

// StatsApi implements `stats_api`.
func StatsApi(w http.ResponseWriter, r *http.Request, s *AppState) {
	var consumption any
	if value, err := s.db.ConsumptionStats(); err == nil {
		consumption = value
	}
	jsonResponse(w, map[string]any{
		"last_cycle":    s.last_cycle.Snapshot(),
		"torrent_stats": s.torrents.Stats(),
		"consumption":   consumption,
	})
}

// TorrentEvents implements `torrent_events`.
func TorrentEvents(w http.ResponseWriter, r *http.Request, s *AppState) {
	events := s.torrent_events.Snapshot()
	if len(events) > 100 {
		events = events[len(events)-100:]
	}
	if events == nil {
		events = []models.TorrentEvent{}
	}
	jsonResponse(w, events)
}

// TorrentTrackers implements `torrent_trackers`.
func TorrentTrackers(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	trackers, ok, err := s.torrents.Trackers(hash)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusNotFound, "torrent unavailable in dry-run")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "trackers": trackers})
}

// TraktSettings implements `trakt_settings`.
func TraktSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := (&TraktClient{}).FromSettings(cfg.Settings)
	clientID := cfg.Settings["trakt_client_id"]
	_, clientIDPresent := cfg.Settings["trakt_client_id"]
	secret := cfg.Settings["trakt_client_secret"]
	_, secretPresent := cfg.Settings["trakt_client_secret"]
	calendarDays := "7"
	if value, ok := cfg.Settings["trakt_calendar_days"]; ok {
		calendarDays = value
	}
	jsonResponse(w, map[string]any{
		"ok":                       true,
		"configured":               client.Configured(),
		"authenticated":            client.Authenticated(),
		"client_id":                clientID,
		"client_id_configured":     clientIDPresent && clientID != "",
		"client_secret_configured": secretPresent && secret != "",
		"watchlist_sync":           gh2_truthy(cfg.Settings["trakt_watchlist_sync"]),
		"scrobble_enabled":         gh2_truthy(cfg.Settings["trakt_scrobble_enabled"]),
		"calendar_days":            calendarDays,
	})
}

// UpdateMovie implements `update_movie`.
func UpdateMovie(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid path parameter")
		return
	}
	var input MovieUpdateInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	current := latestConfig(s)
	movies := make([]MovieConfig, len(current.Movies))
	copy(movies, current.Movies)
	var movie *MovieConfig
	for index := range movies {
		if movies[index].ID == id {
			movie = &movies[index]
			break
		}
	}
	if movie == nil {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		jsonError(w, http.StatusBadRequest, "movie name is required")
		return
	}
	movie.Name = strings.TrimSpace(input.Name)
	movie.Year = input.Year
	movie.Quality = input.Quality
	movie.Language = input.Language
	movie.Subtitle = input.Subtitle
	movie.Exclude = input.Exclude
	movie.LanguageRequirements = SanitizeLanguageRequirements(input.LanguageRequirements)
	movie.SubtitleRequirements = SanitizeSubtitleRequirements(input.SubtitleRequirements)
	movie.TmdbID = strings.TrimSpace(input.TmdbId)
	movie.TvdbID = strings.TrimSpace(input.TvdbId)
	if input.Enabled != nil {
		movie.Enabled = *input.Enabled
	}
	if err := SaveLibrary(s.cfg.DataDir, current.Series, movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}
