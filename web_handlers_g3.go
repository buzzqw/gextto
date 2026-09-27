package gextto

// Handler group 3 and 4: library, series and movie handlers.
// Handlers use the shared `web.go` helpers (`jsonResponse`, `jsonStatus`,
// `jsonError`, `pathParam`, `pathInt`, `queryParam`, `queryInt`, `decodeJSON`,
// `latestConfig`) and the input structs declared by the core port. Private
// helpers below are prefixed `gh3_` to avoid clashing with sibling handler files.

import (
	"bytes"
	"context"
	"crypto/rand"
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

// gh3ExternalSearchTimeout implements `EXTERNAL_SEARCH_TIMEOUT`.
const gh3ExternalSearchTimeout = 12 * time.Second

// gh3ManualSearchBudget implements `MANUAL_SEARCH_BUDGET`.
const gh3ManualSearchBudget = 90 * time.Second

// ---------------------------------------------------------------------------
// group3 / group4 handlers
// ---------------------------------------------------------------------------

// AddSearchResult is `add_search_result`.
func AddSearchResult(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SearchAddInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, value := gh3AddRelease(s, input.Release)
	jsonStatus(w, status, value)
}

// BackupTestFtp is `backup_test_ftp`.
func BackupTestFtp(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	var input FtpTestInput
	if !gh3DecodeOptionalJSON(w, r, &input) {
		return
	}
	pick := func(inline *string, key string) string {
		if inline != nil {
			value := strings.TrimSpace(*inline)
			if value != "" {
				return value
			}
		}
		return cfg.Settings[key]
	}
	host := pick(input.Host, "backup_ftp_host")
	user := pick(input.User, "backup_ftp_user")
	password := pick(input.Password, "backup_ftp_password")
	path := pick(input.Path, "backup_ftp_path")
	if host == "" || user == "" || password == "" {
		jsonError(w, http.StatusBadRequest, "host, utente e password FTP sono obbligatori")
		return
	}
	// Always 200 so the UI can show exactly which step succeeded/failed.
	report := TestFTP(host, user, password, path)
	logging.Info("FTP test result",
		"host", report.Host,
		"path", report.Path,
		"test_file", report.TestFile,
		"connected", report.Connected,
		"logged_in", report.LoggedIn,
		"entered_path", report.EnteredPath,
		"uploaded", report.Uploaded,
		"deleted", report.Deleted,
		"error", gh3DerefString(report.Error))
	var reportError any
	if report.Error != nil {
		reportError = *report.Error
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":           report.OK(),
		"host":         report.Host,
		"user":         report.User,
		"path":         report.Path,
		"test_file":    report.TestFile,
		"connected":    report.Connected,
		"logged_in":    report.LoggedIn,
		"entered_path": report.EnteredPath,
		"uploaded":     report.Uploaded,
		"deleted":      report.Deleted,
		"error":        reportError,
	})
}

// CleanTrash is `clean_trash`.
func CleanTrash(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not delete trash")
		return
	}
	if s.cfg.TrashPath == nil {
		jsonError(w, http.StatusConflict, "trash path is not configured")
		return
	}
	var input CleanTrashInput
	if !gh3DecodeOptionalJSON(w, r, &input) {
		return
	}
	retentionDays := int64(0)
	if !input.Force {
		if parsed, err := strconv.ParseInt(latestConfig(s).Settings["trash_retention_days"], 10, 64); err == nil {
			retentionDays = parsed
		}
		if retentionDays < 0 {
			retentionDays = 0
		}
	}
	files, byteCount, err := gh3RemoveTrashContents(*s.cfg.TrashPath, retentionDays)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":             true,
		"files":          files,
		"bytes":          byteCount,
		"retention_days": retentionDays,
	})
}

// ComicDownloadResume is `comic_download_resume`.
func ComicDownloadResume(w http.ResponseWriter, r *http.Request, s *AppState) {
	id := pathParam(r, "id")
	if ResumeHTTPDownload(id) {
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "download non in pausa"})
}

// ComicsHistory is `comics_history`.
func ComicsHistory(w http.ResponseWriter, r *http.Request, s *AppState) {
	value, err := s.comics.History(100)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": value})
}

// DbPruneByIds is `db_prune_by_ids`.
func DbPruneByIds(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input PruneByIdsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.cfg.DryRun {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "dry-run does not modify the database"})
		return
	}
	movies, series, err := s.db.PruneSeenByIDs(input.MovieSeen, input.SeriesSeen)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"movie_seen_removed":  movies,
		"series_seen_removed": series,
	})
}

// DeleteTrashEntries is `delete_trash_entries`.
func DeleteTrashEntries(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TrashDeleteInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.cfg.DryRun {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "dry-run does not delete trash"})
		return
	}
	cfg := latestConfig(s)
	root := cfg.DataDir + "/trash"
	if cfg.TrashPath != nil {
		root = *cfg.TrashPath
	}
	var names []string
	if input.All {
		names = []string{}
		if entries, err := os.ReadDir(root); err == nil {
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
		}
	} else {
		names = input.Names
	}
	if len(names) == 0 {
		jsonError(w, http.StatusBadRequest, "nessun elemento da eliminare")
		return
	}
	removed := 0
	var byteCount uint64
	errors := []string{}
	for _, name := range names {
		if strings.TrimSpace(name) == "" || !gh3SingleComponent(name) || name == "." || name == ".." {
			errors = append(errors, name)
			continue
		}
		target := filepath.Join(root, name)
		info, err := os.Lstat(target)
		if err != nil {
			errors = append(errors, name)
			continue
		}
		var size uint64
		if info.IsDir() {
			size = gh3DirectorySize(target)
		} else {
			size = uint64(info.Size())
		}
		var outcome error
		if info.IsDir() {
			outcome = os.RemoveAll(target)
		} else {
			outcome = os.Remove(target)
		}
		if outcome != nil {
			errors = append(errors, name)
			continue
		}
		removed++
		byteCount += size
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":      true,
		"removed": removed,
		"bytes":   byteCount,
		"errors":  errors,
	})
}

// ForceEpisode is `force_episode`.
func ForceEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := pathParam(r, "series")
	season, okSeason := pathInt(r, "season")
	episode, okEpisode := pathInt(r, "episode")
	if !okSeason || !okEpisode {
		jsonError(w, http.StatusBadRequest, "invalid episode path")
		return
	}
	if err := s.db.ResetEpisode(series, season, episode, false); err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "forced": true})
}

// LibraryView is `library_view`.
func LibraryView(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	statuses, err := s.db.SeriesStatuses()
	if err != nil || statuses == nil {
		statuses = map[string]string{}
	}
	seasonCounts, err := s.db.SeriesSeasonCountsBulk()
	if err != nil || seasonCounts == nil {
		seasonCounts = map[string][][2]int64{}
	}
	seriesItems := make([]any, 0, len(cfg.Series))
	for index := range cfg.Series {
		series := cfg.Series[index]
		ignoredSeasons := append([]int64(nil), series.IgnoredSeasons...)
		for _, counts := range seasonCounts[series.Name] {
			season := counts[0]
			if !SeasonAllowedForScan(series.Seasons, season) && !containsInt64(ignoredSeasons, season) {
				ignoredSeasons = append(ignoredSeasons, season)
			}
		}
		episodes, err := s.db.EpisodesForSeries(series.Name, ignoredSeasons)
		if err != nil {
			episodes = nil
		}
		total := int64(len(episodes))
		downloaded := int64(0)
		var last *string
		for i := range episodes {
			episode := episodes[i]
			if episode.Status == "downloaded" || (episode.ArchivePath != nil && *episode.ArchivePath != "") {
				downloaded++
			}
			if episode.DownloadedAt != nil {
				if last == nil || *episode.DownloadedAt > *last {
					last = episode.DownloadedAt
				}
			}
		}
		seriesItems = append(seriesItems, map[string]any{
			"name":                series.Name,
			"seasons":             series.Seasons,
			"quality":             series.Quality,
			"language":            series.Language,
			"archive_path":        series.ArchivePath,
			"enabled":             series.Enabled,
			"aliases":             series.Aliases,
			"tmdb_id":             series.TmdbID,
			"subtitle":            series.Subtitle,
			"exclude":             series.Exclude,
			"ignored_seasons":     ignoredSeasons,
			"season_subfolders":   series.SeasonSubfolders,
			"timeframe":           series.Timeframe,
			"episodes_total":      total,
			"episodes_downloaded": downloaded,
			"last_downloaded_at":  last,
			"tmdb_status":         statuses[series.Name],
		})
	}
	jsonResponse(w, map[string]any{"series": seriesItems, "movies": cfg.Movies})
}

// ManualSearchGet is `manual_search_get`.
func ManualSearchGet(w http.ResponseWriter, r *http.Request, s *AppState) {
	values := r.URL.Query()
	query := values.Get("query")
	if qs, ok := values["q"]; ok && len(qs) > 0 {
		query = qs[0]
	}
	status, value := gh3ManualSearch(s, query)
	jsonStatus(w, status, value)
}

// MovieMetadataSearch is `movie_metadata_search`.
func MovieMetadataSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	found := false
	for i := range cfg.Movies {
		if cfg.Movies[i].ID == id {
			found = true
			break
		}
	}
	if !found {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	var input MovieMetadataSearchInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, "query must contain 1-256 characters")
		return
	}
	source := strings.ToLower(strings.TrimSpace(input.Source))
	ctx, cancel := context.WithTimeout(context.Background(), gh3ExternalSearchTimeout)
	defer cancel()
	items := []any{}
	var lookupErr error
	switch source {
	case "tvdb":
		tvdb := WithLanguage(cfg.TvdbAPIKey(), cfg.TvdbLanguage())
		if !tvdb.Configured() {
			jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "TVDB API key is not configured"})
			return
		}
		items, lookupErr = tvdb.SearchMovies(ctx, query)
	case "tmdb":
		key := cfg.TmdbAPIKey
		if key == nil {
			jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "TMDB API key is not configured"})
			return
		}
		tmdb := NewTmdbClientWithLanguage(key, cfg.TmdbLanguage())
		tmdbItems, err := tmdb.SearchMovies(ctx, query)
		lookupErr = err
		for i := range tmdbItems {
			items = append(items, tmdbItems[i])
		}
	default:
		jsonError(w, http.StatusBadRequest, "unsupported metadata source")
		return
	}
	if ctx.Err() == context.DeadlineExceeded {
		jsonError(w, http.StatusGatewayTimeout, "metadata provider timed out")
		return
	}
	if lookupErr != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": lookupErr.Error()})
		return
	}
	if items == nil {
		items = []any{}
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "source": source, "items": items})
}

// PlexRefresh is `plex_refresh`.
func PlexRefresh(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	url := cfg.Settings["plex_url"]
	token := cfg.Settings["plex_token"]
	if strings.TrimSpace(url) == "" || strings.TrimSpace(token) == "" {
		jsonError(w, http.StatusBadRequest, "Plex non configurato")
		return
	}
	endpoint := strings.TrimRight(strings.TrimSpace(url), "/") + "/library/sections/all/refresh"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	response, err := HTTPGet(ctx, endpoint, map[string]string{"X-Plex-Token": token})
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": fmt.Sprintf("HTTP %d", response.StatusCode)})
}

// RedownloadMovie is `redownload_movie`.
func RedownloadMovie(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	var name string
	found := false
	for i := range cfg.Movies {
		if cfg.Movies[i].ID == id {
			name = cfg.Movies[i].Name
			found = true
			break
		}
	}
	if !found {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	reset, err := s.db.ResetMovieByName(name)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":      true,
		"reset":   reset,
		"message": "film rimesso in coda al prossimo ciclo",
	})
}

// RenameProgressView is `rename_progress_view`.
func RenameProgressView(w http.ResponseWriter, r *http.Request, s *AppState) {
	progress := *s.rename_progress
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "progress": progress})
}

// SaveEventHooks is `save_event_hooks`.
func SaveEventHooks(w http.ResponseWriter, r *http.Request, s *AppState) {
	var hooks []EventHook
	if err := decodeJSON(r, &hooks); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if message := ValidateHooks(hooks); message != "" {
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": message})
		return
	}
	if err := SaveHooks(s.cfg.DataDir, hooks); err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.notifier.ReloadHooks(hooks)
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": hooks})
}

// SaveWatchedFolders is `save_watched_folders`.
func SaveWatchedFoldersHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	var folders []WatchedFolder
	if err := decodeJSON(r, &folders); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if message := ValidateWatchedFolders(folders); message != "" {
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": message})
		return
	}
	if err := gh3SaveWatchedFolders(s.cfg.DataDir, folders); err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": folders})
}

// SeriesDetail is `series_detail`.
func SeriesDetail(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	name := pathParam(r, "name")
	series := gh3FindSeries(cfg, name)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	ignoredSeasons := append([]int64(nil), series.IgnoredSeasons...)
	seasonCounts, err := s.db.SeriesSeasonCounts(series.Name)
	if err != nil {
		seasonCounts = nil
	}
	for _, counts := range seasonCounts {
		season := counts[0]
		if !SeasonAllowedForScan(series.Seasons, season) && !containsInt64(ignoredSeasons, season) {
			ignoredSeasons = append(ignoredSeasons, season)
		}
	}
	episodes, err := s.db.EpisodesForSeries(series.Name, ignoredSeasons)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	gaps := []any{}
	archiveGaps, err := s.db.ArchiveGapsForSeries(series.Name)
	if err != nil {
		archiveGaps = nil
	}
	for _, gap := range archiveGaps {
		season := gap[0]
		if containsInt64(ignoredSeasons, season) || !SeasonAllowedForScan(series.Seasons, season) {
			continue
		}
		gaps = append(gaps, map[string]any{"season": gap[0], "episode": gap[1]})
	}
	metadata := []any{}
	for _, counts := range seasonCounts {
		metadata = append(metadata, map[string]any{"season": counts[0], "count": counts[1]})
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":       true,
		"series":   *series,
		"episodes": episodes,
		"gaps":     gaps,
		"metadata": metadata,
	})
}

// SeriesSeenGroupedView is `series_seen_grouped_view`.
func SeriesSeenGroupedView(w http.ResponseWriter, r *http.Request, s *AppState) {
	status, value := gh3SeenGrouped(s, "series", r)
	jsonStatus(w, status, value)
}

// SetSeriesPath is `set_series_path`.
func SetSeriesPath(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	var input SeriesPathInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := *latestConfig(s)
	cfg.Series = append([]SeriesConfig(nil), cfg.Series...)
	index := -1
	for i := range cfg.Series {
		if cfg.Series[i].Name == name {
			index = i
			break
		}
		for _, alias := range cfg.Series[i].Aliases {
			if alias == name {
				index = i
				break
			}
		}
		if index >= 0 {
			break
		}
	}
	if index < 0 {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	series := &cfg.Series[index]
	if input.ArchivePath != nil {
		series.ArchivePath = strings.TrimSpace(*input.ArchivePath)
	}
	if input.Timeframe != nil {
		series.Timeframe = *input.Timeframe
		if series.Timeframe < 0 {
			series.Timeframe = 0
		}
	}
	archivePath := series.ArchivePath
	timeframe := series.Timeframe
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":           true,
		"archive_path": archivePath,
		"timeframe":    timeframe,
	})
}

// SetTorrentTrackers is `set_torrent_trackers`.
func SetTorrentTrackers(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input TrackersInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Trackers) > 500 {
		jsonError(w, http.StatusBadRequest, "too many trackers")
		return
	}
	trackers := make([]TrackerEntry, 0, len(input.Trackers))
	for i := range input.Trackers {
		tracker := input.Trackers[i]
		if strings.TrimSpace(tracker.Url) == "" {
			continue
		}
		trackers = append(trackers, TrackerEntry{Tier: tracker.Tier, URL: strings.TrimSpace(tracker.Url)})
	}
	ok, err := s.torrents.SetTrackers(hash, trackers)
	if err != nil {
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !ok {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "torrent unavailable in dry-run"})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

// SimklScrobble is `simkl_scrobble`.
func SimklScrobble(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.cfg.DryRun {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "dry-run does not mark watched items"})
		return
	}
	if !(&SimklClient{}).FromSettings(latestConfig(s).Settings).Configured() {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "Simkl non configurato"})
		return
	}
	var input ScrobbleInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	client := (&SimklClient{}).FromSettings(cfg.Settings)
	value, err := client.MarkWatched(context.Background(), input.Payload)
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, value)
}

// SystemStats is `system_stats`.
func SystemStats(w http.ResponseWriter, r *http.Request, s *AppState) {
	trash := s.cfg.DataDir + "/trash"
	if s.cfg.TrashPath != nil {
		trash = *s.cfg.TrashPath
	}
	ramdisk := ""
	if value, ok := s.cfg.Settings["libtorrent_ramdisk_dir"]; ok && strings.TrimSpace(value) != "" {
		ramdisk = value
	}
	health := CheckWithPaths(&HealthPaths{
		DataDir:      s.cfg.DataDir,
		TrashPath:    trash,
		DownloadPath: s.cfg.LibtorrentDir,
		ArchiveRoot:  gh3DerefString(s.cfg.ArchiveRoot),
		RamdiskPath:  ramdisk,
	})
	jsonResponse(w, map[string]any{
		"ok":            true,
		"health":        health,
		"torrent_stats": s.torrents.Stats(),
		"last_cycle":    s.last_cycle.Snapshot(),
	})
}

// TorrentFiles is `torrent_files`.
func TorrentFiles(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	files, ok, err := s.torrents.Files(hash)
	if err != nil {
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !ok {
		jsonStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "torrent unavailable in dry-run"})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "files": files})
}

// Torrents is `torrents`.
func Torrents(w http.ResponseWriter, r *http.Request, s *AppState) {
	live := s.torrents.List()
	if len(live) == 0 && s.cfg.DryRun {
		jsonResponse(w, gh3DecorateTorrents(s, gh3DryRunSessionPreview(s)))
		return
	}
	jsonResponse(w, gh3DecorateTorrents(s, live))
}

// TraktStatus is `trakt_status`.
func TraktStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := (&TraktClient{}).FromSettings(cfg.Settings)
	jsonResponse(w, map[string]any{
		"configured":    client.Configured(),
		"authenticated": client.Authenticated(),
	})
}

// UploadTorrent is `upload_torrent`.
func UploadTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	if !gh3SetupComplete(s.cfg) {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"})
		return
	}
	if s.cfg.DryRun {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "dry-run does not add torrents"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(body) == 0 || len(body) > 5_000_000 {
		jsonError(w, http.StatusBadRequest, "torrent file is empty or too large")
		return
	}
	cfg := latestConfig(s)
	dir := cfg.DataDir + "/incomplete"
	if cfg.LibtorrentTempDir != nil {
		dir = *cfg.LibtorrentTempDir
	}
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, fmt.Sprintf("upload-%s.torrent", gh3UUID()))
	if err := os.WriteFile(path, body, 0o644); err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// An explicit `save_path` lets the caller point the torrent at data that is
	// already on disk (the torrent is then re-checked and seeded).
	savePath := cfg.LibtorrentDir
	if requested := strings.TrimSpace(r.URL.Query().Get("save_path")); requested != "" {
		savePath = requested
	}
	hash, err := s.torrents.AddTorrentFileEx(path, savePath, AddOptions{Preallocate: cfg.LibtorrentPreallocate()})
	if err != nil {
		_ = os.Remove(path)
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if hash == nil {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "torrent client unavailable"})
		return
	}
	_ = os.Remove(path)
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true, "kind": "torrent", "hash": *hash})
}

// ---------------------------------------------------------------------------
// private helpers
// ---------------------------------------------------------------------------

// gh3DecodeOptionalJSON implements `Option<Json<T>>`: an empty body yields the zero
// value, a present body is decoded (a malformed body is a 400).
func gh3DecodeOptionalJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Body == nil {
		return true
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	if err := json.Unmarshal(body, target); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

// gh3DerefString returns the pointed-to string or "".
func gh3DerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// gh3UUID returns a random RFC 4122 version 4 UUID string.
func gh3UUID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

// gh3FindSeries implements `find_series`: exact name or alias match.
func gh3FindSeries(cfg *Config, name string) *SeriesConfig {
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

// gh3SetupComplete implements `setup_complete`.
func gh3SetupComplete(cfg *Config) bool {
	info, err := os.Stat(filepath.Join(cfg.DataDir, ".gextto-setup.json"))
	return err == nil && !info.IsDir()
}

// gh3SaveWatchedFolders stores watched folders under the `watched_folders`
// settings key. It mirrors `watcher::save_watched_folders`; the handler cannot
// call the same-named watcher helper from inside the same-named handler, so it
// keeps a private copy of the logic.
func gh3SaveWatchedFolders(dataDir string, folders []WatchedFolder) error {
	if folders == nil {
		folders = []WatchedFolder{}
	}
	encoded, err := json.Marshal(folders)
	if err != nil {
		return fmt.Errorf("serialize watched folders: %w", err)
	}
	return SaveSetting(dataDir, "watched_folders", string(encoded))
}

// gh3SingleComponent reports whether `name` is exactly one path component.
func gh3SingleComponent(name string) bool {
	if name == "" {
		return false
	}
	if strings.Contains(name, "/") {
		return false
	}
	return filepath.Clean(name) == name
}

// gh3DirectorySize implements `directory_size`.
func gh3DirectorySize(path string) uint64 {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	var total uint64
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		info, err := os.Lstat(child)
		if err != nil {
			continue
		}
		if info.IsDir() {
			total += gh3DirectorySize(child)
		} else {
			total += uint64(info.Size())
		}
	}
	return total
}

// gh3RemoveTrashContents implements `remove_trash_contents`.
func gh3RemoveTrashContents(root string, olderThanDays int64) (int, uint64, error) {
	files := 0
	var byteCount uint64
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return 0, 0, nil
	}
	var cutoff time.Time
	if olderThanDays > 0 {
		cutoff = time.Now().Add(-time.Duration(olderThanDays) * 24 * time.Hour)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return files, byteCount, err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		linkInfo, err := os.Lstat(path)
		if err != nil {
			return files, byteCount, err
		}
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if linkInfo.IsDir() {
			nestedFiles, nestedBytes, err := gh3RemoveTrashContents(path, olderThanDays)
			files += nestedFiles
			byteCount += nestedBytes
			if err != nil {
				return files, byteCount, err
			}
			remaining, err := os.ReadDir(path)
			if err != nil {
				return files, byteCount, err
			}
			if len(remaining) == 0 {
				if err := os.Remove(path); err != nil {
					return files, byteCount, err
				}
			}
		} else {
			if olderThanDays > 0 {
				metadata, err := os.Stat(path)
				tooNew := err == nil && metadata.ModTime().After(cutoff)
				if tooNew {
					continue
				}
			}
			metadata, err := os.Stat(path)
			if err == nil {
				byteCount += uint64(metadata.Size())
			}
			files++
			if err := os.Remove(path); err != nil {
				return files, byteCount, err
			}
		}
	}
	return files, byteCount, nil
}

// gh3SeenGrouped implements `seen_grouped`.
func gh3SeenGrouped(state *AppState, kind string, r *http.Request) (int, any) {
	limit := queryInt(r, "limit", 50)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	page := queryInt(r, "page", 1)
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit
	queryText := queryParam(r, "q")
	var groups any
	var total int64
	var err error
	if kind == "series" {
		grouped, aggregated, callErr := state.db.SeriesSeenGrouped(int(offset), int(limit), queryText)
		groups, total, err = grouped, aggregated, callErr
	} else {
		grouped, aggregated, callErr := state.db.MoviesSeenGrouped(int(offset), int(limit), queryText)
		groups, total, err = grouped, aggregated, callErr
	}
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
	}
	pages := total / limit
	if total%limit != 0 {
		pages++
	}
	if pages < 1 {
		pages = 1
	}
	return http.StatusOK, map[string]any{
		"ok":     true,
		"groups": groups,
		"total":  total,
		"page":   page,
		"pages":  pages,
	}
}

// gh3ManualSearch implements `manual_search`, reused by `manual_search_get`.
func gh3ManualSearch(s *AppState, query string) (int, any) {
	if !gh3SetupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": "query must contain 1-256 characters"}
	}
	s.cycle_lock.Lock()
	defer s.cycle_lock.Unlock()
	cfg := latestConfig(s)
	ctx, cancel := context.WithTimeout(context.Background(), gh3ManualSearchBudget)
	defer cancel()
	results := s.engine.SearchQueryManual(ctx, cfg, query)
	if ctx.Err() != nil {
		logging.Warn("manual search budget expired; returning archive results",
			"query", query,
			"timeout_secs", int64(gh3ManualSearchBudget.Seconds()))
		results = nil
	}
	results = append(results, gh3ArchiveReleasesForQuery(s, cfg, query)...)
	seen := map[string]struct{}{}
	deduped := results[:0]
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
	return http.StatusOK, map[string]any{"ok": true, "query": query, "results": results}
}

// gh3ArchiveReleasesForQuery implements `archive_releases_for_query`.
func gh3ArchiveReleasesForQuery(s *AppState, cfg *Config, query string) []models.Release {
	results := []models.Release{}
	entries, err := s.archive.Search(query)
	if err != nil {
		return results
	}
	for _, entry := range entries {
		release := ParseRelease(entry[0], entry[1], "archive:"+entry[2])
		if release == nil {
			continue
		}
		if reason := cfg.AllReleaseDeniedReason(release); reason != "" {
			if cfg.ReleaseIsMonitored(release) {
				rules.LogRejection(release, reason)
			}
			continue
		}
		results = append(results, *release)
	}
	return results
}

// gh3AddRelease implements `add_release`.
func gh3AddRelease(s *AppState, release models.Release) (int, any) {
	if !gh3SetupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	isURL := gh3IsTorrentURL(release.Magnet)
	status, value := gh3AddParsedRelease(s, release)
	if isURL && status == http.StatusBadRequest {
		if found := gh3ResolveBySearch(s, release.Title); found != nil {
			retryStatus, retryValue := gh3AddParsedRelease(s, *found)
			if retryStatus != http.StatusBadRequest {
				return retryStatus, retryValue
			}
		}
	}
	return status, value
}

// gh3AddParsedRelease implements `add_parsed_release`.
func gh3AddParsedRelease(s *AppState, release models.Release) (int, any) {
	source := strings.TrimSpace(release.Magnet)
	if gh3IsTorrentURL(source) {
		hash, err := gh3DownloadAndAdd(s, source, AddOptions{})
		if err != nil {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
		}
		if hash == nil {
			return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
		}
		release.Magnet = "magnet:?xt=urn:btih:" + *hash
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
	go func() {
		added, err := s.torrents.Add(source, s.cfg)
		switch {
		case err != nil:
			logging.Error("manual torrent add failed", "title", title, "error", err.Error())
		case added:
			logging.Info("manual torrent queued", "title", title)
		default:
			logging.Info("manual torrent already queued", "title", title)
		}
	}()
	return http.StatusAccepted, map[string]any{"ok": true, "title": title, "queued": true}
}

// gh3IsTorrentURL implements `is_torrent_url`.
func gh3IsTorrentURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// gh3DownloadAndAdd implements `download_and_add`.
func gh3DownloadAndAdd(s *AppState, rawURL string, options AddOptions) (*string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "gextto/0.1")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.cfg.StateDir, fmt.Sprintf(".manual-%s.torrent", gh3UUID()))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return nil, err
	}
	hash, err := s.torrents.AddTorrentFileWithOptions(path, s.cfg, nil, options)
	_ = os.Remove(path)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// gh3ResolveBySearch implements `resolve_by_search`.
func gh3ResolveBySearch(s *AppState, title string) *models.Release {
	query := gh3SearchQueryFromTitle(title)
	normalized := gh3NormalizeSearch(query)
	if normalized == "" {
		return nil
	}
	results := s.engine.SearchQueryManual(context.Background(), s.cfg, query)
	for i := range results {
		release := &results[i]
		if gh3SourceIsUsable(release.Magnet) && strings.HasPrefix(gh3NormalizeSearch(release.Title), normalized) {
			return release
		}
	}
	return nil
}

// gh3SearchQueryFromTitle implements `search_query_from_title`.
func gh3SearchQueryFromTitle(title string) string {
	head := title
	if index := strings.Index(title, "["); index >= 0 {
		head = title[:index]
	}
	cleaned := make([]rune, 0, len(head))
	for _, character := range head {
		if (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') {
			cleaned = append(cleaned, character)
		} else {
			cleaned = append(cleaned, ' ')
		}
	}
	markers := map[string]struct{}{
		"4k": {}, "uhd": {}, "bluray": {}, "bdrip": {}, "web": {}, "webdl": {},
		"webrip": {}, "hdtv": {}, "dvdrip": {}, "h264": {}, "h265": {}, "x264": {},
		"x265": {}, "hevc": {}, "avc": {}, "aac": {}, "ac3": {}, "eac3": {},
		"ddp": {}, "dts": {}, "proper": {}, "repack": {},
	}
	words := []string{}
	for _, word := range strings.Fields(string(cleaned)) {
		lower := strings.ToLower(word)
		isYear := len(word) == 4 && gh3AllASCIIDigits(word)
		_, isMarker := markers[lower]
		if !isMarker {
			isMarker = strings.Contains(lower, "1080p") ||
				strings.Contains(lower, "720p") ||
				strings.Contains(lower, "2160p") ||
				strings.Contains(lower, "480p")
		}
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

// gh3AllASCIIDigits reports whether every character is an ASCII digit.
func gh3AllASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// gh3NormalizeSearch implements `normalize_search`.
func gh3NormalizeSearch(value string) string {
	head := value
	if index := strings.Index(value, "["); index >= 0 {
		head = value[:index]
	}
	cleaned := make([]rune, 0, len(head))
	for _, character := range strings.ToLower(head) {
		if (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'z') {
			cleaned = append(cleaned, character)
		} else {
			cleaned = append(cleaned, ' ')
		}
	}
	return strings.Join(strings.Fields(string(cleaned)), " ")
}

// gh3SourceIsUsable implements `source_is_usable`.
func gh3SourceIsUsable(source string) bool {
	source = strings.TrimSpace(source)
	if gh3IsTorrentURL(source) {
		return true
	}
	if !strings.HasPrefix(source, "magnet:") {
		return false
	}
	_, ok := utils.MagnetHash(source)
	return ok
}

// gh3DecorateTorrents implements `decorate_torrents`.
func gh3DecorateTorrents(s *AppState, items []models.TorrentView) []map[string]any {
	hashes := make([]string, 0, len(items))
	for i := range items {
		hashes = append(hashes, strings.ToLower(items[i].Hash))
	}
	aux, err := s.db.TorrentAuxBulk(hashes)
	if err != nil {
		aux = map[string][3]string{}
	}
	decorated := make([]map[string]any, 0, len(items))
	for i := range items {
		item := items[i]
		entry := aux[strings.ToLower(item.Hash)]
		archived := strings.TrimSpace(entry[0]) != ""
		value := map[string]any{}
		if raw, err := json.Marshal(item); err == nil {
			if err := json.Unmarshal(raw, &value); err != nil {
				value = map[string]any{}
			}
		}
		value["archived"] = archived
		value["source"] = entry[1]
		value["reason"] = entry[2]
		decorated = append(decorated, value)
	}
	return decorated
}

// gh3DryRunSessionPreview implements `dry_run_session_preview`.
func gh3DryRunSessionPreview(s *AppState) []models.TorrentView {
	hashes := map[string]struct{}{}
	if entries, err := os.ReadDir(s.cfg.StateDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if filepath.Ext(name) != ".fastresume" {
				continue
			}
			stem := strings.TrimSuffix(name, ".fastresume")
			hashes[strings.ToLower(stem)] = struct{}{}
		}
	}
	if len(hashes) == 0 {
		return []models.TorrentView{}
	}
	stored, err := s.db.StoredTorrentsAll(500)
	if err != nil {
		stored = nil
	}
	preview := make([]models.TorrentView, 0, len(stored))
	for i := range stored {
		item := stored[i]
		if _, ok := hashes[strings.ToLower(item.Hash)]; !ok {
			continue
		}
		state := "restored · dry-run"
		if item.Paused {
			state = "restored · in pausa"
		}
		preview = append(preview, models.TorrentView{
			Hash:            item.Hash,
			Name:            item.Name,
			Progress:        gh3ClampFloat(item.Progress*100.0, 0.0, 100.0),
			State:           state,
			DownloadRate:    0,
			UploadRate:      0,
			SavePath:        s.cfg.LibtorrentDir,
			DownloadLimit:   -1,
			UploadLimit:     -1,
			AllTimeUpload:   0,
			AllTimeDownload: item.Downloaded,
			SeedingSeconds:  0,
			QueuePosition:   0,
			NumPeers:        0,
			NumSeeds:        0,
			SeedRatio:       -1.0,
			SeedDays:        -1,
			HasMetadata:     true,
			AutoManaged:     false,
			TorrentVersion:  "",
			TotalSize:       0,
			TotalDone:       0,
			Stalled:         false,
		})
	}
	return preview
}

// gh3ClampFloat clamps a float to the inclusive range.
func gh3ClampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
