package gextto

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// candidatePass is the state one cycle shares across its candidate releases:
// the lookups loaded once per cycle, the caches filled while the candidates
// are evaluated, and the counters of the final report.
type candidatePass struct {
	cfg      *Config
	engine   *Engine
	db       *Database
	archive  *Archive
	notifier *Notifier
	torrents TorrentEngine
	stats    *models.CycleStats

	blocklistedHashes map[string]struct{}
	blocklistedTitles map[string]struct{}
	gapTargets        map[gapTarget]struct{}
	quarantinedLinks  map[string]string
	readyPending      map[string]struct{}
	liveDownloads     *models.LiveDownloads
	stallAlternatives map[models.LiveEpisodeKey]int64
	stallScoreDrop    int64
	minFreeBytes      *uint64

	emptyArchiveIndex *models.ArchiveQualityIndex
	archiveIndexCache map[string]*models.ArchiveQualityIndex
	movieArchiveCache map[string]*models.ArchiveQuality
	// movieArchiveIndex is one walk of the archive root, built at the first
	// movie candidate and shared by the others.
	movieArchiveIndex *MovieArchiveIndex
	// linkFailures holds the distinct .torrent links that failed per source
	// in this cycle: two of them mean the source is broken, not the torrent.
	linkFailures map[string]map[string]struct{}

	upgrades int
	newItems int
	skips    cycleSkipCounts
}

// candidateRun is the state of one candidate while the pass evaluates it: the
// release (its link may switch to a mirror), the staged .torrent file, the
// identity used before the link is opened, and what the judgement computed.
type candidateRun struct {
	p   *candidatePass
	ctx context.Context

	release models.Release
	// torrentFile is the .torrent downloaded from a feed that exposes only a
	// `.torrent` link; it is kept until the add completes (private trackers
	// require it for announcing) and removed on every exit (cleanup).
	torrentFile *string
	// mirrorUsable accepts the mirrors whose link is not quarantined.
	mirrorUsable func(*models.Release) bool
	// deferredHash is the hash-only identity of a link not opened yet;
	// provisional means it is derived from the link, not declared.
	deferredHash string
	provisional  bool

	releaseScore  int64
	gapEpisodes   []int64
	archiveIndex  *models.ArchiveQualityIndex
	movieArchive  *models.ArchiveQuality
	forbidUpgrade bool
}

// try evaluates one candidate release and, when it wins, starts its download:
// the link is resolved, the release is checked against the blocklist, the
// delay profiles, the library and the free space, then added to the torrent
// engine (or to aMule) and recorded. It returns stop when the cycle must end
// now (the daemon is shutting down) and an error only for a database failure
// that ends the cycle; a refused or failed candidate returns neither.
func (p *candidatePass) try(ctx context.Context, candidate models.Release) (stop bool, err error) {
	// Stop before starting another download when the daemon is shutting
	// down: the native session is destroyed right after the workers stop.
	if cycleCancelled(ctx) {
		return true, nil
	}
	r := &candidateRun{p: p, ctx: ctx, release: candidate, mirrorUsable: mirrorNotQuarantined(p.quarantinedLinks)}
	defer r.cleanup()
	// Older persisted entries and a few third-party feeds can store a valid
	// magnet in TorrentURL. Recover it before deciding that a torrent file
	// must be fetched over HTTP; a magnet is already directly usable by the
	// torrent engine and must never be passed to HTTPGetBytes.
	promoteTorrentURLMagnet(&r.release)
	if !r.prepareLink() {
		return false, nil
	}
	return r.judge()
}

// cleanup removes the staged .torrent: the engine keeps its own copy while
// the torrent is in the session. It runs on every exit from the candidate,
// refusals after the link was resolved included, so the folder cannot grow
// with releases never added.
func (r *candidateRun) cleanup() {
	if r.torrentFile != nil {
		removeTorrentFile(*r.torrentFile)
	}
}

// prepareLink decides when the link is opened and reports whether the
// candidate goes on.
//
// A link whose infohash the indexer declared (indexarr, Jackett and Prowlarr
// with "extended") is opened only for the release that is actually
// downloaded. The decision needs only the hash: opening the link of every
// candidate cost minutes per cycle, and an aggregator that counts downloads
// (indexarr) counted each candidate as one. An in-progress pack is checked
// against its file list after the link is opened (staleInProgressPack).
//
// A link without a declared hash (a MirCrew topic whose magnet indexarr
// fetches on demand) gets a provisional identity derived from the link: the
// candidate is judged on its title and quality first, and the link is opened
// only when that judgement would download it. The decision is then made again
// with the real hash (blocklist, live downloads).
func (r *candidateRun) prepareLink() bool {
	release := &r.release
	if strings.TrimSpace(release.Magnet) == "" && release.TorrentURL != nil && IsTorrentURL(*release.TorrentURL) {
		if hash := canonicalInfoHash(release.InfoHash); len(hash) == 40 {
			r.deferredHash = hash
		} else {
			r.deferredHash = provisionalLinkHash(*release.TorrentURL)
			r.provisional = true
		}
		release.Magnet = "magnet:?xt=urn:btih:" + r.deferredHash
	}
	// A link with no declared hash cannot be matched by infohash before it
	// is opened. Its title can: if the same release is on the blocklist,
	// skip it here instead of asking the indexer for the magnet only to
	// reject it a moment later.
	if r.provisional && releaseBlockedByTitle(release, r.p.blocklistedTitles) {
		logging.Debug("release skipped: its title is on the blocklist",
			"target", releaseTarget(release),
			"release", release.Title)
		return false
	}
	if r.deferredHash == "" && !r.resolveLink() {
		return false
	}
	reconcilePackIdentityFromMagnet(release)
	return true
}

// resolveLink turns a `.torrent` link into a magnet (and a staged file when
// the link serves one) and reports whether it worked. A mirror is another
// copy of the same torrent from another source (release_dedup.go): when this
// link fails, the next copy is tried in the same cycle instead of leaving the
// episode for the next one.
func (r *candidateRun) resolveLink() bool {
	p, release := r.p, &r.release
	for strings.TrimSpace(release.Magnet) == "" && release.TorrentURL != nil {
		magnet, path, err := resolveTorrentURL(r.ctx, p.engine, p.cfg, *release.TorrentURL, &release.Title)
		if err != nil {
			failedSource := release.Source
			r.recordLinkFailure(err)
			if switchToMirror(release, r.mirrorUsable) {
				logging.Info(fmt.Sprintf("🔁 %s: the link from %s did not work, trying the same release from %s",
					logTarget(release), failedSource, release.Source))
				continue
			}
			return false
		}
		// The link works: forget any previous failure so it is eligible
		// again if the indexer breaks later.
		_ = p.db.ClearTorrentLinkFailure(*release.TorrentURL)
		clearIndexerFailureNotice(release.Source)
		release.Magnet = magnet
		// The feed was persisted before selection. Replace its short-lived
		// Jackett URL with the stable magnet so a later archive search can
		// use it without re-downloading an already expired link. Skipped in
		// dry-run: it rewrites the archive DB.
		if !p.cfg.DryRun {
			if err := p.archive.CanonicalizeRelease(release, release.Magnet); err != nil {
				logging.Warn("could not canonicalize archived torrent URL", "error", err)
			}
		}
		if path != "" {
			if r.torrentFile != nil && *r.torrentFile != path {
				removeTorrentFile(*r.torrentFile)
			}
			r.torrentFile = &path
		}
	}
	return true
}

// recordLinkFailure records a `.torrent` link that could not be fetched.
func (r *candidateRun) recordLinkFailure(err error) {
	p, release := r.p, &r.release
	if errors.Is(err, errTorrentNotAvailable) {
		// The indexer knows the release but cannot serve it yet (mircrew:
		// "no magnet for this request" until the topic is thanked, or a
		// login that needs renewing). It is not a dead link: keep it
		// eligible, no cycle error and no strike. The next cycle retries it.
		// It still counts for the "indexer keeps failing" notice.
		logging.Debug("torrent link not available yet",
			"target", releaseTarget(release),
			"source", release.Source,
			"error", err)
		noteTorrentLinkFailure(p.linkFailures, release.Source, *release.TorrentURL, err, p.notifier)
		return
	}
	// Two failures in different cycles mean the link is dead: quarantine it
	// so the archive stops proposing it (and the cycle stops counting it as
	// an error) until the retry.
	if failures, qerr := p.db.RecordTorrentLinkFailure(*release.TorrentURL, err.Error(), time.Now()); qerr != nil {
		logging.Debug("could not record the failed torrent link", "error", qerr)
	} else if failures == torrentLinkFailureLimit {
		logging.Info(fmt.Sprintf("🚫 The .torrent link of %s from %s failed again: it is set aside for %s",
			releaseTarget(release), release.Source, logDuration(torrentLinkQuarantine(failures))))
	}
	p.stats.Error("torrent_link")
	logging.Warn(fmt.Sprintf("⚠️ Could not fetch the .torrent of %s from %s", releaseTarget(release), release.Source), "error", err)
	if source := strings.TrimSpace(release.Source); source != "" {
		noteTorrentLinkFailure(p.linkFailures, source, *release.TorrentURL, err, p.notifier)
	}
}

// resolveDeferred opens the deferred link and replaces the hash-only magnet
// with the real one (and the .torrent file when there is one).
func (r *candidateRun) resolveDeferred() bool {
	r.release.Magnet = ""
	if !r.resolveLink() {
		return false
	}
	if hash, ok := utils.MagnetHash(r.release.Magnet); ok && hash != r.deferredHash && !r.provisional {
		logging.Debug("the downloaded torrent has another infohash than the one declared",
			"target", releaseTarget(&r.release), "declared", r.deferredHash, "real", hash)
	}
	reconcilePackIdentityFromMagnet(&r.release)
	r.deferredHash = ""
	r.provisional = false
	return true
}

// judge runs the decision on a candidate whose link is ready (or deferred):
// the delay profile, the in-progress pack check and the approval against the
// library, then the start of the download or the record of the skip.
func (r *candidateRun) judge() (stop bool, err error) {
	p, cfg, release := r.p, r.p.cfg, &r.release
	isReadyPending := false
	if hash, ok := releaseHash(release); ok {
		_, isReadyPending = p.readyPending[hash]
	}
	// AcquisitionScore chooses between candidates; the approval and the
	// stored score must use ReleaseScore, so a swarm-health bonus can never
	// masquerade as an upgrade of a file already in the library.
	r.releaseScore = cfg.ReleaseScore(release)
	// A candidate that fills a known archive gap, or that scores above the
	// bypass threshold, is never held by a delay profile.
	r.gapEpisodes = gapEpisodesForRelease(release, p.gapTargets)
	isGap := len(r.gapEpisodes) > 0
	bypassDelay := isGap || (cfg.DelayBypassScore() > 0 && r.releaseScore >= cfg.DelayBypassScore())
	if !isReadyPending && !bypassDelay {
		if held, err := p.holdForDelay(release, r.releaseScore, r.provisional, r.resolveDeferred); err != nil {
			return true, err
		} else if held {
			return false, nil
		}
	}
	r.archiveIndex, r.movieArchive = p.archiveFor(release)
	r.forbidUpgrade = p.forbidUpgradeFor(release)
	if r.staleInProgressPack() {
		return false, nil
	}
	approvalContext := &models.ApprovalContext{
		Archive:           r.archiveIndex,
		MovieArchive:      r.movieArchive,
		Live:              p.liveDownloads,
		ForbidUpgrade:     r.forbidUpgrade,
		UpgradeUntilScore: cfg.UpgradeUntilScore,
		ScoreSettings:     cfg.Settings,
		GapEpisode:        isGap,
		DryRun:            cfg.DryRun,
		StallAlternatives: p.stallAlternatives,
		StallScoreDrop:    p.stallScoreDrop,
		Blocklist:         p.blocklistedHashes,
	}
	// Decide only: the library is not written until the torrent is in the
	// engine (see startApprovedRelease).
	approved, approvalReason, err := evaluateReleaseApproval(p.db, release, r.releaseScore, cfg.UpgradeMinScoreDiff, approvalContext, r.forbidUpgrade, true)
	if err != nil {
		return true, err
	}
	if approved && r.provisional {
		if !r.resolveDeferred() {
			return false, nil
		}
		approved, approvalReason, err = evaluateReleaseApproval(p.db, release, r.releaseScore, cfg.UpgradeMinScoreDiff, approvalContext, r.forbidUpgrade, true)
		if err != nil {
			return true, err
		}
	}
	fromArchive := strings.HasPrefix(release.Source, "archive:")
	decisionReason, stallStuckScore, stallApplies := p.decisionReasonFor(release, approvalReason, fromArchive, r.gapEpisodes)
	if !approved {
		p.recordSkip(release, r.releaseScore, decisionReason, approvalReason, r.gapEpisodes)
		return false, nil
	}
	r.start(approvalContext, approvalReason, decisionReason, fromArchive, isReadyPending, stallStuckScore, stallApplies)
	return false, nil
}

// staleInProgressPack reads the file list of an in-progress pack and skips it
// when everything inside is already better on disk. An "in progress" pack
// declares how many episodes it holds long before the .torrent is updated: it
// is kept when it holds at least one episode we need (a gap or a real
// upgrade). It can only refuse, so a deferred link is checked once it is
// opened for a release that would be downloaded.
func (r *candidateRun) staleInProgressPack() bool {
	p, cfg, release := r.p, r.p.cfg, &r.release
	if r.torrentFile == nil || !release.IsPack || release.InProgressEpisodes <= 0 {
		return false
	}
	reason, checkErr := incompletePackReason(
		*r.torrentFile, release, r.gapEpisodes, r.archiveIndex,
		r.releaseScore, cfg.UpgradeMinScoreDiff, cfg.UpgradeUntilScore, r.forbidUpgrade, cfg.Settings,
	)
	if checkErr != nil {
		logging.Debug("could not read the pack torrent contents",
			"target", releaseTarget(release), "error", checkErr)
		return false
	}
	if reason == "" {
		return false
	}
	logging.Warn(fmt.Sprintf("⏭️ Season pack skipped — «%s» · %s", release.Title, reason),
		"target", releaseTarget(release))
	if !cfg.DryRun {
		if release.TorrentURL != nil {
			if qerr := p.db.QuarantineTorrentLink(*release.TorrentURL, reason, time.Now(), 7*24*time.Hour); qerr != nil {
				logging.Debug("could not quarantine the stale pack link", "error", qerr)
			}
		}
		if berr := p.db.Blocklist(release, "in_progress_pack_incomplete"); berr != nil {
			logging.Debug("could not blocklist the stale pack", "error", berr)
		}
	}
	return true
}

// start hands an approved candidate to the engine (or to aMule), records it
// and announces it. A single release that cannot be started does not abort
// the cycle: the failure is recorded and the pass goes on.
func (r *candidateRun) start(approvalContext *models.ApprovalContext, approvalReason, decisionReason string, fromArchive, isReadyPending bool, stallStuckScore int64, stallApplies bool) {
	p, cfg, release := r.p, r.p.cfg, &r.release
	// The explicit path (or nil, to let the engine choose) is decided by the
	// engine-independent RAM-disk policy: a known-too-big or unknown-size
	// release is staged off the tmpfs, and the RAM disk is used only for
	// backends that support it.
	preferredPath := automaticDownloadPath(release, cfg, engineSupportsRamdisk(p.torrents))
	// The global guard measures the default download volume; a tag rule, the
	// RAM-disk overflow or the RAM disk itself can redirect the download to
	// another filesystem, so verify the chosen path too. When no explicit
	// path is forced, mirror the engine fallback so the volume actually used
	// is the one being checked.
	if p.freeSpaceRefuses(release, preferredPath) {
		return
	}
	if r.deferredHash != "" && !r.resolveDeferred() {
		return
	}
	if r.staleInProgressPack() {
		return
	}
	checkFilesFirst := packFileListUnknown(cfg, release, r.torrentFile)
	add := func() (bool, error) {
		if release.Ed2kLink != "" {
			return addEd2kRelease(cfg, release)
		}
		if r.torrentFile != nil && *r.torrentFile != "" {
			return p.torrents.AddFileWithPath(*r.torrentFile, cfg, preferredPath)
		}
		if checkFilesFirst {
			// Only the metadata is fetched: the pack inspector starts the
			// download once the file list shows a useful episode.
			return p.torrents.AddWithOptions(release.Magnet, cfg, preferredPath, AddOptions{StopAtMetadata: true})
		}
		return p.torrents.AddWithPath(release.Magnet, cfg, preferredPath)
	}
	commit := func() (bool, string, error) {
		return commitReleaseApproval(p.db, release, r.releaseScore, cfg.UpgradeMinScoreDiff, approvalContext, r.forbidUpgrade)
	}
	outcome, commitReason, err := startApprovedRelease(p.db, p.torrents, release, r.releaseScore, cfg.DryRun, add, commit)
	if outcome != "" {
		logStartFailure(p.stats, release, outcome, commitReason, err)
		return
	}
	if !cfg.DryRun {
		approvalReason = commitReason
		if hash, ok := releaseHash(release); ok {
			_ = p.db.SetTorrentReason(hash, decisionReason)
		}
	}
	why := downloadWhy(release, approvalReason, fromArchive, r.gapEpisodes, approvalContext.Archive)
	if stallApplies {
		why = "an alternative while the current download is stuck"
		logging.Info(fmt.Sprintf("⏱️ %s: current download stuck for over %s, starting an alternative within %d points",
			logTarget(release), logDuration(time.Duration(cfg.StallAlternativeAfterMin)*time.Minute), p.stallScoreDrop),
			"stuck_score", stallStuckScore, "candidate_score", r.releaseScore)
	}
	p.announceDownload(release, why, r.releaseScore, decisionReason, approvalReason, r.gapEpisodes, approvalContext.Archive, preferredPath, isReadyPending, fromArchive, checkFilesFirst)
}

// forbidUpgradeFor reads the per-title "allow upgrades" switch (on by default).
func (p *candidatePass) forbidUpgradeFor(release *models.Release) bool {
	cfg := p.cfg
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
		if movie := cfg.FindMovieMatchForRelease(release, false); movie != nil {
			forbidUpgrade = movie.DisableUpgrades
		}
	}
	return forbidUpgrade
}

// decisionReasonFor is the reason a started download records: approved, a
// gap filled from the feeds or the archive, the last resort, or an alternative
// to a stuck download (with the stuck download's score).
func (p *candidatePass) decisionReasonFor(release *models.Release, approvalReason string, fromArchive bool, gapEpisodes []int64) (decisionReason string, stallStuckScore int64, stallApplies bool) {
	stallAlternatives, stallScoreDrop := p.stallAlternatives, p.stallScoreDrop
	decisionReason = approvalReason
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
	if stuckScore, has := releaseStallScore(release, stallAlternatives); has && stallScoreDrop > 0 {
		stallStuckScore = stuckScore
		stallApplies = true
		decisionReason = "stall_alternative"
	}
	return decisionReason, stallStuckScore, stallApplies
}

// freeSpaceRefuses checks the free space of the folder the download will use,
// which a tag rule or the RAM disk can move off the default volume.
func (p *candidatePass) freeSpaceRefuses(release *models.Release, preferredPath *string) bool {
	cfg, stats, minFreeBytes := p.cfg, p.stats, p.minFreeBytes
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
			logging.Warn(fmt.Sprintf("⚠️ %s not downloaded: cannot read the free space of its download folder", logTarget(release)), "folder", checkPath)
			return true
		}
		if *free < *floor {
			stats.Error("min_free_space")
			logging.Warn("cycle: free space below minimum for chosen download path, download skipped",
				"path", checkPath, "free", logging.HumanBytes(*free), "minimum", logging.HumanBytes(*floor))
			return true
		}
	}
	return false
}

// downloadWhy is the reason the download line gives, in words.
func downloadWhy(release *models.Release, approvalReason string, fromArchive bool, gapEpisodes []int64, archive *models.ArchiveQualityIndex) string {
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
		if archiveCoversRelease(archive, release) {
			why = "better version than the one you have"
		} else {
			why = "fills a missing episode"
		}
	}
	if release.LastResort {
		why = "🆘 last resort: the only copy in the right language, outside the quality profile"
	}
	return why
}

// announceDownload logs a started download, clears its delay row, counts it
// and sends the download_started notification.
// packFileListUnknown reports a pack the cycle adds without knowing what is
// inside: a magnet (no .torrent was read) of a series pack, with the pack
// inspection on. Such a pack is added stopped at its metadata, so a name that
// promises a season and holds one episode already in the library costs a few
// kilobytes and no announcement.
func packFileListUnknown(cfg *Config, release *models.Release, torrentFile *string) bool {
	if cfg == nil || cfg.DryRun || release == nil || !packInspectionEnabled(cfg) {
		return false
	}
	if release.Kind != "series" || !release.IsPack || release.Season == nil || release.Ed2kLink != "" {
		return false
	}
	return torrentFile == nil || *torrentFile == ""
}

func (p *candidatePass) announceDownload(release *models.Release, why string, score int64, decisionReason, approvalReason string, gapEpisodes []int64, archive *models.ArchiveQualityIndex, preferredPath *string, isReadyPending, fromArchive, checkFilesFirst bool) {
	cfg, db, notifier, stats := p.cfg, p.db, p.notifier, p.stats
	logMessage := fmt.Sprintf("📥 Downloading %s — %s (%s)", logTarget(release), why, friendlyQuality(release.Quality))
	if cfg.DryRun {
		logMessage = fmt.Sprintf("🧪 Test mode: would download %s — %s (%s)", logTarget(release), why, friendlyQuality(release.Quality))
	}
	wholeSeason := release.Kind == "series" && release.IsPack && hasCompleteRange(release) && release.Season != nil
	if approvalReason == "upgrade" && !wholeSeason {
		if from := upgradeSourceText(archive, release); from != "" {
			logMessage += " · replacing: " + from
		}
	}
	if wholeSeason && len(gapEpisodes) > 0 {
		// A whole-season pack lists no episodes of its own: say which missing
		// ones it is meant to fill.
		logMessage += " · for the missing " + episodeRangeLabel(*release.Season, gapEpisodes)
	}
	if destination := releaseDestinationLabel(cfg, release, preferredPath); destination != "" {
		logMessage += " → " + destination
	}
	logHash, _ := releaseHash(release)
	if checkFilesFirst {
		// The download line waits for the file list: what is announced now is
		// only that the list is being read, and why the pack was chosen.
		logging.Info(fmt.Sprintf("📋 Reading the file list of %s before downloading it — %s (%s)%s",
			logTarget(release), why, friendlyQuality(release.Quality), packChosenForText(release, gapEpisodes)),
			"release", shortTorrentName(release.Title), "from", release.Source, "hash", logHash)
	} else {
		logging.Info(logMessage, "release", shortTorrentName(release.Title), "from", release.Source, "hash", logHash)
	}
	logging.Debug("download decision",
		"target", releaseTarget(release),
		"quality", releaseQualityLabel(release),
		"score", score,
		"reason", decisionReason,
		"approval_reason", approvalReason,
		"gap_episodes", episodesLabel(gapEpisodes))
	if isReadyPending && !cfg.DryRun {
		// The download is registered: a stale pending row is harmless (the
		// active hash blocks a second start), so never abort the cycle here.
		if release.Series != nil && release.Season != nil && release.Episode != nil {
			if err := db.RemovePending(*release.Series, *release.Season, *release.Episode); err != nil {
				logging.Warn("could not clear the delayed release", "target", releaseTarget(release), "error", err)
			}
		} else if release.Kind == "movie" {
			if err := db.RemovePendingMovie(release.Title, release.Year); err != nil {
				logging.Warn("could not clear the delayed movie", "target", releaseTarget(release), "error", err)
			}
		}
	}
	if !cfg.DryRun {
		recordFeedAccepted(db, cfg, release)
	}
	if checkFilesFirst {
		p.skips.CheckingFiles++
	} else {
		stats.DownloadsStarted++
		if approvalReason == "upgrade" {
			p.upgrades++
		} else {
			p.newItems++
		}
		if fromArchive || len(gapEpisodes) > 0 {
			stats.GapsFilled++
		}
	}
	var magnetHashValue any
	if hash, ok := releaseHash(release); ok {
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
	if checkFilesFirst {
		// The download line and the notification are sent by the pack
		// inspector when the files turn out to be useful, never otherwise.
		if hash, ok := releaseHash(release); ok {
			if err := db.SetPackFileCheck(hash, logMessage, payload); err != nil {
				logging.Warn("could not record the pack waiting for its file list", "target", releaseTarget(release), "error", err)
			}
		}
		return
	}
	if err := notifier.NotifyEvent("download_started", payload); err != nil {
		logging.Warn("download notification failed", "error", err)
	}
}

// packChosenForText closes the file-list line with the episodes the pack is
// expected to bring.
func packChosenForText(release *models.Release, gapEpisodes []int64) string {
	if release.Season == nil {
		return ""
	}
	if len(gapEpisodes) > 0 {
		return " · for the missing " + episodeRangeLabel(*release.Season, gapEpisodes)
	}
	return ""
}

// recordSkip counts and logs a refused candidate; a release nobody shares
// waits on the blocklist.
func (p *candidatePass) recordSkip(release *models.Release, score int64, decisionReason, approvalReason string, gapEpisodes []int64) {
	cfg, db := p.cfg, p.db
	// "Already present / already active" rejections (duplicate or episode
	// active in the session) are routine, so keep them at debug level to
	// keep the cycle log readable.
	switch approvalReason {
	case "duplicate":
		p.skips.InLibrary++
	case "active_episode", "active_pack":
		p.skips.Downloading++
	case "dead_swarm_empty", "dead_swarm_upgrade":
		// Nobody is sharing it: the release waits on the blocklist for the
		// configured period instead of coming back at every search.
		p.skips.Other++
		blockDeadSwarmRelease(db, cfg, release)
	default:
		p.skips.Other++
	}
	if routineSkipReason(approvalReason) {
		logging.Debug("download skipped (routine rejection)",
			"target", releaseTarget(release),
			"score", score,
			"reason", decisionReason,
			"approval_reason", approvalReason)
	} else {
		logging.Info(fmt.Sprintf("⏭️ Skipped %s: %s", logTarget(release), skipReasonText(approvalReason)),
			"release", release.Title)
		logging.Debug("download skipped",
			"target", releaseTarget(release),
			"source", release.Source,
			"score", score,
			"reason", decisionReason,
			"approval_reason", approvalReason,
			"gap_episodes", episodesLabel(gapEpisodes))
	}
}

// holdForDelay puts the release in the delay queue when its series (its own
// timeframe first, then the global setting) or the movie delay asks to wait
// for a better version. A provisional identity is resolved first, because
// the queue keeps only the magnet. held is true when the candidate is done
// for this cycle: queued, or its deferred link could not be opened.
func (p *candidatePass) holdForDelay(release *models.Release, releaseScore int64, provisional bool, resolveDeferred func() bool) (held bool, err error) {
	cfg := p.cfg
	delayMinutes := int64(0)
	switch release.Kind {
	case "series":
		seriesName := ""
		if release.Series != nil {
			seriesName = *release.Series
		}
		series := cfg.FindSeriesMatch(seriesName, release.Season)
		if series == nil {
			return false, nil
		}
		// Per-series `timeframe` (hours) wins over the global setting.
		delayMinutes = cfg.DelayMinutes("series")
		if series.Timeframe > 0 {
			delayMinutes = saturatingMul(series.Timeframe, 60)
		}
	case "movie":
		delayMinutes = cfg.DelayMinutes("movie")
	}
	if delayMinutes <= 0 {
		return false, nil
	}
	logging.Info(fmt.Sprintf("⏳ %s: waiting %s before downloading, in case a better version appears",
		logTarget(release), logDuration(time.Duration(delayMinutes)*time.Minute)))
	if provisional && !resolveDeferred() {
		return true, nil
	}
	if release.Kind == "series" {
		err = p.db.QueuePendingScored(release, delayMinutes, releaseScore)
	} else {
		err = p.db.QueuePendingMovieScored(release, delayMinutes, releaseScore)
	}
	if err != nil {
		return false, err
	}
	p.skips.Waiting++
	return true, nil
}

// archiveFor returns what the archive already holds for the release: the
// quality index of its series (decisions consider real files on disk, not
// only database rows) or the best copy of its movie (a file on the archive
// volume is authoritative even when its database row is missing). Both are
// computed once per cycle and title: a full archive walk per candidate would
// be wasteful.
func (p *candidatePass) archiveFor(release *models.Release) (*models.ArchiveQualityIndex, *models.ArchiveQuality) {
	cfg := p.cfg
	switch release.Kind {
	case "series":
		key := ""
		if release.Series != nil {
			key = *release.Series
		}
		if cached, ok := p.archiveIndexCache[key]; ok {
			return cached, nil
		}
		series := cfg.FindSeriesMatch(key, release.Season)
		if series == nil {
			// The match depends on the season: a candidate from a season the
			// series does not follow must not leave an empty index in the
			// cache for the seasons it does.
			return p.emptyArchiveIndex, nil
		}
		archivePath := ""
		if resolved := cfg.ResolveArchivePath(series); resolved != nil {
			archivePath = *resolved
		}
		indexed := IndexArchive(series.Name, archivePath, cfg)
		p.archiveIndexCache[key] = &indexed
		return &indexed, nil
	case "movie":
		key := strings.ToLower(release.Title) + "\x00"
		if release.Year != nil {
			key += strconv.FormatInt(*release.Year, 10)
		}
		if cached, ok := p.movieArchiveCache[key]; ok {
			return p.emptyArchiveIndex, cached
		}
		if p.movieArchiveIndex == nil {
			p.movieArchiveIndex = BuildMovieArchiveIndex(cfg)
		}
		movieArchive := p.movieArchiveIndex.Best(cfg, release)
		p.movieArchiveCache[key] = movieArchive
		return p.emptyArchiveIndex, movieArchive
	}
	return p.emptyArchiveIndex, nil
}
