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
	if !cfg.DryRun && settingTruthy(cfg.Settings[settingAutotuneAdaptive]) {
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

	releases, sources, err := engine.ScrapeAllDomain(ctx, cfg, domain)
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
	// A `.torrent`-only listing has no infohash yet: its title is the only
	// identity available before the link is opened, so keep the blocklisted
	// titles too instead of asking the indexer for a torrent that is already
	// refused.
	blocklistedTitles, err := db.BlocklistedTitles()
	if err != nil {
		return nil, fmt.Errorf("load blocklist titles: %w", err)
	}
	blockedHashes := map[string]struct{}{}
	for i := range releases {
		if hash := releaseInfoHash(&releases[i]); hash != "" {
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
	if len(blockedHashes) > 0 || len(blocklistedTitles) > 0 {
		if len(blockedHashes) > 0 {
			logging.Debug("blocked releases removed from this cycle", "count", len(blockedHashes))
		}
		// Filter in place: most releases survive, and a second slice would cost
		// the whole batch again (tens of MB on a large cycle).
		kept := releases[:0]
		for i := range releases {
			if hash := releaseInfoHash(&releases[i]); hash != "" {
				if _, blocked := blocklistedHashes[hash]; !blocked {
					kept = append(kept, releases[i])
				}
				continue
			}
			if releases[i].Ed2kLink == "" && releaseBlockedByTitle(&releases[i], blocklistedTitles) {
				logging.Debug("release skipped: its title is on the blocklist",
					"title", releases[i].Title)
				continue
			}
			kept = append(kept, releases[i])
		}
		// Drop the references in the dropped tail: they would otherwise keep
		// every blocked release's strings alive for the rest of the cycle.
		clear(releases[len(kept):])
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
		// Alternative titles let a film monitored under one name match the
		// releases published under another (the international one, usually).
		refreshMovieAliases(ctx, cfg)
		for i := range cfg.Movies {
			movie := &cfg.Movies[i]
			if movie.Enabled {
				addArchiveQuery(movie.Name)
				if orig := strings.TrimSpace(movie.OriginalTitle); orig != "" && !strings.EqualFold(orig, movie.Name) {
					addArchiveQuery(orig)
				}
				for _, alias := range movie.Aliases {
					if strings.TrimSpace(alias) != "" && !strings.EqualFold(alias, movie.Name) && !strings.EqualFold(alias, movie.OriginalTitle) {
						addArchiveQuery(alias)
					}
				}
			}
		}
	}
	// The feed releases were just saved into the archive, so the archive
	// queries below return them again: seed the set with the feed hashes so
	// each release is evaluated (and counted) once.
	archiveHashes := map[string]struct{}{}
	for i := range releases {
		if hash := releaseInfoHash(&releases[i]); hash != "" {
			archiveHashes[hash] = struct{}{}
		}
	}
	archiveMatches := 0
	// Opening a `.torrent` link costs a request to the indexer (indexarr
	// counts it as a download). Only the listings whose title cannot be judged
	// need it, and a per-cycle budget keeps a large archive from turning one
	// cycle into hundreds of requests. A resolved row is stored, so it is paid
	// for once.
	archiveLinkBudget := 20
	for _, query := range archiveQueries {
		items, err := archive.SearchEntries(query)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			release := ParseRelease(item.Title, item.Magnet, "archive:"+item.Source)
			if release == nil && IsTorrentURL(item.Magnet) {
				// A `.torrent`-only listing (for example indexarr's redirect
				// links) has no magnet yet: keep the link so the candidate pass
				// can open it, instead of dropping the entry as unparsable.
				link := strings.TrimSpace(item.Magnet)
				release = ParseReleaseSource(item.Title, "", &link, "archive:"+item.Source, time.Now().UTC())
			}
			if release == nil {
				continue
			}
			// A listing whose `.torrent` lives on an indexer the operator
			// switched off, or that came from a feed removed from Sources,
			// cannot be downloaded any more: the manager no longer serves it
			// and the feed is gone. The archive keeps the row for its whole
			// retention, so without this skip one cycle after another would ask
			// a disabled source and report its error.
			if release.Magnet == "" && release.TorrentURL != nil &&
				cfg.ArchiveTorrentLinkUnavailable(*release.TorrentURL, item.FeedURL) {
				logging.Debug("archive: listing skipped, its source is switched off",
					"title", item.Title, "source", item.Source)
				continue
			}
			// The archive may know the infohash an indexer declared next to a
			// `.torrent`-only listing: use it, so the blocklist and the
			// duplicate checks do not need to open the link.
			if release.InfoHash == "" && item.InfoHash != "" {
				release.InfoHash = canonicalInfoHash(item.InfoHash)
			}
			// A listing whose title does not declare its quality cannot be
			// judged (indexarr often shows just the film title). Open the link
			// once, learn the real torrent name and infohash, and rewrite the
			// archived row so the next cycles do not download it again.
			if release.Magnet == "" && release.TorrentURL != nil &&
				cfg.ReleaseIsMonitored(release) && !releaseAllowedByTitleProfile(cfg, release) &&
				archiveLinkBudget > 0 {
				archiveLinkBudget--
				link := strings.TrimSpace(*release.TorrentURL)
				magnet, staged, realTitle, resolveErr := resolveTorrentSource(ctx, engine, cfg, link, &release.Title)
				// Only the name was needed here: the archive row keeps the
				// magnet, so the staged .torrent must not outlive this call.
				removeTorrentFile(staged)
				if resolveErr != nil {
					logging.Debug("archive: could not open the .torrent link to read its name",
						"title", item.Title, "source", item.Source, "error", resolveErr)
				} else if realTitle != "" && !strings.EqualFold(realTitle, item.Title) {
					if parsed := ParseRelease(realTitle, magnet, release.Source); parsed != nil {
						parsed.DiscoveredAt = release.DiscoveredAt
						release = parsed
						if !cfg.DryRun {
							if _, saveErr := archive.ResolveListing(link, magnet, realTitle, cfg.QualityScore(&release.Quality)); saveErr != nil {
								logging.Warn(fmt.Sprintf("🗄️ The release archive could not save the real name of «%s»", item.Title),
									"error", saveErr)
							}
						}
					}
				}
			}
			hash := releaseInfoHash(release)
			provisional := false
			if hash == "" {
				if release.Magnet == "" && release.TorrentURL != nil && IsTorrentURL(*release.TorrentURL) {
					// The candidate pass resolves the link when it decides to
					// download it; until then the link itself is the identity.
					hash = provisionalLinkHash(*release.TorrentURL)
					provisional = true
				} else {
					continue
				}
			}
			if provisional {
				if releaseBlockedByTitle(release, blocklistedTitles) {
					continue
				}
			} else if _, blocked := blocklistedHashes[hash]; blocked {
				continue
			}
			if _, seen := archiveHashes[hash]; seen {
				continue
			}
			archiveHashes[hash] = struct{}{}
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

	releases, gapTargets, err := collectGapReleases(ctx, cfg, engine, db, archive, domain, blocklistedHashes, releases)
	if err != nil {
		return nil, err
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
	if value, ok := cfg.Settings[settingMinFreeSpaceGb]; ok {
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

	// Distinct `.torrent` links that failed per source in this cycle. Two of
	// them mean the source is broken (an expired login, a dead proxy), not the
	// single torrent.
	linkFailures := map[string]map[string]struct{}{}
	if cycleCancelled(ctx) {
		return stats, nil
	}
	pass := &candidatePass{
		cfg: cfg, engine: engine, db: db, archive: archive, notifier: notifier, torrents: torrents,
		stats:             stats,
		blocklistedHashes: blocklistedHashes,
		blocklistedTitles: blocklistedTitles,
		gapTargets:        gapTargets,
		quarantinedLinks:  quarantinedLinks,
		readyPending:      readyPending,
		liveDownloads:     liveDownloads,
		stallAlternatives: stallAlternatives,
		stallScoreDrop:    stallScoreDrop,
		minFreeBytes:      minFreeBytes,
		emptyArchiveIndex: emptyArchiveIndex,
		archiveIndexCache: archiveIndexCache,
		movieArchiveCache: movieArchiveCache,
		movieArchiveIndex: movieArchiveIndex,
		linkFailures:      linkFailures,
	}
	for i := range best {
		stop, err := pass.try(ctx, best[i])
		if err != nil {
			return nil, err
		}
		if stop {
			return stats, nil
		}
	}
	upgrades, newItems, skips := pass.upgrades, pass.newItems, pass.skips
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

// collectGapReleases adds to releases the candidates for the missing episodes
// of the followed series (gap filling). Every cycle looks in the local
// archive; a deep pass, at most every gap_deep_interval_hours, also searches
// online for the gaps the archive does not hold, a bounded number per cycle
// and per series, resuming from where the previous deep pass stopped. eD2k is
// asked once per season instead of once per episode.
//
// It returns the releases with the gap candidates appended and the set of
// gaps they answer, which the candidate pass uses to prefer them.
func collectGapReleases(ctx context.Context, cfg *Config, engine *Engine, db *Database, archive *Archive,
	domain *string, blocklistedHashes map[string]struct{}, releases []models.Release) ([]models.Release, map[gapTarget]struct{}, error) {
	gapFilling := true
	if value, ok := cfg.Settings[settingGapFilling]; ok {
		gapFilling = settingTruthy(value)
	}
	var archiveGaps []SeriesGap
	if domainIs(domain, "movies") || !gapFilling {
		archiveGaps = nil
	} else {
		gaps, err := db.ArchiveGaps()
		if err != nil {
			return nil, nil, err
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

	policy := readGapFillPolicy(cfg, time.Now())
	deepLabel := "in the local release archive"
	if policy.deep {
		deepLabel = fmt.Sprintf("in the local release archive, plus up to %d online searches", policy.deepMaxPerCycle)
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
	// Gaps the archive does not hold, grouped by series and season: eD2k is
	// asked once for the whole season, then the candidates go to every gap of
	// it instead of one search per episode.
	seasonGaps := map[seriesSeason][]int64{}
	seasonSeries := map[seriesSeason]*SeriesConfig{}
	// One query for the search times of every gap: a point query per gap made
	// the deep pass cost grow with the library.
	searchTimes, err := db.GapSearchedTimes()
	if err != nil {
		return nil, nil, err
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
				hash, ok := utils.MagnetHash(item.Magnet)
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
		seasonKey := seriesSeason{Series: series.Name, Season: gap.Season}
		seasonGaps[seasonKey] = append(seasonGaps[seasonKey], gap.Episode)
		seasonSeries[seasonKey] = series
		// Phase 2 candidate: deep live search only.
		if !policy.deep {
			continue
		}
		// The log is keyed by the configured name, which may differ from the
		// archive title when the gap came in through an alias.
		last, searched := searchTimes[gapTarget{Series: series.Name, Season: gap.Season, Episode: gap.Episode}]
		if searched && int64(time.Now().UTC().Sub(last).Hours()) < policy.researchHours {
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
		if len(liveCandidates) >= policy.deepMaxPerCycle {
			break
		}
		name := entry.candidate.Series.Name
		if policy.perSeries > 0 && livePerSeries[name] >= policy.perSeries {
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
				// eD2k is asked once for the whole season after every budget
				// gap has been searched: see the phase after gapWG.Wait().
				logging.Debug("gap-fill torrent search completed",
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

		// eD2k answers one query per season and aMule serialises its searches,
		// so asking a single episode at a time left a season with many holes as
		// slow as the pacing of the deep pass — a missing episode per day where
		// the missing search finds a whole season in two minutes. Ask eD2k for
		// the whole season once, the same query the series missing search makes
		// on demand, and hand the candidates to every gap of it. Only the
		// seasons this pass picked are asked, so the pacing still holds.
		if searchesValid && ed2kFallbackEligible(cfg) {
			candidateIndex := make(map[gapTarget]int, len(liveCandidates))
			for i := range liveCandidates {
				candidateIndex[gapTarget{
					Series:  liveCandidates[i].Series.Name,
					Season:  liveCandidates[i].Season,
					Episode: liveCandidates[i].Episode,
				}] = i
			}
			askedSeasons := map[seriesSeason]bool{}
			for i := range liveCandidates {
				candidate := liveCandidates[i]
				if len(foundResults[i]) > 0 {
					continue
				}
				key := seriesSeason{Series: candidate.Series.Name, Season: candidate.Season}
				if askedSeasons[key] {
					continue
				}
				askedSeasons[key] = true
				series := seasonSeries[key]
				if series == nil {
					series = candidate.Series
				}
				episodes := seasonGaps[key]
				if len(episodes) == 0 {
					episodes = []int64{candidate.Episode}
				}
				var found []models.Release
				if len(episodes) > 1 {
					found = ed2kAutomaticSeasonFallback(ctx, engine, cfg, series, key.Season)
				} else {
					// One gap only: the episode query is narrower and can find a
					// file the season query does not list.
					found = engine.SearchSeriesED2K(ctx, cfg, series, key.Season, episodes[0], false)
				}
				extra, accepted := distributeSeasonED2K(cfg, series, key.Season, episodes, found, candidateIndex, foundResults, lastResorts)
				releases = append(releases, extra...)
				if accepted > 0 {
					logging.Info(fmt.Sprintf("🔎 eD2k answered for %s season %d: %s for its missing episodes",
						series.Name, key.Season, countLabel(accepted, "copy", "copies")))
				}
				for _, episode := range episodes {
					_ = db.MarkGapSearched(series.Name, key.Season, episode)
				}
			}
		}

		// Nothing else for a gap this pass looked up: take the copies in the
		// right language that miss only the resolution, then the ones the
		// release archive holds.
		for i := range liveCandidates {
			candidate := liveCandidates[i]
			if len(foundResults[i]) == 0 && len(lastResorts[i]) > 0 {
				foundResults[i] = lastResorts[i]
				logging.Info(fmt.Sprintf("🆘 Nothing within the quality profile for the missing %s S%02dE%02d: %s in the right language but outside the resolution, taken as a last resort",
					candidate.Series.Name, candidate.Season, candidate.Episode,
					countLabel(int64(len(lastResorts[i])), "copy", "copies")))
			}
			// The archive may hold the only copy, outside the profile: the
			// live search does not look there, so the pass adds it now that
			// every online source has answered.
			if len(foundResults[i]) == 0 {
				key := gapTarget{Series: candidate.Series.Name, Season: candidate.Season, Episode: candidate.Episode}
				if archived := archiveLastResorts[key]; len(archived) > 0 {
					foundResults[i] = archived
					logging.Info(fmt.Sprintf("🆘 Nothing within the quality profile for the missing %s S%02dE%02d: %s from the release archive, taken as a last resort",
						candidate.Series.Name, candidate.Season, candidate.Episode,
						countLabel(int64(len(archived)), "copy", "copies")))
				}
			}
		}
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
	if policy.deep && !cycleCancelled(ctx) {
		if err := SaveSetting(cfg.DataDir, "last_deep_gap_fill_ts", strconv.FormatInt(policy.now, 10)); err != nil {
			logging.Warn("could not record the deep gap pass time", "error", err)
		}
		if deepCursor != "" {
			if err := SaveSetting(cfg.DataDir, "gap_deep_cursor", deepCursor); err != nil {
				logging.Warn("could not record the deep gap pass cursor", "error", err)
			}
		}
	}
	return releases, gapTargets, nil
}

// gapFillPolicy is how far one cycle goes looking for missing episodes, read
// from the settings of the "Missing episodes" panel.
type gapFillPolicy struct {
	// perSeries caps the gaps searched per series in one cycle; 0 is no cap
	// (gap_fill_max_per_series).
	perSeries int
	// deepIntervalHours is the minimum time between two deep passes, at
	// least one hour (gap_deep_interval_hours, default 6).
	deepIntervalHours int64
	// deepMaxPerCycle caps the online searches of a deep pass
	// (gap_deep_max_per_cycle, default 5).
	deepMaxPerCycle int
	// researchHours is how long a gap searched online is left alone
	// (gap_research_hours, default 23), never shorter than the deep
	// interval, or every deep pass would search the same gaps again.
	researchHours int64
	// now is the time of this cycle, in Unix seconds.
	now int64
	// deep is true when the previous deep pass (last_deep_gap_fill_ts) is at
	// least deepIntervalHours old: this cycle also searches online.
	deep bool
}

func readGapFillPolicy(cfg *Config, now time.Time) gapFillPolicy {
	policy := gapFillPolicy{deepIntervalHours: 6, deepMaxPerCycle: 5, researchHours: 23, now: now.UTC().Unix()}
	if value, ok := cfg.Settings[settingGapFillMaxPerSeries]; ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed >= 0 {
			policy.perSeries = parsed
		}
	}
	if value, ok := cfg.Settings[settingGapDeepIntervalHours]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			policy.deepIntervalHours = parsed
		}
	}
	if policy.deepIntervalHours < 1 {
		policy.deepIntervalHours = 1
	}
	if value, ok := cfg.Settings[settingGapDeepMaxPerCycle]; ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed >= 0 {
			policy.deepMaxPerCycle = parsed
		}
	}
	if value, ok := cfg.Settings["gap_research_hours"]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && parsed > 0 {
			policy.researchHours = parsed
		}
	}
	if policy.researchHours < policy.deepIntervalHours {
		policy.researchHours = policy.deepIntervalHours
	}
	lastDeep := int64(0)
	if value, ok := cfg.Settings["last_deep_gap_fill_ts"]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			lastDeep = parsed
		}
	}
	policy.deep = saturatingSub(policy.now, lastDeep) >= saturatingMul(policy.deepIntervalHours, 3600)
	return policy
}

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

// distributeSeasonED2K hands the candidates of one eD2k season search to the
// gaps of that season. A copy within the profile goes to the results; one that
// misses only the resolution becomes the last resort of its gap, but only when
// that gap is one of the candidates the pass picked, because the others are not
// started this cycle. extra holds the copies for episodes the pass did not pick:
// they are downloaded anyway — the query already paid for them — and keep the
// season from needing a search per episode.
func distributeSeasonED2K(cfg *Config, series *SeriesConfig, season int64, episodes []int64, found []models.Release,
	candidateIndex map[gapTarget]int, foundResults, lastResorts [][]models.Release) (extra []models.Release, accepted int) {
	if cfg == nil || series == nil || len(episodes) == 0 {
		return nil, 0
	}
	wanted := make(map[int64]struct{}, len(episodes))
	for _, episode := range episodes {
		wanted[episode] = struct{}{}
	}
	for i := range found {
		release := found[i]
		if release.Episode == nil || release.Season == nil || *release.Season != season {
			continue
		}
		if _, isGap := wanted[*release.Episode]; !isGap {
			continue
		}
		if cfg.AllReleaseDeniedReason(&release) != "" {
			continue
		}
		canonicalName := series.Name
		release.Series = &canonicalName
		target := gapTarget{Series: series.Name, Season: season, Episode: *release.Episode}
		index, isCandidate := candidateIndex[target]
		if !cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title) {
			if isCandidate && cfg.SeriesLastResortAllowed(series, &release.Quality, release.Title) {
				release.LastResort = true
				lastResorts[index] = append(lastResorts[index], release)
			}
			continue
		}
		if isCandidate {
			foundResults[index] = append(foundResults[index], release)
		} else {
			extra = append(extra, release)
		}
		accepted++
	}
	return extra, accepted
}

// filterArchiveEpisode keeps the listings whose title carries the episode as
// an FTS token prefix, the same condition the per-episode query asked for.
func filterArchiveEpisode(items []ArchiveListing, season, episode int64) []ArchiveListing {
	// The season's rows answer every gap of that season: leave the input
	// untouched, or narrowing it for one episode empties it for the next.
	kept := make([]ArchiveListing, 0, len(items))
	for _, item := range items {
		if archiveTitleHasEpisode(item.Title, season, episode) {
			kept = append(kept, item)
		}
	}
	return kept
}

func archiveTitleHasEpisode(title string, season, episode int64) bool {
	token := fmt.Sprintf("s%02de%02d", season, episode)
	low := strings.ToLower(title)
	for i := 0; i+len(token) <= len(low); i++ {
		if i > 0 && isArchiveTokenByte(low[i-1]) {
			continue
		}
		if strings.HasPrefix(low[i:], token) {
			return true
		}
	}
	return false
}

// isArchiveTokenByte follows the FTS5 unicode61 tokenizer: letters and digits
// form a token. Non-ASCII bytes are treated as token bytes too, so an episode
// token that starts right after an accented letter is not a false match.
func isArchiveTokenByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
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
	if value, ok := cfg.Settings[settingGapFilling]; ok {
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
	// One archive search per series/season instead of one per episode: a gap
	// list of hundreds of episodes runs hundreds of FTS queries, each with its
	// own sort. The season query selects the same rows (same series words,
	// plus the season as a token prefix); the exact episode token is checked
	// locally, so the result is the one the per-episode query returned.
	type gapGroup struct {
		series *SeriesConfig
		season int64
		gaps   []SeriesGap
	}
	groups := []*gapGroup{}
	byKey := map[string]*gapGroup{}
	for _, gap := range gaps {
		season := gap.Season
		series := cfg.FindSeriesMatch(gap.Series, &season)
		if series == nil {
			continue
		}
		key := gap.Series + "\x00" + strconv.FormatInt(gap.Season, 10)
		group := byKey[key]
		if group == nil {
			group = &gapGroup{series: series, season: gap.Season}
			byKey[key] = group
			groups = append(groups, group)
		}
		group.gaps = append(group.gaps, gap)
	}

	processGap := func(gap SeriesGap, series *SeriesConfig, items []ArchiveListing) {
		for _, item := range items {
			hash, ok := utils.MagnetHash(item.Magnet)
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

	for _, group := range groups {
		if group.series.Anime {
			// Anime numbering needs the episode to resolve the absolute one:
			// keep the exact query per gap.
			for _, gap := range group.gaps {
				query := fmt.Sprintf("%s S%02dE%02d", gap.Series, gap.Season, gap.Episode)
				items, searchErr := searchArchiveForEpisode(ctx, cfg, archive, group.series, query, gap.Season, gap.Episode)
				if searchErr != nil {
					return searchErr
				}
				processGap(gap, group.series, items)
			}
			continue
		}
		query := fmt.Sprintf("%s S%02d", group.gaps[0].Series, group.season)
		items, searchErr := searchArchiveForEpisode(ctx, cfg, archive, group.series, query, group.season, group.gaps[0].Episode)
		if searchErr != nil {
			return searchErr
		}
		if len(items) >= archiveSearchLimit {
			// The season has more matches than one search returns: the oldest
			// may be beyond the limit, so ask each episode exactly.
			for _, gap := range group.gaps {
				exact := fmt.Sprintf("%s S%02dE%02d", gap.Series, gap.Season, gap.Episode)
				gapItems, err := searchArchiveForEpisode(ctx, cfg, archive, group.series, exact, gap.Season, gap.Episode)
				if err != nil {
					return err
				}
				processGap(gap, group.series, gapItems)
			}
			continue
		}
		for _, gap := range group.gaps {
			processGap(gap, group.series, filterArchiveEpisode(items, gap.Season, gap.Episode))
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
			if reason == "dead_swarm_empty" || reason == "dead_swarm_upgrade" {
				blockDeadSwarmRelease(db, cfg, &release)
			}
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
		recordFeedAccepted(db, cfg, &release)
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
		// The argument is authoritative for both kinds: the movie branch below
		// reads it directly, so the series decision must not depend on the
		// caller having also set ApprovalContext.ForbidUpgrade.
		if forbidUpgrade {
			evaluation.ForbidUpgrade = true
		}
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

// retainKind keeps only the releases of the requested kind. It filters in
// place: the caller replaces the slice it passed, and a second copy of a
// 30k-release batch costs tens of MB on every cycle.
func retainKind(releases []models.Release, kind string) []models.Release {
	kept := releases[:0]
	for i := range releases {
		if releases[i].Kind == kind {
			kept = append(kept, releases[i])
		}
	}
	clear(releases[len(kept):])
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
	magnet, path, _, err := resolveTorrentSource(ctx, engine, cfg, rawURL, fallbackTitle)
	return magnet, path, err
}

// resolveTorrentSource is resolveTorrentURL plus the real torrent name when the
// source reveals it: the magnet's `dn`, or the `name` field of the `.torrent`.
// Callers that judge quality need it because a listing title can omit the
// resolution and the language the torrent itself declares.
func resolveTorrentSource(ctx context.Context, engine *Engine, cfg *Config, rawURL string, fallbackTitle *string) (magnet, path, name string, err error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "magnet:?") {
		name = magnetDisplayName(rawURL)
		if magnet, ok := utils.SanitizeMagnet(rawURL, fallbackTitle); ok {
			return magnet, "", name, nil
		}
		return rawURL, "", name, nil
	}
	if !IsTorrentURL(rawURL) {
		return "", "", "", fmt.Errorf("torrent source is not an HTTP(S) torrent link")
	}
	payload, resolved, err := engine.FetchTorrent(ctx, rawURL)
	if err != nil {
		return "", "", "", err
	}
	if resolved != "" {
		name = magnetDisplayName(resolved)
		if magnet, ok := utils.SanitizeMagnet(resolved, fallbackTitle); ok {
			return magnet, "", name, nil
		}
		return resolved, "", name, nil
	}
	hash, ok := utils.TorrentInfoHash(payload)
	if !ok {
		return "", "", "", fmt.Errorf("invalid torrent payload")
	}
	name, _ = utils.TorrentName(payload)
	dir := filepath.Join(cfg.StateDir, "feed_torrents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", "", err
	}
	fileName := stateTorrentName(hash)
	if fileName == "" {
		return "", "", "", fmt.Errorf("invalid torrent infohash")
	}
	path = filepath.Join(dir, fileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", "", "", err
	}
	return "magnet:?xt=urn:btih:" + hash, path, name, nil
}

// feedTorrentRetention is how long a staged .torrent may stay in
// StateDir/feed_torrents. The cycle deletes it as soon as the candidate is
// over; this bound only catches leftovers of an interrupted cycle or of a
// version that did not clean up. A file being downloaded right now is always
// newer, so it is never touched. The days form names the bound in the
// housekeeping summary.
const (
	feedTorrentRetentionDays = 7
	feedTorrentRetention     = feedTorrentRetentionDays * 24 * time.Hour
)

// sweepStagedFeedTorrents removes the staged .torrent files older than
// feedTorrentRetention and returns how many it deleted. It never reports a
// missing folder: the first `.torrent`-only listing creates it.
func sweepStagedFeedTorrents(stateDir string, now time.Time) int {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return 0
	}
	dir := filepath.Join(stateDir, "feed_torrents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logging.Debug("cannot read the staged .torrent folder", "path", dir, "error", err.Error())
		}
		return 0
	}
	cutoff := now.Add(-feedTorrentRetention)
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".torrent") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err == nil {
			removed++
		}
	}
	return removed
}

// releaseAllowedByTitleProfile reports whether the release's parsed title
// already satisfies the profile of the monitored title it belongs to. A
// `.torrent`-only listing whose title does not declare its quality fails this
// check even when the torrent itself would be accepted: that is the signal to
// open the link and read the real torrent name.
func releaseAllowedByTitleProfile(cfg *Config, release *models.Release) bool {
	if cfg == nil || release == nil || !cfg.ReleaseIsMonitored(release) {
		return false
	}
	switch release.Kind {
	case "series":
		series := cfg.FindSeriesMatch(*release.Series, release.Season)
		if series == nil {
			return false
		}
		return cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title) ||
			(release.LastResort && cfg.SeriesLastResortAllowed(series, &release.Quality, release.Title))
	case "movie":
		movie := cfg.FindMovieMatchForRelease(release, false)
		if movie == nil {
			return false
		}
		return cfg.MovieReleaseAllowedForTitle(movie, &release.Quality, release.Title)
	}
	return false
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
	// Filter in place: this runs once per candidate and a fresh slice would
	// copy the whole selection every time (hundreds of MB over a cycle).
	filtered := best[:0]
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
	// Drop the removed tail so the selection does not keep those releases
	// alive through the array behind it.
	clear(best[len(filtered):])
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
			// 0 here is a special (a whole-season pack took the branch above).
			if episode < 0 {
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

// episodeSet returns the episodes of a release as a set. The 0 of a pack is
// the whole-season sentinel and stays out; the 0 of a single release is a real
// episode, a special such as S03E00.
func episodeSet(release *models.Release) map[int64]struct{} {
	set := map[int64]struct{}{}
	for _, episode := range release.EpisodeRange {
		if episode > 0 || (episode == 0 && !release.IsPack) {
			set[episode] = struct{}{}
		}
	}
	return set
}

// hasCompleteRange reports the `0` sentinel that marks a complete season pack.
// Only a pack carries it: a single release numbered 0 is a special (S03E00),
// not the whole season.
func hasCompleteRange(release *models.Release) bool {
	return release.IsPack && slices.Contains(release.EpisodeRange, 0)
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
	knownStatuses, _ := db.SeriesEndRecords()
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
		previous, known := knownStatuses[series.Name]
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
		// Persist the end-of-series signals (provider status, next episode,
		// TVmaze) for the series-list badge.
		if info, err := meta.SeriesInfo(ctx, series.Name, seriesID); err == nil && info != nil {
			if _, err := refreshSeriesEndRecord(ctx, cfg, db, series, info, previous); err != nil {
				logging.Warn("The status of a series could not be saved", "series", series.Name, "error", err)
			}
		}
	}
}

// movieAliasLimit caps how many alternative titles one movie stores. TMDB
// returns every localized title; a handful of them are enough for the match,
// and a shorter list keeps movies_config readable.
const movieAliasLimit = 40

// movieMetadataLookupsPerCycle caps how many movies a single cycle asks TMDB
// about for the names that are not yet stored. The lookup happens once per
// movie; on a large library a cap keeps the first cycle after an upgrade from
// turning into hundreds of requests, and the rest is filled in the cycles
// after.
const movieMetadataLookupsPerCycle = 25

// refreshMovieAliases fills the names the monitored movies are known under
// from TMDB: the alternative titles (used by the title match, the archive and
// the release names) and the per-language titles (used by the indexer search:
// the language chosen for the film and English). It runs at most once per
// movie: an empty list/map is stored as non-nil and not looked up again, while
// an error leaves it nil for the next cycle. It stops at the first TMDB error
// to avoid hammering an outage.
func refreshMovieAliases(ctx context.Context, cfg *Config) {
	if cfg == nil || cfg.DryRun || cfg.TmdbAPIKey == nil || len(cfg.Movies) == 0 {
		return
	}
	client := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	if !client.Configured() {
		return
	}
	lookups := 0
	for i := range cfg.Movies {
		if cycleCancelled(ctx) {
			break
		}
		movie := &cfg.Movies[i]
		if !movie.Enabled || strings.TrimSpace(movie.TmdbID) == "" {
			continue
		}
		needAliases := movie.Aliases == nil
		needTranslations := movie.TitleTranslations == nil
		if !needAliases && !needTranslations {
			continue
		}
		if lookups >= movieMetadataLookupsPerCycle {
			break
		}
		lookups++
		if needAliases {
			if err := refreshMovieAlias(ctx, cfg, movie, client); err != nil {
				logging.Debug("TMDB alternative titles unavailable", "movie", movie.Name, "error", err)
				return
			}
		}
		if needTranslations {
			if err := refreshMovieTranslations(ctx, cfg, movie, client); err != nil {
				logging.Debug("TMDB localized titles unavailable", "movie", movie.Name, "error", err)
				return
			}
		}
	}
}

// refreshMovieTranslations looks up one movie's localized titles and stores
// them, so the indexer search can use the title in the language chosen for the
// film without another TMDB call.
func refreshMovieTranslations(ctx context.Context, cfg *Config, movie *MovieConfig, client *TmdbClient) error {
	titles, err := client.MovieTranslations(ctx, movie.TmdbID)
	if err != nil {
		return err
	}
	if titles == nil {
		titles = map[string]string{}
	}
	if err := SaveMovieTitleTranslations(cfg.DataDir, movie.ID, titles); err != nil {
		return err
	}
	movie.TitleTranslations = titles
	return nil
}

// refreshMovieAlias looks up one movie's alternative titles and stores them.
// A movie with none is marked as checked (an empty non-nil list) so it is not
// queried again on every cycle.
func refreshMovieAlias(ctx context.Context, cfg *Config, movie *MovieConfig, client *TmdbClient) error {
	aliases, err := movieAliasTitles(ctx, cfg, movie, client)
	if err != nil {
		return err
	}
	if err := SaveMovieAliases(cfg.DataDir, movie.ID, aliases); err != nil {
		return err
	}
	movie.Aliases = aliases
	if len(aliases) > 0 {
		logging.Info(fmt.Sprintf("🏷️ %s: %s added from TMDB",
			movie.Name, countLabel(int64(len(aliases)), "alternative title", "alternative titles")))
	}
	return nil
}

// movieAliasTitles reads the alternative titles of one film from TMDB and
// returns them cleaned: the film's own name and its original title are
// dropped, the order of TMDB is kept, and at most movieAliasLimit entries are
// returned. Shared by the periodic refresh and by «Aggiorna TMDB» on the
// movie page, which then shows them at once.
func movieAliasTitles(ctx context.Context, cfg *Config, movie *MovieConfig, client *TmdbClient) ([]string, error) {
	titles, err := client.MovieAlternativeTitles(ctx, movie.TmdbID)
	if err != nil {
		return nil, err
	}
	// Indexers publish the international (English) title far more often than
	// the localized one, and TMDB's alternative titles do not always contain
	// it. The English details title is the most useful alias of all.
	if cfg.TmdbAPIKey != nil {
		if english := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, "en-US"); english.Configured() {
			if details, detailErr := english.MovieDetails(ctx, movie.TmdbID); detailErr == nil && details.Title != nil {
				titles = append([]string{*details.Title}, titles...)
			}
		}
	}
	aliases := make([]string, 0, len(titles))
	for _, title := range titles {
		title = strings.TrimSpace(title)
		if title == "" || strings.EqualFold(title, movie.Name) || strings.EqualFold(title, movie.OriginalTitle) {
			continue
		}
		aliases = append(aliases, title)
		if len(aliases) >= movieAliasLimit {
			break
		}
	}
	return cleanMovieAliases(aliases), nil
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
		logging.Warn("Could not read the comics settings, so the comics check is skipped", "error", err)
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
		logging.Warn("Could not read when the comics check last ran, so it is skipped", "error", err)
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
			logging.Warn("Could not save when the next comics check is due", "error", err)
		}
		logging.Warn("⚠️ The comics check failed and will be tried again in about an hour", "error", runErr)
		return
	}
	logging.Info(fmt.Sprintf("📚 Comics checked: %s downloaded", countLabel(int64(downloaded), "comic", "comics")))
	if err := comics.SetSetting("last_comics_check_ts", strconv.FormatInt(nowTs, 10)); err != nil {
		logging.Warn("Could not save when the comics check ran", "error", err)
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
