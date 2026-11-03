package gextto

// Status and diagnostics endpoints of the API: recent downloads, the last
// cycle, system statistics, provider and service health, the missing-episode
// search, watched folders, blocklist, rescoring and archive scans, media info,
// RAM disk, folder browsing, setup completion, event hooks, speed limits, log
// level, browser handlers and the magnet feed.

import (
	"context"
	"encoding/json"
	"errors"
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

type mkdirInput struct {
	Path string `json:"path"`
}

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
	meta := seriesMetadataWith(cfg, tmdb)

	out := make([]map[string]any, len(items))
	var wg sync.WaitGroup
	for index := range items {
		wg.Add(1)
		go func(position int, item RecentDownload) {
			defer recoverGoroutine("recent downloads")
			defer wg.Done()
			value := toMap(item)
			var poster *string
			if item.Kind == "series" {
				if found, err := meta.PosterForSeries(r.Context(), item.Name); err == nil {
					poster = found
				}
			} else if meta.Source() == "tvdb" {
				poster = tvdbMoviePoster(r.Context(), meta.(tvdbSeriesMetadata).TvdbClient, item.Name, item.Year)
			} else if movie, err := tmdb.SearchMovie(r.Context(), item.Name, item.Year); err == nil && movie != nil {
				poster = movie.PosterPath
			}
			if poster != nil && *poster != "" {
				value["poster"] = metadataImageURL(*poster, "w154")
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
	// The filter is applied twice: the eD2k fallback only runs when the torrent
	// plane leaves nothing usable, not merely when it returns raw rows that the
	// quality or language rules then reject.
	filter := func(candidates []models.Release) []models.Release {
		seen := map[string]bool{}
		filtered := []models.Release{}
		for i := range candidates {
			release := candidates[i]
			if !releaseMatchesSeriesEpisode(release, series, input.Season, input.Episode) {
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
			return cfg.ReleaseScore(&filtered[a]) > cfg.ReleaseScore(&filtered[b])
		})
		return filtered
	}
	filtered := filter(results)
	// Nothing usable from the torrent plane: try eD2k/Kad, exactly like the
	// series missing search and the automatic gap fallback.
	if len(filtered) == 0 {
		results = append(results, ed2kManualEpisodeFallback(r.Context(), s.engine, cfg, series, input.Season, input.Episode)...)
		filtered = filter(results)
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "query": query, "results": filtered})
}

func MakeDirectory(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input mkdirInput
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

var (
	feedStatusMu    sync.Mutex
	feedStatusAt    time.Time
	feedStatusValue map[string]any
)

// computeFeedStatus matches every enabled series and movie against the recent
// archive entries, and caches the result for FeedStatus.
func computeFeedStatus(s *AppState) map[string]any {
	cfg := latestConfig(s)
	entries, _ := s.archive.RecentEntries(40000)
	type feedEntry struct {
		lower  string
		title  string
		magnet string
		source string
	}
	list := make([]feedEntry, 0, len(entries))
	for _, entry := range entries {
		title, magnet, source := entry[0], entry[1], entry[2]
		if strings.HasPrefix(source, "archive:") || strings.HasPrefix(source, "timeframe") {
			continue
		}
		list = append(list, feedEntry{strings.ToLower(title), title, magnet, source})
	}
	find := func(query string) []map[string]any {
		words := strings.Fields(strings.ToLower(query))
		matches := []map[string]any{}
		if len(words) == 0 {
			return matches
		}
		for _, entry := range list {
			all := true
			for _, word := range words {
				if !strings.Contains(entry.lower, word) {
					all = false
					break
				}
			}
			if !all {
				continue
			}
			matches = append(matches, map[string]any{
				"title":  entry.title,
				"magnet": entry.magnet,
				"source": entry.source,
			})
			if len(matches) >= 8 {
				break
			}
		}
		return matches
	}
	items := []map[string]any{}
	seriesCount := 0
	for _, series := range cfg.Series {
		if !series.Enabled {
			continue
		}
		if seriesCount >= 120 {
			break
		}
		seriesCount++
		items = append(items, map[string]any{
			"kind":    "series",
			"name":    series.Name,
			"matches": find(series.Name),
		})
	}
	movieCount := 0
	for _, movie := range cfg.Movies {
		if !movie.Enabled {
			continue
		}
		if movieCount >= 120 {
			break
		}
		movieCount++
		query := fmt.Sprintf("%s %s", movie.Name, movie.Year)
		items = append(items, map[string]any{
			"kind":    "movie",
			"name":    movie.Name,
			"matches": find(query),
		})
	}
	value := map[string]any{"ok": true, "items": items}
	feedStatusMu.Lock()
	feedStatusAt = time.Now()
	feedStatusValue = value
	feedStatusMu.Unlock()
	return value
}

func FeedStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	feedStatusMu.Lock()
	cached := feedStatusValue
	computedAt := feedStatusAt
	feedStatusMu.Unlock()
	if cached != nil && time.Since(computedAt) < 60*time.Second {
		jsonResponse(w, cached)
		return
	}
	jsonResponse(w, computeFeedStatus(s))
}

func RunNow(w http.ResponseWriter, r *http.Request, s *AppState) {
	var domain *string
	switch queryParam(r, "domain") {
	case "series", "movies", "comics":
		value := queryParam(r, "domain")
		domain = &value
	}
	running, queued, err := requestCycle(s, domain, "Search requested from the interface")
	switch {
	case errors.Is(err, errSetupIncomplete), errors.Is(err, errDaemonInactive):
		jsonError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	case queued:
		jsonStatus(w, http.StatusAccepted, map[string]any{
			"ok":      true,
			"started": false,
			"queued":  true,
			"message": "Ciclo manuale già avviato o accodato",
		})
		return
	}
	message := "Ciclo avviato"
	if running {
		message = "Ciclo già in corso: richiesta accodata"
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"started": !running,
		"queued":  running,
		"message": message,
	})
}

func SelectRamdisk(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RamDiskSelectInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := strings.TrimSpace(input.Path)
	info, statErr := os.Stat(path)
	if statErr != nil || !info.IsDir() || !DirectoryWritable(path) {
		jsonError(w, http.StatusBadRequest, "il percorso RAM disk non esiste o non è scrivibile")
		return
	}
	recommended, err := SaveRamdiskSettings(s.cfg.DataDir, path)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("configurazione RAM disk fallita: %v", err),
		})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":          true,
		"path":        path,
		"recommended": recommended,
	})
}

func SetLogLevel(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input LogLevel
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Level) > 128 || strings.TrimSpace(input.Level) == "" {
		jsonError(w, http.StatusBadRequest, "invalid log level")
		return
	}
	logging.SetLevel(input.Level)
	logging.Info("runtime log level changed", "level", input.Level)
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "level": input.Level})
}

// AddDownloadTag adds a tag to the stored download tags, skipping one that
// differs from an existing tag only by case.
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
	tags := storedDownloadTags(cfg)
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

func LastCycle(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{"ok": true, "cycle": s.last_cycle.Snapshot()})
}

// SendMagnet adds a magnet link, or a remote http(s) .torrent, to the active
// engine.
func SendMagnet(w http.ResponseWriter, r *http.Request, s *AppState) {
	if !SetupComplete(s.cfg) {
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

// StatsApi returns the last cycle, the engine statistics and the consumption
// figures.
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

// ManualSearchBudget bounds a manual search, so the request cannot wait forever
// on a slow source.
const ManualSearchBudget = 90 * time.Second

// ManualSearchGet runs a manual search for the query in the URL parameters.
func ManualSearchGet(w http.ResponseWriter, r *http.Request, s *AppState) {
	values := r.URL.Query()
	query := values.Get("query")
	if qs, ok := values["q"]; ok && len(qs) > 0 {
		query = qs[0]
	}
	kad := values.Get("kad")
	status, value := gh3ManualSearch(s, query, settingIsTrue(kad) || kad == "1")
	jsonStatus(w, status, value)
}

// PlexRefresh asks Plex to refresh all of its library sections.
func PlexRefresh(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	url := cfg.Settings["plex_url"]
	token := cfg.Settings["plex_token"]
	if strings.TrimSpace(url) == "" || strings.TrimSpace(token) == "" {
		jsonError(w, http.StatusBadRequest, "Plex non configurato")
		return
	}
	endpoint := strings.TrimRight(strings.TrimSpace(url), "/") + "/library/sections/all/refresh"
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
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

// SaveEventHooks validates and stores the event hooks, then reloads them in the
// notifier.
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
	if err := SaveWatchedFolders(s.cfg.DataDir, folders); err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": folders})
}

// SystemStats returns the disk health checks, the engine statistics and the last
// cycle.
func SystemStats(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := LatestConfig(s)
	trash := cfg.DataDir + "/trash"
	if cfg.TrashPath != nil {
		trash = *cfg.TrashPath
	}
	ramdisk := ""
	if value, ok := cfg.Settings["torrent_ramdisk_dir"]; ok && strings.TrimSpace(value) != "" {
		ramdisk = value
	}
	health := CheckWithPaths(&HealthPaths{
		DataDir:      cfg.DataDir,
		TrashPath:    trash,
		DownloadPath: cfg.DownloadDir,
		ArchiveRoot:  derefString(cfg.ArchiveRoot),
		RamdiskPath:  ramdisk,
	})
	jsonResponse(w, map[string]any{
		"ok":            true,
		"health":        health,
		"torrent_stats": s.activeEngine().Stats(),
		"last_cycle":    s.last_cycle.Snapshot(),
	})
}

// UploadTorrent adds the .torrent file sent in the request body to the active
// engine.
func UploadTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := LatestConfig(s)
	if !SetupComplete(cfg) {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"})
		return
	}
	if cfg.DryRun {
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
	dir := cfg.DataDir + "/incomplete"
	if cfg.DownloadTempDir != nil {
		dir = *cfg.DownloadTempDir
	}
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, fmt.Sprintf("upload-%s.torrent", newUUID()))
	if err := os.WriteFile(path, body, 0o644); err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// An explicit `save_path` lets the caller point the torrent at data that is
	// already on disk (the torrent is then re-checked and seeded).
	savePath := cfg.DownloadDir
	if requested := strings.TrimSpace(r.URL.Query().Get("save_path")); requested != "" {
		savePath = requested
	}
	hash, err := s.activeEngine().AddTorrentFileEx(path, savePath, AddOptions{Preallocate: cfg.TorrentPreallocate()})
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

// Runs a manual query on the engine, the archive and, when asked, eD2k, and
// returns the HTTP status and the payload.
func gh3ManualSearch(s *AppState, query string, kad ...bool) (int, any) {
	if !SetupComplete(LatestConfig(s)) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": "query must contain 1-256 characters"}
	}
	s.cycle_lock.Lock()
	defer s.cycle_lock.Unlock()
	cfg := latestConfig(s)
	ctx, cancel := context.WithTimeout(context.Background(), ManualSearchBudget)
	defer cancel()
	results := s.engine.SearchQueryManual(ctx, cfg, query)
	if ctx.Err() != nil {
		logging.Warn("manual search budget expired; returning archive results",
			"query", query,
			"timeout_secs", int64(ManualSearchBudget.Seconds()))
		results = nil
	}
	results = append(results, archiveReleasesForQuery(s, cfg, query)...)
	if len(kad) > 0 && kad[0] {
		results = append(results, SearchED2KManual(ctx, cfg, query)...)
	}
	seen := map[string]struct{}{}
	deduped := results[:0]
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
	// The score is a function of the release and of the settings, so it is
	// computed once per result instead of at every comparison: the sort called
	// ReleaseScore (series lookup + score breakdown) O(n log n) times.
	type scoredRelease struct {
		release models.Release
		score   int64
	}
	ranked := make([]scoredRelease, len(results))
	for i := range results {
		ranked[i] = scoredRelease{release: results[i], score: cfg.ReleaseScore(&results[i])}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	for i := range ranked {
		results[i] = ranked[i].release
	}
	return http.StatusOK, map[string]any{"ok": true, "query": query, "results": results}
}

type SpeedLimitsInput struct {
	DownloadKib int64 `json:"download_kib"`
	UploadKib   int64 `json:"upload_kib"`
}

// xmlAttribute returns the value of an XML attribute of body, or nil when the
// attribute does not appear.
func xmlAttribute(body, name string) *string {
	needle := name + "=\""
	start := strings.Index(body, needle)
	if start < 0 {
		return nil
	}
	rest := body[start+len(needle):]
	end := strings.Index(rest, "\"")
	if end < 0 {
		return nil
	}
	value := rest[:end]
	return &value
}

var seasonEpisodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bs(?:tagione)?\s*(\d{1,2})\s*e(?:p(?:isodio)?)?\.?\s*(\d{1,4})`),
	regexp.MustCompile(`(?i)\b(\d{1,2})\s*x\s*(\d{1,4})`),
	regexp.MustCompile(`(?i)stagione\s*(\d{1,2}).*?episodio\s*(\d{1,4})`),
}

// parseSeasonEpisode reads the season and episode numbers from a file name, and
// reports whether it found both.
func parseSeasonEpisode(name string) (int64, int64, bool) {
	for _, pattern := range seasonEpisodePatterns {
		match := pattern.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		season, errSeason := strconv.ParseInt(match[1], 10, 64)
		episode, errEpisode := strconv.ParseInt(match[2], 10, 64)
		if errSeason == nil && errEpisode == nil {
			return season, episode, true
		}
	}
	return 0, 0, false
}

// Scans one archive folder and registers every recognised video file, returning
// the number of files found and the number of files updated.
func gh4_scanArchivePath(db *Database, series *SeriesConfig, path string, cfg *Config) (int, int, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return 0, 0, fmt.Errorf("archive path is not a directory: %s", path)
	}
	files, err := VideoFiles(path)
	if err != nil {
		return 0, 0, err
	}
	found := 0
	records := make([]ArchiveFileRecord, 0, len(files))
	for _, file := range files {
		name := filepath.Base(file)
		season, episode, ok := parseSeasonEpisode(name)
		if !ok || season <= 0 || episode <= 0 {
			continue
		}
		// Never register a broken file as the archived copy.
		if invalidErr := validateCompletedFile(file); invalidErr != nil {
			logging.Warn("⚠️ Archive scan skipped an invalid video file",
				"path", file, "error", invalidErr.Error())
			continue
		}
		found++
		records = append(records, ArchiveFileRecord{
			Season:       season,
			Episode:      episode,
			Title:        archiveFileTitle(name),
			Path:         file,
			SizeBytes:    archiveFileSize(file),
			QualityScore: cfg.FileScore(file, "series", series.Name),
		})
	}
	// One transaction for the whole series instead of one commit per file. The
	// returned count stays "files updated", as the page has always reported it:
	// a range file is one file even when it registers several episodes.
	if _, batchErr := db.SyncArchiveFilesScored(series.Name, records); batchErr != nil {
		return found, 0, batchErr
	}
	return found, found, nil
}

// CleanupDatabase applies the archive retention and then cleans the database.
func CleanupDatabase(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.dryRun() {
		jsonError(w, http.StatusConflict, "dry-run does not clean database")
		return
	}
	cfg := latestConfig(s)
	archiveRemoved, archiveErr := applyArchiveRetention(cfg, s.archive)
	if archiveErr != nil {
		logging.Warn(fmt.Sprintf("⚠️ The release archive cleanup failed: %v", archiveErr))
	}
	report, err := s.db.Cleanup(1000, 30)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "report": report, "archive_removed": archiveRemoved})
}

func DownloadTagsList(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": storedDownloadTags(cfg)})
}

// PlexTest probes the configured Plex server and reports its identity.
func PlexTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	base := cfg.Settings["plex_url"]
	token := cfg.Settings["plex_token"]
	if strings.TrimSpace(base) == "" || strings.TrimSpace(token) == "" {
		jsonError(w, http.StatusBadRequest, "Plex non configurato")
		return
	}
	base = strings.TrimRight(base, "/")
	token = strings.TrimSpace(token)
	client := &http.Client{Timeout: 15 * time.Second}
	get := func(path string) (*http.Response, error) {
		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, base+path, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("X-Plex-Token", token)
		return client.Do(request)
	}
	response, err := get("/identity")
	if err != nil {
		logging.Info("plex test", "host", domain_of(base), "ok", false, "error", err.Error())
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		identity, _ := readLimitedBody(response.Body, maxAPIResponseBytes)
		machine := derefString(xmlAttribute(string(identity), "machineIdentifier"))
		server := ""
		if root, rootErr := get("/"); rootErr == nil {
			defer root.Body.Close()
			if root.StatusCode >= 200 && root.StatusCode < 300 {
				body, _ := readLimitedBody(root.Body, maxAPIResponseBytes)
				server = derefString(xmlAttribute(string(body), "friendlyName"))
			}
		}
		logging.Info("plex test", "host", domain_of(base), "ok", true, "status", response.StatusCode, "server", server)
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "server": server, "version": "", "machine": machine})
		return
	}
	mediaError := mediaTestError("Plex", response.StatusCode)
	logging.Info("plex test", "host", domain_of(base), "ok", false, "status", response.StatusCode, "error", mediaError)
	jsonStatus(w, http.StatusBadGateway, map[string]any{
		"ok":    false,
		"error": mediaError,
	})
}

func RemoveBlocklistEntry(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	removed, err := s.db.RemoveBlocklist(hash)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		jsonError(w, http.StatusNotFound, "hash not blocklisted")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

// RescoreDatabase recomputes the stored scores with the current settings.
func RescoreDatabase(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.dryRun() {
		jsonError(w, http.StatusConflict, "dry-run does not modify database scores")
		return
	}
	cfg := latestConfig(s)
	updated, err := s.db.Rescore(cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "updated": updated})
}

// ScanAllArchives starts the archive scan as a background job.
//
// The scan is long (it walks every archive folder) and used to run inside the
// HTTP request. It is now a background job: the request validates quickly and
// returns 202 with a job id, while /api/jobs/{id} exposes progress, result and
// errors. Cancellation is observed between series; a single folder scan is never
// interrupted half-way.
func ScanAllArchives(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	type scanTarget struct {
		name string
		path string
	}
	targets := []scanTarget{}
	for index := range cfg.Series {
		series := &cfg.Series[index]
		resolved := cfg.ResolveArchivePath(series)
		if !series.Enabled || resolved == nil || strings.TrimSpace(*resolved) == "" {
			continue
		}
		if ArchiveImportBusyContains(series.Name) {
			continue
		}
		targets = append(targets, scanTarget{name: series.Name, path: *resolved})
	}

	job, created := s.jobs.Create("scan-archives", "scan-archives")
	if !created {
		jsonError(w, http.StatusConflict, "an archive scan is already running")
		return
	}
	if err := s.jobs.Start(job.ID, func(ctx context.Context, jobID string) {
		updated := 0
		errorsList := []map[string]any{}
		for index, target := range targets {
			if ctx.Err() != nil {
				return
			}
			s.jobs.SetProgress(jobID, float64(index)/float64(len(targets)), target.name)
			series := findSeries(cfg, target.name)
			if series == nil {
				continue
			}
			_, count, err := gh4_scanArchivePath(s.db, series, target.path, cfg)
			if err != nil {
				errorsList = append(errorsList, map[string]any{"series": target.name, "error": err.Error()})
				continue
			}
			updated += count
		}
		if len(errorsList) > 0 {
			logging.Warn("⚠️ Archive scan finished with errors", "updated", updated, "errors", len(errorsList), "job_id", jobID)
			s.jobs.Fail(jobID, fmt.Errorf("archive scan finished with %d error(s); updated=%d", len(errorsList), updated))
			return
		}
		logging.Info("🗂️ Archive scan completed", "updated", updated, "job_id", jobID)
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"job_id":  job.ID,
		"total":   len(targets),
		"message": "Scansione archivi avviata",
	})
}

// SetSpeedLimits stores the global transfer limits and applies them to the
// active engine.
func SetSpeedLimits(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SpeedLimitsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	download := input.DownloadKib
	if download < 0 {
		download = 0
	}
	upload := input.UploadKib
	if upload < 0 {
		upload = 0
	}
	if err := SaveSetting(s.cfg.DataDir, "torrent_dl_limit", strconv.FormatInt(download, 10)); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = SaveSetting(s.cfg.DataDir, "torrent_ul_limit", strconv.FormatInt(upload, 10))
	if _, err := s.activeEngine().SetGlobalSpeedLimits(download, upload); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "download_kib": download, "upload_kib": upload})
}

// TestNotification sends a test message through the configured notification
// channels.
func TestNotification(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TestNotificationInput
	_ = decodeJSON(r, &input)
	message := "Gextto: notifica di test"
	if input.Message != nil && strings.TrimSpace(*input.Message) != "" {
		message = *input.Message
	}
	notifier := FromConfig(latestConfig(s))
	if !notifier.Enabled() {
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "no notification channel is configured"})
		return
	}
	if err := notifier.Notify(message); err != nil {
		logging.Info("notification test", "ok", false, "error", err.Error())
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	logging.Info("notification test", "ok", true, "enabled", notifier.Enabled())
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "enabled": notifier.Enabled()})
}

func WatchedFoldersView(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":    true,
		"items": LoadWatchedFolders(cfg.Settings),
	})
}

func BlocklistEntries(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.BlocklistEntries(500)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

// ClearProvidersStatus drops the saved backoff rows of one provider or of all of
// them.
func ClearProvidersStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input struct {
		Provider *string `json:"provider"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var provider *string
	if input.Provider != nil {
		trimmed := strings.TrimSpace(*input.Provider)
		if trimmed != "" {
			provider = &trimmed
		}
	}
	if err := s.db.ClearProviderStatus(provider); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

func EventHooksView(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{"ok": true, "items": s.notifier.EventHooks()})
}

// ProvidersStatusView returns the provider health rows: the saved backoffs, the
// live service probes and a row for every configured indexer.
func ProvidersStatusView(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.ProviderStatuses()
	if err != nil {
		items = []models.ProviderStatus{}
	}
	cfg := latestConfig(s)
	// A saved backoff row belongs to one source: once the source is removed from
	// the integrations, or switched off, its row must not stay in the Health
	// page as if the integration were still there. Drop it and forget the stale
	// state, so adding the same name again later starts from a clean backoff.
	if cfg != nil {
		kept := items[:0]
		for _, entry := range items {
			if activeProvider(cfg, entry.Kind, entry.Provider) {
				kept = append(kept, entry)
				continue
			}
			_ = s.db.DeleteProviderStatus(entry.Kind, entry.Provider)
		}
		items = kept
	}
	for index := range items {
		items[index].UserMessage, items[index].SuggestedAction = providerGuidance(items[index])
		if items[index].URL == "" {
			items[index].URL = providerURL(cfg, items[index].Provider)
		}
	}
	// Live service rows: MirCrew, each enabled Prowlarr manager and the
	// gx-torrent engine. Prowlarr/gx-torrent do not leave a backoff row when
	// they are healthy, so probe them and show their current state.
	live := []models.ProviderStatus{}
	if extra := mirCrewServiceStatus(cfg); extra != nil {
		live = append(live, *extra)
	}
	live = append(live, prowlarrServiceStatuses(cfg)...)
	if extra := gxTorrentServiceStatus(r.FormValue("public_base"), s, r, cfg); extra != nil {
		live = append(live, *extra)
	}
	// Every configured indexer must be visible even when it has no problem: the
	// person expects the full provider list, not only the failing ones. A name
	// that already has a saved failure row or a live row is left to those, so a
	// healthy row never hides a real error.
	if cfg != nil {
		known := make(map[string]struct{}, len(items)+len(live))
		for _, entry := range items {
			known[strings.ToLower(strings.TrimSpace(entry.Provider))] = struct{}{}
		}
		for _, entry := range live {
			known[strings.ToLower(strings.TrimSpace(entry.Provider))] = struct{}{}
		}
		live = append(live, healthyIndexerStatuses(cfg, known)...)
	}
	// A live probe supersedes a saved backoff row for the same provider: a
	// service that has both a stale failure row and a live row showed up twice
	// (for example Prowlarr as both "indexer" and "servizio"). The live state is
	// the single source of truth here; the saved row is kept in the DB and
	// cleared by the next successful search.
	if len(live) > 0 {
		probed := make(map[string]struct{}, len(live))
		for _, entry := range live {
			probed[strings.ToLower(strings.TrimSpace(entry.Provider))] = struct{}{}
		}
		kept := items[:0]
		for _, entry := range items {
			if _, ok := probed[strings.ToLower(strings.TrimSpace(entry.Provider))]; ok {
				continue
			}
			kept = append(kept, entry)
		}
		items = append(kept, live...)
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

// mirCrewServiceStatus probes the configured mircrew-indexer Torznab
// indexer and returns a synthetic "Stato provider" row with its live health.
// The result is cached briefly so loading the Salute page does not probe the
// service on every request. The row is only backoff/DB independent: it always
// reflects the current reachability.
var mirCrewProbe struct {
	sync.Mutex
	at    time.Time
	entry *models.ProviderStatus
}

var prowlarrProbe struct {
	sync.Mutex
	entries map[string]ProwlarrProbeEntry
}

// SetupCompleteExisting marks the initial setup as complete for an existing
// installation.
func SetupCompleteExisting(w http.ResponseWriter, r *http.Request, s *AppState) {
	if err := CompleteSetup(s.cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// BrowseDir lists the sub-folders of a directory for the folder pickers. With
// files=video it also lists the video files, for the pickers that choose a file
// (manual import).
func BrowseDir(w http.ResponseWriter, r *http.Request, s *AppState) {
	requested := strings.TrimSpace(queryParam(r, "path"))
	if requested == "" {
		requested = browseDefaultPath()
	}
	absolute, err := filepath.Abs(requested)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		// The path may not exist yet (the setup wizard offers folders that are
		// created on save, and a magnet download can name a still-missing
		// folder): open the nearest existing ancestor so the picker stays
		// navigable instead of failing and stranding the user.
		canonical = nearestDir(absolute)
		if canonical == "" {
			jsonError(w, http.StatusNotFound, err.Error())
			return
		}
	} else if info, statErr := os.Stat(canonical); statErr != nil || !info.IsDir() {
		canonical = filepath.Dir(canonical)
	}

	dirs := []string{}
	files := []string{}
	withVideos := queryParam(r, "files") == "video"
	message := ""
	entries, err := os.ReadDir(canonical)
	if err != nil {
		// An unreadable directory (permissions of the service user) still has
		// to keep the picker usable: report the reason but let it go back up.
		message = err.Error()
	} else {
		for _, entry := range entries {
			child := filepath.Join(canonical, entry.Name())
			if directoryExists(child) {
				dirs = append(dirs, child)
			} else if withVideos && entry.Type().IsRegular() && isVideoExtension(entry.Name()) {
				files = append(files, child)
			}
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)
	var parent any
	if canonical != "/" {
		parent = filepath.Dir(canonical)
	}
	response := map[string]any{
		"ok":     true,
		"path":   canonical,
		"parent": parent,
		"dirs":   dirs,
	}
	if withVideos {
		response["files"] = files
	}
	if message != "" {
		response["error"] = message
	}
	jsonResponse(w, response)
}

// browseDefaultPath is where the folder picker starts with no path: the
// service user's home when it is usable, otherwise the root.
func browseDefaultPath() string {
	if home := os.Getenv("HOME"); home != "" && directoryExists(home) {
		return home
	}
	return "/"
}

// nearestDir returns the closest existing directory at or above path, or ""
// when there is none. A hit that is not a directory resolves to its parent.
func nearestDir(path string) string {
	for current := path; ; {
		if info, err := os.Stat(current); err == nil {
			if info.IsDir() {
				return current
			}
			current = filepath.Dir(current)
			continue
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// CreateRamdisk creates /dev/shm/gextto and saves it as the RAM disk of the
// downloads.
func CreateRamdisk(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RamDiskCreateInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Only the fixed path is accepted; the request value is compared, never used.
	const requested = "/dev/shm/gextto"
	if input.Path != nil && *input.Path != requested {
		jsonError(w, http.StatusBadRequest, "il RAM disk automatico può essere creato solo in /dev/shm/gextto")
		return
	}
	if err := os.MkdirAll(requested, 0o755); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("creazione RAM disk fallita: %v", err))
		return
	}
	if err := os.Chmod(requested, 0o700); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("permessi RAM disk non impostati: %v", err))
		return
	}
	recommendation, err := SaveRamdiskSettings(s.cfg.DataDir, requested)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("RAM disk creato ma configurazione non salvata: %v", err))
		return
	}
	jsonResponse(w, map[string]any{
		"ok":          true,
		"path":        requested,
		"recommended": recommendation,
		"warning":     "Il contenuto di /dev/shm non sopravvive al riavvio della macchina.",
	})
}

func LogLevelGet(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{"ok": true, "level": "runtime"})
}

// RamdiskView lists the detected RAM disks and reports the state of the
// configured one.
func RamdiskView(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	configured := cfg.RamdiskDir()
	items := []map[string]any{}
	for _, mount := range DetectedRamdiskMounts() {
		items = append(items, RamdiskEntry(mount[0], mount[1], configured))
	}
	if configured != nil && *configured != "" {
		found := false
		for _, item := range items {
			if item["path"] == *configured {
				found = true
				break
			}
		}
		if !found {
			items = append(items, RamdiskEntry(*configured, "configured", configured))
		}
	}
	var configuredValue any
	configuredOK := false
	problem := ""
	if configured != nil && *configured != "" {
		configuredValue = *configured
		// Make the panel self-diagnosing: a configured RAM disk that vanished
		// after a reboot (tmpfs is volatile) or lost its mount must be obvious
		// instead of silently disabling the RAM-disk tier.
		mountType, isRamdisk := ramdiskMountType(*configured)
		switch {
		case !directoryExists(*configured):
			problem = fmt.Sprintf("La cartella RAM disk configurata non esiste più: %s", *configured)
		case !DirectoryWritable(*configured):
			problem = fmt.Sprintf("La cartella RAM disk configurata non è scrivibile: %s", *configured)
		case !isRamdisk:
			problem = fmt.Sprintf("La cartella configurata è su un filesystem '%s', non tmpfs/ramfs: i download andranno su disco.", mountType)
		default:
			configuredOK = true
		}
	}
	createRoot := "/dev/shm"
	jsonResponse(w, map[string]any{
		"ok":               true,
		"enabled":          cfg.RamdiskEnabled(),
		"configured":       configuredValue,
		"configured_ok":    configuredOK,
		"problem":          problem,
		"paths":            items,
		"create_path":      filepath.Join(createRoot, "gextto"),
		"create_available": directoryExists(createRoot) && DirectoryWritable(createRoot),
		"warning":          "Il contenuto di /dev/shm e degli altri tmpfs non sopravvive al riavvio della macchina.",
	})
}

const handlerMagnet = `#!/bin/bash
# Gextto - handler protocollo magnet: invia il link a Gextto.
GEXTTO_URL="${GEXTTO_URL:-__GEXTTO_URL__}"
LOG="/tmp/gextto-magnet.log"
MAGNET="${*}"

echo "$(date '+%F %T') magnet=${MAGNET}" >> "$LOG"
if [[ -z "$MAGNET" ]]; then
  notify-send "Gextto" "Nessun link magnet ricevuto" --icon=dialog-error 2>/dev/null
  exit 1
fi
JSON=$(python3 -c "import json,sys; print(json.dumps({'magnet': sys.argv[1]}))" "$MAGNET")
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${GEXTTO_URL}/api/send-magnet" \
  -H 'Content-Type: application/json' --data-raw "$JSON" --max-time 10 2>/dev/null)
HTTP_CODE=$(echo "$RESPONSE" | tail -1)
if [[ "$HTTP_CODE" == "200" || "$HTTP_CODE" == "202" ]]; then
  notify-send "Gextto" "Torrent aggiunto" --icon=emblem-downloads 2>/dev/null
else
  notify-send "Gextto" "Gextto non raggiungibile (${GEXTTO_URL}) - HTTP ${HTTP_CODE}" --icon=dialog-error 2>/dev/null
  echo "$(date '+%F %T') HTTP ${HTTP_CODE} ${RESPONSE}" >> "$LOG"
fi
`

const handlerTorrent = `#!/bin/bash
# Gextto - handler file .torrent: invia il file locale (o l'URL) a Gextto.
GEXTTO_URL="${GEXTTO_URL:-__GEXTTO_URL__}"
LOG="/tmp/gextto-torrent.log"
INPUT="$1"

if [[ -z "$INPUT" ]]; then
  notify-send "Gextto" "Nessun file .torrent ricevuto" --icon=dialog-error 2>/dev/null
  exit 1
fi

if [[ "$INPUT" == http://* || "$INPUT" == https://* ]]; then
  JSON=$(python3 -c "import json,sys; print(json.dumps({'magnet': sys.argv[1]}))" "$INPUT")
  RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${GEXTTO_URL}/api/send-magnet" \
    -H 'Content-Type: application/json' --data-raw "$JSON" --max-time 30 2>/dev/null)
else
  FILEPATH="${INPUT#file://}"
  if [[ ! -f "$FILEPATH" ]]; then
    notify-send "Gextto" "File non trovato: $FILEPATH" --icon=dialog-error 2>/dev/null
    exit 1
  fi
  RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${GEXTTO_URL}/api/upload-torrent" \
    --data-binary @"$FILEPATH" -H 'Content-Type: application/x-bittorrent' --max-time 30 2>/dev/null)
fi
HTTP_CODE=$(echo "$RESPONSE" | tail -1)
if [[ "$HTTP_CODE" == "200" || "$HTTP_CODE" == "202" ]]; then
  notify-send "Gextto" "Torrent aggiunto" --icon=emblem-downloads 2>/dev/null
else
  notify-send "Gextto" "Gextto non raggiungibile (${GEXTTO_URL}) - HTTP ${HTTP_CODE}" --icon=dialog-error 2>/dev/null
  echo "$(date '+%F %T') HTTP ${HTTP_CODE} ${RESPONSE}" >> "$LOG"
fi
`

const handlerMagnetDesktop = `[Desktop Entry]
Name=Gextto Magnet Handler
Comment=Invia link magnet a Gextto
Exec=/usr/local/bin/gextto-magnet %U
Type=Application
MimeType=x-scheme-handler/magnet;
NoDisplay=true
StartupNotify=false
Terminal=false
`

const handlerTorrentDesktop = `[Desktop Entry]
Name=Gextto Torrent Handler
Comment=Invia file .torrent a Gextto
Exec=/usr/local/bin/gextto-torrent %U
Type=Application
MimeType=application/x-bittorrent;x-scheme-handler/magnet;
NoDisplay=true
StartupNotify=false
Terminal=false
`

const handlerInstall = `#!/bin/bash
# Gextto - installazione handler magnet/.torrent (Linux, xdg-utils).
# Richiede: curl, python3, xdg-utils, libnotify (opzionale).
set -e

GEXTTO_URL="${GEXTTO_URL:-__GEXTTO_URL__}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "Gextto - installazione handler torrent/magnet"
echo "URL Gextto: $GEXTTO_URL"
echo ""

for cmd in curl python3 xdg-mime xdg-open; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "Mancante: $cmd (sudo apt install xdg-utils curl python3)"
    exit 1
  fi
done

echo "Installo gli script in /usr/local/bin ..."
for f in gextto-magnet gextto-torrent; do
  sudo cp "$SCRIPT_DIR/$f" "/usr/local/bin/$f"
  sudo chmod +x "/usr/local/bin/$f"
  echo "  $f"
done

DESKTOP_DIR="$HOME/.local/share/applications"
mkdir -p "$DESKTOP_DIR"
for f in gextto-magnet.desktop gextto-torrent.desktop; do
  cp "$SCRIPT_DIR/$f" "$DESKTOP_DIR/$f"
  echo "  $f"
done

echo "Registro i MIME handler ..."
xdg-mime default gextto-magnet.desktop x-scheme-handler/magnet
xdg-mime default gextto-torrent.desktop application/x-bittorrent
update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true

for PREFS in $(find "$HOME/.mozilla/firefox" -name prefs.js 2>/dev/null); do
  sed -i '/network.protocol-handler.expose.magnet/d' "$PREFS"
  sed -i '/network.protocol-handler.external.magnet/d' "$PREFS"
  echo 'user_pref("network.protocol-handler.expose.magnet", false);' >> "$PREFS"
  echo 'user_pref("network.protocol-handler.external.magnet", true);' >> "$PREFS"
  echo "  Firefox: $PREFS"
done

if curl -s --max-time 3 "$GEXTTO_URL/api/status" &>/dev/null; then
  echo "Gextto raggiungibile."
else
  echo "Attenzione: Gextto non raggiungibile su $GEXTTO_URL"
fi

echo ""
echo "Fatto. Riavvia i browser aperti."
echo "Verifica: xdg-mime query default x-scheme-handler/magnet"
`

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	value = strings.ReplaceAll(value, "'", "&apos;")
	return value
}

// buildMagnetFeed builds the rolling RSS 2.0 magnet feed.
func buildMagnetFeed(entries [][3]string) string {
	var xml strings.Builder
	xml.Grow(len(entries)*256 + 256)
	xml.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<rss version=\"2.0\"><channel>")
	xml.WriteString("<title>Gextto Magnet Feed</title>")
	xml.WriteString("<description>Feed automatico di magnet link da Gextto</description>")
	xml.WriteString(fmt.Sprintf("<lastBuildDate>%s</lastBuildDate>", time.Now().UTC().Format(time.RFC1123Z)))
	for _, entry := range entries {
		title, magnet, source := entry[0], entry[1], entry[2]
		xml.WriteString("<item><title>")
		xml.WriteString(xmlEscape(title))
		xml.WriteString("</title><link>")
		xml.WriteString(xmlEscape(magnet))
		xml.WriteString("</link><description>")
		xml.WriteString(xmlEscape(title))
		if source != "" {
			xml.WriteString(" [")
			xml.WriteString(xmlEscape(source))
			xml.WriteString("]")
		}
		xml.WriteString("</description></item>")
	}
	xml.WriteString("</channel></rss>")
	return xml.String()
}

// hostPattern accepts a host name, IPv4 or bracketed IPv6 address with an
// optional port: nothing that could break out of the shell scripts.
var hostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9.-]+|\[[0-9A-Fa-f:.]+\])(?::[0-9]{1,5})?$`)

// BrowserHandlerDownload handles GET /api/browser-handlers/download.
func BrowserHandlerDownload(w http.ResponseWriter, r *http.Request, s *AppState) {
	// net/http moves the Host header into r.Host. Only a plain host[:port]
	// is copied into the generated scripts, which the user runs locally.
	host := "127.0.0.1:5000"
	if hostPattern.MatchString(r.Host) {
		host = r.Host
	}
	base := "http://" + host
	requested := strings.TrimSpace(queryParam(r, "file"))

	var template, mime string
	switch requested {
	case "gextto-magnet":
		template, mime = handlerMagnet, "text/x-shellscript"
	case "gextto-torrent":
		template, mime = handlerTorrent, "text/x-shellscript"
	case "gextto-magnet.desktop":
		template, mime = handlerMagnetDesktop, "text/plain"
	case "gextto-torrent.desktop":
		template, mime = handlerTorrentDesktop, "text/plain"
	case "install.sh":
		template, mime = handlerInstall, "text/x-shellscript"
	default:
		jsonError(w, http.StatusBadRequest, "file non valido")
		return
	}
	body := strings.ReplaceAll(template, "__GEXTTO_URL__", base)
	w.Header().Set("Content-Type", mime+"; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", requested))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// Cycles handles GET /api/cycles (and the aliases routed to it).
func Cycles(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.RecentCycles(100)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "cycles": items})
}

// MagnetFeed handles GET /feed.xml (and /api/feed.xml).
func MagnetFeed(w http.ResponseWriter, r *http.Request, s *AppState) {
	entries, err := s.archive.RecentEntries(1000)
	if err != nil {
		entries = nil
	}
	xml := buildMagnetFeed(entries)
	_ = os.WriteFile(filepath.Join(s.cfg.DataDir, "gextto_magnet_feed.xml"), []byte(xml), 0o644)
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml))
}
