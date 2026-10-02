package gextto

// web_handlers_g0.go is the group-0 implementation of `the reference daemon`.
// Every handler keeps the shared signature
//
//	func Name(w http.ResponseWriter, r *http.Request, s *AppState)
//
// and is registered by web.go through `handle`. Shared symbols (`jsonStatus`,
// `jsonError`, `jsonResponse`, `decodeJSON`, `pathParam`, `pathInt`,
// `latestConfig`, `cacheGet`, `cachePut`, `AppState` and the input structs) are
// defined by web.go and are never redeclared here. Private helpers carry the
// unique `gh0_` prefix so sibling group files can be generated independently.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

var (
	gh0_weeklyDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// The input structs below live outside the `web.go` core range (they are
// declared later in the web module), so group 0 defines private copies with the `gh0_`
// prefix rather than relying on web.go.
type gh0_renameInput struct {
	Force      bool `json:"force"`
	SourceOnly bool `json:"source_only"`
}

type gh0_mkdirInput struct {
	Path string `json:"path"`
}

type gh0_filePrioritiesInput struct {
	Priorities []int32 `json:"priorities"`
}

type gh0_backfillMediaInfoInput struct {
	Limit *int `json:"limit"`
}

// ---------------------------------------------------------------------------
// small generic helpers
// ---------------------------------------------------------------------------

func gh0_derefStr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func gh0_derefI64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func gh0_containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func gh0_anyPathStartsWith(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if pathStartsWith(path, prefix) {
			return true
		}
	}
	return false
}

// gh0_toMap converts a JSON-marshalable value into a map, returning an empty map
// on failure (mirrors encoding a value that may fail for objects).
func gh0_toMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// gh0_decodeOptional decodes a body that modelled as `Option<Json<T>>`:
// an absent/empty body yields has=false with no error.
func gh0_decodeOptional(r *http.Request, target any) (bool, error) {
	if r.Body == nil {
		return false, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false, err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return false, err
	}
	return true, nil
}

func gh0_parseIntPtr(value string) *int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func gh0_parseIntOrNil(value string) any {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return nil
	}
	return parsed
}

func gh0_idFromAny(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// source_filters_view
// ---------------------------------------------------------------------------

func SourceFiltersView(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "filters": cfg.SourceFilters})
}

// ---------------------------------------------------------------------------
// toggle_season
// ---------------------------------------------------------------------------

func ToggleSeason(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	var input SeasonToggleInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	seriesList := make([]SeriesConfig, len(cfg.Series))
	copy(seriesList, cfg.Series)
	index := -1
	for i := range seriesList {
		if seriesList[i].Name == name || gh0_containsString(seriesList[i].Aliases, name) {
			index = i
			break
		}
	}
	if index < 0 {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	if input.Enabled {
		kept := []int64{}
		for _, season := range seriesList[index].IgnoredSeasons {
			if season != input.Season {
				kept = append(kept, season)
			}
		}
		seriesList[index].IgnoredSeasons = kept
	} else if !containsInt64(seriesList[index].IgnoredSeasons, input.Season) {
		seriesList[index].IgnoredSeasons = append(seriesList[index].IgnoredSeasons, input.Season)
		sort.Slice(seriesList[index].IgnoredSeasons, func(a, b int) bool {
			return seriesList[index].IgnoredSeasons[a] < seriesList[index].IgnoredSeasons[b]
		})
	}
	ignoredSeasons := seriesList[index].IgnoredSeasons
	if err := SaveLibrary(s.cfg.DataDir, seriesList, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "ignored_seasons": ignoredSeasons})
}

// ---------------------------------------------------------------------------
// series_rename_execute (via shared seriesRenameApply)
// ---------------------------------------------------------------------------

func SeriesRenameExecute(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	var input gh0_renameInput
	hasInput, err := gh0_decodeOptional(r, &input)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	force := false
	sourceOnly := false
	if hasInput {
		force = input.Force
		sourceOnly = input.SourceOnly
	}
	status, body := seriesRenameApply(s, name, true, force, sourceOnly)
	jsonStatus(w, status, body)
}

// ---------------------------------------------------------------------------
// movie_detail
// ---------------------------------------------------------------------------

func MovieDetail(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	id, _ := pathInt(r, "id")
	var movie *MovieConfig
	for i := range cfg.Movies {
		if cfg.Movies[i].ID == id {
			movie = &cfg.Movies[i]
			break
		}
	}
	if movie == nil {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	movieCopy := *movie

	var history []MovieHistory
	if all, err := s.db.DownloadedMovies(200); err == nil {
		for _, item := range all {
			if item.Name == movieCopy.Name {
				history = append(history, item)
			}
		}
	}
	if history == nil {
		history = []MovieHistory{}
	}

	query := movieCopy.Name
	if strings.TrimSpace(movieCopy.Year) != "" {
		query = fmt.Sprintf("%s %s", movieCopy.Name, movieCopy.Year)
	}
	matches := []any{}
	if entries, err := s.archive.Search(query); err == nil {
		for index, entry := range entries {
			if index >= 20 {
				break
			}
			matches = append(matches, map[string]any{"title": entry[0], "magnet": entry[1], "source": entry[2]})
		}
	}

	storedMetadata := map[string]any{
		"id":             gh0_parseIntOrNil(movieCopy.TmdbID),
		"title":          movieCopy.Name,
		"original_title": movieCopy.OriginalTitle,
		"overview":       movieCopy.Overview,
		"poster_path":    movieCopy.PosterPath,
		"release_date":   movieCopy.Year,
		"source":         gh0_metadataSource(movieCopy.TvdbID),
	}
	hasStoredMetadata := movieCopy.TmdbID != "" || movieCopy.TvdbID != "" || movieCopy.Overview != "" || movieCopy.PosterPath != ""

	var metadata any
	cast := []CastMember{}
	if cfg.TmdbAPIKey != nil {
		key := *cfg.TmdbAPIKey
		tmdb := NewTmdbClientWithLanguage(&key, cfg.TmdbLanguage())
		if tmdbID, err := strconv.ParseInt(strings.TrimSpace(movieCopy.TmdbID), 10, 64); err == nil {
			details, err := tmdb.MovieDetails(r.Context(), strconv.FormatInt(tmdbID, 10))
			if err == nil {
				metadata = map[string]any{
					"id":             details.ID,
					"title":          details.Title,
					"original_title": details.OriginalTitle,
					"overview":       details.Overview,
					"poster_path":    details.PosterPath,
					"release_date":   details.ReleaseDate,
					"source":         "tmdb",
				}
			} else {
				metadata = storedMetadata
			}
		} else if hasStoredMetadata {
			metadata = storedMetadata
		} else {
			item, err := tmdb.SearchMovie(r.Context(), movieCopy.Name, gh0_parseIntPtr(movieCopy.Year))
			if err == nil && item != nil {
				metadata = gh0_toMap(item)
			}
		}

		var creditID *int64
		if value, err := strconv.ParseInt(strings.TrimSpace(movieCopy.TmdbID), 10, 64); err == nil {
			creditID = &value
		} else if metadataMap, ok := metadata.(map[string]any); ok {
			if value, ok := gh0_idFromAny(metadataMap["id"]); ok {
				creditID = &value
			}
		}
		if creditID != nil {
			if credits, err := tmdb.MovieCredits(r.Context(), strconv.FormatInt(*creditID, 10)); err == nil {
				cast = credits
			}
		}
	} else {
		metadata = storedMetadata
	}

	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":       true,
		"movie":    movieCopy,
		"history":  history,
		"metadata": metadata,
		"cast":     cast,
		"matches":  matches,
	})
}

func gh0_metadataSource(tvdbID string) string {
	if tvdbID == "" {
		return "tmdb"
	}
	return "tvdb"
}

// ---------------------------------------------------------------------------
// db_action
// ---------------------------------------------------------------------------

func DbAction(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input DbActionInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action != "vacuum" && action != "analyze" {
		jsonError(w, http.StatusBadRequest, "action must be vacuum or analyze")
		return
	}
	beforeSize, beforeRows, afterSize, afterRows, err := gh0_runDbAction(s, action)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":     true,
		"action": action,
		"before": map[string]any{"size_bytes": beforeSize, "rows": beforeRows},
		"after":  map[string]any{"size_bytes": afterSize, "rows": afterRows},
	})
}

func gh0_runDbAction(s *AppState, action string) (int64, int64, int64, int64, error) {
	configPath := filepath.Join(s.cfg.DataDir, "gextto_config.db")
	configSize := func(path string) int64 {
		conn, err := OpenConfigDB(path)
		if err != nil {
			return 0
		}
		defer conn.Close()
		return ConnectionSizeBytes(conn)
	}

	beforeSize := s.db.DBSizeBytes()
	beforeRows := s.db.DBTotalRows()
	beforeSize += s.archive.SizeBytes()
	if count, err := s.archive.Count(); err == nil {
		beforeRows += count
	}
	beforeSize += s.comics.SizeBytes() + configSize(configPath)

	if err := s.db.Optimize(action); err != nil {
		return 0, 0, 0, 0, err
	}
	if err := s.archive.Optimize(action); err != nil {
		return 0, 0, 0, 0, err
	}
	if err := s.comics.Optimize(action); err != nil {
		return 0, 0, 0, 0, err
	}
	if conn, err := OpenConfigDB(configPath); err == nil {
		_ = OptimizeConnection(conn, action)
		conn.Close()
	}

	afterSize := s.db.DBSizeBytes()
	afterRows := s.db.DBTotalRows()
	afterSize += s.archive.SizeBytes()
	if count, err := s.archive.Count(); err == nil {
		afterRows += count
	}
	afterSize += s.comics.SizeBytes() + configSize(configPath)
	return beforeSize, beforeRows, afterSize, afterRows, nil
}

// ---------------------------------------------------------------------------
// save_simkl_settings
// ---------------------------------------------------------------------------

func SaveSimklSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SettingsPatch
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, body := gh0_saveProviderSettings(s, input, []string{
		"simkl_client_id",
		"simkl_watchlist_status",
		"simkl_calendar_days",
		"simkl_mark_watched",
	})
	jsonStatus(w, status, body)
}

func gh0_settingsValue(value any) (string, bool) {
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
	case int:
		return strconv.Itoa(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case json.Number:
		return typed.String(), true
	}
	return "", false
}

func gh0_saveProviderSettings(s *AppState, input SettingsPatch, allowed []string) (int, map[string]any) {
	keys := make([]string, 0, len(input.Values))
	for key := range input.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	saved := []string{}
	for _, key := range keys {
		value := input.Values[key]
		if value == nil || !gh0_containsString(allowed, key) {
			continue
		}
		encoded, ok := gh0_settingsValue(value)
		if !ok {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid setting value"}
		}
		if len(encoded) > 4096 {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": "setting too long"}
		}
		if err := SaveSetting(s.cfg.DataDir, key, encoded); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
		saved = append(saved, key)
	}
	return http.StatusOK, map[string]any{"ok": true, "saved": saved}
}

// ---------------------------------------------------------------------------
// trakt_calendar
// ---------------------------------------------------------------------------

func TraktCalendar(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := (&TraktClient{}).FromSettings(cfg.Settings)
	if !client.Configured() || !client.Authenticated() {
		jsonError(w, http.StatusConflict, "Trakt non configurato")
		return
	}
	days := int64(7)
	if raw, ok := cfg.Settings["trakt_calendar_days"]; ok {
		if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); parseErr == nil {
			days = parsed
		}
	}
	value, err := client.Calendar(r.Context(), days)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, value)
}

// ---------------------------------------------------------------------------
// simkl_auth_revoke
// ---------------------------------------------------------------------------

func SimklAuthRevoke(w http.ResponseWriter, r *http.Request, s *AppState) {
	status, body := gh0_revokeIntegrationTokens(s, []string{"simkl_access_token"})
	jsonStatus(w, status, body)
}

func gh0_revokeIntegrationTokens(s *AppState, keys []string) (int, map[string]any) {
	if s.cfg.DryRun {
		return http.StatusConflict, map[string]any{"ok": false, "error": "dry-run does not modify integration tokens"}
	}
	for _, key := range keys {
		if err := SaveSetting(s.cfg.DataDir, key, ""); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
	}
	return http.StatusOK, map[string]any{"ok": true, "authenticated": false}
}

// ---------------------------------------------------------------------------
// recent_downloads
// ---------------------------------------------------------------------------

func RecentDownloads(w http.ResponseWriter, r *http.Request, s *AppState) {
	if cached, ok := cacheGet("recent_downloads", 30*time.Second); ok {
		jsonResponse(w, cached)
		return
	}
	items, err := s.db.RecentDownloads(100)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg := latestConfig(s)
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())

	out := make([]map[string]any, len(items))
	var wg sync.WaitGroup
	for index := range items {
		wg.Add(1)
		go func(position int, item RecentDownload) {
			defer wg.Done()
			value := gh0_toMap(item)
			var poster *string
			if item.Kind == "series" {
				if found, err := tmdb.PosterForSeries(r.Context(), item.Name); err == nil {
					poster = found
				}
			} else if movie, err := tmdb.SearchMovie(r.Context(), item.Name, item.Year); err == nil && movie != nil {
				poster = movie.PosterPath
			}
			if poster != nil {
				value["poster"] = fmt.Sprintf("https://image.tmdb.org/t/p/w154%s", *poster)
			}
			out[position] = value
		}(index, items[index])
	}
	wg.Wait()

	ordered := make([]any, 0, len(out))
	for _, value := range out {
		ordered = append(ordered, value)
	}
	response := map[string]any{"ok": true, "items": ordered}
	cachePut("recent_downloads", response)
	jsonResponse(w, response)
}

// ---------------------------------------------------------------------------
// delete_episode
// ---------------------------------------------------------------------------

func DeleteEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := pathParam(r, "series")
	season, _ := pathInt(r, "season")
	episode, _ := pathInt(r, "episode")
	if err := s.db.ResetEpisode(series, season, episode, true); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "deleted": true})
}

// ---------------------------------------------------------------------------
// search_missing
// ---------------------------------------------------------------------------

func SearchMissing(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input MissingSearchInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Series) == "" || input.Season < 1 || input.Episode < 1 {
		jsonError(w, http.StatusBadRequest, "invalid missing episode")
		return
	}
	cfg := latestConfig(s)
	series := cfg.FindSeriesByName(input.Series)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	if containsInt64(series.IgnoredSeasons, input.Season) || !SeasonAllowedForScan(series.Seasons, input.Season) {
		jsonError(w, http.StatusUnprocessableEntity, "season is not monitored")
		return
	}
	query := fmt.Sprintf("%s S%02dE%02d", strings.TrimSpace(input.Series), input.Season, input.Episode)
	results := s.engine.SearchSeriesEpisode(r.Context(), cfg, series, input.Season, input.Episode, true)
	if entries, err := s.archive.Search(query); err == nil {
		for _, entry := range entries {
			if release := ParseRelease(entry[0], entry[1], "archive:"+entry[2]); release != nil {
				results = append(results, *release)
			}
		}
	}
	seen := map[string]bool{}
	filtered := []models.Release{}
	for i := range results {
		release := results[i]
		if !gh0_releaseMatchesSeriesEpisode(release, series, input.Season, input.Episode) {
			continue
		}
		if !cfg.ReleaseAllowed(&release) {
			continue
		}
		if !cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title) {
			continue
		}
		key, ok := releaseDedupKey(&release)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		filtered = append(filtered, release)
	}
	sort.SliceStable(filtered, func(a, b int) bool {
		return s.cfg.ReleaseScore(&filtered[a]) > s.cfg.ReleaseScore(&filtered[b])
	})
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "query": query, "results": filtered})
}

func gh0_releaseMatchesSeriesEpisode(release models.Release, series *SeriesConfig, season, episode int64) bool {
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
	} else {
		found := false
		for _, value := range release.EpisodeRange {
			if value == episode || value == 0 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
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

// ---------------------------------------------------------------------------
// calendar
// ---------------------------------------------------------------------------

func Calendar(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if cfg.TmdbAPIKey == nil {
		jsonError(w, http.StatusConflict, "TMDB API key is not configured")
		return
	}
	if cached, ok := cacheGet("calendar", 120*time.Second); ok {
		jsonResponse(w, cached)
		return
	}
	response := gh0_buildCalendar(r.Context(), cfg)
	cachePut("calendar", response)
	jsonStatus(w, http.StatusOK, response)
}

func gh0_buildCalendar(ctx context.Context, cfg *Config) map[string]any {
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	enabled := []SeriesConfig{}
	for _, series := range cfg.Series {
		if series.Enabled {
			enabled = append(enabled, series)
		}
	}
	items := make([]map[string]any, len(enabled))
	var wg sync.WaitGroup
	for index := range enabled {
		wg.Add(1)
		go func(position int, series SeriesConfig) {
			defer wg.Done()
			var tmdbID *string
			if strings.TrimSpace(series.TmdbID) == "" {
				resolved, err := tmdb.ResolveSeriesID(ctx, series.Name)
				if err != nil {
					return
				}
				tmdbID = resolved
			} else {
				value := series.TmdbID
				tmdbID = &value
			}
			if tmdbID == nil {
				return
			}
			episode, err := tmdb.NextEpisode(ctx, *tmdbID)
			if err != nil {
				logging.Debug("TMDB calendar lookup failed", "series", series.Name, "error", err)
				return
			}
			if episode == nil {
				return
			}
			var poster *string
			if path, err := tmdb.PosterForSeries(ctx, series.Name); err == nil && path != nil {
				value := fmt.Sprintf("https://image.tmdb.org/t/p/w154%s", *path)
				poster = &value
			}
			items[position] = map[string]any{
				"series":  series.Name,
				"tmdb_id": *tmdbID,
				"tvdb_id": strings.TrimSpace(series.TvdbID),
				"episode": gh0_toMap(episode),
				"poster":  poster,
			}
		}(index, enabled[index])
	}
	wg.Wait()

	out := []any{}
	for _, item := range items {
		if item != nil {
			out = append(out, item)
		}
	}
	gh0_sortCalendarItems(out)
	return map[string]any{"ok": true, "items": out}
}

func gh0_sortCalendarItems(items []any) {
	airDate := func(value any) string {
		entry, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		episode, ok := entry["episode"].(map[string]any)
		if !ok {
			return ""
		}
		text, ok := episode["air_date"].(string)
		if !ok {
			return ""
		}
		return text
	}
	sort.SliceStable(items, func(a, b int) bool {
		left := airDate(items[a])
		right := airDate(items[b])
		if left == "" && right == "" {
			return false
		}
		if left == "" {
			return false
		}
		if right == "" {
			return true
		}
		return left < right
	})
}

// ---------------------------------------------------------------------------
// comic_download
// ---------------------------------------------------------------------------

func ComicDownloadHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "download manuali disabilitati in dry-run")
		return
	}
	var input ComicDownloadInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Url) == "" || strings.TrimSpace(input.Title) == "" {
		jsonError(w, http.StatusBadRequest, "url e titolo sono obbligatori")
		return
	}
	cfg := latestConfig(s)
	monitoredPaths := []string{}
	if monitored, err := s.comics.ListMonitored(false); err == nil {
		for _, comic := range monitored {
			if strings.TrimSpace(comic.SavePath) != "" {
				monitoredPaths = append(monitoredPaths, comic.SavePath)
			}
		}
	}
	comicDir, hasComicDir := ComicDownloadDir(cfg)
	target := cfg.LibtorrentDir
	if strings.TrimSpace(input.SavePath) == "" {
		if hasComicDir {
			target = comicDir
		}
	} else {
		target = strings.TrimSpace(input.SavePath)
	}
	allowed := pathStartsWith(target, cfg.DataDir) ||
		pathStartsWith(target, cfg.LibtorrentDir) ||
		(hasComicDir && pathStartsWith(target, comicDir)) ||
		gh0_anyPathStartsWith(target, monitoredPaths)
	if !allowed {
		jsonError(w, http.StatusForbidden, "percorso comics non configurato")
		return
	}
	_ = os.MkdirAll(target, 0o755)

	client := NewGetComicsClient()
	method := strings.ToLower(input.Method)
	var result any
	var runErr error
	switch method {
	case "download_now", "direct", "http":
		historyURL := strings.TrimSpace(input.PostUrl)
		if historyURL == "" {
			historyURL = strings.TrimSpace(input.Url)
		}
		id, err := client.StartDirectDownloadWithCompletion(strings.TrimSpace(input.Url), target, strings.TrimSpace(input.Title), func(path string) {
			if s.comics == nil || historyURL == "" {
				return
			}
			if _, historyErr := s.comics.AddHistory(0, historyURL, strings.TrimSpace(input.Title), "", ""); historyErr != nil {
				logging.Warn("comic manual download history record failed", "title", input.Title, "error", historyErr)
			}
		})
		if err != nil {
			runErr = err
		} else {
			result = map[string]any{"id": id, "method": "http", "status": "downloading"}
		}
	case "mega":
		executable := os.Getenv("GEXTTO_MEGADL")
		if executable == "" {
			executable = "megadl"
		}
		resolved, err := client.ResolveMega(strings.TrimSpace(input.Url))
		if err != nil {
			runErr = err
		} else if path, err := DownloadMega(executable, resolved, target); err != nil {
			runErr = err
		} else {
			result = map[string]any{"path": path, "method": "mega"}
		}
	case "torrent", "torrents":
		path, err := client.DownloadTorrent(strings.TrimSpace(input.Url), target)
		if err != nil {
			runErr = err
			break
		}
		hash, err := s.activeEngine().AddTorrentFile(path, target)
		if err != nil {
			runErr = err
			break
		}
		if hash == nil {
			runErr = fmt.Errorf("torrent non aggiunto")
			break
		}
		_ = os.Remove(path)
		_ = s.db.SetTorrentTag(*hash, "Comic")
		if strings.TrimSpace(input.PostUrl) != "" {
			_ = s.comics.AddTorrent(*hash, strings.TrimSpace(input.PostUrl), strings.TrimSpace(input.Title), target)
		}
		result = map[string]any{"hash": *hash, "method": "torrent"}
	case "magnet", "magnets":
		ok, err := s.activeEngine().AddWithPath(input.Url, cfg, &target)
		if err != nil {
			runErr = err
			break
		}
		if !ok {
			runErr = fmt.Errorf("magnet duplicato")
			break
		}
		if hash, ok := utils.MagnetHash(input.Url); ok {
			_ = s.db.SetTorrentTag(hash, "Comic")
			_ = s.comics.AddTorrent(hash, strings.TrimSpace(input.PostUrl), strings.TrimSpace(input.Title), target)
		}
		result = map[string]any{"method": "magnet"}
	default:
		runErr = fmt.Errorf("metodo comics non supportato")
	}
	if runErr != nil {
		jsonError(w, http.StatusBadGateway, runErr.Error())
		return
	}
	if method != "download_now" && method != "direct" && method != "http" && s.comics != nil {
		historyURL := strings.TrimSpace(input.PostUrl)
		if historyURL == "" {
			historyURL = strings.TrimSpace(input.Url)
		}
		if historyURL != "" {
			if _, err := s.comics.AddHistory(0, historyURL, strings.TrimSpace(input.Title), "", ""); err != nil {
				logging.Warn("comic manual download history record failed", "title", input.Title, "error", err)
			}
		}
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true, "result": result})
}

// ---------------------------------------------------------------------------
// comic_weekly_links
// ---------------------------------------------------------------------------

func ComicWeeklyLinks(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ComicWeeklyInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !gh0_weeklyDatePattern.MatchString(strings.TrimSpace(input.Date)) {
		jsonError(w, http.StatusBadRequest, "data weekly non valida")
		return
	}
	url, links, err := NewGetComicsClient().WeeklyLinks(strings.TrimSpace(input.Date))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "post_url": url, "links": links, "date": input.Date})
}

// ---------------------------------------------------------------------------
// make_directory
// ---------------------------------------------------------------------------

func MakeDirectory(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input gh0_mkdirInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := strings.TrimSpace(input.Path)
	if path == "" || strings.ContainsRune(path, '\x00') {
		jsonError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "path": path})
}

// ---------------------------------------------------------------------------
// torrent_stats
// ---------------------------------------------------------------------------

func TorrentStats(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{"ok": true, "stats": s.activeEngine().Stats()})
}

// ---------------------------------------------------------------------------
// add_archive_entry
// ---------------------------------------------------------------------------

func AddArchiveEntry(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ArchiveAddInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	source := "archive"
	if strings.TrimSpace(input.Source) != "" {
		source = input.Source
	}
	release := ParseRelease(input.Title, input.Magnet, source)
	if release == nil {
		status, body := gh0_addRawMagnet(r.Context(), s, input.Magnet)
		jsonStatus(w, status, body)
		return
	}
	status, body := gh0_addRelease(r.Context(), s, *release)
	jsonStatus(w, status, body)
}

func gh0_setupComplete(cfg *Config) bool {
	info, err := os.Stat(filepath.Join(cfg.DataDir, ".gextto-setup.json"))
	return err == nil && !info.IsDir()
}

func gh0_isTorrentURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func gh0_sourceIsUsable(source string) bool {
	source = strings.TrimSpace(source)
	if gh0_isTorrentURL(source) {
		return true
	}
	if !strings.HasPrefix(source, "magnet:") {
		return false
	}
	_, ok := utils.MagnetHash(source)
	return ok
}

func gh0_downloadAndAdd(ctx context.Context, s *AppState, url string) (*string, string) {
	requestCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	body, status, err := HTTPGetBytes(requestCtx, url, map[string]string{"User-Agent": "gextto/0.1"})
	if err != nil {
		return nil, err.Error()
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Sprintf("HTTP status %d", status)
	}
	path := filepath.Join(s.cfg.StateDir, fmt.Sprintf(".manual-%d.torrent", time.Now().UnixNano()))
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return nil, err.Error()
	}
	result, err := s.activeEngine().AddTorrentFileWithOptions(path, s.cfg, nil, AddOptions{})
	_ = os.Remove(path)
	if err != nil {
		return nil, err.Error()
	}
	return result, ""
}

func gh0_addParsedRelease(ctx context.Context, s *AppState, release models.Release) (int, map[string]any) {
	source := strings.TrimSpace(release.Magnet)
	if gh0_isTorrentURL(source) {
		hash, errMessage := gh0_downloadAndAdd(ctx, s, source)
		if errMessage != "" {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": errMessage}
		}
		if hash == nil {
			return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
		}
		release.Magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s", *hash)
		score := latestConfig(s).ReleaseScore(&release)
		if err := s.db.RegisterTorrentScored(&release, score); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
		_ = s.db.SetTorrentReason(*hash, "manual")
		return http.StatusAccepted, map[string]any{"ok": true, "title": release.Title}
	}

	score := latestConfig(s).ReleaseScore(&release)
	if err := s.db.RegisterTorrentScored(&release, score); err != nil {
		return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
	}
	if hash, ok := utils.MagnetHash(release.Magnet); ok {
		_ = s.db.SetTorrentReason(hash, "manual")
	}
	title := release.Title
	engine := s.activeEngine()
	cfg := s.cfg
	go func() {
		added, err := engine.Add(source, cfg)
		if err != nil {
			logging.Error("manual torrent add failed", "title", title, "error", err)
		} else if added {
			logging.Info("manual torrent queued", "title", title)
		} else {
			logging.Info("manual torrent already queued", "title", title)
		}
	}()
	return http.StatusAccepted, map[string]any{"ok": true, "title": release.Title, "queued": true}
}

func gh0_addRelease(ctx context.Context, s *AppState, release models.Release) (int, map[string]any) {
	if !gh0_setupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	isURL := gh0_isTorrentURL(release.Magnet)
	status, body := gh0_addParsedRelease(ctx, s, release)
	if isURL && status == http.StatusBadRequest {
		if found := gh0_resolveBySearch(ctx, s, release.Title); found != nil {
			retryStatus, retryBody := gh0_addParsedRelease(ctx, s, *found)
			if retryStatus != http.StatusBadRequest {
				return retryStatus, retryBody
			}
		}
	}
	return status, body
}

func gh0_addRawMagnet(ctx context.Context, s *AppState, source string) (int, map[string]any) {
	if !gh0_setupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	trimmed := strings.TrimSpace(source)
	if gh0_isTorrentURL(trimmed) {
		hash, errMessage := gh0_downloadAndAdd(ctx, s, trimmed)
		if errMessage != "" {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": errMessage}
		}
		if hash == nil {
			return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
		}
		return http.StatusAccepted, map[string]any{"ok": true}
	}
	added, err := s.activeEngine().Add(trimmed, s.cfg)
	if err != nil {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
	}
	if !added {
		return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
	}
	return http.StatusAccepted, map[string]any{"ok": true}
}

func gh0_resolveBySearch(ctx context.Context, s *AppState, title string) *models.Release {
	query := gh0_searchQueryFromTitle(title)
	normalizedQuery := gh0_normalizeSearch(query)
	if normalizedQuery == "" {
		return nil
	}
	results := s.engine.SearchQueryManual(ctx, s.cfg, query)
	for i := range results {
		if gh0_sourceIsUsable(results[i].Magnet) && strings.HasPrefix(gh0_normalizeSearch(results[i].Title), normalizedQuery) {
			return &results[i]
		}
	}
	return nil
}

func gh0_searchQueryFromTitle(title string) string {
	cleaned := []rune{}
	beforeBracket := strings.SplitN(title, "[", 2)[0]
	for _, character := range beforeBracket {
		if (character >= '0' && character <= '9') || (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') {
			cleaned = append(cleaned, character)
		} else {
			cleaned = append(cleaned, ' ')
		}
	}
	words := []string{}
	for _, word := range strings.Fields(string(cleaned)) {
		lower := strings.ToLower(word)
		isYear := len(word) == 4
		if isYear {
			for _, character := range word {
				if character < '0' || character > '9' {
					isYear = false
					break
				}
			}
		}
		isMarker := strings.Contains(lower, "1080p") ||
			strings.Contains(lower, "720p") ||
			strings.Contains(lower, "2160p") ||
			strings.Contains(lower, "480p") ||
			gh0_isQualityMarker(lower)
		if isMarker {
			break
		}
		words = append(words, word)
		if isYear || len(words) >= 6 {
			break
		}
	}
	return strings.Join(words, " ")
}

func gh0_isQualityMarker(lower string) bool {
	switch lower {
	case "4k", "uhd", "bluray", "bdrip", "web", "webdl", "webrip", "hdtv", "dvdrip",
		"h264", "h265", "x264", "x265", "hevc", "avc", "aac", "ac3", "eac3", "ddp",
		"dts", "proper", "repack":
		return true
	}
	return false
}

func gh0_normalizeSearch(value string) string {
	beforeBracket := strings.ToLower(strings.SplitN(value, "[", 2)[0])
	cleaned := []rune{}
	for _, character := range beforeBracket {
		if (character >= '0' && character <= '9') || (character >= 'a' && character <= 'z') {
			cleaned = append(cleaned, character)
		} else {
			cleaned = append(cleaned, ' ')
		}
	}
	return strings.Join(strings.Fields(string(cleaned)), " ")
}

// ---------------------------------------------------------------------------
// backfill_media_info
// ---------------------------------------------------------------------------

func BackfillMediaInfo(w http.ResponseWriter, r *http.Request, s *AppState) {
	limit := 100
	var input gh0_backfillMediaInfoInput
	if has, err := gh0_decodeOptional(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	} else if has && input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 5000 {
		limit = 5000
	}

	// The backfill runs ffprobe on many files and used to block the request.
	// It is now a background job: the request returns 202 immediately while the
	// detailed report stays available on /api/jobs/{id} (job result).
	job, created := s.jobs.Create("media-info-backfill", "media-info-backfill")
	if !created {
		jsonError(w, http.StatusConflict, "a MediaInfo backfill is already running")
		return
	}
	if err := s.jobs.Start(job.ID, func(ctx context.Context, jobID string) {
		report, err := gh0_runMediaInfoBackfill(ctx, s, limit, func(done, total int) {
			if total > 0 {
				s.jobs.SetProgress(jobID, float64(done)/float64(total), "")
			}
		})
		if err != nil {
			s.jobs.Fail(jobID, err)
			return
		}
		s.jobs.SetResult(jobID, report)
		logging.Info("media info backfill completed", "job_id", jobID)
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"job_id":  job.ID,
		"message": "Aggiornamento MediaInfo avviato",
	})
}

func gh0_logMediaInfoFailure(target MediaInfoBackfillTarget, reason string, items *[]any) {
	logging.Warn("🔬 MediaInfo backfill: file non analizzato",
		"path", target.Path, "kind", target.Kind, "series", target.Series,
		"season", target.Season, "episode", target.Episode, "name", target.Name,
		"year", target.Year, "reason", reason)
	*items = append(*items, map[string]any{
		"path":    target.Path,
		"kind":    target.Kind,
		"series":  target.Series,
		"season":  target.Season,
		"episode": target.Episode,
		"name":    target.Name,
		"year":    target.Year,
		"reason":  reason,
	})
}

func gh0_runMediaInfoBackfill(ctx context.Context, s *AppState, limit int, onProgress func(done, total int)) (map[string]any, error) {
	targets, err := s.db.MediaInfoBackfillTargets(limit)
	if err != nil {
		return nil, err
	}
	total := len(targets)
	probed := 0
	failed := 0
	skipped := 0
	failedItems := []any{}
	missingItems := []any{}
	missingFiles := []string{}
	for index, target := range targets {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		if onProgress != nil {
			onProgress(index, total)
		}
		if _, err := os.Stat(target.Path); err != nil {
			skipped++
			reason := "file non esistente"
			if !os.IsNotExist(err) {
				reason = "errore di accesso: " + err.Error()
			}
			missingFiles = append(missingFiles, target.Path+" ("+reason+")")
			missingItems = append(missingItems, map[string]any{
				"path":    target.Path,
				"kind":    target.Kind,
				"series":  target.Series,
				"season":  target.Season,
				"episode": target.Episode,
				"name":    target.Name,
				"year":    target.Year,
				"reason":  reason,
			})
			logging.Debug("media info backfill: file assente, salto",
				"kind", target.Kind, "series", target.Series, "season", target.Season,
				"episode", target.Episode, "name", target.Name, "path", target.Path)
			continue
		}
		info, probeErr := ProbeBestResult(target.Path)
		if probeErr != nil {
			failed++
			gh0_logMediaInfoFailure(target, probeErr.Error(), &failedItems)
			continue
		}
		encoded, err := json.Marshal(info)
		if err != nil {
			return nil, err
		}
		var updated int
		if target.Kind == "movie" {
			updated, err = s.db.SetMovieMediaInfo(target.Name, target.Year, string(encoded))
		} else {
			updated, err = s.db.SetEpisodeMediaInfo(target.Series, gh0_derefI64(target.Season), gh0_derefI64(target.Episode), string(encoded))
		}
		if err != nil {
			return nil, err
		}
		if updated > 0 {
			probed++
			logging.Debug("media info backfill: probed",
				"kind", target.Kind, "series", target.Series, "season", target.Season,
				"episode", target.Episode, "name", target.Name, "resolution", info.Resolution(),
				"hdr", info.HDR, "bit_depth", info.BitDepth)
		} else {
			failed++
			gh0_logMediaInfoFailure(target, "nessuna riga corrispondente nel database (episodio o film non trovato)", &failedItems)
		}
	}
	return map[string]any{
		"ok":            true,
		"candidates":    len(targets),
		"total":         total,
		"canceled":      ctx != nil && ctx.Err() != nil,
		"probed":        probed,
		"analyzed":      probed,
		"failed":        failed,
		"skipped":       skipped,
		"failed_items":  failedItems,
		"missing_items": missingItems,
		"missing_files": missingFiles,
	}, nil
}

// ---------------------------------------------------------------------------
// run_housekeeping_now
// ---------------------------------------------------------------------------

func RunHousekeepingNow(w http.ResponseWriter, r *http.Request, s *AppState) {
	report, ok := gh0_runHousekeeping(s)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "housekeeping failed")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "report": report})
}

func gh0_runHousekeeping(s *AppState) (*HousekeepingReport, bool) {
	cfg := latestConfig(s)
	params := HousekeepingParams{}.FromSettings(cfg.Settings)
	report, err := s.db.Housekeeping(params)
	if err != nil {
		logging.Warn("housekeeping failed", "error", err)
		return nil, false
	}
	if _, _, _, _, err := gh0_runDbAction(s, "vacuum"); err != nil {
		logging.Warn("housekeeping failed", "error", err)
		return nil, false
	}
	return &report, true
}

// ---------------------------------------------------------------------------
// tvdb_series
// ---------------------------------------------------------------------------

func TvdbSeries(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := WithLanguage(cfg.TvdbAPIKey(), cfg.TvdbLanguage())
	if !client.Configured() {
		jsonError(w, http.StatusPreconditionRequired, "TVDB API key non configurata")
		return
	}
	id, _ := pathInt(r, "id")
	series, err := client.SeriesExtended(r.Context(), id)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "series": series})
}

// ---------------------------------------------------------------------------
// set_file_priorities
// ---------------------------------------------------------------------------

func SetFilePriorities(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input gh0_filePrioritiesInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Priorities) > 100000 {
		jsonError(w, http.StatusBadRequest, "too many file priorities")
		return
	}
	ok, err := s.activeEngine().SetFilePriorities(hash, input.Priorities)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusConflict, "torrent unavailable in dry-run")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// export_torrent
// ---------------------------------------------------------------------------

func ExportTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	path, ok := s.activeEngine().TorrentFilePath(hash)
	if !ok {
		jsonError(w, http.StatusNotFound, "torrent file not available")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------------------
// remove_torrent_legacy
// ---------------------------------------------------------------------------

func RemoveTorrentLegacy(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RemoveTorrentInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	removed, err := SafeRemoveTorrent(s, cfg, input.Hash, input.DeleteFiles)
	status, body := gh0_torrentAction(removed, err)
	jsonStatus(w, status, body)
}

func gh0_torrentFilesAreDisposable(db *Database, hash string) bool {
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return false
	}
	if !meta.Release.IsPack {
		return false
	}
	status, err := db.TorrentStatus(hash)
	if err != nil || status == nil {
		return false
	}
	return *status == "completed"
}

func gh0_torrentAction(ok bool, err error) (int, map[string]any) {
	if err != nil {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
	}
	if !ok {
		return http.StatusNotFound, map[string]any{"ok": false, "error": "torrent unavailable in dry-run"}
	}
	return http.StatusOK, map[string]any{"ok": true}
}

// ---------------------------------------------------------------------------
// jellyfin_refresh
// ---------------------------------------------------------------------------

func JellyfinRefresh(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	url := cfg.Settings["jellyfin_url"]
	key := cfg.Settings["jellyfin_api_key"]
	if strings.TrimSpace(url) == "" || strings.TrimSpace(key) == "" {
		jsonError(w, http.StatusBadRequest, "Jellyfin non configurato")
		return
	}
	endpoint := strings.TrimRight(url, "/") + "/Library/Refresh"
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	headers := map[string]string{
		"Authorization": fmt.Sprintf("MediaBrowser Token=\"%s\"", key),
		"X-Emby-Token":  key,
	}
	response, err := HTTPRequest(ctx, http.MethodPost, endpoint, headers, nil, "")
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	jsonError(w, http.StatusBadGateway, "HTTP "+response.Status)
}

// ---------------------------------------------------------------------------
// optimize_libtorrent_settings
// ---------------------------------------------------------------------------

type gh0_libtorrentChange struct {
	key   string
	value string
}

type gh0_libtorrentOptimization struct {
	changes      []gh0_libtorrentChange
	memoryMB     uint64
	cacheSize    int64
	cacheMB      int64
	queueMB      int64
	sendBufferKB int64
	peerList     int64
}

func gh0_libtorrentOptimizationFor(cfg *Config) gh0_libtorrentOptimization {
	trash := filepath.Join(cfg.DataDir, "trash")
	if cfg.TrashPath != nil {
		trash = *cfg.TrashPath
	}
	var ramdisk *string
	if value, ok := cfg.Settings["libtorrent_ramdisk_dir"]; ok {
		copied := value
		ramdisk = &copied
	}
	health := CheckWithPaths(&HealthPaths{
		DataDir:      cfg.DataDir,
		TrashPath:    trash,
		DownloadPath: cfg.LibtorrentDir,
		ArchiveRoot:  gh0_derefStr(cfg.ArchiveRoot),
		RamdiskPath:  gh0_derefStr(ramdisk),
	})
	memoryMB := health.MemoryTotalBytes / (1024 * 1024)

	var queueMB, sendBufferKB, peerList int64
	if memoryMB > 0 && memoryMB < 2048 {
		queueMB, sendBufferKB, peerList = 8, 256, 100
	} else if memoryMB < 4096 {
		queueMB, sendBufferKB, peerList = 32, 512, 200
	} else if memoryMB < 8192 {
		queueMB, sendBufferKB, peerList = 64, 1024, 300
	} else if memoryMB < 16384 {
		queueMB, sendBufferKB, peerList = 64, 1024, 500
	} else {
		queueMB, sendBufferKB, peerList = 128, 2048, 500
	}
	cacheSize := int64(-1)
	cacheMB := int64(-1)
	activeDownloads := 3
	activeSeeds := 3
	activeLimit := 5

	extraSettings := []string{}
	if raw, ok := cfg.Settings["libtorrent_extra_settings"]; ok && raw != "" {
		for _, line := range strings.Split(raw, "\n") {
			key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
			if key == "max_queued_disk_bytes" || key == "send_buffer_watermark" || key == "max_peerlist_size" {
				continue
			}
			extraSettings = append(extraSettings, line)
		}
	}
	extraSettings = append(extraSettings,
		fmt.Sprintf("max_queued_disk_bytes=%d", queueMB*1024*1024),
		fmt.Sprintf("send_buffer_watermark=%d", sendBufferKB*1024),
		fmt.Sprintf("max_peerlist_size=%d", peerList),
	)
	changes := []gh0_libtorrentChange{
		{"libtorrent_active_downloads", strconv.Itoa(activeDownloads)},
		{"libtorrent_active_seeds", strconv.Itoa(activeSeeds)},
		{"libtorrent_active_limit", strconv.Itoa(activeLimit)},
		{"libtorrent_dynamic_queue", "yes"},
		{"libtorrent_dynamic_queue_min", "1"},
		{"libtorrent_dynamic_queue_max", "10"},
		{"libtorrent_dont_count_slow_torrents", "yes"},
		{"libtorrent_cache_size", strconv.FormatInt(cacheSize, 10)},
		{"libtorrent_cache_expiry", "300"},
		{"libtorrent_extra_settings", strings.Join(extraSettings, "\n")},
	}
	return gh0_libtorrentOptimization{
		changes:      changes,
		memoryMB:     memoryMB,
		cacheSize:    cacheSize,
		cacheMB:      cacheMB,
		queueMB:      queueMB,
		sendBufferKB: sendBufferKB,
		peerList:     peerList,
	}
}

func OptimizeLibtorrentSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	// On qBittorrent the same operational goal is expressed with the backend's
	// own MiB-based disk cache preferences.
	if qb, ok := s.activeEngine().(*qbittorrentEngine); ok {
		result, err := qb.ApplyOptimization(latestConfig(s))
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":          true,
			"applied":     true,
			"backend":     BackendQbittorrent,
			"optimized":   result,
			"explanation": "Cache disco qBittorrent proporzionata alla RAM e limitata; le altre preferenze restano invariate.",
		})
		return
	}
	if _, err := s.requireEmbedded("optimize_settings"); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	cfg := latestConfig(s)
	optimization := gh0_libtorrentOptimizationFor(cfg)
	for _, change := range optimization.changes {
		if err := SaveSetting(s.cfg.DataDir, change.key, change.value); err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("%s: %s", change.key, err.Error()))
			return
		}
	}
	optimized := latestConfig(s)
	if _, err := s.torrents.ApplySettings(optimized); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":      true,
		"applied": true,
		"hardware": map[string]any{
			"memory_mb": optimization.memoryMB,
		},
		"settings": map[string]any{
			"active_downloads":         3,
			"active_seeds":             3,
			"active_limit":             5,
			"dynamic_queue":            true,
			"dynamic_queue_min":        1,
			"dynamic_queue_max":        10,
			"dont_count_slow_torrents": true,
			"cache_blocks":             optimization.cacheSize,
			"cache_mb":                 optimization.cacheMB,
			"queue_mb":                 optimization.queueMB,
			"send_buffer_kb":           optimization.sendBufferKB,
			"peer_list":                optimization.peerList,
		},
		"explanation":                 fmt.Sprintf("Base: 3 download, 3 seed, limite 5. Coda dinamica: active_downloads tra 1 e 10, a passi di uno, dopo campioni coerenti e con raffreddamento di 10 minuti. I torrent senza trasferimento non contano negli slot attivi. RAM rilevata: %d MB; cache: automatica libtorrent (-1), coda disco: %d MB, send-buffer: %d KiB, peer-list: %d. Connessioni e limiti globali di banda lasciati invariati.", optimization.memoryMB, optimization.queueMB, optimization.sendBufferKB, optimization.peerList),
		"bandwidth_limits_preserved":  true,
		"connections_limit_preserved": true,
	})
}

// ---------------------------------------------------------------------------
// set_torrent_limits_legacy
// ---------------------------------------------------------------------------

func SetTorrentLimitsLegacy(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TorrentLimitsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	seedRatio := -1.0
	if input.SeedRatio != nil {
		seedRatio = *input.SeedRatio
	}
	seedDays := int64(-1)
	if input.SeedDays != nil {
		seedDays = *input.SeedDays
	}
	ok, err := s.activeEngine().SetLimits(
		input.Hash,
		saturatingMulInt64(input.DlKbps, 1024),
		saturatingMulInt64(input.UlKbps, 1024),
		seedRatio,
		seedDays,
	)
	status, body := gh0_torrentAction(ok, err)
	jsonStatus(w, status, body)
}
