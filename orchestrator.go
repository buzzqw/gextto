package gextto

// orchestrator.go implements the core module: one acquisition cycle,
// from the optional comics pass through feed scraping, archive/pending
// reconciliation, gap filling, candidate selection and download start.
//
// Cross-module calls rely on the following (not written at the time of this
// port) root helpers; see the assumptions listed at the bottom of the file:
//
//	comics.go ComicsDb, GetComicsClient, NewGetComicsClient, RunComicsCycle
//	libtorrent.go LibtorrentClient.List/AddWithPath/AddFileWithPath, FreeSpaceBytes
//	cleaner.go IndexArchive
//	postprocess.go DownloadDirFor
//	tmdb.go TmdbClient (already written)

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/rules"
	"github.com/buzzqw/gextto/internal/utils"
)

// cycleDivider is the visual boundary that makes each cycle easy to locate in
// the log.
const cycleDivider = "══════════════════════════════════════════════════════════════"

// RunCycle runs a full cycle (no domain restriction).
func RunCycle(
	ctx context.Context,
	cfg *Config,
	engine *Engine,
	db *Database,
	archive *Archive,
	comics *ComicsDb,
	notifier *Notifier,
	torrents TorrentEngine,
) (*models.CycleStats, error) {
	return RunCycleDomain(ctx, cfg, engine, db, archive, comics, notifier, torrents, nil)
}

// RunCycleDomain runs a cycle restricted to an optional domain (`series`,
// `movies` or `comics`). A nil domain means "full".
func RunCycleDomain(
	ctx context.Context,
	cfg *Config,
	engine *Engine,
	db *Database,
	archive *Archive,
	comics *ComicsDb,
	notifier *Notifier,
	torrents TorrentEngine,
	domain *string,
) (*models.CycleStats, error) {
	startedAt := time.Now().UTC()
	stats := &models.CycleStats{LastStartedAt: &startedAt}
	mode := "full"
	if domain != nil {
		mode = *domain
	}
	logging.Info(cycleDivider)
	logging.Info(fmt.Sprintf("🔄 CYCLE STARTED (mode: %s)", mode))
	if cycleCancelled(ctx) {
		return stats, nil
	}

	comicsIntervalValue, err := comics.Setting("comics_check_interval", "604800")
	if err != nil {
		return nil, err
	}
	comicsInterval, parseErr := strconv.ParseInt(strings.TrimSpace(comicsIntervalValue), 10, 64)
	if parseErr != nil {
		comicsInterval = 604800
	}
	if comicsInterval < 0 {
		comicsInterval = 0
	}
	lastComicsValue, err := comics.Setting("last_comics_check_ts", "0")
	if err != nil {
		return nil, err
	}
	lastComicsCheck, parseErr := strconv.ParseInt(strings.TrimSpace(lastComicsValue), 10, 64)
	if parseErr != nil {
		lastComicsCheck = 0
	}
	nowTs := time.Now().UTC().Unix()
	// An explicitly requested comics cycle must start immediately instead of
	// waiting for the automatic interval; otherwise the Comics button appears
	// to do nothing when the next scheduled check is not due yet.
	comicsRequested := domain != nil && *domain == "comics"
	if cycleCancelled(ctx) {
		return stats, nil
	}
	if !domainIs(domain, "series") && !domainIs(domain, "movies") &&
		(comicsRequested || comicsInterval == 0 || saturatingSub(nowTs, lastComicsCheck) >= comicsInterval) {
		downloaded, runErr := RunComicsCycle(comics, NewGetComicsClient(), notifier, cfg.LibtorrentDir, torrents, db, cfg)
		if runErr != nil {
			stats.Error("comics")
			logging.Warn("comics cycle failed", "error", runErr)
		} else {
			logging.Info("comics cycle completed", "downloaded", downloaded)
		}
		if err := comics.SetSetting("last_comics_check_ts", strconv.FormatInt(nowTs, 10)); err != nil {
			return nil, err
		}
	}
	if domainIs(domain, "comics") {
		if err := db.SaveCycle(stats); err != nil {
			return nil, err
		}
		return stats, nil
	}

	// Tracked torrents that are no longer in the session would remain "active"
	// without reconciliation and block re-downloads forever. An external backend
	// may return an empty/stale snapshot while offline; that is never evidence
	// that every tracked torrent was removed.
	snapshot := torrents.List() // refresh an external backend before health check
	health, reportsHealth := torrents.(TorrentSessionHealth)
	if reportsHealth && !health.SessionHealthy() {
		logging.Warn("torrent reconciliation skipped: backend snapshot unavailable")
	} else {
		liveHashes := map[string]struct{}{}
		for _, torrent := range snapshot {
			liveHashes[strings.ToLower(torrent.Hash)] = struct{}{}
		}
		if count, err := db.ReconcileMissingTorrents(liveHashes); err != nil {
			logging.Warn("torrent reconciliation failed", "error", err)
		} else if count > 0 {
			logging.Info("torrents reconciled: marked missing from session", "count", count)
		}
	}
	// A known missing episode must not wait for the broad title sweep below.
	// That sweep fans out across every monitored title and can take many minutes
	// when public web engines are slow. Archive candidates are already local and
	// are therefore evaluated and started first; the normal cycle still runs
	// afterwards to discover new releases and to cover gaps not in the archive.
	if !domainIs(domain, "movies") {
		if err := prioritizeArchiveGapDownloads(ctx, cfg, db, archive, notifier, torrents, stats); err != nil {
			if cycleCancelled(ctx) {
				return stats, nil
			}
			return nil, err
		}
	}

	releases, err := engine.ScrapeAll(ctx, cfg)
	if err != nil {
		if cycleCancelled(ctx) {
			return stats, nil
		}
		return nil, err
	}
	if domainIs(domain, "series") {
		releases = retainKind(releases, "series")
	}
	if domainIs(domain, "movies") {
		releases = retainKind(releases, "movie")
	}

	// Do not keep or reconsider releases whose infohash was permanently
	// rejected (for example a season pack whose real files belong to another
	// season). This also removes stale conflicting rows from the archive. The
	// blocklist does not change during candidate selection, so keep one
	// snapshot for all feed/archive/pending lookups.
	// Fail closed: if the blocklist cannot be read we must not start downloads,
	// or a DB error would silently disable the blocklist.
	blocklistedHashes, err := db.BlocklistedHashes()
	if err != nil {
		return nil, fmt.Errorf("load blocklist: %w", err)
	}
	blockedHashes := map[string]struct{}{}
	for i := range releases {
		if hash, ok := utils.MagnetHash(releases[i].Magnet); ok {
			if _, blocked := blocklistedHashes[hash]; blocked {
				blockedHashes[hash] = struct{}{}
			}
		}
	}
	for hash := range blockedHashes {
		if _, err := archive.RemoveHash(hash); err != nil {
			return nil, err
		}
	}
	if len(blockedHashes) > 0 {
		logging.Info("blocked releases removed from this cycle", "count", len(blockedHashes))
		var kept []models.Release
		for i := range releases {
			hash, ok := utils.MagnetHash(releases[i].Magnet)
			if !ok {
				kept = append(kept, releases[i])
				continue
			}
			if _, blocked := blockedHashes[hash]; !blocked {
				kept = append(kept, releases[i])
			}
		}
		releases = kept
	}
	if err := archive.SaveBatch(releases, cfg); err != nil {
		return nil, err
	}
	// "Seen from feed": record every collected release, including unmonitored
	// titles, so it remains available for archive browsing.
	if err := db.RecordSeenBatch(releases, cfg); err != nil {
		logging.Debug("feed seen recording failed", "error", err)
	}

	var archiveQueries []string
	seenArchiveQueries := map[string]struct{}{}
	addArchiveQuery := func(query string) {
		if _, ok := seenArchiveQueries[query]; ok {
			return
		}
		seenArchiveQueries[query] = struct{}{}
		archiveQueries = append(archiveQueries, query)
	}
	for i := range cfg.Series {
		series := &cfg.Series[i]
		if !series.Enabled || domainIs(domain, "movies") {
			continue
		}
		addArchiveQuery(series.Name)
		for _, alias := range series.Aliases {
			if strings.TrimSpace(alias) != "" {
				addArchiveQuery(alias)
			}
		}
	}
	if !domainIs(domain, "series") {
		for i := range cfg.Movies {
			movie := &cfg.Movies[i]
			if movie.Enabled {
				addArchiveQuery(movie.Name)
				if orig := strings.TrimSpace(movie.OriginalTitle); orig != "" && !strings.EqualFold(orig, movie.Name) {
					addArchiveQuery(orig)
				}
			}
		}
	}
	archiveHashes := map[string]struct{}{}
	archiveMatches := 0
	for _, query := range archiveQueries {
		items, err := archive.Search(query)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			hash, ok := utils.MagnetHash(item[1])
			if !ok {
				continue
			}
			if _, blocked := blocklistedHashes[hash]; blocked {
				continue
			}
			if _, seen := archiveHashes[hash]; seen {
				continue
			}
			archiveHashes[hash] = struct{}{}
			release := ParseRelease(item[0], item[1], "archive:"+item[2])
			if release == nil {
				continue
			}
			if reason := cfg.AllReleaseDeniedReason(release); reason != "" {
				if cfg.ReleaseIsMonitored(release) {
					rules.LogRejection(release, reason)
				}
			} else {
				releases = append(releases, *release)
				archiveMatches++
			}
		}
	}
	logging.Info(fmt.Sprintf(
		"🔎 Archive: matched %d releases from archive across %d monitored targets",
		archiveMatches,
		len(archiveQueries),
	))

	readyPending := map[string]struct{}{}
	pending, err := db.ReadyPending()
	if err != nil {
		return nil, err
	}
	for i := range pending {
		release := ParseRelease(pending[i].Title, pending[i].Magnet, "timeframe")
		if release == nil {
			continue
		}
		if reason := cfg.AllReleaseDeniedReason(release); reason != "" {
			if cfg.ReleaseIsMonitored(release) {
				rules.LogRejection(release, reason)
			}
			continue
		}
		if hash, ok := utils.MagnetHash(release.Magnet); ok {
			if _, blocked := blocklistedHashes[hash]; blocked {
				continue
			}
			readyPending[hash] = struct{}{}
		}
		releases = append(releases, *release)
	}
	// Movies held back by a delay profile.
	readyPendingMovies, err := db.ReadyPendingMovies()
	if err != nil {
		return nil, err
	}
	for i := range readyPendingMovies {
		release := ParseRelease(readyPendingMovies[i].Title, readyPendingMovies[i].Magnet, "delay")
		if release == nil {
			continue
		}
		if reason := cfg.AllReleaseDeniedReason(release); reason != "" {
			if cfg.ReleaseIsMonitored(release) {
				rules.LogRejection(release, reason)
			}
			continue
		}
		if hash, ok := utils.MagnetHash(release.Magnet); ok {
			if _, blocked := blocklistedHashes[hash]; blocked {
				continue
			}
			readyPending[hash] = struct{}{}
		}
		releases = append(releases, *release)
	}

	if !domainIs(domain, "movies") {
		refreshSeriesMetadata(ctx, cfg, db)
	}

	gapFilling := true
	if value, ok := cfg.Settings["gap_filling"]; ok {
		gapFilling = value == "yes" || value == "true" || value == "1"
	}
	var archiveGaps []SeriesGap
	if domainIs(domain, "movies") || !gapFilling {
		archiveGaps = nil
	} else {
		gaps, err := db.ArchiveGaps()
		if err != nil {
			return nil, err
		}
		// Respect the current configuration: only existing, enabled series and
		// monitored seasons (neither disabled nor outside `seasons`).
		for i := range gaps {
			season := gaps[i].Season
			if cfg.FindSeriesMatch(gaps[i].Series, &season) != nil {
				archiveGaps = append(archiveGaps, gaps[i])
			}
		}
	}

	type seriesSeason struct {
		Series string
		Season int64
	}
	gapSummary := map[seriesSeason][]int64{}
	for i := range archiveGaps {
		key := seriesSeason{Series: archiveGaps[i].Series, Season: archiveGaps[i].Season}
		gapSummary[key] = append(gapSummary[key], archiveGaps[i].Episode)
	}
	for key, episodes := range gapSummary {
		slices.Sort(episodes)
		// Only at debug: the full list of open gaps is noisy. The log reports a
		// gap when it is actually filled.
		logging.Debug(fmt.Sprintf("→ %s S%02d gap: %s", key.Series, key.Season, episodesLabel(episodes)))
	}

	gapLimit := 0
	if value, ok := cfg.Settings["gap_fill_max_per_series"]; ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed >= 0 {
			gapLimit = parsed
		}
	}
	deepIntervalHours := int64(6)
	if value, ok := cfg.Settings["gap_deep_interval_hours"]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			deepIntervalHours = parsed
		}
	}
	if deepIntervalHours < 1 {
		deepIntervalHours = 1
	}
	deepMaxPerCycle := 5
	if value, ok := cfg.Settings["gap_deep_max_per_cycle"]; ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed >= 0 {
			deepMaxPerCycle = parsed
		}
	}
	deepNow := time.Now().UTC().Unix()
	lastDeep := int64(0)
	if value, ok := cfg.Settings["last_deep_gap_fill_ts"]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			lastDeep = parsed
		}
	}
	isDeep := saturatingSub(deepNow, lastDeep) >= saturatingMul(deepIntervalHours, 3600)
	if isDeep {
		_ = SaveSetting(cfg.DataDir, "last_deep_gap_fill_ts", strconv.FormatInt(deepNow, 10))
	}
	deepLabel := "archive pass"
	if isDeep {
		deepLabel = fmt.Sprintf("deep pass, up to %d online searches", deepMaxPerCycle)
	}
	logging.Info(fmt.Sprintf(
		"🔎 Gap fill: %d missing episode(s) to check · %s",
		len(archiveGaps),
		deepLabel,
	))

	// Phase 1 (every cycle, free): local archive search only.
	type liveCandidate struct {
		Series  *SeriesConfig
		Season  int64
		Episode int64
	}
	var liveCandidates []liveCandidate
	livePerSeries := map[string]int{}
	archiveHits := 0
	gapTargets := map[gapTarget]struct{}{}
	for i := range archiveGaps {
		gapTargets[gapTarget{Series: archiveGaps[i].Series, Season: archiveGaps[i].Season, Episode: archiveGaps[i].Episode}] = struct{}{}
	}
	for i := range archiveGaps {
		gap := archiveGaps[i]
		season := gap.Season
		series := cfg.FindSeriesMatch(gap.Series, &season)
		if series == nil {
			continue
		}
		query := fmt.Sprintf("%s S%02dE%02d", gap.Series, gap.Season, gap.Episode)
		found := false
		if items, err := archive.Search(query); err == nil {
			for _, item := range items {
				hash, ok := utils.MagnetHash(item[1])
				if !ok {
					continue
				}
				if _, blocked := blocklistedHashes[hash]; blocked {
					continue
				}
				release := ParseRelease(item[0], item[1], "archive:"+item[2])
				if !releaseMatchesConfiguredEpisode(release, series, gap.Season, gap.Episode) {
					continue
				}
				if reason := cfg.AllReleaseDeniedReason(release); reason != "" {
					if cfg.ReleaseIsMonitored(release) {
						rules.LogRejection(release, reason)
					}
					continue
				}
				if !cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title) {
					continue
				}
				canonicalName := series.Name
				release.Series = &canonicalName
				logging.Debug("archive release found",
					"series", gap.Series,
					"season", gap.Season,
					"episode", gap.Episode,
					"title", release.Title,
					"source", release.Source)
				releases = append(releases, *release)
				found = true
			}
		}
		if found {
			archiveHits++
			continue
		}
		// Phase 2 candidate: deep live search only.
		if !isDeep || len(liveCandidates) >= deepMaxPerCycle {
			continue
		}
		if gapLimit > 0 && livePerSeries[gap.Series] >= gapLimit {
			continue
		}
		if recentlySearched, err := db.GapRecentlySearched(gap.Series, gap.Season, gap.Episode, 23); err != nil {
			return nil, err
		} else if recentlySearched {
			continue
		}
		livePerSeries[gap.Series]++
		liveCandidates = append(liveCandidates, liveCandidate{
			Series:  series,
			Season:  gap.Season,
			Episode: gap.Episode,
		})
	}
	logging.Info(fmt.Sprintf(
		"🔎 Gap fill: %d found in the archive, %d to look up online",
		archiveHits,
		len(liveCandidates),
	))

	if len(liveCandidates) > 0 {
		foundResults := make([][]models.Release, len(liveCandidates))
		var gapWG sync.WaitGroup
		gapSem := make(chan struct{}, 3)
		for i := range liveCandidates {
			candidate := liveCandidates[i]
			gapWG.Add(1)
			gapSem <- struct{}{}
			go func(index int, candidate liveCandidate) {
				defer gapWG.Done()
				defer func() { <-gapSem }()
				query := fmt.Sprintf("%s S%02dE%02d", candidate.Series.Name, candidate.Season, candidate.Episode)
				logging.Debug("gap-fill live search started",
					"series", candidate.Series.Name,
					"season", candidate.Season,
					"episode", candidate.Episode,
					"query", query)
				started := time.Now()
				appendValid := func(found []models.Release) {
					for i := range found {
						release := found[i]
						if !releaseMatchesConfiguredEpisode(&release, candidate.Series, candidate.Season, candidate.Episode) ||
							cfg.AllReleaseDeniedReason(&release) != "" ||
							!cfg.SeriesReleaseAllowed(candidate.Series, &release.Quality, release.Title) {
							continue
						}
						canonicalName := candidate.Series.Name
						release.Series = &canonicalName
						foundResults[index] = append(foundResults[index], release)
					}
				}
				appendValid(engine.SearchSeriesEpisode(ctx, cfg, candidate.Series, candidate.Season, candidate.Episode, false))
				if len(foundResults[index]) == 0 {
					seasonQuery := fmt.Sprintf("%s S%02d", candidate.Series.Name, candidate.Season)
					logging.Debug("gap-fill season fallback started",
						"series", candidate.Series.Name,
						"season", candidate.Season,
						"episode", candidate.Episode,
						"query", seasonQuery)
					appendValid(engine.SearchSeriesSeason(ctx, cfg, candidate.Series, candidate.Season, false))
				}
				logging.Debug("gap-fill live search completed",
					"series", candidate.Series.Name,
					"season", candidate.Season,
					"episode", candidate.Episode,
					"query", query,
					"elapsed_ms", time.Since(started).Milliseconds(),
					"results", len(foundResults[index]))
			}(i, candidate)
		}
		gapWG.Wait()
		for i := range liveCandidates {
			releases = append(releases, foundResults[i]...)
			_ = db.MarkGapSearched(liveCandidates[i].Series.Name, liveCandidates[i].Season, liveCandidates[i].Episode)
		}
	}
	if domainIs(domain, "series") {
		releases = retainKind(releases, "series")
	}
	if domainIs(domain, "movies") {
		releases = retainKind(releases, "movie")
	}
	stats.Scraped = len(releases)

	var best []models.Release
	// Smallest archived episode per series, for the adaptive size floor.
	seriesMinCache := map[string]*int64{}
	for i := range releases {
		release := releases[i]
		if release.Kind == "series" {
			if release.Series == nil {
				continue
			}
			seriesName := *release.Series
			series := cfg.FindSeriesMatch(seriesName, release.Season)
			if series == nil {
				// Not monitored: never log these, they are pure noise.
				continue
			}
			if !cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title) {
				logCandidateRejected(&release, "excluded by the series quality/language/exclude rules")
				continue
			}
			name := series.Name
			release.Series = &name
			seriesMin, cached := seriesMinCache[series.Name]
			if !cached {
				seriesMin, _ = db.SeriesArchivedMinSize(series.Name)
				seriesMinCache[series.Name] = seriesMin
			}
			if reason := rules.SaneSizeDeniedReason(&release, seriesMin); reason != "" {
				rules.LogRejection(&release, reason)
				continue
			}
		} else {
			movie := cfg.FindMovieMatchForRelease(&release, false)
			if movie == nil {
				// Not monitored: never log these, they are pure noise.
				continue
			}
			if !cfg.MovieReleaseAllowedForTitle(movie, &release.Quality, release.Title) {
				logCandidateRejected(&release, "excluded by the movie quality/language/subtitle rules")
				continue
			}
			release.Title = movie.Name
			if year, err := strconv.ParseInt(strings.TrimSpace(movie.Year), 10, 64); err == nil {
				release.Year = &year
			}
			movieMin, _ := db.MovieArchivedSize(movie.Name, release.Year)
			if reason := rules.SaneSizeDeniedReason(&release, movieMin); reason != "" {
				rules.LogRejection(&release, reason)
				continue
			}
		}
		score := cfg.ReleaseScore(&release)
		if release.Kind == "series" {
			seriesName := ""
			if release.Series != nil {
				seriesName = *release.Series
			}
			season := int64(0)
			if release.Season != nil {
				season = *release.Season
			}
			rangeSet := episodeSet(&release)
			complete := hasCompleteRange(&release)
			superseded := false
			for j := range best {
				old := &best[j]
				if old.Kind != "series" || old.Series == nil || *old.Series != seriesName ||
					old.Season == nil || *old.Season != season {
					continue
				}
				oldRange := episodeSet(old)
				oldComplete := hasCompleteRange(old)
				condition := ((complete && oldComplete) ||
					(!complete && oldComplete && incumbentWins(&release, score, old, cfg)) ||
					(!complete && !oldComplete && intSetSuperset(oldRange, rangeSet))) &&
					incumbentWins(&release, score, old, cfg)
				if condition {
					superseded = true
					break
				}
			}
			if superseded {
				// Rejected because the selection already contains an equal or
				// better release: routine deduplication, not an INFO event.
				logging.Debug("🚫 release rejected (superseded)",
					"target", releaseTarget(&release),
					"score", score,
					"reason", "superseded by an equal or better release already selected")
				continue
			}
			var filtered []models.Release
			for j := range best {
				old := &best[j]
				if old.Kind != "series" || old.Series == nil || *old.Series != seriesName ||
					old.Season == nil || *old.Season != season {
					filtered = append(filtered, *old)
					continue
				}
				oldRange := episodeSet(old)
				oldComplete := hasCompleteRange(old)
				remove := (complete || (len(rangeSet) > 1 && intSetSuperset(rangeSet, oldRange))) &&
					!incumbentWins(&release, score, old, cfg) &&
					(!oldComplete || complete)
				if !remove {
					filtered = append(filtered, *old)
				}
			}
			best = filtered
			index := -1
			for j := range best {
				old := &best[j]
				if old.Kind == "series" && old.Series != nil && *old.Series == seriesName &&
					old.Season != nil && *old.Season == season &&
					slices.Equal(old.EpisodeRange, release.EpisodeRange) {
					index = j
					break
				}
			}
			if index >= 0 {
				if !incumbentWins(&release, score, &best[index], cfg) {
					best[index] = release
				}
			} else {
				best = append(best, release)
			}
		} else {
			index := -1
			for j := range best {
				old := &best[j]
				if old.Kind == "movie" && old.Title == release.Title && optionalInt64Equal(old.Year, release.Year) {
					index = j
					break
				}
			}
			if index >= 0 {
				if !incumbentWins(&release, score, &best[index], cfg) {
					best[index] = release
				}
			} else {
				best = append(best, release)
			}
		}
	}
	stats.Candidates = len(best)
	logging.Info(fmt.Sprintf("🎯 CANDIDATES — %d release(s) survived the filters", len(best)))
	for i := range best {
		release := &best[i]
		logging.Debug("candidate ready for evaluation",
			"target", releaseTarget(release),
			"kind", release.Kind,
			"episodes", episodesLabel(release.EpisodeRange),
			"gap_episodes", episodesLabel(gapEpisodesForRelease(release, gapTargets)),
			"source", release.Source,
			"score", cfg.ReleaseScore(release))
	}

	// Advanced setting: refuse to start downloads when the download disk is
	// almost full. `min_free_space_gb = 0` disables the guard.
	var minFreeBytes *uint64
	if value, ok := cfg.Settings["min_free_space_gb"]; ok {
		if gib, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && gib > 0 {
			bytes := uint64(gib * 1024.0 * 1024.0 * 1024.0)
			minFreeBytes = &bytes
		}
	}
	if minFreeBytes != nil {
		free := FreeSpaceBytes(cfg.LibtorrentDir)
		if free == nil {
			logging.Warn("cycle: cannot determine free space, downloads skipped",
				"path", cfg.LibtorrentDir,
				"minimum", logging.HumanBytes(*minFreeBytes))
			stats.Error("min_free_space_unavailable")
			return stats, nil
		}
		if *free < *minFreeBytes {
			logging.Warn("cycle: free space below min_free_space_gb, downloads skipped",
				"free", logging.HumanBytes(*free),
				"minimum", logging.HumanBytes(*minFreeBytes))
			stats.Error("min_free_space")
			return stats, nil
		}
	}

	emptyArchiveIndex := &models.ArchiveQualityIndex{}
	archiveIndexCache := map[string]*models.ArchiveQualityIndex{}
	// Downloads currently in the libtorrent session: do not propose an existing
	// hash or episode again, even when the database has not recorded it yet.
	liveDownloads := &models.LiveDownloads{
		Hashes:   map[string]struct{}{},
		Episodes: map[models.LiveEpisodeKey]struct{}{},
	}
	for _, torrent := range torrents.List() {
		liveDownloads.Hashes[strings.ToLower(torrent.Hash)] = struct{}{}
		if key := ParseEpisodeKey(torrent.Name); key != nil {
			liveDownloads.Episodes[*key] = struct{}{}
		}
	}

	upgrades := 0
	newItems := 0
	var newDetails []string
	var upgradeDetails []string
	if cycleCancelled(ctx) {
		return stats, nil
	}
	for i := range best {
		// Stop before starting another download when the daemon is shutting
		// down: the native session is destroyed right after the workers stop.
		if cycleCancelled(ctx) {
			return stats, nil
		}
		release := best[i]
		// Older persisted entries and a few third-party feeds can store a valid
		// magnet in TorrentURL. Recover it before deciding that a torrent file
		// must be fetched over HTTP; a magnet is already directly usable by the
		// torrent engine and must never be passed to HTTPGetBytes.
		promoteTorrentURLMagnet(&release)
		// Feeds that expose only a `.torrent` link (for example TorrentLeech)
		// have no magnet: download the file, derive its infohash, and retain the
		// file for adding it (private trackers require it for announcing).
		var torrentFile *string
		if strings.TrimSpace(release.Magnet) == "" {
			if release.TorrentURL != nil {
				magnet, path, err := resolveTorrentURL(ctx, engine, cfg, *release.TorrentURL, &release.Title)
				if err != nil {
					stats.Error("torrent_link")
					logging.Warn("torrent link resolution failed",
						"target", releaseTarget(&release),
						"error", err)
					continue
				}
				release.Magnet = magnet
				// The feed was persisted before selection. Replace its
				// short-lived Jackett URL with the stable magnet so a later
				// archive search can use it without re-downloading an already
				// expired link.
				if err := archive.CanonicalizeTorrentURL(*release.TorrentURL, release.Magnet); err != nil {
					logging.Warn("could not canonicalize archived torrent URL", "error", err)
				}
				if path != "" {
					torrentFile = &path
				}
			}
		}
		reconcilePackIdentityFromMagnet(&release)
		isReadyPending := false
		if hash, ok := utils.MagnetHash(release.Magnet); ok {
			if _, ok := readyPending[hash]; ok {
				isReadyPending = true
			}
		}
		releaseScore := cfg.ReleaseScore(&release)
		// A candidate that fills a known archive gap, or that scores above the
		// bypass threshold, is never held by a delay profile.
		gapEpisodes := gapEpisodesForRelease(&release, gapTargets)
		isGap := len(gapEpisodes) > 0
		bypassDelay := isGap || (cfg.DelayBypassScore() > 0 && releaseScore >= cfg.DelayBypassScore())
		if release.Kind == "series" && !isReadyPending {
			seriesName := ""
			if release.Series != nil {
				seriesName = *release.Series
			}
			if series := cfg.FindSeriesMatch(seriesName, release.Season); series != nil {
				// Per-series `timeframe` (hours) wins over the global setting.
				delayMinutes := cfg.DelayMinutes("series")
				if series.Timeframe > 0 {
					delayMinutes = saturatingMul(series.Timeframe, 60)
				}
				if delayMinutes > 0 && !bypassDelay {
					logging.Info("queued for delay",
						"target", releaseTarget(&release),
						"delay_minutes", delayMinutes)
					if err := db.QueuePendingScored(&release, delayMinutes, releaseScore); err != nil {
						return nil, err
					}
					continue
				}
			}
		}
		if release.Kind == "movie" && !isReadyPending {
			delayMinutes := cfg.DelayMinutes("movie")
			if delayMinutes > 0 && !bypassDelay {
				logging.Info("movie queued for delay",
					"target", releaseTarget(&release),
					"delay_minutes", delayMinutes)
				if err := db.QueuePendingMovieScored(&release, delayMinutes, releaseScore); err != nil {
					return nil, err
				}
				continue
			}
		}

		// Archive index (per series, computed once per cycle): decisions also
		// consider real files on disk, not only database rows.
		archiveIndex := emptyArchiveIndex
		if release.Kind == "series" {
			key := ""
			if release.Series != nil {
				key = *release.Series
			}
			if cached, ok := archiveIndexCache[key]; ok {
				archiveIndex = cached
			} else {
				computed := &models.ArchiveQualityIndex{}
				if series := cfg.FindSeriesMatch(key, release.Season); series != nil {
					archivePath := ""
					if resolved := cfg.ResolveArchivePath(series); resolved != nil {
						archivePath = *resolved
					}
					indexed := IndexArchive(series.Name, archivePath, cfg.Settings)
					computed = &indexed
				}
				archiveIndexCache[key] = computed
				archiveIndex = computed
			}
		}
		// Per-title "allow upgrades" toggle (default on). The gap flag feeds the
		// always-on "no orphan older episode" best practice.
		forbidUpgrade := false
		if release.Kind == "series" {
			seriesName := ""
			if release.Series != nil {
				seriesName = *release.Series
			}
			if series := cfg.FindSeriesMatch(seriesName, release.Season); series != nil {
				forbidUpgrade = series.DisableUpgrades
			}
		} else {
			if movie := cfg.FindMovieMatchForRelease(&release, false); movie != nil {
				forbidUpgrade = movie.DisableUpgrades
			}
		}
		approvalContext := &models.ApprovalContext{
			Archive:       archiveIndex,
			Live:          liveDownloads,
			ForbidUpgrade: forbidUpgrade,
			GapEpisode:    isGap,
		}
		var approved bool
		var approvalReason string
		if release.Kind == "series" {
			approved, approvalReason, err = db.CheckSeriesScored(&release, releaseScore, cfg.UpgradeMinScoreDiff, approvalContext)
		} else {
			approved, approvalReason, err = db.CheckMovieScoredWith(&release, releaseScore, cfg.UpgradeMinScoreDiff, forbidUpgrade)
		}
		if err != nil {
			return nil, err
		}
		score := releaseScore
		fromArchive := strings.HasPrefix(release.Source, "archive:")
		decisionReason := approvalReason
		if fromArchive {
			decisionReason = "gap_filled"
		} else if len(gapEpisodes) > 0 {
			decisionReason = "gap_fill"
		}
		if approved {
			var preferredPath *string
			if downloadDir, ok := DownloadDirFor(&release, cfg); ok {
				preferredPath = &downloadDir
			} else if !ReleaseFitsRamdisk(&release, cfg) {
				// The known size already exceeds the RAM disk threshold: download
				// straight to disk instead of staging a multi-gigabyte season pack
				// on the tmpfs and moving it out right after the metadata arrives.
				if dir, ok := ramdiskOverflowDir(cfg); ok {
					preferredPath = &dir
				}
			}
			added := false
			if torrentFile != nil && *torrentFile != "" {
				added, err = torrents.AddFileWithPath(*torrentFile, cfg, preferredPath)
			} else {
				added, err = torrents.AddWithPath(release.Magnet, cfg, preferredPath)
			}
			if err != nil {
				if rollbackErr := db.RollbackRelease(&release); rollbackErr != nil {
					logging.Warn("release rollback failed", "error", rollbackErr)
				}
				// A single release refused by the engine must not abort the whole
				// cycle: the placeholder is already rolled back, so record the
				// failure and continue with the remaining candidates.
				stats.Error("add_failed")
				logging.Warn("release add failed; continuing the cycle", "target", releaseTarget(&release), "error", err)
				continue
			}
			if !added {
				if err := db.RollbackRelease(&release); err != nil {
					return nil, err
				}
				stats.Error("torrent_rejected")
				continue
			}
			startedDetail := fmt.Sprintf("%s [%s]: %s · qualità: %s · score: %d",
				releaseTarget(&release), release.Source, release.Title,
				releaseQualityLabel(&release), score)
			if len(gapEpisodes) > 0 {
				startedDetail = fmt.Sprintf("%s · episodi %s · [%s]: %s · qualità: %s · score: %d",
					releaseTarget(&release), episodesLabel(gapEpisodes), release.Source,
					release.Title, releaseQualityLabel(&release), score)
			}
			if approvalReason == "upgrade" {
				upgradeDetails = append(upgradeDetails, startedDetail)
			} else {
				newDetails = append(newDetails, startedDetail)
			}
			if err := db.RegisterTorrentScored(&release, score); err != nil {
				return nil, err
			}
			if hash, ok := utils.MagnetHash(release.Magnet); ok {
				_ = db.SetTorrentReason(hash, decisionReason)
			}
			logging.Info("📥 download started",
				"target", releaseTarget(&release),
				"title", release.Title,
				"source", release.Source,
				"quality", releaseQualityLabel(&release),
				"score", score,
				"reason", decisionReason,
				"approval_reason", approvalReason,
				"gap_episodes", episodesLabel(gapEpisodes))
			if isReadyPending {
				if release.Series != nil && release.Season != nil && release.Episode != nil {
					if err := db.RemovePending(*release.Series, *release.Season, *release.Episode); err != nil {
						return nil, err
					}
				} else if release.Kind == "movie" {
					if err := db.RemovePendingMovie(release.Title, release.Year); err != nil {
						return nil, err
					}
				}
			}
			stats.DownloadsStarted++
			if approvalReason == "upgrade" {
				upgrades++
			} else {
				newItems++
			}
			if fromArchive || len(gapEpisodes) > 0 {
				stats.GapsFilled++
			}
			var magnetHashValue any
			if hash, ok := utils.MagnetHash(release.Magnet); ok {
				magnetHashValue = hash
			}
			payload := map[string]any{
				"title":           release.Title,
				"kind":            release.Kind,
				"series":          release.Series,
				"season":          release.Season,
				"episode":         release.Episode,
				"source":          release.Source,
				"magnet_hash":     magnetHashValue,
				"quality_score":   score,
				"reason":          decisionReason,
				"approval_reason": approvalReason,
				"gap_episodes":    gapEpisodes,
				"resolution":      release.Quality.Resolution,
				"video_codec":     release.Quality.Codec,
				"audio":           release.Quality.Audio,
				"hdr":             release.Quality.HDR,
				"language":        release.Quality.Language,
			}
			if err := notifier.NotifyEvent("download_started", payload); err != nil {
				logging.Warn("download notification failed", "error", err)
			}
		} else {
			// "Already present / already active" rejections (duplicate or episode
			// active in the session) are routine, so keep them at debug level to
			// keep the cycle log readable.
			if approvalReason == "duplicate" || approvalReason == "active_episode" {
				logging.Debug("download skipped (already present or active)",
					"target", releaseTarget(&release),
					"score", score,
					"reason", decisionReason,
					"approval_reason", approvalReason)
			} else {
				logging.Info("⏭️ download skipped",
					"target", releaseTarget(&release),
					"kind", release.Kind,
					"source", release.Source,
					"score", score,
					"reason", decisionReason,
					"approval_reason", approvalReason,
					"gap_episodes", episodesLabel(gapEpisodes))
			}
		}
	}
	if err := db.SaveCycle(stats); err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	if stats.LastStartedAt != nil {
		started = *stats.LastStartedAt
	}
	elapsed := int64(time.Since(started).Seconds())
	if elapsed < 0 {
		elapsed = 0
	}
	logging.Info(fmt.Sprintf(
		"📊 CYCLE REPORT — duration %s — scraped: %d | candidates: %d | downloads started: %d (upgrade: %d · new: %d) | gaps filled: %d | errors: %d",
		humanDuration(elapsed),
		stats.Scraped,
		stats.Candidates,
		stats.DownloadsStarted,
		upgrades,
		newItems,
		stats.GapsFilled,
		stats.Errors,
	))
	if len(newDetails) > 0 {
		logging.Info("🆕 NEW DOWNLOADS — " + strings.Join(newDetails, " · "))
	}
	if len(upgradeDetails) > 0 {
		logging.Info("⬆️ UPGRADES — " + strings.Join(upgradeDetails, " · "))
	}
	if len(newDetails) == 0 && len(upgradeDetails) == 0 {
		logging.Info("📦 CYCLE DOWNLOADS — no downloads started")
	}
	logging.Info(cycleDivider)
	return stats, nil
}

// prioritizeArchiveGapDownloads starts archive-backed missing episodes before
// the expensive all-title remote search. It intentionally uses the same
// approval and torrent-registration path as the main candidate loop, while
// limiting its input to releases that demonstrably cover a current gap.
func prioritizeArchiveGapDownloads(
	ctx context.Context,
	cfg *Config,
	db *Database,
	archive *Archive,
	notifier *Notifier,
	torrents TorrentEngine,
	stats *models.CycleStats,
) error {
	gapFilling := true
	if value, ok := cfg.Settings["gap_filling"]; ok {
		gapFilling = value == "yes" || value == "true" || value == "1"
	}
	if !gapFilling {
		return nil
	}
	gaps, err := db.ArchiveGaps()
	if err != nil {
		return err
	}
	blocklisted, err := db.BlocklistedHashes()
	if err != nil {
		return fmt.Errorf("load blocklist for priority gaps: %w", err)
	}

	type priorityCandidate struct {
		release models.Release
		gaps    map[gapTarget]struct{}
		score   int64
	}
	candidates := map[string]*priorityCandidate{}
	for _, gap := range gaps {
		season := gap.Season
		series := cfg.FindSeriesMatch(gap.Series, &season)
		if series == nil {
			continue
		}
		query := fmt.Sprintf("%s S%02dE%02d", gap.Series, gap.Season, gap.Episode)
		items, searchErr := archive.Search(query)
		if searchErr != nil {
			return searchErr
		}
		for _, item := range items {
			hash, ok := utils.MagnetHash(item[1])
			if !ok {
				continue
			}
			if _, blocked := blocklisted[hash]; blocked {
				continue
			}
			release := ParseRelease(item[0], item[1], "archive:"+item[2])
			if !releaseMatchesConfiguredEpisode(release, series, gap.Season, gap.Episode) {
				continue
			}
			if cfg.AllReleaseDeniedReason(release) != "" || !cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title) {
				continue
			}
			// Keep the configured spelling so the database, archive index and
			// torrent metadata all identify the same monitored series.
			canonicalName := series.Name
			release.Series = &canonicalName
			score := cfg.ReleaseScore(release)
			candidate, exists := candidates[hash]
			if !exists || score > candidate.score {
				gapSet := map[gapTarget]struct{}{}
				if candidate != nil {
					for existingGap := range candidate.gaps {
						gapSet[existingGap] = struct{}{}
					}
				}
				candidate = &priorityCandidate{release: *release, gaps: gapSet, score: score}
				candidates[hash] = candidate
			}
			candidate.gaps[gapTarget{Series: series.Name, Season: gap.Season, Episode: gap.Episode}] = struct{}{}
		}
	}
	if len(candidates) == 0 {
		logging.Info("priority gap pass: no archive candidates")
		return nil
	}

	live := &models.LiveDownloads{Hashes: map[string]struct{}{}, Episodes: map[models.LiveEpisodeKey]struct{}{}}
	for _, torrent := range torrents.List() {
		live.Hashes[strings.ToLower(torrent.Hash)] = struct{}{}
		if key := ParseEpisodeKey(torrent.Name); key != nil {
			live.Episodes[*key] = struct{}{}
		}
	}
	logging.Info("priority gap pass: archive candidates ready", "count", len(candidates))
	started := 0
	for hash, candidate := range candidates {
		if cycleCancelled(ctx) {
			return nil
		}
		release := candidate.release
		series := cfg.FindSeriesMatch(*release.Series, release.Season)
		if series == nil {
			continue
		}
		approved, reason, err := db.CheckSeriesScored(&release, candidate.score, cfg.UpgradeMinScoreDiff, &models.ApprovalContext{
			Archive:       &models.ArchiveQualityIndex{},
			Live:          live,
			ForbidUpgrade: series.DisableUpgrades,
			GapEpisode:    true,
		})
		if err != nil {
			return err
		}
		if !approved {
			logging.Debug("priority gap skipped", "target", releaseTarget(&release), "reason", reason)
			continue
		}
		var preferredPath *string
		if directory, ok := DownloadDirFor(&release, cfg); ok {
			preferredPath = &directory
		}
		added, err := torrents.AddWithPath(release.Magnet, cfg, preferredPath)
		if err != nil {
			_ = db.RollbackRelease(&release)
			stats.Error("add_failed")
			logging.Warn("priority gap add failed", "target", releaseTarget(&release), "error", err)
			continue
		}
		if !added {
			if err := db.RollbackRelease(&release); err != nil {
				return err
			}
			stats.Error("torrent_rejected")
			continue
		}
		if err := db.RegisterTorrentScored(&release, candidate.score); err != nil {
			return err
		}
		_ = db.SetTorrentReason(hash, "gap_filled")
		live.Hashes[hash] = struct{}{}
		for gap := range candidate.gaps {
			live.Episodes[models.LiveEpisodeKey{Series: gap.Series, Season: gap.Season, Episode: gap.Episode}] = struct{}{}
		}
		stats.DownloadsStarted++
		stats.GapsFilled++
		started++
		logging.Info("✅ priority gap download started", "target", releaseTarget(&release), "source", release.Source, "score", candidate.score)
		if err := notifier.NotifyEvent("download_started", map[string]any{
			"title": release.Title, "kind": release.Kind, "series": release.Series, "season": release.Season,
			"episode": release.Episode, "source": release.Source, "magnet_hash": hash,
			"quality_score": candidate.score, "reason": "gap_filled", "approval_reason": reason,
		}); err != nil {
			logging.Warn("download notification failed", "error", err)
		}
	}
	logging.Info("priority gap pass completed", "started", started, "candidates", len(candidates))
	return nil
}

func releaseHasEpisode(release *models.Release, episode int64) bool {
	if release.Episode != nil && *release.Episode == episode {
		return true
	}
	for _, item := range release.EpisodeRange {
		if item == episode {
			return true
		}
	}
	return false
}

// releaseMatchesConfiguredEpisode verifies the identity of a gap hit before it
// suppresses an online lookup or enters candidate selection. Archive FTS is a
// discovery mechanism, not proof that its result covers the requested episode.
func releaseMatchesConfiguredEpisode(release *models.Release, series *SeriesConfig, season, episode int64) bool {
	if release == nil || series == nil || release.Kind != "series" || release.Series == nil || release.Season == nil {
		return false
	}
	if *release.Season != season || (!releaseHasEpisode(release, episode) && !hasCompleteRange(release)) {
		return false
	}
	if SeriesNamesMatch(series.Name, *release.Series) {
		return true
	}
	for _, alias := range series.Aliases {
		if SeriesNamesMatch(alias, *release.Series) {
			return true
		}
	}
	return false
}

// domainIs reports whether the optional domain equals value.
// cycleCancelled reports whether the daemon is shutting down and the cycle must
// stop before it touches the torrent engine again. It logs once per call site so
// an aborted cycle is visible in the log.
func cycleCancelled(ctx context.Context) bool {
	if ctx == nil || ctx.Err() == nil {
		return false
	}
	logging.Info("cycle interrupted: daemon is shutting down")
	return true
}

func domainIs(domain *string, value string) bool {
	return domain != nil && *domain == value
}

// retainKind keeps only the releases of the requested kind.
func retainKind(releases []models.Release, kind string) []models.Release {
	kept := make([]models.Release, 0, len(releases))
	for i := range releases {
		if releases[i].Kind == kind {
			kept = append(kept, releases[i])
		}
	}
	return kept
}

// saturatingSub implements `i64::saturating_sub`.
func saturatingSub(a, b int64) int64 {
	result := a - b
	if b > 0 && result > a {
		return math.MinInt64
	}
	if b < 0 && result < a {
		return math.MaxInt64
	}
	return result
}

// saturatingMul implements `i64::saturating_mul`.
func saturatingMul(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	result := a * b
	if result/b != a || (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
		if (a > 0) == (b > 0) {
			return math.MaxInt64
		}
		return math.MinInt64
	}
	return result
}

// releaseTarget is the human-readable target of a release: `Series S01E02`,
// `Series S01`, `Series S01pack`, or the title for movies. Keeps log lines free
// of pointer noise.
func releaseTarget(release *models.Release) string {
	if release.Kind == "series" && release.Series != nil {
		season := ""
		if release.Season != nil {
			season = fmt.Sprintf("S%02d", *release.Season)
		}
		episode := ""
		if release.Episode != nil {
			if *release.Episode == 0 {
				episode = "pack"
			} else {
				episode = fmt.Sprintf("E%02d", *release.Episode)
			}
		}
		suffix := season + episode
		if suffix == "" {
			return *release.Series
		}
		return *release.Series + " " + suffix
	}
	return release.Title
}

// reconcilePackIdentityFromMagnet prefers the torrent display name's season and
// episode range over the (possibly wrong) indexer title for season packs.
func reconcilePackIdentityFromMagnet(release *models.Release) {
	if release.Kind != "series" || !release.IsPack {
		return
	}
	parsed, err := url.Parse(release.Magnet)
	if err != nil {
		return
	}
	displayName := ""
	found := false
	for key, values := range parsed.Query() {
		if key == "dn" && len(values) > 0 {
			displayName = values[0]
			found = true
			break
		}
	}
	if !found || strings.TrimSpace(displayName) == "" || displayName == release.Title {
		return
	}
	corrected := ReconcilePackIdentity(release, displayName)
	if corrected == nil {
		return
	}
	logging.Warn("correcting season-pack identity from torrent display name",
		"title", release.Title,
		"torrent_name", displayName,
		"declared_season", release.Season,
		"torrent_season", corrected.Season)
	*release = *corrected
}

// episodesLabel renders `1,2,3`, or `none` when empty; avoids `[]` in the log.
func episodesLabel(episodes []int64) string {
	if len(episodes) == 0 {
		return "none"
	}
	parts := make([]string, len(episodes))
	for i, episode := range episodes {
		parts[i] = strconv.FormatInt(episode, 10)
	}
	return strings.Join(parts, ",")
}

// releaseQualityLabel renders the quality attributes that made a release
// useful to the selector. It keeps cycle download logs actionable instead of
// reporting only the aggregate score.
func releaseQualityLabel(release *models.Release) string {
	quality := release.Quality
	parts := make([]string, 0, 6)
	for _, value := range []string{
		quality.Resolution,
		quality.Source,
		quality.Codec,
		quality.Audio,
		quality.HDR,
		quality.Language,
	} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	if quality.IsDV {
		parts = append(parts, "Dolby Vision")
	}
	if len(parts) == 0 {
		return "unclassified"
	}
	return strings.Join(parts, "/")
}

// logCandidateRejected logs, at debug only, a monitored release that does not
// meet the quality/language/exclude rules (thousands per cycle).
func logCandidateRejected(release *models.Release, reason string) {
	logging.Debug("release excluded by rules",
		"target", releaseTarget(release),
		"kind", release.Kind,
		"source", release.Source,
		"reason", reason)
}

// humanDuration is the local `human_duration` of orchestrator.rs (distinct from
// the logging package one: the minutes are not zero-padded).
func humanDuration(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, seconds%60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// resolveTorrentURL downloads a `.torrent`, derives its v1 infohash, and saves
// it in the state dir, or resolves an HTTP redirect to a magnet link.
// Returns the equivalent magnet and the file path used when adding it (empty
// when a magnet was resolved directly).
func resolveTorrentURL(ctx context.Context, engine *Engine, cfg *Config, rawURL string, fallbackTitle *string) (string, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "magnet:?") {
		if magnet, ok := utils.SanitizeMagnet(rawURL, fallbackTitle); ok {
			return magnet, "", nil
		}
		return rawURL, "", nil
	}
	if !IsTorrentURL(rawURL) {
		return "", "", fmt.Errorf("torrent source is not an HTTP(S) torrent link")
	}
	payload, magnet, err := engine.FetchTorrent(ctx, rawURL)
	if err != nil {
		return "", "", err
	}
	if magnet != "" {
		if sanitized, ok := utils.SanitizeMagnet(magnet, fallbackTitle); ok {
			return sanitized, "", nil
		}
		return magnet, "", nil
	}
	hash, ok := utils.TorrentInfoHash(payload)
	if !ok {
		return "", "", fmt.Errorf("invalid torrent payload")
	}
	dir := filepath.Join(cfg.StateDir, "feed_torrents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	path := filepath.Join(dir, hash+".torrent")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", "", err
	}
	return "magnet:?xt=urn:btih:" + hash, path, nil
}

// promoteTorrentURLMagnet repairs a legacy/malformed release that carries a
// valid magnet in TorrentURL instead of Magnet. It returns true when a direct
// magnet source was recovered.
func promoteTorrentURLMagnet(release *models.Release) bool {
	if release == nil || strings.TrimSpace(release.Magnet) != "" || release.TorrentURL == nil {
		return false
	}
	magnet, ok := utils.SanitizeMagnet(strings.TrimSpace(*release.TorrentURL), &release.Title)
	if !ok {
		return false
	}
	release.Magnet = magnet
	release.TorrentURL = nil
	return true
}

// incumbentWins is true when `incumbent` must not be replaced by `candidate`:
// at equal scores a REMUX (full-resolution version) wins, especially when it is
// the remux of an episode that was already selected or downloaded.
func incumbentWins(candidate *models.Release, candidateScore int64, incumbent *models.Release, cfg *Config) bool {
	// Use the same policy-aware score the candidate was ranked with, so score
	// rules and custom formats cannot make the two sides inconsistent.
	incumbentScore := cfg.ReleaseScore(incumbent)
	return incumbentScore > candidateScore ||
		(incumbentScore == candidateScore &&
			!(candidate.Quality.IsRemux() && !incumbent.Quality.IsRemux()))
}

// gapTarget identifies one open archive gap.
type gapTarget struct {
	Series  string
	Season  int64
	Episode int64
}

// gapEpisodesForRelease lists the sorted, deduplicated gap episodes covered by
// a release.
func gapEpisodesForRelease(release *models.Release, gapTargets map[gapTarget]struct{}) []int64 {
	if release.Series == nil || release.Season == nil {
		return nil
	}
	series := *release.Series
	season := *release.Season
	var episodes []int64
	if hasCompleteRange(release) {
		for target := range gapTargets {
			if target.Series == series && target.Season == season {
				episodes = append(episodes, target.Episode)
			}
		}
	} else {
		for _, episode := range release.EpisodeRange {
			if episode <= 0 {
				continue
			}
			if _, ok := gapTargets[gapTarget{Series: series, Season: season, Episode: episode}]; ok {
				episodes = append(episodes, episode)
			}
		}
	}
	slices.Sort(episodes)
	return slices.Compact(episodes)
}

// episodeSet returns the positive episodes of a release as a set.
func episodeSet(release *models.Release) map[int64]struct{} {
	set := map[int64]struct{}{}
	for _, episode := range release.EpisodeRange {
		if episode > 0 {
			set[episode] = struct{}{}
		}
	}
	return set
}

// hasCompleteRange reports the `0` sentinel that marks a complete season pack.
func hasCompleteRange(release *models.Release) bool {
	return slices.Contains(release.EpisodeRange, 0)
}

// intSetSuperset reports whether `superset` contains every element of `subset`.
func intSetSuperset(superset, subset map[int64]struct{}) bool {
	for value := range subset {
		if _, ok := superset[value]; !ok {
			return false
		}
	}
	return true
}

// refreshSeriesMetadata refreshes the TMDB season counts and status of enabled
// series when their metadata is stale or the status was never persisted.
func refreshSeriesMetadata(ctx context.Context, cfg *Config, db *Database) {
	if cfg.TmdbAPIKey == nil {
		return
	}
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	knownStatuses, _ := db.SeriesStatuses()
	for i := range cfg.Series {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		series := &cfg.Series[i]
		if !series.Enabled {
			continue
		}
		stale, err := db.SeriesMetadataStale(series.Name, 24)
		if err != nil {
			stale = true
		}
		// Fetch the TMDB status even when season metadata is recent if the
		// status has never been persisted (the first refresh).
		_, known := knownStatuses[series.Name]
		needsStatus := !known
		if !stale && !needsStatus {
			continue
		}
		var tmdbID *string
		if strings.TrimSpace(series.TmdbID) != "" {
			id := series.TmdbID
			tmdbID = &id
		} else {
			resolved, err := tmdb.ResolveSeriesID(ctx, series.Name)
			if err == nil {
				tmdbID = resolved
			}
		}
		if tmdbID == nil {
			continue
		}
		if stale {
			counts, err := tmdb.SeasonCounts(ctx, *tmdbID)
			if err == nil {
				values := make([][2]int64, 0, len(counts))
				for season, count := range counts {
					values = append(values, [2]int64{season, count})
				}
				if err := db.SaveSeriesMetadata(series.Name, values); err != nil {
					logging.Warn("TMDB season metadata save failed", "series", series.Name, "error", err)
				}
			} else {
				logging.Debug("TMDB season metadata refresh failed", "series", series.Name, "error", err)
			}
		}
		// Persist TMDB status ("Ended"/"Returning Series") for the series-list
		// badge.
		if info, err := tmdb.SeriesInfo(ctx, series.Name, tmdbID); err == nil && info != nil {
			status, _ := info["status"].(string)
			lastAirDate, _ := info["last_air_date"].(string)
			if err := db.SaveSeriesStatus(series.Name, status, lastAirDate); err != nil {
				logging.Warn("series status save failed", "series", series.Name, "error", err)
			}
		}
	}
}

// Cross-module calls reconciled with the shared ports:
//
//	libtorrent.LibtorrentClient.List() []models.TorrentView
//	libtorrent.LibtorrentClient.AddWithPath(magnet string, cfg *Config,
// preferredPath *string) (bool, error)
//	libtorrent.LibtorrentClient.AddFileWithPath(torrentPath string, cfg *Config,
// preferredPath *string) (bool, error)
//	libtorrent.FreeSpaceBytes(path string) *uint64
//	cleaner.IndexArchive(seriesName, archivePath string,
// settings map[string]string) models.ArchiveQualityIndex
//	postprocess.DownloadDirFor(release *models.Release, cfg *Config) (string, bool)
//
// The comics module was implemented separately; this file assumes:
//
//	comics.ComicsDb.Setting(key, defaultValue string) (string, error)
//	comics.ComicsDb.SetSetting(key, value string) error
//	comics.NewGetComicsClient() *GetComicsClient
//	comics.RunComicsCycle(comics *ComicsDb, client *GetComicsClient,
// notifier *Notifier, defaultRoot string, torrents *LibtorrentClient,
// mainDB *Database, cfg *Config) (int, error)
