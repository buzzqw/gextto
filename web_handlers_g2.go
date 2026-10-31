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
	"sync"
	"sync/atomic"
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
	return settingTruthy(value)
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
func gh2_protectedTorrentPaths(torrents TorrentEngine) map[string]struct{} {
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
				results = append(results, gh2_episodeSource{Release: *release, Origin: episodeOriginFeed})
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
				results = append(results, gh2_episodeSource{Release: *release, Origin: episodeOriginArchive})
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
		key, ok := releaseDedupKey(&result.Release)
		if !ok {
			continue
		}
		key += ":" + result.Origin
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

// gh2_lastResortEpisodeSources lists the copies of a gap that miss only the
// resolution of the profile, marked as the last resort. The caller uses them
// only when the gap has nothing within the profile, after every source
// (indexers, archive, eD2k) has answered: the same rule the automatic cycle
// applies, so the panel shows what the cycle would actually take.
func gh2_lastResortEpisodeSources(results []gh2_episodeSource, cfg *Config, series *SeriesConfig) []gh2_episodeSource {
	kept := make([]gh2_episodeSource, 0, len(results))
	for _, result := range results {
		if !cfg.ReleaseAllowed(&result.Release) {
			continue
		}
		if !cfg.SeriesLastResortAllowed(series, &result.Release.Quality, result.Release.Title) {
			continue
		}
		result.Release.LastResort = true
		kept = append(kept, result)
	}
	return kept
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
		"extra_paths":             value("backup_extra_paths", ""),
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
	protected := gh2_protectedTorrentPaths(s.activeEngine())
	candidates := []DuplicateCandidate{}
	removed := 0
	for index := range cfg.Series {
		series := &cfg.Series[index]
		resolved := cfg.ResolveArchivePath(series)
		if resolved == nil || strings.TrimSpace(*resolved) == "" {
			continue
		}
		directory := *resolved
		if execute {
			count, err := CleanupInferiorDuplicatesInDir(cfg, series.Name, directory, protected)
			if err != nil {
				logging.Warn("duplicate cleanup failed", "series", series.Name, "error", err)
			} else {
				removed += count
			}
		} else {
			found, err := FindSeriesDuplicatesInDir(cfg, series.Name, directory, protected)
			if err != nil {
				logging.Warn("duplicate scan failed", "series", series.Name, "error", err)
			} else {
				candidates = append(candidates, found...)
			}
		}
	}
	if execute {
		logging.Info("duplicate cleanup completed", "removed", removed)
		jsonResponse(w, map[string]any{"ok": true, "execute": true, "removed": removed})
		return
	}
	// Record what the scan found, with enough detail to locate each file, so the
	// log answers "what, where and why" without opening the UI again.
	for _, candidate := range candidates {
		logging.Warn("inferior duplicate found",
			"series", candidate.Series,
			"season", candidate.Season,
			"episode", candidate.Episode,
			"file", candidate.Path,
			"rank", candidate.ResolutionRank,
			"best", candidate.BestRank,
		)
	}
	logging.Info("duplicate scan completed", "found", len(candidates))
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
	endpoint := flareSolverrEndpoint(configured)
	host := domain_of(configured)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, statusCode, err := HTTPPostJSON(ctx, endpoint, nil, []byte(`{"cmd": "sessions.list"}`))
	if err != nil {
		logging.Info("flaresolverr test", "host", host, "ok", false, "error", err.Error())
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if statusCode >= http.StatusBadRequest {
		err := fmt.Errorf("HTTP %d", statusCode)
		logging.Info("flaresolverr test", "host", host, "ok", false, "error", err.Error())
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		logging.Info("flaresolverr test", "host", host, "ok", false, "error", "invalid response")
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	status, _ := value["status"].(string)
	sessions, ok := value["sessions"]
	if !ok {
		sessions = []any{}
	}
	logging.Info("flaresolverr test", "host", host, "ok", status == "ok", "status", status)
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
	manualSearch(w, r, s, false)
}

// ManualSearchDashboard is the v2 manual search. Unlike the classic endpoint,
// it keeps globally rejected releases visible because the user is explicitly
// inspecting arbitrary RSS/indexer/web results and may choose to queue one.
func ManualSearchDashboard(w http.ResponseWriter, r *http.Request, s *AppState) {
	manualSearch(w, r, s, true)
}

func manualSearch(w http.ResponseWriter, r *http.Request, s *AppState, includeRejected bool) {
	if !gh2_setupComplete(s.cfg) {
		jsonError(w, http.StatusConflict, uiText(s, "completa prima la configurazione iniziale"))
		return
	}
	var input SearchInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, uiText(s, "la ricerca deve contenere da 1 a 256 caratteri"))
		return
	}
	startedAt := time.Now()
	logging.Info(fmt.Sprintf("🔎 Searching for «%s» by hand", query))
	cfg := latestConfig(s)
	manualSearchBudget := 90 * time.Second
	if includeRejected {
		// The Dashboard search must not wait behind the automatic cycle. The
		// cycle can legitimately hold cycle_lock for several minutes while
		// scraping feeds, leaving the browser with a fetch that appears to have
		// failed at the network layer. Search providers still receive this
		// bounded request context and return completed sources.
		manualSearchBudget = 20 * time.Second
	} else {
		// Preserve the classic endpoint's existing serialization with cycles.
		s.cycle_lock.Lock()
		defer s.cycle_lock.Unlock()
	}
	ctx, cancel := context.WithTimeout(r.Context(), manualSearchBudget)
	defer cancel()
	var results []models.Release
	if includeRejected {
		results = s.engine.SearchQueryManualAll(ctx, cfg, query)
	} else {
		results = s.engine.SearchQueryManual(ctx, cfg, query)
	}
	// eD2k/Kad is a search provider, not a torrent engine: when it is enabled
	// and this search explicitly asked for it, its results sit next to torrent
	// ones, labelled by Network. The UI only offers them for inspection while
	// gx-ed2k reports download=false.
	if input.Kad {
		// The checkbox is an explicit per-search opt-in: start the managed
		// daemon if the global provider is off, without persisting the switch.
		results = append(results, SearchED2KManual(ctx, cfg, query)...)
	}
	if ctx.Err() == context.DeadlineExceeded {
		logging.Warn(fmt.Sprintf("⏱️ The search for «%s» ran out of its %s: keeping the %s found so far",
			query, logDuration(manualSearchBudget), countLabel(len(results), "result", "results")))
		// searchOneWithDB deliberately preserves releases received before the
		// deadline. The v2 dashboard must show those partial RSS/indexer/web
		// results instead of throwing them away and falling back to the archive.
		if !includeRejected {
			// Keep the classic endpoint's historical behaviour unchanged.
			results = nil
		}
	}
	results = append(results, gh2_archiveReleasesForQuery(s, cfg, query)...)
	seen := map[string]struct{}{}
	deduped := make([]models.Release, 0, len(results))
	for _, release := range results {
		key, ok := releaseDedupKey(&release)
		if !ok {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, release)
	}
	results = deduped
	for index := range results {
		results[index].Score = cfg.ReleaseScore(&results[index])
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	logging.Info(fmt.Sprintf("🔎 The search for «%s» found %s in %s",
		query, countLabel(len(results), "result", "results"), logDuration(time.Since(startedAt))))
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
	result, err := s.activeEngine().SetPin(hash, true)
	if err == nil {
		_ = SaveSetting(s.cfg.DataDir, "torrent_pinned_hash", hash)
	}
	status, value := gh2_torrentAction(result, err)
	jsonStatus(w, status, value)
}

// RedownloadEpisode implements `redownload_episode`. It is the "Riscarica"
// action: it moves the archived file to the trash (so the disk check does not
// see it), deletes the episode row and records an explicit forced gap, then
// starts a series search. The forced gap is what makes the episode eligible
// again even when a season pack covers the season or a later episode is
// already archived.
func RedownloadEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := pathParam(r, "series")
	season, seasonOK := pathInt(r, "season")
	episode, episodeOK := pathInt(r, "episode")
	if !seasonOK || !episodeOK {
		jsonError(w, http.StatusBadRequest, "invalid path parameter")
		return
	}
	cfg := latestConfig(s)
	keepFiles := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("keep_files")), "1")
	avoidPrevious := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("avoid_previous")), "1")
	blockedTitle := ""
	oldTitle := ""
	if avoidPrevious {
		if release, found, err := s.db.EpisodeCurrentRelease(series, season, episode); err != nil {
			logging.Warn("could not read the release to blocklist before the re-download", "series", series, "season", season, "episode", episode, "error", err)
		} else if found {
			oldTitle = release.Title
			if err := s.db.Blocklist(&release, "corrupt_file"); err != nil {
				logging.Warn("could not blocklist the damaged release; the search may pick it again", "series", series, "season", season, "episode", episode, "error", err)
			} else {
				blockedTitle = release.Title
				logging.Info(fmt.Sprintf("🚫 «%s» S%02dE%02d: the damaged release was blocklisted, a different one will be searched", series, season, episode))
			}
		}
	}
	oldPath := ""
	if avoidPrevious {
		oldPath, _ = s.db.ArchivedEpisodePath(series, season, episode)
	}
	moved := ""
	if !keepFiles {
		if archived, err := s.db.ArchivedEpisodePath(series, season, episode); err == nil && archived != "" {
			if avoidPrevious {
				// A damaged file must leave the library even when no trash
				// folder is configured: quarantineCorruptFile renames it in
				// place as a last resort.
				if destination := quarantineCorruptFile(archived, cfg); destination != "" {
					moved = destination
				}
			} else if trash := trashRoot(cfg); trash != "" {
				if destination, err := MoveToTrash(archived, trash); err != nil {
					logging.Warn("redownload: could not move the file to the trash", "path", archived, "error", err.Error())
				} else {
					moved = destination
				}
			}
		}
	}
	if avoidPrevious {
		_ = s.db.MarkMediaIntegrityReplacement(MediaIntegrityProblem{
			Kind: "episode", Series: series, Season: season, Episode: episode, Path: oldPath,
		}, oldTitle)
	}
	if err := s.db.ResetEpisode(series, season, episode, true); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.db.AddForcedGap(series, season, episode); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if avoidPrevious {
		_ = s.db.ClearMediaIntegrityProblem(MediaIntegrityProblem{Kind: "episode", Series: series, Season: season, Episode: episode})
	}
	// Kick off a series search right away so the episode is re-queued without
	// waiting for the next scheduled cycle.
	clone := r.Clone(r.Context())
	query := clone.URL.Query()
	query.Set("domain", "series")
	clone.URL.RawQuery = query.Encode()
	RunNow(&v2DiscardWriter{}, clone, s)
	logging.Info(fmt.Sprintf("🔁 «%s» S%02dE%02d marked for re-download%s", series, season, episode, func() string {
		if moved == "" {
			return ""
		}
		return " (file moved to the trash)"
	}()))
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":            true,
		"redownload":    true,
		"moved_to":      moved,
		"blocked_title": blockedTitle,
		"message":       "episodio reso nuovamente scaricabile: ricerca serie avviata",
	})
}

// trashRoot returns the configured trash directory, or "" when none is set.
func trashRoot(cfg *Config) string {
	if cfg == nil || cfg.TrashPath == nil {
		return ""
	}
	return strings.TrimSpace(*cfg.TrashPath)
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
	names := []string{}
	for index := range cfg.Series {
		if cfg.Series[index].Enabled {
			names = append(names, cfg.Series[index].Name)
		}
	}
	total := len(names)

	// The job manager owns de-duplication, observability and cancellation. The
	// legacy rename_progress struct is kept in sync for /api/rename-progress
	// API compatibility; the job id is additive.
	job, created := s.jobs.Create("rename-all", "rename-all")
	if !created {
		jsonError(w, http.StatusConflict, "a rename is already running")
		return
	}

	s.rename_progress_mu.Lock()
	s.rename_progress.Running = true
	s.rename_progress.Current = 0
	s.rename_progress.Total = total
	s.rename_progress.Series = ""
	s.rename_progress.Message = "running"
	s.rename_progress.Errors = 0
	s.rename_progress_mu.Unlock()

	if err := s.jobs.Start(job.ID, func(ctx context.Context, jobID string) {
		errors := 0
		canceled := false
		for index, name := range names {
			// Cancellation is observed between series, so the single series is
			// never interrupted half-way.
			if ctx.Err() != nil {
				canceled = true
				break
			}
			s.rename_progress_mu.Lock()
			s.rename_progress.Current = index
			s.rename_progress.Series = name
			s.rename_progress_mu.Unlock()
			if total > 0 {
				s.jobs.SetProgress(jobID, float64(index)/float64(total), name)
			}
			status, _ := seriesRenameApply(s, name, true, force, sourceOnly)
			if status != http.StatusOK {
				errors++
			}
		}
		s.rename_progress_mu.Lock()
		s.rename_progress.Running = false
		s.rename_progress.Current = total
		s.rename_progress.Series = ""
		s.rename_progress.Errors = errors
		switch {
		case canceled:
			s.rename_progress.Message = "cancelled"
		case errors == 0:
			s.rename_progress.Message = "completed"
		default:
			s.rename_progress.Message = fmt.Sprintf("completed with %d error(s)", errors)
		}
		s.rename_progress_mu.Unlock()
		logging.Info(fmt.Sprintf("✏️ Renaming the whole library finished: %s checked%s", countLabel(total, "item", "items"), cycleErrorsSuffix(errors)))
	}); err != nil {
		s.rename_progress_mu.Lock()
		s.rename_progress.Running = false
		s.rename_progress.Message = "not started"
		s.rename_progress_mu.Unlock()
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true, "total": total, "job_id": job.ID})
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
		"backup_extra_paths":    {},
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
		if key == "backup_ftp_password" && value == "" {
			continue
		}
		if len(value) > 4096 {
			jsonError(w, http.StatusBadRequest, "backup setting too long")
			return
		}
		if key == "backup_extra_paths" {
			for _, field := range strings.FieldsFunc(value, func(r rune) bool {
				return r == '\n' || r == '\r' || r == ','
			}) {
				path := strings.TrimSpace(field)
				if path != "" && !filepath.IsAbs(path) {
					jsonError(w, http.StatusBadRequest, "backup_extra_paths: usa percorsi assoluti: "+path)
					return
				}
			}
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
	preallocate := cfg.TorrentPreallocate()
	if input.Preallocate != nil {
		preallocate = *input.Preallocate
	}
	options := AddOptions{
		Paused:         startPaused,
		Sequential:     input.Sequential,
		SeedMode:       input.SeedMode,
		QueueTop:       input.QueueTop,
		FirstLast:      input.FirstLast,
		StopAtMetadata: input.StopAtMetadata,
		Preallocate:    preallocate,
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
		added, err := s.activeEngine().AddWithOptions(target, cfg, preferred, options)
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
	if cfg.DownloadTempDir != nil {
		dir = *cfg.DownloadTempDir
	}
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, fmt.Sprintf("upload-%s.torrent", backupUUID()))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	preferredPath := cfg.DownloadDir
	if input.SavePath != nil && strings.TrimSpace(*input.SavePath) != "" {
		preferredPath = strings.TrimSpace(*input.SavePath)
	}
	hash, err := s.activeEngine().AddTorrentFileWithOptions(path, cfg, &preferredPath, options)
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
// gh2_missingSearchOutcome is the result of a whole-series missing search.
// Searched is how many gaps were actually looked up: a synchronous caller stops
// at its deadline and gets the partial answer, while the background job goes on
// to the end.
type gh2_missingSearchOutcome struct {
	Results       []gh2_episodeResult
	Gaps          [][2]int64
	TotalMissing  int
	Searched      int
	Complete      bool
	MetadataReady bool
}

// gh2_missingSearchProgress reports how many episodes the live search has
// finished and which phase is running ("episodes" or "ed2k").
type gh2_missingSearchProgress func(done, total int, phase string)

// gh2_searchMissingEpisodes searches every missing episode of one series: the
// local archive and feeds first, then each episode on the indexers, then eD2k
// per season when the operator enabled the fallback. There is no cap: a series
// with a hundred gaps is searched in full, and the caller can report progress.
//
// The context is the whole budget: a synchronous caller gives it a deadline and
// receives the episodes looked up so far, while the background job gives it the
// cancel button. Only the gaps whose search actually ran are marked as
// searched, so an interrupted pass does not push them behind the deep interval.
func gh2_searchMissingEpisodes(ctx context.Context, s *AppState, cfg *Config, series *SeriesConfig, progress gh2_missingSearchProgress) gh2_missingSearchOutcome {
	outcome := gh2_missingSearchOutcome{}
	// A newly added series may not have season counts yet. Populate them before
	// calculating gaps so the first manual search does not misleadingly report
	// zero episodes just because the background refresh has not run.
	if metadataConfigured(cfg) {
		stale, err := s.db.SeriesMetadataStale(series.Name, 24)
		if err != nil || stale {
			if err := refreshSeriesMetadataForSearch(ctx, cfg, s.db, series); err != nil {
				logging.Debug("manual missing search: metadata refresh unavailable", "series", series.Name, "error", err)
			}
		}
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
	}
	outcome.Gaps = gaps
	outcome.TotalMissing = len(gaps)
	if progress != nil {
		progress(0, len(gaps), "episodes")
	}
	liveReleases := make([][]models.Release, len(gaps))
	searchedGaps := make([]bool, len(gaps))
	if s.engine == nil {
		// Nothing to ask online (dry run or a deliberately engine-less daemon):
		// the gaps were covered by the archive and the stored results.
		for index := range searchedGaps {
			searchedGaps[index] = true
		}
	}
	if s.engine != nil && len(gaps) > 0 {
		// A generic title query can return only recent episodes and miss an old
		// gap. Search every gap with the structured TV identity, a few at a
		// time so the indexers are not flooded.
		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		var done atomic.Int64
	search:
		for index, gap := range gaps {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				break search
			}
			wg.Add(1)
			go func(index int, season, episode int64) {
				defer recoverGoroutine("search missing episode")
				defer wg.Done()
				defer func() { <-sem }()
				liveReleases[index] = s.engine.SearchSeriesEpisode(ctx, cfg, series, season, episode, true)
				if ctx.Err() == nil {
					searchedGaps[index] = true
				}
				if progress != nil {
					progress(int(done.Add(1)), len(gaps), "episodes")
				}
			}(index, gap[0], gap[1])
		}
		wg.Wait()
	}
	cancelled := ctx.Err() != nil
	results := []gh2_episodeResult{}
	perGap := make([][]gh2_episodeSource, len(gaps))
	for index, gap := range gaps {
		season, episode := gap[0], gap[1]
		episodeResults := gh2_storedSeriesEpisodeSources(s, series, season, episode)
		for releaseIndex := range liveReleases[index] {
			if gh2_releaseMatchesSeriesEpisode(&liveReleases[index][releaseIndex], series, season, episode) {
				episodeResults = append(episodeResults, gh2_episodeSource{Release: liveReleases[index][releaseIndex], Origin: episodeOriginIndexer})
			}
		}
		perGap[index] = episodeResults
	}
	accepted := make([][]gh2_episodeSource, len(gaps))
	for index := range gaps {
		accepted[index] = gh2_finalizeEpisodeSearchResults(perGap[index], cfg, series)
	}
	// Nothing usable from the torrent plane for some gaps: ask eD2k/Kad too, on
	// the same explicit switch as the automatic fallback. The check uses the
	// filtered results, so a stored release rejected by quality or language
	// does not hide the gap. One query per season (not per episode) keeps the
	// wait bounded: aMule serialises its searches.
	//
	// A caller that has already given up does not start it: aMule answers one
	// season at a time and the wait is minutes.
	if ed2kFallbackEligible(cfg) && !cancelled {
		searched := map[int64]bool{}
		for _, gap := range gaps {
			if ctx.Err() != nil {
				break
			}
			season := gap[0]
			if searched[season] {
				continue
			}
			needsFallback := false
			for index := range gaps {
				if gaps[index][0] == season && len(accepted[index]) == 0 {
					needsFallback = true
					break
				}
			}
			if !needsFallback {
				continue
			}
			searched[season] = true
			if progress != nil {
				progress(len(gaps), len(gaps), "ed2k")
			}
			candidates := ed2kManualSeasonFallback(ctx, s.engine, cfg, series, season)
			if len(candidates) == 0 {
				continue
			}
			for index, candidateGap := range gaps {
				if candidateGap[0] != season || len(accepted[index]) != 0 {
					continue
				}
				for _, release := range candidates {
					if gh2_releaseMatchesSeriesEpisode(&release, series, season, candidateGap[1]) {
						perGap[index] = append(perGap[index], gh2_episodeSource{Release: release, Origin: episodeOriginED2K})
					}
				}
				accepted[index] = gh2_finalizeEpisodeSearchResults(perGap[index], cfg, series)
			}
		}
	}
	for index, gap := range gaps {
		season, episode := gap[0], gap[1]
		listed := accepted[index]
		if len(listed) == 0 {
			// Nothing within the profile: show the copies in the right language
			// that miss only the resolution, marked, exactly as the cycle would
			// take them for this episode.
			listed = gh2_lastResortEpisodeSources(perGap[index], cfg, series)
		}
		for _, result := range listed {
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
		key, ok := releaseDedupKey(&result.Release)
		if !ok {
			continue
		}
		key += ":" + result.Origin
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, result)
	}
	results = deduped
	for index := range results {
		results[index].Release.Score = cfg.ReleaseScore(&results[index].Release)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Release.Score > results[j].Release.Score
	})
	// Persist what the search found, exactly like a single-episode search: the
	// archive can then answer the next click locally instead of throwing the
	// result away with this response.
	if len(results) > 0 {
		releases := make([]models.Release, 0, len(results))
		for _, result := range results {
			releases = append(releases, result.Release)
		}
		if s.archive != nil {
			if err := s.archive.SaveBatch(releases, cfg); err != nil {
				logging.Warn("failed to save missing-episode search results to archive",
					"series", series.Name, "error", err)
			}
		}
		if s.db != nil {
			if err := s.db.RecordSeenBatch(releases, cfg); err != nil {
				logging.Warn("missing-episode search seen recording failed", "error", err)
			}
		}
	}
	for index, gap := range gaps {
		if !searchedGaps[index] {
			continue
		}
		_ = s.db.MarkGapSearched(series.Name, gap[0], gap[1])
	}
	// Searched is what the caller may rely on: a synchronous call that hit its
	// deadline returns a partial set, and says so with Complete.
	for _, ok := range searchedGaps {
		if ok {
			outcome.Searched++
		}
	}
	outcome.Complete = !cancelled && outcome.Searched == len(gaps)
	if cancelled {
		logging.Info(fmt.Sprintf("🔎 Search for the missing episodes of %s stopped early: %s for %d of %d episodes",
			series.Name, countLabel(len(results), "result", "results"), outcome.Searched, len(gaps)))
	} else {
		logging.Info(fmt.Sprintf("🔎 Search for the missing episodes of %s: %s for %s",
			series.Name, countLabel(len(results), "result", "results"), countLabel(len(gaps), "episode", "episodes")))
	}
	seasonMetadata, _ := s.db.SeriesSeasonCounts(series.Name)
	outcome.Results = results
	outcome.MetadataReady = len(seasonMetadata) > 0 || len(allGaps) > 0
	return outcome
}

// missingSearchSyncBudget is how long the synchronous API call waits before
// answering with the episodes it has already searched. The UI runs the same
// search as a background job: no deadline, live progress and a cancel button.
const missingSearchSyncBudget = 90 * time.Second

func SeriesSearchMissing(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	cfg := latestConfig(s)
	series := gh2_findSeries(cfg, name)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), missingSearchSyncBudget)
	defer cancel()
	outcome := gh2_searchMissingEpisodes(ctx, s, cfg, series, nil)
	episodes := make([]map[string]any, 0, len(outcome.Gaps))
	for _, gap := range outcome.Gaps {
		episodes = append(episodes, map[string]any{"season": gap[0], "episode": gap[1]})
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"series":             series.Name,
		"searched":           outcome.Searched,
		"total":              outcome.TotalMissing,
		"complete":           outcome.Complete,
		"episodes":           episodes,
		"results":            outcome.Results,
		"metadata_available": outcome.MetadataReady,
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
	_ = SaveSetting(s.cfg.DataDir, "torrent_sequential", value)
	result, err := s.activeEngine().SetSequential(input.Enabled)
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
		"torrent_stats": s.activeEngine().Stats(),
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
	trackers, ok, err := s.activeEngine().Trackers(hash)
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
	if input.Audio != nil {
		movie.Audio = normalizeAudioRequirement(*input.Audio)
	}
	if input.AudioChannels != nil {
		movie.AudioChannels = normalizeAudioChannels(*input.AudioChannels)
	}
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
	if input.DisableUpgrades != nil {
		movie.DisableUpgrades = *input.DisableUpgrades
	}
	if input.QualityProfileID != nil && *input.QualityProfileID > 0 {
		profile, ok := qualityProfileExact(ensureQualityProfiles(s), *input.QualityProfileID)
		if !ok {
			jsonError(w, http.StatusBadRequest, "quality profile not found")
			return
		}
		movie.Quality, movie.Language, movie.Subtitle = profile.Quality, profile.Language, profile.Subtitle
		movie.Audio, movie.AudioChannels = profile.Audio, ""
		movie.LanguageRequirements = ""
		movie.DisableUpgrades = profile.DisableUpgrades
		movie.Exclude = mergeExclusions(profile.Exclude, movie.Exclude)
	}
	if input.SubtitleRequired != nil {
		movie.SubtitleRequirements = ""
		if *input.SubtitleRequired {
			movie.SubtitleRequirements = SanitizeSubtitleRequirements(movie.Subtitle)
		}
	}
	if err := SaveLibrary(s.cfg.DataDir, current.Series, movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}
