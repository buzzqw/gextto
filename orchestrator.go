package gextto

// orchestrator.go implements the core module: one acquisition cycle,
// from the optional comics pass through feed scraping, archive/pending
// reconciliation, gap filling, candidate selection and download start.
//
// Cross-module calls rely on the following (not written at the time of this
// port) root helpers; see the assumptions listed at the bottom of the file:
//
//	comics.go ComicsDb, GetComicsClient, NewGetComicsClient, RunComicsCycle
//	transfer_policy.go FreeSpaceBytes, DiagnoseTorrent
//	torrent_engine.go TorrentSession (List/AddWithPath/AddFileWithPath)
//	cleaner.go IndexArchive
//	postprocess.go DownloadDirFor
//	tmdb.go TmdbClient (already written)

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/rules"
	"github.com/buzzqw/gextto/internal/utils"
)

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
	logging.Info(fmt.Sprintf("🔄 Search started (%s)", cycleModeLabel(mode)))
	if cycleCancelled(ctx) {
		return stats, nil
	}

	// A scoring-weight change (from the settings page or the JSON API) leaves
	// every stored quality score stale: the next comparison would then look like
	// a genuine improvement and re-download content that already has the right
	// quality. Recompute them before the cycle reads any score. The fingerprint
	// check makes this a no-op unless a weight actually changed, so it does not
	// touch the library on a normal cycle.
	if !cfg.DryRun && db != nil {
		if updated, err := db.SyncScoresWithSettings(cfg); err != nil {
			logging.Warn("stored score refresh failed", "error", err)
		} else if updated > 0 {
			logging.Info(fmt.Sprintf("The scoring settings changed: %s updated", countLabel(updated, "stored score", "stored scores")))
		}
	}
	// The guided tuning's slow loop: once per cycle, re-fit the values the system
	// already chose (never the user's) when the facts moved. The cycle is the
	// natural cadence, and it keeps the values gentle rather than wobbling.
	if !cfg.DryRun && settingTruthy(cfg.Settings["autotune_adaptive"]) {
		if changed := RunAdaptiveTuning(cfg.DataDir, cfg); changed > 0 {
			logging.Info("🛠️ Automatic tuning adjusted values for this hardware", "changed", changed)
		}
	}

	// The comics settings live in their own database: read them only when this
	// cycle can run comics, and never let a comics database problem abort a
	// series/movies cycle.
	if !domainIs(domain, "series") && !domainIs(domain, "movies") {
		runComicsIfDue(ctx, cfg, db, comics, notifier, torrents, stats, domain != nil && *domain == "comics")
	}
	if cycleCancelled(ctx) {
		return stats, nil
	}
	if domainIs(domain, "comics") {
		stats.DurationSeconds = int(cycleElapsedSeconds(stats))
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
		// Adds a previous process left half done are finished or undone first,
		// so the reconciliation below sees their final torrent_meta rows.
		if !cfg.DryRun {
			if _, err := recoverInterruptedAdds(db, torrents, liveHashes, reportsHealth || len(snapshot) > 0, processStartedAt); err != nil {
				logging.Warn("interrupted torrent adds could not be checked", "error", err)
			}
		}
		if count, err := db.ReconcileMissingTorrents(liveHashes); err != nil {
			logging.Warn("torrent reconciliation failed", "error", err)
		} else if count > 0 {
			logging.Info(fmt.Sprintf("🧹 %s no longer in the torrent engine; marked as removed", countLabel(int64(count), "download was", "downloads were")))
		}
	}
	// A known missing episode must not wait for the broad title sweep below.
	// That sweep fans out across every monitored title and can take many minutes
	// when public web engines are slow. Archive candidates are already local and
	// are therefore evaluated and started first; the normal cycle still runs
	// afterwards to discover new releases and to cover gaps not in the archive.
	// Dry-run must be a true no-op: never start the archive-gap priority pass,
	// because it approves releases and writes placeholders/torrent rows.
	if !domainIs(domain, "movies") && !cfg.DryRun {
		if err := prioritizeArchiveGapDownloads(ctx, cfg, db, archive, notifier, torrents, stats); err != nil {
			if cycleCancelled(ctx) {
				return stats, nil
			}
			return nil, err
		}
	}

	releases, sources, err := engine.ScrapeAll(ctx, cfg)
	if err != nil {
		if cycleCancelled(ctx) {
			return stats, nil
		}
		return nil, err
	}
	stats.Sources = sources
	// Anime numbered by absolute episode become season/episode releases
	// before anything is filtered by kind.
	releases = resolveAnimeReleases(ctx, cfg, releases)
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
		logging.Debug("blocked releases removed from this cycle", "count", len(blockedHashes))
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
	if !cfg.DryRun {
		if err := archive.SaveBatch(releases, cfg); err != nil {
			return nil, err
		}
		// "Seen from feed": record every collected release, including unmonitored
		// titles, so it remains available for archive browsing.
		if err := db.RecordSeenBatch(releases, cfg); err != nil {
			logging.Warn("feed seen recording failed", "error", err)
		}
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
	// The feed releases were just saved into the archive, so the archive
	// queries below return them again: seed the set with the feed hashes so
	// each release is evaluated (and counted) once.
	archiveHashes := map[string]struct{}{}
	for i := range releases {
		if hash, ok := utils.MagnetHash(releases[i].Magnet); ok {
			archiveHashes[hash] = struct{}{}
		}
	}
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
	logging.Debug(fmt.Sprintf(
		"archive: matched %d releases from archive across %d monitored targets",
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
		if hash, ok := releaseHash(release); ok {
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
		if hash, ok := releaseHash(release); ok {
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
		gapFilling = settingTruthy(value)
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
		if logging.Enabled(logging.LevelDebug) {
			logging.Debug(fmt.Sprintf("→ %s S%02d gap: %s", key.Series, key.Season, episodesLabel(episodes)))
		}
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
	// How long a gap searched online is left alone (`gap_research_hours`,
	// default 23). Never shorter than the deep interval, or every deep pass
	// would search the same gaps again.
	gapResearchHours := int64(23)
	if value, ok := cfg.Settings["gap_research_hours"]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && parsed > 0 {
			gapResearchHours = parsed
		}
	}
	if gapResearchHours < deepIntervalHours {
		gapResearchHours = deepIntervalHours
	}
	deepNow := time.Now().UTC().Unix()
	lastDeep := int64(0)
	if value, ok := cfg.Settings["last_deep_gap_fill_ts"]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			lastDeep = parsed
		}
	}
	isDeep := saturatingSub(deepNow, lastDeep) >= saturatingMul(deepIntervalHours, 3600)
	deepLabel := "in the local release archive"
	if isDeep {
		deepLabel = fmt.Sprintf("in the local release archive, plus up to %d online searches", deepMaxPerCycle)
	}
	if len(archiveGaps) > 0 {
		logging.Info(fmt.Sprintf("🧩 Looking for %s %s",
			countLabel(int64(len(archiveGaps)), "missing episode", "missing episodes"), deepLabel))
	}

	// Phase 1 (every cycle, free): local archive search only.
	var liveCandidates []liveCandidate
	var waiting []waitingGap
	livePerSeries := map[string]int{}
	archiveHits := 0
	gapTargets := map[gapTarget]struct{}{}
	// Copies the archive holds only outside the profile: the cycle may take
	// them as a last resort, but only after the online search for the same gap
	// found nothing better, like the live ones.
	archiveLastResorts := map[gapTarget][]models.Release{}
	// One query for the search times of every gap: a point query per gap made
	// the deep pass cost grow with the library.
	searchTimes, err := db.GapSearchedTimes()
	if err != nil {
		return nil, err
	}
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
		if items, err := searchArchiveForEpisode(ctx, cfg, archive, series, query, gap.Season, gap.Episode); err == nil {
			for _, item := range items {
				hash, ok := utils.MagnetHash(item[1])
				if !ok {
					continue
				}
				if _, blocked := blocklistedHashes[hash]; blocked {
					continue
				}
				release := parseArchiveRelease(ctx, cfg, series, item)
				if !releaseMatchesConfiguredEpisode(release, series, gap.Season, gap.Episode) {
					continue
				}
				if reason := cfg.AllReleaseDeniedReason(release); reason != "" {
					if cfg.ReleaseIsMonitored(release) {
						rules.LogRejection(release, reason)
					}
					continue
				}
				canonicalName := series.Name
				release.Series = &canonicalName
				if !cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title) {
					if cfg.SeriesLastResortAllowed(series, &release.Quality, release.Title) {
						release.LastResort = true
						key := gapTarget{Series: canonicalName, Season: gap.Season, Episode: gap.Episode}
						archiveLastResorts[key] = append(archiveLastResorts[key], *release)
					}
					continue
				}
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
		if !isDeep {
			continue
		}
		// The log is keyed by the configured name, which may differ from the
		// archive title when the gap came in through an alias.
		last, searched := searchTimes[gapTarget{Series: series.Name, Season: gap.Season, Episode: gap.Episode}]
		if searched && int64(time.Now().UTC().Sub(last).Hours()) < gapResearchHours {
			continue
		}
		waiting = append(waiting, waitingGap{
			candidate: liveCandidate{Series: series, Season: gap.Season, Episode: gap.Episode},
			last:      last,
			searched:  searched,
		})
	}
	orderWaitingGaps(waiting, cfg.Settings["gap_deep_cursor"])
	deepCursor := ""
	for _, entry := range waiting {
		if len(liveCandidates) >= deepMaxPerCycle {
			break
		}
		name := entry.candidate.Series.Name
		if gapLimit > 0 && livePerSeries[name] >= gapLimit {
			continue
		}
		livePerSeries[name]++
		liveCandidates = append(liveCandidates, entry.candidate)
		if !entry.searched {
			deepCursor = name
		}
	}
	logging.Debug(fmt.Sprintf(
		"gap fill: %d found in the archive, %d to look up online",
		archiveHits,
		len(liveCandidates),
	))

	if len(liveCandidates) > 0 {
		foundResults := make([][]models.Release, len(liveCandidates))
		// Copies in the right language that miss only the resolution of the
		// profile: taken when nothing else exists for the episode.
		lastResorts := make([][]models.Release, len(liveCandidates))
		var gapWG sync.WaitGroup
		gapSem := make(chan struct{}, 3)
		for i := range liveCandidates {
			candidate := liveCandidates[i]
			gapWG.Add(1)
			gapSem <- struct{}{}
			go func(index int, candidate liveCandidate) {
				defer recoverGoroutine("gap-fill live search")
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
							cfg.AllReleaseDeniedReason(&release) != "" {
							continue
						}
						canonicalName := candidate.Series.Name
						release.Series = &canonicalName
						if !cfg.SeriesReleaseAllowed(candidate.Series, &release.Quality, release.Title) {
							if cfg.SeriesLastResortAllowed(candidate.Series, &release.Quality, release.Title) {
								release.LastResort = true
								lastResorts[index] = append(lastResorts[index], release)
							}
							continue
						}
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
				// Last resort, and only when the operator explicitly enabled the
				// automatic eD2k fallback and aMule is reachable.
				if len(foundResults[index]) == 0 && ed2kFallbackEligible(cfg) {
					before := len(foundResults[index])
					appendValid(engine.SearchSeriesED2K(ctx, cfg, candidate.Series, candidate.Season, candidate.Episode, false))
					// Logged only when it found something: a gap with nothing on
					// eD2k either would repeat the same line every cycle.
					if found := len(foundResults[index]) - before; found > 0 {
						logging.Info(fmt.Sprintf("🔎 eD2k found %s for the missing %s S%02dE%02d",
							countLabel(found, "candidate", "candidates"), candidate.Series.Name, candidate.Season, candidate.Episode))
					}
				}
				if len(foundResults[index]) == 0 && len(lastResorts[index]) > 0 {
					foundResults[index] = lastResorts[index]
					logging.Info(fmt.Sprintf("🆘 Nothing within the quality profile for the missing %s S%02dE%02d: %s in the right language but outside the resolution, taken as a last resort",
						candidate.Series.Name, candidate.Season, candidate.Episode,
						countLabel(int64(len(lastResorts[index])), "copy", "copies")))
				}
				// The archive may hold the only copy, outside the profile: the
				// live search does not look there, so the pass adds it now that
				// every online source has answered.
				if len(foundResults[index]) == 0 {
					key := gapTarget{Series: candidate.Series.Name, Season: candidate.Season, Episode: candidate.Episode}
					if archived := archiveLastResorts[key]; len(archived) > 0 {
						foundResults[index] = archived
						logging.Info(fmt.Sprintf("🆘 Nothing within the quality profile for the missing %s S%02dE%02d: %s from the release archive, taken as a last resort",
							candidate.Series.Name, candidate.Season, candidate.Episode,
							countLabel(int64(len(archived)), "copy", "copies")))
					}
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
		// A cancelled cycle returns empty searches: do not record those gaps as
		// searched, or they would be skipped for the next 23 hours.
		searchesValid := !cycleCancelled(ctx)
		for i := range liveCandidates {
			releases = append(releases, foundResults[i]...)
			if searchesValid {
				_ = db.MarkGapSearched(liveCandidates[i].Series.Name, liveCandidates[i].Season, liveCandidates[i].Episode)
			}
		}
	}
	// Record the deep pass only once it actually ran: saving the timestamp
	// before the searches lost the pass for gap_deep_interval_hours whenever the
	// cycle was cancelled or failed in between.
	if isDeep && !cycleCancelled(ctx) {
		if err := SaveSetting(cfg.DataDir, "last_deep_gap_fill_ts", strconv.FormatInt(deepNow, 10)); err != nil {
			logging.Warn("could not record the deep gap pass time", "error", err)
		}
		if deepCursor != "" {
			if err := SaveSetting(cfg.DataDir, "gap_deep_cursor", deepCursor); err != nil {
				logging.Warn("could not record the deep gap pass cursor", "error", err)
			}
		}
	}
	// Anime numbered by absolute episode become season/episode releases
	// before anything is filtered by kind.
	releases = resolveAnimeReleases(ctx, cfg, releases)
	if domainIs(domain, "series") {
		releases = retainKind(releases, "series")
	}
	if domainIs(domain, "movies") {
		releases = retainKind(releases, "movie")
	}
	// The gap and pending passes can add a release already collected from the
	// feeds or the archive: keep the first copy, so the count is of distinct
	// releases.
	releases = dedupeReleasesByHash(releases)
	stats.Scraped = len(releases)

	var best []models.Release
	// Dead .torrent links (torrent_link_quarantine.go): a URL that failed in two
	// different cycles is not proposed again for a while. Loaded once per cycle:
	// the map is tiny and the release loop is not.
	quarantinedLinks, err := db.QuarantinedTorrentLinks(time.Now())
	if err != nil {
		logging.Debug("could not read the quarantined torrent links", "error", err)
	}
	// Smallest archived episode per series, for the adaptive size floor.
	seriesMinCache := map[string]*int64{}
	for i := range releases {
		release := releases[i]
		// Never let a dead link compete with a working alternative: skipping it
		// here (before the score) leaves the episode open for the next release
		// instead of picking it and failing at the download step.
		if release.Magnet == "" && release.TorrentURL != nil {
			if reason, dead := quarantinedLinks[*release.TorrentURL]; dead {
				// Another copy of the same torrent can still compete.
				if !switchToMirror(&release, mirrorNotQuarantined(quarantinedLinks)) {
					logging.Debug("release skipped: its torrent link is in quarantine",
						"target", releaseTarget(&release),
						"reason", reason)
					continue
				}
			}
		}
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
			if !cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title) &&
				!(release.LastResort && cfg.SeriesLastResortAllowed(series, &release.Quality, release.Title)) {
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
		score := cfg.AcquisitionScore(&release)
		if release.Kind == "series" {
			var superseded bool
			best, superseded = mergeSeriesCandidate(best, release, score, cfg.AcquisitionScore)
			if superseded {
				// Rejected because the selection already contains an equal or
				// better release: routine deduplication, not an INFO event.
				logging.Debug("🚫 release rejected (superseded)",
					"target", releaseTarget(&release),
					"score", score,
					"reason", "superseded by an equal or better release already selected")
				continue
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
	logging.Info(fmt.Sprintf("🎯 %s match your titles and filters; choosing what to download", countLabel(int64(len(best)), "release", "releases")))
	if logging.Enabled(logging.LevelDebug) {
		for i := range best {
			release := &best[i]
			logging.Debug("candidate ready for evaluation",
				"target", releaseTarget(release),
				"kind", release.Kind,
				"episodes", episodesLabel(release.EpisodeRange),
				"gap_episodes", episodesLabel(gapEpisodesForRelease(release, gapTargets)),
				"source", release.Source,
				"score", cfg.AcquisitionScore(release))
		}
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
		free := FreeSpaceBytes(cfg.DownloadDir)
		if free == nil {
			logging.Warn("⚠️ No downloads this time: cannot read the free space of the download folder",
				"folder", cfg.DownloadDir)
			stats.Error("min_free_space_unavailable")
			return finishCycleWithoutDownloads(db, stats)
		}
		if *free < *minFreeBytes {
			logging.Warn(fmt.Sprintf("⚠️ No downloads this time: only %s free on the download disk (minimum %s)",
				logging.HumanBytes(*free), logging.HumanBytes(*minFreeBytes)))
			stats.Error("min_free_space")
			return finishCycleWithoutDownloads(db, stats)
		}
	}

	emptyArchiveIndex := &models.ArchiveQualityIndex{}
	archiveIndexCache := map[string]*models.ArchiveQualityIndex{}
	movieArchiveCache := map[string]*models.ArchiveQuality{}
	// One walk of the archive root per cycle, shared by every movie candidate:
	// walking it once per candidate was the largest filesystem cost of a cycle.
	var movieArchiveIndex *MovieArchiveIndex
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
	// Episodes whose download is stuck for long enough may accept a temporary
	// alternative within a few points, without removing the stuck torrent.
	stallAlternatives, stallScoreDrop := stalledAlternativeContext(cfg, db, torrents)

	upgrades := 0
	var skips cycleSkipCounts
	newItems := 0
	// Distinct `.torrent` links that failed per source in this cycle. Two of
	// them mean the source is broken (an expired login, a dead proxy), not the
	// single torrent.
	linkFailures := map[string]map[string]struct{}{}
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
		// A mirror is another copy of the same torrent from another source
		// (release_dedup.go): when this link fails, the next copy is tried in
		// the same cycle instead of leaving the episode for the next one.
		mirrorUsable := mirrorNotQuarantined(quarantinedLinks)
		linkResolved := true
		resolveLink := func() {
			for strings.TrimSpace(release.Magnet) == "" && release.TorrentURL != nil {
				magnet, path, err := resolveTorrentURL(ctx, engine, cfg, *release.TorrentURL, &release.Title)
				if err != nil {
					failedSource := release.Source
					if errors.Is(err, errTorrentNotAvailable) {
						// The indexer knows the release but cannot serve it yet
						// (mircrew: "no magnet for this request" until the topic
						// is thanked, or a login that needs renewing). It is not
						// a dead link: keep it eligible, no cycle error and no
						// strike. The next cycle retries it. It still counts for
						// the "indexer keeps failing" notice.
						logging.Debug("torrent link not available yet",
							"target", releaseTarget(&release),
							"source", release.Source,
							"error", err)
						noteTorrentLinkFailure(linkFailures, release.Source, *release.TorrentURL, err, notifier)
					} else {
						// Two failures in different cycles mean the link is dead:
						// quarantine it so the archive stops proposing it (and the
						// cycle stops counting it as an error) until the retry.
						if failures, qerr := db.RecordTorrentLinkFailure(*release.TorrentURL, err.Error(), time.Now()); qerr != nil {
							logging.Debug("could not record the failed torrent link", "error", qerr)
						} else if failures == torrentLinkFailureLimit {
							logging.Info(fmt.Sprintf("🚫 The .torrent link of %s from %s failed again: it is set aside for %s",
								releaseTarget(&release), release.Source, logDuration(torrentLinkQuarantine(failures))))
						}
						stats.Error("torrent_link")
						logging.Warn(fmt.Sprintf("⚠️ Could not fetch the .torrent of %s from %s: %v", releaseTarget(&release), release.Source, err))
						if source := strings.TrimSpace(release.Source); source != "" {
							noteTorrentLinkFailure(linkFailures, source, *release.TorrentURL, err, notifier)
						}
					}
					if switchToMirror(&release, mirrorUsable) {
						logging.Info(fmt.Sprintf("🔁 %s: the link from %s did not work, trying the same release from %s",
							logTarget(&release), failedSource, release.Source))
						continue
					}
					linkResolved = false
					break
				}
				// The link works: forget any previous failure so it is eligible
				// again if the indexer breaks later.
				_ = db.ClearTorrentLinkFailure(*release.TorrentURL)
				clearIndexerFailureNotice(release.Source)
				release.Magnet = magnet
				// The feed was persisted before selection. Replace its
				// short-lived Jackett URL with the stable magnet so a later
				// archive search can use it without re-downloading an already
				// expired link. Skipped in dry-run: it rewrites the archive DB.
				if !cfg.DryRun {
					if err := archive.CanonicalizeRelease(&release, release.Magnet); err != nil {
						logging.Warn("could not canonicalize archived torrent URL", "error", err)
					}
				}
				if path != "" {
					torrentFile = &path
				}
			}
		}
		// A link whose infohash the indexer declared (indexarr, Jackett and
		// Prowlarr with "extended") is opened only for the release that is
		// actually downloaded. The decision needs only the hash: opening the
		// link of every candidate cost minutes per cycle, and an aggregator that
		// counts downloads (indexarr) counted each candidate as one. An
		// in-progress pack is checked against its file list after the link is
		// opened (staleInProgressPack).
		//
		// A link without a declared hash (a MirCrew topic whose magnet indexarr
		// fetches on demand) gets a provisional identity derived from the link:
		// the candidate is judged on its title and quality first, and the link
		// is opened only when that judgement would download it. The decision
		// is then made again with the real hash (blocklist, live downloads).
		deferredHash := ""
		provisional := false
		if strings.TrimSpace(release.Magnet) == "" && release.TorrentURL != nil && IsTorrentURL(*release.TorrentURL) {
			if hash := canonicalInfoHash(release.InfoHash); len(hash) == 40 {
				deferredHash = hash
			} else {
				deferredHash = provisionalLinkHash(*release.TorrentURL)
				provisional = true
			}
			release.Magnet = "magnet:?xt=urn:btih:" + deferredHash
		}
		// resolveDeferred opens the deferred link and replaces the hash-only
		// magnet with the real one (and the .torrent file when there is one).
		resolveDeferred := func() bool {
			release.Magnet = ""
			resolveLink()
			if !linkResolved {
				return false
			}
			if hash, ok := utils.MagnetHash(release.Magnet); ok && hash != deferredHash && !provisional {
				logging.Debug("the downloaded torrent has another infohash than the one declared",
					"target", releaseTarget(&release), "declared", deferredHash, "real", hash)
			}
			reconcilePackIdentityFromMagnet(&release)
			deferredHash = ""
			provisional = false
			return true
		}
		if deferredHash == "" {
			resolveLink()
		}
		if !linkResolved {
			continue
		}
		reconcilePackIdentityFromMagnet(&release)
		isReadyPending := false
		if hash, ok := releaseHash(&release); ok {
			if _, ok := readyPending[hash]; ok {
				isReadyPending = true
			}
		}
		// AcquisitionScore chooses between candidates; the approval and the
		// stored score must use ReleaseScore, so a swarm-health bonus can never
		// masquerade as an upgrade of a file already in the library.
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
					logging.Info(fmt.Sprintf("⏳ %s: waiting %s before downloading, in case a better version appears",
						logTarget(&release), logDuration(time.Duration(delayMinutes)*time.Minute)))
					// The queue keeps only the magnet: a provisional identity must
					// become the real one first.
					if provisional {
						if !resolveDeferred() {
							continue
						}
					}
					if err := db.QueuePendingScored(&release, delayMinutes, releaseScore); err != nil {
						return nil, err
					}
					skips.Waiting++
					continue
				}
			}
		}
		if release.Kind == "movie" && !isReadyPending {
			delayMinutes := cfg.DelayMinutes("movie")
			if delayMinutes > 0 && !bypassDelay {
				logging.Info(fmt.Sprintf("⏳ %s: waiting %s before downloading, in case a better version appears",
					logTarget(&release), logDuration(time.Duration(delayMinutes)*time.Minute)))
				// The queue keeps only the magnet: a provisional identity must
				// become the real one first.
				if provisional {
					if !resolveDeferred() {
						continue
					}
				}
				if err := db.QueuePendingMovieScored(&release, delayMinutes, releaseScore); err != nil {
					return nil, err
				}
				skips.Waiting++
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
					indexed := IndexArchive(series.Name, archivePath, cfg)
					computed = &indexed
				}
				archiveIndexCache[key] = computed
				archiveIndex = computed
			}
		}
		// A movie file already on the archive volume is authoritative even when
		// its database row is missing. Scan once per cycle for the same title
		// and year: a full archive walk for every candidate would be wasteful.
		var movieArchive *models.ArchiveQuality
		if release.Kind == "movie" {
			key := strings.ToLower(release.Title) + "\x00"
			if release.Year != nil {
				key += strconv.FormatInt(*release.Year, 10)
			}
			if cached, ok := movieArchiveCache[key]; ok {
				movieArchive = cached
			} else {
				if movieArchiveIndex == nil {
					movieArchiveIndex = BuildMovieArchiveIndex(cfg)
				}
				movieArchive = movieArchiveIndex.Best(cfg, &release)
				movieArchiveCache[key] = movieArchive
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
		// An "in progress" pack declares how many episodes it holds long before
		// the .torrent is updated. The file list is already here: keep the pack
		// when it holds at least one episode we need (a gap or a real upgrade),
		// and skip it only when everything inside is already better on disk.
		// staleInProgressPack reads the file list of an in-progress pack and
		// skips it when everything inside is already better on disk. It can
		// only refuse, so a deferred link is checked once it is opened for a
		// release that would be downloaded.
		staleInProgressPack := func() bool {
			if torrentFile != nil && release.IsPack && release.InProgressEpisodes > 0 {
				reason, checkErr := incompletePackReason(
					*torrentFile, &release, gapEpisodes, archiveIndex,
					releaseScore, cfg.UpgradeMinScoreDiff, cfg.UpgradeUntilScore, forbidUpgrade, cfg.Settings,
				)
				if checkErr != nil {
					logging.Debug("could not read the pack torrent contents",
						"target", releaseTarget(&release), "error", checkErr)
				} else if reason != "" {
					logging.Warn(fmt.Sprintf("⏭️ Season pack skipped — «%s» · %s", release.Title, reason),
						"target", releaseTarget(&release))
					if !cfg.DryRun {
						if release.TorrentURL != nil {
							if qerr := db.QuarantineTorrentLink(*release.TorrentURL, reason, time.Now(), 7*24*time.Hour); qerr != nil {
								logging.Debug("could not quarantine the stale pack link", "error", qerr)
							}
						}
						if berr := db.Blocklist(&release, "in_progress_pack_incomplete"); berr != nil {
							logging.Debug("could not blocklist the stale pack", "error", berr)
						}
					}
					return true
				}
			}
			return false
		}
		if staleInProgressPack() {
			continue
		}
		approvalContext := &models.ApprovalContext{
			Archive:           archiveIndex,
			MovieArchive:      movieArchive,
			Live:              liveDownloads,
			ForbidUpgrade:     forbidUpgrade,
			UpgradeUntilScore: cfg.UpgradeUntilScore,
			ScoreSettings:     cfg.Settings,
			GapEpisode:        isGap,
			DryRun:            cfg.DryRun,
			StallAlternatives: stallAlternatives,
			StallScoreDrop:    stallScoreDrop,
			Blocklist:         blocklistedHashes,
		}
		// Decide only: the library is not written until the torrent is in the
		// engine (see startApprovedRelease).
		approved, approvalReason, err := evaluateReleaseApproval(db, &release, releaseScore, cfg.UpgradeMinScoreDiff, approvalContext, forbidUpgrade, true)
		if err != nil {
			return nil, err
		}
		if approved && provisional {
			if !resolveDeferred() {
				continue
			}
			approved, approvalReason, err = evaluateReleaseApproval(db, &release, releaseScore, cfg.UpgradeMinScoreDiff, approvalContext, forbidUpgrade, true)
			if err != nil {
				return nil, err
			}
		}
		score := releaseScore
		fromArchive := strings.HasPrefix(release.Source, "archive:")
		decisionReason := approvalReason
		if fromArchive {
			decisionReason = "gap_filled"
		} else if len(gapEpisodes) > 0 {
			decisionReason = "gap_fill"
		}
		if release.LastResort {
			decisionReason = "last_resort"
		}
		// An episode whose download is stuck may now accept an alternative
		// within the configured tolerance: mark it so the log, the notification
		// and the torrent's "Motivo" say why this download was started.
		stallStuckScore := int64(0)
		stallApplies := false
		if stuckScore, has := releaseStallScore(&release, stallAlternatives); has && stallScoreDrop > 0 {
			stallStuckScore = stuckScore
			stallApplies = true
			decisionReason = "stall_alternative"
		}
		if approved {
			// The explicit path (or nil, to let the engine choose) is decided by
			// the engine-independent RAM-disk policy: a known-too-big or
			// unknown-size release is staged off the tmpfs, and the RAM disk is
			// used only for backends that support it.
			preferredPath := automaticDownloadPath(&release, cfg, engineSupportsRamdisk(torrents))
			// The global guard measures the default download volume; a tag rule,
			// the RAM-disk overflow or the RAM disk itself can redirect the
			// download to another filesystem, so verify the chosen path too.
			// When no explicit path is forced, mirror the engine fallback so the
			// volume actually used is the one being checked.
			checkPath := ""
			if preferredPath != nil {
				checkPath = *preferredPath
			} else {
				checkPath = preferredDownloadPath(cfg)
			}
			if floor := downloadPathFreeSpaceFloor(cfg, checkPath, minFreeBytes); floor != nil && checkPath != "" {
				free := FreeSpaceBytes(checkPath)
				if free == nil {
					stats.Error("min_free_space_unavailable")
					logging.Warn(fmt.Sprintf("⚠️ %s not downloaded: cannot read the free space of its download folder", logTarget(&release)), "folder", checkPath)
					continue
				}
				if *free < *floor {
					stats.Error("min_free_space")
					logging.Warn("cycle: free space below minimum for chosen download path, download skipped",
						"path", checkPath, "free", logging.HumanBytes(*free), "minimum", logging.HumanBytes(*floor))
					continue
				}
			}
			if deferredHash != "" && !resolveDeferred() {
				continue
			}
			if staleInProgressPack() {
				continue
			}
			add := func() (bool, error) {
				if release.Ed2kLink != "" {
					return addEd2kRelease(cfg, &release)
				}
				if torrentFile != nil && *torrentFile != "" {
					return torrents.AddFileWithPath(*torrentFile, cfg, preferredPath)
				}
				return torrents.AddWithPath(release.Magnet, cfg, preferredPath)
			}
			commit := func() (bool, string, error) {
				return commitReleaseApproval(db, &release, releaseScore, cfg.UpgradeMinScoreDiff, approvalContext, forbidUpgrade)
			}
			outcome, commitReason, err := startApprovedRelease(db, torrents, &release, score, cfg.DryRun, add, commit)
			if outcome != "" {
				// A single release that cannot be started must not abort the whole
				// cycle: record the failure and continue with the other candidates.
				logStartFailure(stats, &release, outcome, commitReason, err)
				continue
			}
			if !cfg.DryRun {
				approvalReason = commitReason
				if hash, ok := releaseHash(&release); ok {
					_ = db.SetTorrentReason(hash, decisionReason)
				}
			}
			why := "new"
			switch {
			case release.Kind == "movie" && approvalReason == "upgrade":
				why = "better version than the one you have"
			case release.Kind == "movie" && approvalReason == "restored":
				// The database remembered this film as removed, but its file is
				// no longer anywhere: this is a new attempt, not an upgrade.
				why = "the copy in the library is missing"
			case release.Kind == "movie" && fromArchive:
				// A film is not an episode: the archive branch below said
				// "fills a missing episode" for a movie proposed from the
				// release archive, which read as nonsense in the log.
				why = "taken from the release archive"
			case release.Kind == "movie":
				why = "new"
			case fromArchive || len(gapEpisodes) > 0:
				why = "fills a missing episode"
				if approvalReason == "upgrade" {
					why = "fills a missing episode, better quality"
				}
			case approvalReason == "upgrade":
				if archiveCoversRelease(approvalContext.Archive, &release) {
					why = "better version than the one you have"
				} else {
					why = "fills a missing episode"
				}
			}
			if release.LastResort {
				why = "🆘 last resort: the only copy in the right language, outside the quality profile"
			}
			if stallApplies {
				why = "an alternative while the current download is stuck"
				logging.Info(fmt.Sprintf("⏱️ %s: current download stuck for over %s, starting an alternative within %d points",
					logTarget(&release), logDuration(time.Duration(cfg.StallAlternativeAfterMin)*time.Minute), stallScoreDrop),
					"stuck_score", stallStuckScore, "candidate_score", releaseScore)
			}
			logMessage := fmt.Sprintf("📥 Downloading %s — %s (%s)", logTarget(&release), why, friendlyQuality(release.Quality))
			if cfg.DryRun {
				logMessage = fmt.Sprintf("🧪 Test mode: would download %s — %s (%s)", logTarget(&release), why, friendlyQuality(release.Quality))
			}
			if approvalReason == "upgrade" {
				if from := upgradeSourceText(approvalContext.Archive, &release); from != "" {
					logMessage += " · replacing: " + from
				}
			}
			if destination := releaseDestinationLabel(cfg, &release, preferredPath); destination != "" {
				logMessage += " → " + destination
			}
			logHash, _ := releaseHash(&release)
			logging.Info(logMessage, "release", shortTorrentName(release.Title), "from", release.Source, "hash", logHash)
			logging.Debug("download decision",
				"target", releaseTarget(&release),
				"quality", releaseQualityLabel(&release),
				"score", score,
				"reason", decisionReason,
				"approval_reason", approvalReason,
				"gap_episodes", episodesLabel(gapEpisodes))
			if isReadyPending && !cfg.DryRun {
				// The download is registered: a stale pending row is harmless (the
				// active hash blocks a second start), so never abort the cycle here.
				if release.Series != nil && release.Season != nil && release.Episode != nil {
					if err := db.RemovePending(*release.Series, *release.Season, *release.Episode); err != nil {
						logging.Warn("could not clear the delayed release", "target", releaseTarget(&release), "error", err)
					}
				} else if release.Kind == "movie" {
					if err := db.RemovePendingMovie(release.Title, release.Year); err != nil {
						logging.Warn("could not clear the delayed movie", "target", releaseTarget(&release), "error", err)
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
			if hash, ok := releaseHash(&release); ok {
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
			switch approvalReason {
			case "duplicate":
				skips.InLibrary++
			case "active_episode", "active_pack":
				skips.Downloading++
			default:
				skips.Other++
			}
			if approvalReason == "duplicate" || approvalReason == "active_episode" {
				logging.Debug("download skipped (already present or active)",
					"target", releaseTarget(&release),
					"score", score,
					"reason", decisionReason,
					"approval_reason", approvalReason)
			} else {
				logging.Info(fmt.Sprintf("⏭️ Skipped %s: %s", logTarget(&release), skipReasonText(approvalReason)),
					"release", release.Title)
				logging.Debug("download skipped",
					"target", releaseTarget(&release),
					"source", release.Source,
					"score", score,
					"reason", decisionReason,
					"approval_reason", approvalReason,
					"gap_episodes", episodesLabel(gapEpisodes))
			}
		}
	}
	elapsed := cycleElapsedSeconds(stats)
	stats.DurationSeconds = int(elapsed)
	if err := db.SaveCycle(stats); err != nil {
		return nil, err
	}
	logging.Info(cycleReportText(logDuration(time.Duration(elapsed)*time.Second), stats, upgrades, newItems, skips))
	return stats, nil
}

// cycleElapsedSeconds is how long the cycle has been running, from its recorded
// start time, rounded down.
func cycleElapsedSeconds(stats *models.CycleStats) int64 {
	started := time.Now().UTC()
	if stats != nil && stats.LastStartedAt != nil {
		started = *stats.LastStartedAt
	}
	elapsed := int64(time.Since(started).Seconds())
	if elapsed < 0 {
		return 0
	}
	return elapsed
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
		gapFilling = settingTruthy(value)
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
		items, searchErr := searchArchiveForEpisode(ctx, cfg, archive, series, query, gap.Season, gap.Episode)
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
			release := parseArchiveRelease(ctx, cfg, series, item)
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
			score := cfg.AcquisitionScore(release)
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
		logging.Debug("priority gap pass: no archive candidates")
		return nil
	}

	live := &models.LiveDownloads{Hashes: map[string]struct{}{}, Episodes: map[models.LiveEpisodeKey]struct{}{}}
	for _, torrent := range torrents.List() {
		live.Hashes[strings.ToLower(torrent.Hash)] = struct{}{}
		if key := ParseEpisodeKey(torrent.Name); key != nil {
			live.Episodes[*key] = struct{}{}
		}
	}
	stallAlternatives, stallScoreDrop := stalledAlternativeContext(cfg, db, torrents)
	logging.Info(fmt.Sprintf("🧩 %s can be downloaded right away from the local release archive",
		countLabel(int64(len(candidates)), "missing episode", "missing episodes")))
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
		approvalContext := &models.ApprovalContext{
			Archive:           &models.ArchiveQualityIndex{},
			Live:              live,
			ForbidUpgrade:     series.DisableUpgrades,
			UpgradeUntilScore: cfg.UpgradeUntilScore,
			ScoreSettings:     cfg.Settings,
			GapEpisode:        true,
			StallAlternatives: stallAlternatives,
			StallScoreDrop:    stallScoreDrop,
			Blocklist:         blocklisted,
		}
		// candidate.score includes the swarm health, which only picks the best
		// copy among the hits: the decision and the stored score use the
		// release score, like the main cycle.
		releaseScore := cfg.ReleaseScore(&release)
		approved, reason, err := evaluateReleaseApproval(db, &release, releaseScore, cfg.UpgradeMinScoreDiff, approvalContext, series.DisableUpgrades, true)
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
		add := func() (bool, error) {
			if release.Ed2kLink != "" {
				return addEd2kRelease(cfg, &release)
			}
			return torrents.AddWithPath(release.Magnet, cfg, preferredPath)
		}
		commit := func() (bool, string, error) {
			return commitReleaseApproval(db, &release, releaseScore, cfg.UpgradeMinScoreDiff, approvalContext, series.DisableUpgrades)
		}
		if outcome, commitReason, err := startApprovedRelease(db, torrents, &release, releaseScore, false, add, commit); outcome != "" {
			logStartFailure(stats, &release, outcome, commitReason, err)
			continue
		}
		_ = db.SetTorrentReason(hash, "gap_filled")
		live.Hashes[hash] = struct{}{}
		for gap := range candidate.gaps {
			live.Episodes[models.LiveEpisodeKey{Series: gap.Series, Season: gap.Season, Episode: gap.Episode}] = struct{}{}
		}
		stats.DownloadsStarted++
		stats.GapsFilled++
		started++
		gapMessage := fmt.Sprintf("📥 Downloading %s — fills a missing episode (%s)", logTarget(&release), friendlyQuality(release.Quality))
		if destination := releaseDestinationLabel(cfg, &release, preferredPath); destination != "" {
			gapMessage += " → " + destination
		}
		logging.Info(gapMessage,
			"release", release.Title, "from", release.Source, "score", releaseScore, "hash", hash)
		if err := notifier.NotifyEvent("download_started", map[string]any{
			"title": release.Title, "kind": release.Kind, "series": release.Series, "season": release.Season,
			"episode": release.Episode, "source": release.Source, "magnet_hash": hash,
			"quality_score": releaseScore, "reason": "gap_filled", "approval_reason": reason,
		}); err != nil {
			logging.Warn("download notification failed", "error", err)
		}
	}
	logging.Debug("priority gap pass completed", "started", started, "candidates", len(candidates))
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
// stop before it touches the torrent engine again. The first call of a
// cancelled cycle logs it, so an aborted cycle is visible in the log once and
// not once per call site.
func cycleCancelled(ctx context.Context) bool {
	if ctx == nil || ctx.Err() == nil {
		return false
	}
	if _, logged := cycleCancelLogged.LoadOrStore(ctx, struct{}{}); !logged {
		logging.Info("⏹️ Search interrupted: Gextto is shutting down")
	}
	return true
}

// cycleCancelLogged remembers the cancelled cycle contexts already logged. It
// only grows when a cycle is cancelled, that is at shutdown.
var cycleCancelLogged sync.Map

// downloadPathFreeSpaceFloor returns the minimum free space to require on a
// download target. The RAM disk is a small tmpfs with its own dedicated budget
// (`libtorrent_ramdisk_min_free_bytes`, falling back to
// `libtorrent_ramdisk_margin_gb`), so it must not be measured against the
// global `min_free_space_gb` floor, which is meant for the primary download
// volume: a 6 GB tmpfs can never satisfy a floor sized for a multi-TB volume,
// which would silently block every download routed there. `global` is the
// already-parsed global floor (nil when disabled).
func downloadPathFreeSpaceFloor(cfg *Config, path string, global *uint64) *uint64 {
	if ramdisk := cfg.RamdiskDir(); ramdisk != nil && path == *ramdisk {
		margin := cfg.RamdiskMinFreeBytes()
		if margin == 0 {
			margin = cfg.RamdiskMarginBytes()
		}
		return &margin
	}
	return global
}

// evaluateReleaseApproval decides whether a release should be downloaded. With
// dryRun it only reads: no series row, episode/movie placeholder or upgrade
// backup is written. The acquisition loops call it this way, start the torrent,
// and only then record the approval with commitReleaseApproval, so a release
// that is never started (space guard, engine refusal) leaves nothing to undo.
func evaluateReleaseApproval(db *Database, release *models.Release, score, minScoreDiff int64, approvalContext *models.ApprovalContext, forbidUpgrade, dryRun bool) (bool, string, error) {
	approved, reason, err := evaluateReleaseApprovalRaw(db, release, score, minScoreDiff, approvalContext, forbidUpgrade, dryRun)
	if err != nil || !approved {
		return approved, reason, err
	}
	if blocked := swarmBlockReason(release, reason); blocked != "" {
		return false, blocked, nil
	}
	return true, reason, nil
}

// evaluateReleaseApprovalRaw is the database decision, before the swarm gate:
// a release the library would accept is still refused when nobody can serve it.
func evaluateReleaseApprovalRaw(db *Database, release *models.Release, score, minScoreDiff int64, approvalContext *models.ApprovalContext, forbidUpgrade, dryRun bool) (bool, string, error) {
	if release.Kind == "series" {
		evaluation := models.ApprovalContext{}
		if approvalContext != nil {
			evaluation = *approvalContext
		}
		evaluation.DryRun = dryRun
		return db.CheckSeriesScored(release, score, minScoreDiff, &evaluation)
	}
	upgradeUntil := int64(0)
	var movieArchive *models.ArchiveQuality
	var scoreSettings map[string]string
	if approvalContext != nil {
		upgradeUntil = approvalContext.UpgradeUntilScore
		movieArchive = approvalContext.MovieArchive
		scoreSettings = approvalContext.ScoreSettings
	}
	return db.checkMovieScoredWithDisk(release, score, minScoreDiff, upgradeUntil, forbidUpgrade, dryRun, movieArchive, scoreSettings)
}

// commitReleaseApproval records the approval of a release whose torrent is
// already in the engine: it runs the same decision again, this time writing the
// placeholder rows and the upgrade backup. It must run before
// RegisterTorrentScored, whose active torrent_meta row would otherwise make the
// release look like an already active episode. A release that is no longer
// approved (the database changed in between) is reported with ok=false.
func commitReleaseApproval(db *Database, release *models.Release, score, minScoreDiff int64, approvalContext *models.ApprovalContext, forbidUpgrade bool) (bool, string, error) {
	return evaluateReleaseApproval(db, release, score, minScoreDiff, approvalContext, forbidUpgrade, false)
}

// withdrawAddedTorrent removes a torrent that was just added to the engine but
// could not be recorded in the database. Without its rows it would be treated
// as a foreign torrent and never archived.
func withdrawAddedTorrent(torrents TorrentEngine, release *models.Release) {
	if release.Ed2kLink != "" {
		// An eD2k file is not in the torrent engine: it stays in aMule and,
		// without its database rows, is treated like a file added by hand.
		return
	}
	hash, ok := utils.MagnetHash(release.Magnet)
	if !ok {
		return
	}
	if _, err := torrents.Remove(hash, true); err != nil {
		logging.Warn("could not withdraw the unrecorded torrent", "hash", hash, "error", err)
	}
}

// logStartFailure records why startApprovedRelease did not start a release.
func logStartFailure(stats *models.CycleStats, release *models.Release, outcome, reason string, err error) {
	if outcome == addRejected {
		// The engine (or aMule) already has this file: a duplicate, not a
		// problem. Counted as an error it showed "33 problems (see the
		// warnings above)" with no warning to see.
		logging.Debug("release already in the download engine", "target", logTarget(release))
		return
	}
	stats.Error(outcome)
	switch outcome {
	case addFailed:
		logging.Warn(fmt.Sprintf("⚠️ Could not start the download of %s; moving on", logTarget(release)), "error", err)
	case addIntentFailed:
		logging.Warn(fmt.Sprintf("⚠️ %s not started: it could not be recorded in the database", logTarget(release)), "error", err)
	default:
		logging.Warn(fmt.Sprintf("⚠️ %s withdrawn: it could not be recorded in the database", logTarget(release)),
			"step", outcome, "reason", reason, "error", err)
	}
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

// releaseStallScore returns the best score of the stuck download among the
// episodes covered by a series release (a single episode or a season pack),
// used to look up the stall tolerance.
func releaseStallScore(release *models.Release, alternatives map[models.LiveEpisodeKey]int64) (int64, bool) {
	best, found := int64(0), false
	for _, key := range releaseEpisodeKeys(release) {
		if score, ok := alternatives[key]; ok && (!found || score > best) {
			best, found = score, true
		}
	}
	return best, found
}

// releaseEpisodeKeys lists every episode covered by a series release: a single
// episode, or the whole range of a season pack.
func releaseEpisodeKeys(release *models.Release) []models.LiveEpisodeKey {
	if release == nil || release.Kind != "series" || release.Series == nil || release.Season == nil {
		return nil
	}
	episodes := release.EpisodeRange
	if len(episodes) == 0 && release.Episode != nil && *release.Episode > 0 {
		episodes = []int64{*release.Episode}
	}
	if len(episodes) == 0 {
		return nil
	}
	series := NormalizeSeriesName(*release.Series)
	keys := make([]models.LiveEpisodeKey, 0, len(episodes))
	for _, episode := range episodes {
		keys = append(keys, models.LiveEpisodeKey{Series: series, Season: *release.Season, Episode: episode})
	}
	return keys
}

// stalledAlternativeContext loads, once per cycle, the stuck episodes that may
// accept a temporary alternative and the configured score tolerance. It returns
// a nil map and 0 when the feature is disabled or nothing is stuck.
//
// Two kinds of stuck download are covered: a torrent parked by the stall
// monitor (no bytes for a while, persisted in the database) and a magnet still
// waiting for its file list, which is handled by the metadata monitor and is
// therefore not parked.
func stalledAlternativeContext(cfg *Config, db *Database, torrents TorrentSession) (map[models.LiveEpisodeKey]int64, int64) {
	if cfg == nil || db == nil || cfg.StallAlternativeAfterMin <= 0 || cfg.StallAlternativeScoreDrop <= 0 {
		return nil, 0
	}
	after := time.Duration(cfg.StallAlternativeAfterMin) * time.Minute
	alternatives, err := db.StalledAlternatives(time.Now().Add(-after))
	if err != nil {
		logging.Debug("could not load stalled alternatives", "error", err)
		alternatives = map[models.LiveEpisodeKey]int64{}
	}
	if alternatives == nil {
		alternatives = map[models.LiveEpisodeKey]int64{}
	}
	if torrents != nil {
		for _, torrent := range torrents.List() {
			if torrent.HasMetadata || torrent.State != "downloading_metadata" {
				continue
			}
			if time.Duration(torrent.ActiveSeconds)*time.Second < after {
				continue
			}
			meta, err := db.TorrentMeta(torrent.Hash)
			if err != nil || meta == nil {
				continue
			}
			release := &meta.Release
			score := cfg.AcquisitionScore(release)
			for _, key := range releaseEpisodeKeys(release) {
				if current, ok := alternatives[key]; !ok || score > current {
					alternatives[key] = score
				}
			}
		}
	}
	if len(alternatives) == 0 {
		return nil, 0
	}
	return alternatives, cfg.StallAlternativeScoreDrop
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
	logging.Warn(fmt.Sprintf("🔧 «%s» is named «%s» in the torrent: the pack is filed by the torrent name", release.Title, displayName))
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

// provisionalLinkHash is a stand-in identity for a .torrent link whose
// infohash is not known yet: stable for the link, never a real torrent's.
func provisionalLinkHash(link string) string {
	sum := sha1.Sum([]byte("gextto-provisional\x00" + link))
	return hex.EncodeToString(sum[:])
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

// mergeSeriesCandidate adds a series release to the candidate selection.
// Within one series and season:
//   - the candidate is dropped (superseded=true) when a selected release covers
//     its episodes and is at least as good;
//   - selected releases that the candidate covers and that are not strictly
//     better than it are removed;
//   - a selected release with the same episode range is replaced when the
//     candidate is not worse.
//
// "Covers" means a complete season pack, or a superset of the episodes. At equal
// quality the release covering more wins in both directions, so the selection
// does not depend on the order in which releases arrive (before, an episode
// seen before a same-score season pack kept both, the reverse order only the
// pack). scoreOf must be the same policy-aware score used for score.
func mergeSeriesCandidate(best []models.Release, release models.Release, score int64, scoreOf func(*models.Release) int64) ([]models.Release, bool) {
	seriesName := ""
	if release.Series != nil {
		seriesName = *release.Series
	}
	season := int64(0)
	if release.Season != nil {
		season = *release.Season
	}
	sameSeason := func(old *models.Release) bool {
		return old.Kind == "series" && old.Series != nil && *old.Series == seriesName &&
			old.Season != nil && *old.Season == season
	}
	rangeSet := episodeSet(&release)
	complete := hasCompleteRange(&release)
	for j := range best {
		old := &best[j]
		if !sameSeason(old) {
			continue
		}
		oldCovers := hasCompleteRange(old) || (!complete && intSetSuperset(episodeSet(old), rangeSet))
		if oldCovers && !releaseStrictlyBetter(&release, score, old, scoreOf(old)) {
			return best, true
		}
	}
	filtered := best[:0:0]
	for j := range best {
		old := &best[j]
		if sameSeason(old) {
			oldComplete := hasCompleteRange(old)
			candidateCovers := complete || (!oldComplete && len(rangeSet) > 1 && intSetSuperset(rangeSet, episodeSet(old)))
			if candidateCovers && !releaseStrictlyBetter(old, scoreOf(old), &release, score) {
				continue
			}
		}
		filtered = append(filtered, *old)
	}
	best = filtered
	for j := range best {
		old := &best[j]
		if sameSeason(old) && slices.Equal(old.EpisodeRange, release.EpisodeRange) {
			if releaseStrictlyBetter(&release, score, old, scoreOf(old)) {
				best[j] = release
			}
			return best, false
		}
	}
	return append(best, release), false
}

// seederTieBreak prefers, at an equal score, the release whose swarm is known
// to have seeders. Indexers report seeders inconsistently, so a missing count
// (<= 0) never wins: it only loses against a positive one. This keeps the
// search from committing a slot to a source that already looks dead when an
// equally good alternative reports healthy seeders.
func seederTieBreak(a *models.Release, b *models.Release) bool {
	return a.Seeders > 0 && a.Seeders > b.Seeders
}

// swarmTier ranks a release by what the indexer said about its swarm. A known
// leechers-only or empty swarm must lose to a release whose swarm is alive or
// unknown, whatever the quality: a better resolution that nobody can serve is
// not a better download. Unknown (-1) stays level with healthy because most
// public sources do not report seeders at all.
func swarmTier(release *models.Release) int {
	switch {
	case release == nil:
		return 2
	case release.Seeders < 0:
		return 2 // unknown: the fresh-download probe will tell
	case release.Seeders > 0:
		return 2
	case release.Peers > 0:
		return 1 // leechers only: it may complete, but it is the last resort
	default:
		return 0 // nobody is sharing it
	}
}

// swarmBlockReason refuses a release before it takes a slot. A completely
// empty swarm never downloads, and a leechers-only swarm must not replace a
// copy already in the library: the library copy stays. An unknown count is
// left to the probe that checks the first minutes of the download.
func swarmBlockReason(release *models.Release, approvalReason string) string {
	if release == nil || release.Seeders < 0 {
		return ""
	}
	if release.Seeders == 0 && release.Peers <= 0 {
		return "dead_swarm_empty"
	}
	if release.Seeders == 0 && approvalReason == "upgrade" {
		return "dead_swarm_upgrade"
	}
	return ""
}

// releaseStrictlyBetter reports whether a must replace b: a higher score, or at
// equal score a REMUX against a non-REMUX, or a known-healthier swarm. It is the
// negation of incumbentWins.
func releaseStrictlyBetter(a *models.Release, aScore int64, b *models.Release, bScore int64) bool {
	// A release whose swarm is known to be dead must not win a slot over one
	// that can actually download, even when its quality score is higher.
	if aTier, bTier := swarmTier(a), swarmTier(b); aTier != bTier {
		return aTier > bTier
	}
	if aScore != bScore {
		return aScore > bScore
	}
	if a.Quality.IsRemux() != b.Quality.IsRemux() {
		return a.Quality.IsRemux()
	}
	return seederTieBreak(a, b)
}

// incumbentWins is true when `incumbent` must not be replaced by `candidate`:
// at equal scores a REMUX (full-resolution version) wins, especially when it is
// the remux of an episode that was already selected or downloaded, and a
// healthier swarm breaks a remaining tie.
func incumbentWins(candidate *models.Release, candidateScore int64, incumbent *models.Release, cfg *Config) bool {
	// A candidate whose swarm is known to be dead does not replace one that can
	// download, even when its quality score is higher.
	if candidateTier, incumbentTier := swarmTier(candidate), swarmTier(incumbent); candidateTier != incumbentTier {
		return incumbentTier > candidateTier
	}
	// Use the same policy-aware score the candidate was ranked with, so score
	// rules and custom formats cannot make the two sides inconsistent.
	incumbentScore := cfg.AcquisitionScore(incumbent)
	if incumbentScore != candidateScore {
		return incumbentScore > candidateScore
	}
	if candidate.Quality.IsRemux() != incumbent.Quality.IsRemux() {
		return !(candidate.Quality.IsRemux() && !incumbent.Quality.IsRemux())
	}
	return !seederTieBreak(candidate, incumbent)
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

// refreshSeriesMetadata refreshes the season counts and status of enabled
// series (from TMDB, or TVDB without a TMDB key) when their metadata is stale
// or the status was never persisted.
func refreshSeriesMetadata(ctx context.Context, cfg *Config, db *Database) {
	meta := seriesMetadataFor(cfg)
	if !meta.Configured() {
		return
	}
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
		// Fetch the status even when season metadata is recent if the status
		// has never been persisted (the first refresh).
		_, known := knownStatuses[series.Name]
		needsStatus := !known
		if !stale && !needsStatus {
			continue
		}
		seriesID, err := metadataSeriesID(ctx, meta, series)
		if err != nil || seriesID == nil {
			continue
		}
		if stale {
			counts, err := meta.SeasonCounts(ctx, *seriesID)
			if err == nil {
				values := make([][2]int64, 0, len(counts))
				for season, count := range counts {
					values = append(values, [2]int64{season, count})
				}
				if err := db.SaveSeriesMetadata(series.Name, values); err != nil {
					logging.Warn("season metadata save failed", "series", series.Name, "source", meta.Source(), "error", err)
				}
			} else {
				logging.Debug("season metadata refresh failed", "series", series.Name, "source", meta.Source(), "error", err)
			}
		}
		// Persist the status ("Ended"/"Returning Series") for the series-list
		// badge.
		if info, err := meta.SeriesInfo(ctx, series.Name, seriesID); err == nil && info != nil {
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
//	torrent_engine.go TorrentSession.List() []models.TorrentView
//	torrent_engine.go TorrentSession.AddWithPath(magnet string, cfg *Config,
// preferredPath *string) (bool, error)
//	torrent_engine.go TorrentSession.AddFileWithPath(torrentPath string,
// cfg *Config, preferredPath *string) (bool, error)
//	transfer_policy.go FreeSpaceBytes(path string) *uint64
//	cleaner.IndexArchive(seriesName, archivePath string,
// cfg *Config) models.ArchiveQualityIndex
//	postprocess.DownloadDirFor(release *models.Release, cfg *Config) (string, bool)
//
// The comics module was implemented separately; this file assumes:
//
//	comics.ComicsDb.Setting(key, defaultValue string) (string, error)
//	comics.ComicsDb.SetSetting(key, value string) error
//	comics.NewGetComicsClient() *GetComicsClient
//	comics.RunComicsCycle(comics *ComicsDb, client *GetComicsClient,
// notifier *Notifier, defaultRoot string, torrents TorrentSession,
// mainDB *Database, cfg *Config) (int, error)

// comicsFailureRetrySecs is the delay before a failed comics cycle is retried.
const comicsFailureRetrySecs = 3600

// runComicsIfDue runs the comics cycle when it is due (or explicitly requested)
// and records the full check time only after a successful run: a failed check
// is retried after about an hour instead of waiting for the whole interval
// (seven days by default). Errors are counted in stats, never returned.
func runComicsIfDue(ctx context.Context, cfg *Config, db *Database, comics *ComicsDb, notifier *Notifier, torrents TorrentEngine, stats *models.CycleStats, requested bool) {
	if comics == nil || cycleCancelled(ctx) {
		return
	}
	comicsInterval := int64(604800)
	if value, err := comics.Setting("comics_check_interval", "604800"); err != nil {
		logging.Warn(fmt.Sprintf("Could not read the comics settings, so the comics check is skipped: %v", err))
		stats.Error("comics")
		return
	} else if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64); parseErr == nil {
		comicsInterval = parsed
	}
	if comicsInterval < 0 {
		comicsInterval = 0
	}
	lastComicsCheck := int64(0)
	if value, err := comics.Setting("last_comics_check_ts", "0"); err != nil {
		logging.Warn(fmt.Sprintf("Could not read when the comics check last ran, so it is skipped: %v", err))
		stats.Error("comics")
		return
	} else if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64); parseErr == nil {
		lastComicsCheck = parsed
	}
	nowTs := time.Now().UTC().Unix()
	// An explicitly requested comics cycle must start immediately instead of
	// waiting for the automatic interval; otherwise the Comics button appears
	// to do nothing when the next scheduled check is not due yet.
	if !requested && comicsInterval != 0 && saturatingSub(nowTs, lastComicsCheck) < comicsInterval {
		// The full comics check is not due, but a weekly pack whose links were
		// published after its page, or whose download failed, must not wait for
		// the whole week: retrying it costs one page read per pending pack.
		retryPendingWeeklyPacks(ctx, cfg, db, comics, notifier, torrents, nil)
		return
	}
	downloaded, runErr := RunComicsCycle(ctx, comics, NewGetComicsClient(), notifier, cfg.DownloadDir, torrents, db, cfg)
	if runErr != nil {
		stats.Error("comics")
		// Retry in an hour instead of after the whole interval, without
		// re-running the (synchronous) comics cycle at every scheduled cycle.
		retryTs := nowTs
		if comicsInterval > comicsFailureRetrySecs {
			retryTs = saturatingSub(nowTs, comicsInterval-comicsFailureRetrySecs)
		}
		if err := comics.SetSetting("last_comics_check_ts", strconv.FormatInt(retryTs, 10)); err != nil {
			logging.Warn(fmt.Sprintf("Could not save when the next comics check is due: %v", err))
		}
		logging.Warn(fmt.Sprintf("⚠️ The comics check failed and will be tried again in about an hour: %v", runErr))
		return
	}
	logging.Info(fmt.Sprintf("📚 Comics checked: %s downloaded", countLabel(int64(downloaded), "comic", "comics")))
	if err := comics.SetSetting("last_comics_check_ts", strconv.FormatInt(nowTs, 10)); err != nil {
		logging.Warn(fmt.Sprintf("Could not save when the comics check ran: %v", err))
	}
}

// finishCycleWithoutDownloads ends a cycle that may not start downloads (for
// example because the download disk is full). The stats are still persisted, so
// the UI and /api/cycles show why nothing was downloaded.
func finishCycleWithoutDownloads(db *Database, stats *models.CycleStats) (*models.CycleStats, error) {
	stats.DurationSeconds = int(cycleElapsedSeconds(stats))
	if err := db.SaveCycle(stats); err != nil {
		return nil, err
	}
	logging.Info(fmt.Sprintf("📊 Search finished: %s checked, %s matched your titles, but nothing was downloaded because of disk space%s",
		countLabel(stats.Scraped, "release", "releases"), logCount(stats.Candidates), cycleErrorsSuffix(stats.Errors)))
	return stats, nil
}

// dedupeReleasesByHash folds the copies of the same torrent collected by the
// different passes (feeds, archive, gap and pending searches) into one release
// with mirrors. Releases without any identity are kept.
func dedupeReleasesByHash(releases []models.Release) []models.Release {
	return mergeDuplicateReleases(releases)
}

// liveCandidate is a gap chosen for an online search in the deep pass.
type liveCandidate struct {
	Series  *SeriesConfig
	Season  int64
	Episode int64
}

// waitingGap is a gap the archive could not fill, waiting for an online
// search.
type waitingGap struct {
	candidate liveCandidate
	last      time.Time
	searched  bool
}

// orderWaitingGaps puts first the gaps never searched online, then the ones
// searched longest ago. Taken in library order, the first series of the
// alphabet with many unfindable gaps used every slot of the deep pass, and a
// series further down waited weeks for its turn. The never-searched gaps go one
// series at a time (the first gap of every series, then the second, ...) and
// the series start after cursor, the last one served by the previous pass, so
// each pass reaches new series instead of starting again from the letter A.
func orderWaitingGaps(waiting []waitingGap, cursor string) {
	rank := make([]int, len(waiting))
	seen := map[string]int{}
	for i := range waiting {
		if waiting[i].searched {
			continue
		}
		name := waiting[i].candidate.Series.Name
		rank[i] = seen[name]
		seen[name]++
	}
	order := make([]int, len(waiting))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := waiting[order[a]], waiting[order[b]]
		if x.searched != y.searched {
			return !x.searched
		}
		if x.searched {
			return x.last.Before(y.last)
		}
		if rank[order[a]] != rank[order[b]] {
			return rank[order[a]] < rank[order[b]]
		}
		xn, yn := x.candidate.Series.Name, y.candidate.Series.Name
		if (xn > cursor) != (yn > cursor) {
			return xn > cursor
		}
		return xn < yn
	})
	sorted := make([]waitingGap, len(waiting))
	for i, index := range order {
		sorted[i] = waiting[index]
	}
	copy(waiting, sorted)
}
