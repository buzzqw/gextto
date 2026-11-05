package gextto

// Settings write path and library configuration endpoints: the writable-key
// index and its validation, source filters with their health probes, the port
// check, saving the library (series and movies) with the purge of removed
// entries, the library rename job with its progress and the score preview.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

func SourceFiltersView(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "filters": cfg.SourceFilters})
}

func CheckPorts(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	ports := []map[string]any{}
	for port := int(cfg.Transfer.PortMin); port <= int(cfg.Transfer.PortMax); port++ {
		address := fmt.Sprintf("0.0.0.0:%d", port)
		tcpAvailable := false
		if listener, err := net.Listen("tcp", address); err == nil {
			tcpAvailable = true
			_ = listener.Close()
		}
		udpAvailable := false
		if packet, err := net.ListenPacket("udp", address); err == nil {
			udpAvailable = true
			_ = packet.Close()
		}
		ports = append(ports, map[string]any{
			"port":          port,
			"available":     tcpAvailable && udpAvailable,
			"tcp_available": tcpAvailable,
			"udp_available": udpAvailable,
		})
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":       true,
		"port_min": cfg.Transfer.PortMin,
		"port_max": cfg.Transfer.PortMax,
		"ports":    ports,
	})
}

func SaveSourceFilters(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SourceFiltersInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Filters) > 200 {
		jsonError(w, http.StatusBadRequest, "too many source filters (max 200)")
		return
	}
	filters := input.Filters
	if filters == nil {
		filters = []SourceFilter{}
	}
	encoded, err := json.Marshal(filters)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload := string(encoded)
	if len(payload) > 100000 {
		jsonError(w, http.StatusRequestEntityTooLarge, "source filters too large")
		return
	}
	if err := SaveSetting(s.cfg.DataDir, "source_filters", payload); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "filters": filters})
}

func SourcesHealth(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	ConfigureCloudflareState(cfg.DataDir)
	rawTerm := queryParam(r, "q")
	kindFilter := strings.ToLower(strings.TrimSpace(queryParam(r, "kind")))
	includeKind := func(kind string) bool {
		return kindFilter == "" || kindFilter == kind
	}
	var term *string
	if strings.TrimSpace(rawTerm) != "" {
		value := rawTerm
		term = &value
	}
	client := &http.Client{Timeout: 45 * time.Second}
	// Health probes are request-bound: a client that navigates away stops them.
	ctx := r.Context()

	tasks := []func() []map[string]any{}
	if term != nil {
		feedMaxPages := cfg.FeedMaxPages()
		maxAgeDays := cfg.MaxReleaseAgeDays
		oldRatio := cfg.StopOnOldPageRatio()
		if includeKind("feed") {
			for _, feed := range cfg.FeedURLs {
				feed := feed
				tasks = append(tasks, func() []map[string]any {
					items, err := FetchFeed(ctx, feed, cfg.FlaresolverrURL, feedMaxPages, maxAgeDays, oldRatio)
					if err != nil {
						return []map[string]any{map[string]any{"kind": "feed", "name": feed, "ok": false, "results": int64(0), "error": err.Error()}}
					}
					return []map[string]any{map[string]any{"kind": "feed", "name": feed, "ok": true, "results": int64(len(items)), "error": nil}}
				})
			}
		}
		if includeKind("indexer") {
			for _, indexer := range cfg.Indexers {
				if !indexer.Enabled {
					continue
				}
				indexer := indexer
				tasks = append(tasks, func() []map[string]any {
					items, err := FetchTorznabFlareSolverr(ctx, indexer, *term, nil, cfg.FlaresolverrURL)
					if err != nil {
						return []map[string]any{map[string]any{"kind": "indexer", "name": indexer.Name, "ok": false, "results": int64(0), "error": err.Error()}}
					}
					return []map[string]any{map[string]any{"kind": "indexer", "name": indexer.Name, "ok": true, "results": int64(len(items)), "error": nil}}
				})
			}
		}
		if includeKind("engine") {
			for _, engine := range cfg.WebsearchEngines {
				engine := engine
				tasks = append(tasks, func() []map[string]any {
					items, err := runWebEngine(ctx, engine, *term, cfg.FlaresolverrURL)
					if err != nil {
						return []map[string]any{map[string]any{"kind": "engine", "name": engine, "ok": false, "results": int64(0), "error": err.Error()}}
					}
					return []map[string]any{map[string]any{"kind": "engine", "name": engine, "ok": true, "results": int64(len(items)), "error": nil}}
				})
			}
		}
	} else {
		if includeKind("feed") {
			for _, feed := range cfg.FeedURLs {
				feed := feed
				tasks = append(tasks, func() []map[string]any {
					request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
					if err != nil {
						return []map[string]any{map[string]any{"kind": "feed", "name": feed, "url": feed, "ok": false, "status": int64(0), "error": err.Error()}}
					}
					response, err := client.Do(request)
					if err != nil {
						return []map[string]any{map[string]any{"kind": "feed", "name": feed, "url": feed, "ok": false, "status": int64(0), "error": err.Error()}}
					}
					_ = response.Body.Close()
					ok := response.StatusCode >= 200 && response.StatusCode < 300
					return []map[string]any{map[string]any{"kind": "feed", "name": feed, "url": feed, "ok": ok, "status": int64(response.StatusCode), "error": nil}}
				})
			}
		}
		if includeKind("indexer") {
			for _, indexer := range cfg.Indexers {
				if !indexer.Enabled {
					continue
				}
				indexer := indexer
				tasks = append(tasks, func() []map[string]any {
					items, err := FetchTorznabFlareSolverr(ctx, indexer, "ita", nil, cfg.FlaresolverrURL)
					if err != nil {
						return []map[string]any{map[string]any{"kind": "indexer", "name": indexer.Name, "url": indexer.URL, "ok": false, "results": int64(0), "error": err.Error()}}
					}
					return []map[string]any{map[string]any{"kind": "indexer", "name": indexer.Name, "url": indexer.URL, "ok": true, "results": int64(len(items)), "error": nil}}
				})
			}
		}
		if includeKind("engine") {
			for _, engine := range cfg.WebsearchEngines {
				engine := engine
				tasks = append(tasks, func() []map[string]any {
					items, err := runWebEngine(ctx, engine, "ita", cfg.FlaresolverrURL)
					if err != nil {
						return []map[string]any{map[string]any{"kind": "engine", "name": engine, "ok": false, "results": int64(0), "error": err.Error()}}
					}
					return []map[string]any{map[string]any{"kind": "engine", "name": engine, "ok": true, "results": int64(len(items)), "error": nil}}
				})
			}
		}
	}

	// Manager-side indexer health: one row per indexer configured in a Prowlarr
	// or indexarr instance, so a failing or disabled indexer is visible in the
	// Sources table instead of showing up only as missing results. Both
	// managers expose the same read-only /api/v1/indexer and
	// /api/v1/indexerstatus documents.
	for _, indexer := range managerHealthSources(cfg, kindFilter) {
		indexer := indexer
		manager := managerKind(indexer)
		tasks = append(tasks, func() []map[string]any {
			health, err := ManagerIndexerHealth(ctx, indexer.URL, indexer.APIKey, manager)
			if err != nil {
				return []map[string]any{{"kind": manager, "name": indexer.Name, "ok": false, "results": nil, "error": err.Error()}}
			}
			return health
		})
	}

	// Keep health probes bounded: each task may need one or more browser
	// sessions, and FlareSolverr has its own shared two-slot queue. Running one
	// worker per configured source would only create a large pile-up behind it.
	workerCount := 2
	if len(tasks) < workerCount {
		workerCount = len(tasks)
	}
	jobs := make(chan func() []map[string]any, len(tasks))
	results := make(chan []map[string]any, len(tasks))
	var wait sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wait.Add(1)
		go func() {
			defer recoverGoroutine("g1 background")
			defer wait.Done()
			for task := range jobs {
				results <- task()
			}
		}()
	}
	for _, task := range tasks {
		jobs <- task
	}
	close(jobs)
	wait.Wait()
	close(results)
	items := make([]map[string]any, 0, len(tasks))
	for batch := range results {
		items = append(items, batch...)
	}

	var query any
	if term != nil {
		query = *term
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":           true,
		"query":        query,
		"items":        items,
		"flaresolverr": cfg.FlaresolverrURL != nil,
		"tmdb":         cfg.TmdbAPIKey != nil,
		"tvdb":         tvdbClientFor(cfg).Configured(),
	})
}

// managerHealthSources selects the managers whose indexers the Sources view
// probes: every enabled Prowlarr or indexarr instance, optionally narrowed by
// the kind filter of the page. Both expose the same read-only
// /api/v1/indexer and /api/v1/indexerstatus documents.
func managerHealthSources(cfg *Config, kindFilter string) []IndexerConfig {
	if cfg == nil {
		return nil
	}
	out := make([]IndexerConfig, 0, len(cfg.Indexers))
	for _, indexer := range cfg.Indexers {
		manager := managerKind(indexer)
		if !indexer.Enabled || (manager != ManagerProwlarr && manager != ManagerIndexarr) {
			continue
		}
		if kindFilter != "" && kindFilter != manager {
			continue
		}
		out = append(out, indexer)
	}
	return out
}

// DeleteSettingHandler deletes a custom score group; only `score_group_` keys
// are accepted here.
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

// RenameAll starts the background job that renames every enabled series.
func RenameAll(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RenameInput
	if err := decodeOptionalJSON(r, &input); err != nil {
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

// LibraryView returns the library configuration with the episode counts and the
// last download of every series.
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
	// One read of the whole library instead of five queries per series.
	library, libraryErr := s.db.AllSeriesEpisodes()
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
		var episodes []EpisodeView
		if libraryErr == nil {
			episodes = library.Episodes(series.Name, ignoredSeasons)
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
	movieItems := make([]map[string]any, 0, len(cfg.Movies))
	if raw, err := json.Marshal(cfg.Movies); err == nil {
		_ = json.Unmarshal(raw, &movieItems)
	}
	copies, copiesErr := s.db.DownloadedMovieCopies()
	for index := range movieItems {
		if copiesErr != nil || index >= len(cfg.Movies) {
			continue
		}
		if copy, ok := movieDownloadedCopyFor(cfg.Movies[index], copies); ok {
			movieItems[index]["downloaded"] = true
			movieItems[index]["downloaded_path"] = copy.Path
			movieItems[index]["downloaded_title"] = copy.Title
			movieItems[index]["downloaded_at"] = copy.DownloadedAt
		}
	}
	jsonResponse(w, map[string]any{"series": seriesItems, "movies": movieItems})
}

// movieDownloadedCopyFor finds the library copy of a monitored movie: the title
// must match (punctuation-insensitive, as the acquisition match does) and, when
// both years are known, stay within the same ±1 tolerance. copies is newest
// first, so the first match is the version currently in the library.
func movieDownloadedCopyFor(movie MovieConfig, copies []DownloadedMovieCopy) (DownloadedMovieCopy, bool) {
	configuredYear, yearErr := strconv.ParseInt(strings.TrimSpace(movie.Year), 10, 64)
	for _, copy := range copies {
		if !movieTitleMatches(&movie, copy.Name) && !movieTitleMatches(&movie, copy.Title) {
			continue
		}
		if yearErr == nil && copy.Year != nil && absInt64(*copy.Year-configuredYear) > 1 {
			continue
		}
		return copy, true
	}
	return DownloadedMovieCopy{}, false
}

// RenameProgressView returns the progress of the library rename.
func RenameProgressView(w http.ResponseWriter, r *http.Request, s *AppState) {
	s.rename_progress_mu.Lock()
	progress := *s.rename_progress
	s.rename_progress_mu.Unlock()
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "progress": progress})
}

// Removes the DB state of the series and movies that are no longer part of the
// saved library.
func gh4PurgeRemovedLibrary(db *Database, previousSeries []SeriesConfig, previousMovies []MovieConfig, series []SeriesConfig, movies []MovieConfig) {
	for _, old := range previousSeries {
		present := false
		for _, item := range series {
			if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) {
				present = true
				break
			}
		}
		if present {
			continue
		}
		if _, err := db.PurgeSeries(old.Name); err != nil {
			logging.Warn("cannot remove the state of the deleted series", "error", err.Error(), "series", old.Name)
		}
	}
	for _, old := range previousMovies {
		present := false
		if old.ID > 0 {
			for _, item := range movies {
				if item.ID == old.ID {
					present = true
					break
				}
			}
		} else {
			for _, item := range movies {
				if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) &&
					strings.TrimSpace(item.Year) == strings.TrimSpace(old.Year) {
					present = true
					break
				}
			}
		}
		if present {
			continue
		}
		if _, err := db.PurgeMovie(old.Name); err != nil {
			logging.Warn("cannot remove the state of the deleted movie", "error", err.Error(), "movie", old.Name)
		}
	}
}

// SaveLibraryHandler saves the whole library configuration (series and movies).
func SaveLibraryHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input LibraryInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	invalid := len(input.Series) > 500 || len(input.Movies) > 500
	if !invalid {
		for _, series := range input.Series {
			if strings.TrimSpace(series.Name) == "" {
				invalid = true
				break
			}
		}
	}
	if !invalid {
		for _, movie := range input.Movies {
			if strings.TrimSpace(movie.Name) == "" {
				invalid = true
				break
			}
		}
	}
	if invalid {
		jsonError(w, http.StatusBadRequest, "invalid or oversized library configuration")
		return
	}
	previous := latestConfig(s)
	if err := SaveLibraryConfig(s.cfg.DataDir, input.Series, input.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gh4PurgeRemovedLibrary(s.db, previous.Series, previous.Movies, input.Series, input.Movies)
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":               true,
		"restart_required": true,
		"series":           len(input.Series),
		"movies":           len(input.Movies),
	})
}

// Removes the DB state of series/movies that are no longer part of the saved
// library.
func gh5PurgeRemovedLibrary(db *Database, previousSeries []SeriesConfig, previousMovies []MovieConfig, series []SeriesConfig, movies []MovieConfig) {
	for _, old := range previousSeries {
		present := false
		for _, item := range series {
			if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) {
				present = true
				break
			}
		}
		if present {
			continue
		}
		if _, err := db.PurgeSeries(old.Name); err != nil {
			logging.Warn("cannot remove the state of the deleted series", "error", err, "series", old.Name)
		}
	}
	for _, old := range previousMovies {
		present := false
		if old.ID > 0 {
			for _, item := range movies {
				if item.ID == old.ID {
					present = true
					break
				}
			}
		} else {
			for _, item := range movies {
				if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) &&
					strings.TrimSpace(item.Year) == strings.TrimSpace(old.Year) {
					present = true
					break
				}
			}
		}
		if present {
			continue
		}
		if _, err := db.PurgeMovie(old.Name); err != nil {
			logging.Warn("cannot remove the state of the deleted movie", "error", err, "movie", old.Name)
		}
	}
}

// SaveMoviesConfig saves the movie list and leaves the series untouched.
func SaveMoviesConfig(w http.ResponseWriter, r *http.Request, s *AppState) {
	var movies []MovieConfig
	if err := decodeJSON(r, &movies); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(movies) > 500 {
		jsonError(w, http.StatusBadRequest, "too many movies")
		return
	}
	cfg := latestConfig(s)
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gh5PurgeRemovedLibrary(s.db, cfg.Series, cfg.Movies, cfg.Series, movies)
	jsonResponse(w, map[string]any{"ok": true, "movies": len(movies)})
}

// SaveSeriesConfig saves the series list and leaves the movies untouched.
func SaveSeriesConfig(w http.ResponseWriter, r *http.Request, s *AppState) {
	var series []SeriesConfig
	if err := decodeJSON(r, &series); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(series) > 500 {
		jsonError(w, http.StatusBadRequest, "too many series")
		return
	}
	cfg := latestConfig(s)
	if err := SaveLibrary(s.cfg.DataDir, series, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gh6PurgeRemovedLibrary(s.db, cfg.Series, cfg.Movies, series, cfg.Movies)
	jsonResponse(w, map[string]any{"ok": true, "series": len(series)})
}

// ScorePreview parses a release title and returns the score breakdown and the
// matching library entry the search would apply.
func ScorePreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ScorePreviewInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	title := strings.TrimSpace(input.Title)
	if title == "" || len(title) > 512 {
		jsonError(w, http.StatusBadRequest, "titolo non valido (1-512 caratteri)")
		return
	}
	cfg := latestConfig(s)
	const magnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	release := ParseRelease(title, magnet, "simulator")
	if release == nil {
		jsonError(w, http.StatusBadRequest, "impossibile analizzare il titolo")
		return
	}
	quality := release.Quality
	breakdown := []map[string]any{}
	for _, item := range quality.ScoreBreakdownWithSettings(cfg.Settings) {
		breakdown = append(breakdown, map[string]any{"label": item.Label, "value": item.Value})
	}
	qualityScore := cfg.QualityScore(&quality)
	releaseScore := cfg.ReleaseScore(release)
	if delta := releaseScore - qualityScore; delta != 0 {
		breakdown = append(breakdown, map[string]any{"label": "Modificatori release", "value": delta})
	}
	probe := release.Title
	if release.Series != nil {
		probe = *release.Series
	}
	var matchedSeries any
	for index := range cfg.Series {
		series := &cfg.Series[index]
		if !series.Enabled {
			continue
		}
		matched := SeriesNamesMatch(series.Name, probe)
		if !matched {
			for _, alias := range series.Aliases {
				if SeriesNamesMatch(alias, probe) {
					matched = true
					break
				}
			}
		}
		if matched {
			matchedSeries = series.Name
			break
		}
	}
	var matchedMovie any
	for index := range cfg.Movies {
		movie := &cfg.Movies[index]
		if movie.Enabled && SeriesNamesMatch(movie.Name, probe) {
			matchedMovie = movie.Name
			break
		}
	}
	jsonResponse(w, map[string]any{
		"ok":             true,
		"kind":           release.Kind,
		"series":         release.Series,
		"season":         release.Season,
		"episode":        release.Episode,
		"is_pack":        release.IsPack,
		"year":           release.Year,
		"quality":        quality,
		"base_score":     qualityScore,
		"score":          releaseScore,
		"breakdown":      breakdown,
		"allowed":        cfg.ReleaseAllowed(release),
		"matched_series": matchedSeries,
		"matched_movie":  matchedMovie,
	})
}

// settingNeverWritable lists keys no endpoint may write, even when a writable
// prefix covers them. gxtorrent_binary used to select a custom executable; it
// was removed so a wrong path cannot break the managed engine. The binary is
// always the one installed next to gexttod (or, as a fallback, the one in the
// PATH).
var settingNeverWritable = map[string]bool{"gxtorrent_binary": true}

// settingWritableAPIOnly lists the writable keys that have no row in the
// settings index: they are edited by dedicated editors (indexers, filters,
// web search engines, rename format) or only through the API.
var settingWritableAPIOnly = map[string]bool{
	"content_filters": true, "dry_run": true, "flaresolverr_url": true,
	"indexers": true, "rename_format": true, "rename_template": true, "rename_movie_template": true,
	"url": true, "websearch_engines": true,
	// Developer-only import of an external verified-file report; it has no
	// row in the settings page.
	"media_integrity_verified_manifest": true,
}

var settingWritablePrefixes = []string{
	"torrent_", "delay_", "housekeeping_", "media_info_", "score_", "tvdb_",
	"simkl_", "backup_", "notify_", "jellyfin_", "plex_", "gxtorrent_",
}

// SaveSetting handles POST /api/config/settings.
func SaveSettingHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SettingInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !settingKeyAllowed(input.Key) || len(input.Key) > 128 || len(input.Value) > 4096 {
		jsonError(w, http.StatusBadRequest, "setting is not writable through this endpoint")
		return
	}
	value := input.Value
	if input.Key == "indexers" {
		var indexers []IndexerConfig
		if err := json.Unmarshal([]byte(value), &indexers); err != nil {
			jsonError(w, http.StatusBadRequest, "indexers must be a JSON array")
			return
		}
		current := latestConfig(s)
		for i := range indexers {
			if indexers[i].APIKey != "" {
				continue
			}
			for _, old := range current.Indexers {
				if old.Name == indexers[i].Name && old.URL == indexers[i].URL {
					indexers[i].APIKey = old.APIKey
					break
				}
			}
		}
		encoded, err := json.Marshal(indexers)
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		value = string(encoded)
	}
	if len(value) > 4096 {
		jsonError(w, http.StatusBadRequest, "setting is too large")
		return
	}
	// A value typed in a shown unit (hours) is stored in its stored unit (seconds).
	value = uiSettingStoredNumber(input.Key, value)
	if err := validateSettingValue(input.Key, value, latestConfig(s)); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := saveConfigSetting(s.cfg.DataDir, input.Key, value); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A value written by the user is theirs: the guided tuning will not propose
	// it again. An empty value is a reset to the default, so the origin is
	// forgotten instead.
	if strings.TrimSpace(value) == "" {
		_ = ClearSettingOrigin(s.cfg.DataDir, input.Key)
	} else {
		_ = MarkSettingOrigin(s.cfg.DataDir, input.Key, OriginManual, "")
	}
	if strings.HasPrefix(input.Key, "ed2k_") || strings.HasPrefix(input.Key, "amule_") {
		ed2kInvalidateHealth()
	}
	jsonResponse(w, map[string]any{"ok": true, "restart_required": true})
}

func parseSettingInt(cfg *Config, key string) int64 {
	if cfg == nil {
		return 0
	}
	value, ok := cfg.Settings[key]
	if !ok {
		return 0
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func settingKeyAllowed(key string) bool {
	if key == "" || settingNeverWritable[key] {
		return false
	}
	// Every option the settings page shows is writable by construction: an
	// indexed key that this function rejected would render as an editable field
	// whose save always failed with «chiave non modificabile».
	if uiSettingIndexed[key] || settingWritableAPIOnly[key] {
		return true
	}
	// Engine and integration families: the API writes keys of these families
	// that have no row of their own in the settings page.
	for _, prefix := range settingWritablePrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
