package gextto

// web_background.go ports the long-lived background workers and the shared
// `series_rename_apply` helper of `the reference daemon`.
//
// The worker entry points are launched by `startBackgroundWorkers` in
// web_serve.go. Private helpers defined here carry the unique `bg_` prefix so
// sibling group files can be generated independently.
//
// The torrent-event machinery the worker depends on (metadata/stall monitors,
// the seed policy, ramdisk reconciliation and the actual event handler) lives in
// its own port; those shared entry points are referenced by their CamelCase Go
// names.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// ---------------------------------------------------------------------------
// small shared helpers
// ---------------------------------------------------------------------------

// bg_archiveEpisodePattern is the series-archive episode matcher copied verbatim
// from `series_rename_apply`.
var bg_archiveEpisodePattern = regexp.MustCompile(`(?i)^(?P<name>.+?)[ ._-]+(?:s(?P<s>\d{1,2})e|(?P<ns>\d{1,2})x)(?P<e>\d{1,4})`)

func bg_derefStr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func bg_copyStrPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func bg_strPtrEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func bg_jsonString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func bg_parseSettingInt(cfg *Config, key string) int64 {
	if value, ok := cfg.Settings[key]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func bg_countKey(values map[string]any, key string) int64 {
	switch typed := values[key].(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	}
	return 0
}

func bg_archiveImportBusyContains(name string) bool {
	return ArchiveImportBusyContains(name)
}

func bg_linkArchiveFile(db *Database, series string, season, episode int64, cfg *Config, path string) {
	base := filepath.Base(path)
	title := strings.TrimSuffix(base, filepath.Ext(base))
	if strings.TrimSpace(title) == "" {
		return
	}
	var size int64
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}
	qualityScore := cfg.FileScore(path, "series", series)
	if err := db.SyncArchiveFileScored(series, season, episode, title, path, size, qualityScore); err != nil {
		logging.Warn("archive file linking failed", "error", err, "series", series, "season", season, "episode", episode)
	}
}

func bg_protectedTorrentPaths(torrents TorrentEngine) map[string]struct{} {
	protected := map[string]struct{}{}
	if torrents == nil {
		return protected
	}
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

// ---------------------------------------------------------------------------
// series_rename_apply
// ---------------------------------------------------------------------------

// seriesRenameApply is the full faithful implementation of `series_rename_apply`: it
// previews (execute=false) or performs (execute=true) the archive rename of one
// series and also repairs files the DB does not track yet.
func seriesRenameApply(s *AppState, name string, execute, force, sourceOnly bool) (int, any) {
	ctx := context.Background()
	cfg := latestConfig(s)
	series := cfg.FindSeriesByName(name)
	if series == nil {
		return 404, map[string]any{"ok": false, "error": "series not found"}
	}
	if !cfg.RenameEpisodes {
		return 409, map[string]any{"ok": false, "error": "rename is disabled"}
	}
	// Never rename a series whose archive is currently being written by a
	// torrent import: the repair would move files that are still being copied.
	// An executing rename also reserves the series, so an import that starts
	// meanwhile waits for it and two renames of the same series never overlap
	// (the periodic repair runs outside the cycle lock).
	if execute {
		release, reason, ok := TryAcquireArchiveRename(series.Name)
		if !ok {
			logging.Info("rename skipped: "+reason, "series", series.Name)
			return 200, map[string]any{
				"ok":      true,
				"skipped": true,
				"reason":  reason,
			}
		}
		defer release()
	} else if bg_archiveImportBusyContains(series.Name) {
		logging.Debug("rename skipped: archive import in progress", "series", series.Name)
		return 200, map[string]any{
			"ok":      true,
			"skipped": true,
			"reason":  "archive import in progress",
		}
	}
	episodes, err := s.db.EpisodesForSeries(series.Name, series.IgnoredSeasons)
	if err != nil {
		episodes = nil
	}
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())

	items := []any{}
	alreadyOk := []string{}
	considered := 0
	withPath := 0

	for _, episode := range episodes {
		considered++
		if episode.ArchivePath == nil || strings.TrimSpace(*episode.ArchivePath) == "" {
			continue
		}
		archivePath := *episode.ArchivePath
		configuredPath := archivePath
		info, statErr := os.Stat(configuredPath)
		if statErr != nil {
			continue
		}
		// Older imports may store the series directory instead of the actual
		// episode file. Resolve it before checking the naming convention;
		// otherwise a correctly named S06E01 is treated as a file named after
		// the directory and gets an unnecessary rename.
		path := configuredPath
		if info.IsDir() {
			files, err := VideoFiles(configuredPath)
			if err != nil {
				continue
			}
			marker := fmt.Sprintf("s%02de%02d", episode.Season, episode.Episode)
			matches := []string{}
			for _, file := range files {
				if strings.Contains(strings.ToLower(filepath.Base(file)), marker) {
					matches = append(matches, file)
				}
			}
			if len(matches) != 1 {
				continue
			}
			path = matches[0]
		}
		withPath++

		// The source (which MediaInfo does not provide) is recovered from the
		// ORIGINAL release title, not from the current file name: if an earlier
		// rename lost it, this restores it instead of leaving `unknown` forever.
		// Fields missing from the original title fall back to the file name.
		fallbackQuality := MergeQuality(ParseQuality(episode.Title), ParseQuality(filepath.Base(path)))
		release := models.Release{
			Title:        episode.Title,
			Magnet:       "",
			Source:       "archive",
			Quality:      fallbackQuality,
			Kind:         "series",
			Series:       bg_copyStrPtr(&series.Name),
			Season:       bg_i64PtrValue(episode.Season),
			Episode:      bg_i64PtrValue(episode.Episode),
			IsPack:       false,
			EpisodeRange: []int64{episode.Episode},
			SizeBytes:    0,
			Seeders:      -1,
			Peers:        -1,
			DiscoveredAt: time.Now().UTC(),
		}
		// "Restore source" mode: rename only files whose name lost the source
		// that the original DB title knows about.
		dbSource := ParseQuality(episode.Title).Source
		fileSource := ParseQuality(filepath.Base(path)).Source
		needsSource := dbSource != "unknown" && strings.TrimSpace(dbSource) != "" && fileSource == "unknown"
		if sourceOnly {
			if !needsSource {
				alreadyOk = append(alreadyOk, archivePath)
				continue
			}
		} else if !force && EpisodeNameConforms(path, &release, cfg) {
			// The video is already correct: on execute still clean up spurious
			// sidecars (cheap, no TMDB/MediaInfo), then realign score/size to the
			// real file so an upgraded episode is not re-downloaded.
			if execute {
				ApplySidecars(path, path, cfg)
				_ = s.db.RefreshEpisodeFileStats(cfg, series.Name, episode.Season, episode.Episode, path)
			}
			alreadyOk = append(alreadyOk, archivePath)
			continue
		}

		var target string
		var renameErr error
		if execute {
			target, renameErr = RenameEpisode(ctx, path, &release, cfg, tmdb)
		} else {
			target, renameErr = PreviewEpisodeRename(ctx, path, &release, cfg, tmdb)
		}
		if renameErr != nil {
			logging.Warn("rename file failed", "series", series.Name, "season", episode.Season, "episode", episode.Episode, "error", renameErr)
			items = append(items, map[string]any{
				"season":  episode.Season,
				"episode": episode.Episode,
				"from":    archivePath,
				"error":   renameErr.Error(),
			})
			continue
		}
		if target == "" {
			continue
		}
		if SamePath(path, target) {
			// No-op: the file already carries the target name (the cheap conform
			// check can miss it). Never log or count this as a rename, but still
			// realign the stored score/size to the file.
			if execute {
				_ = s.db.RefreshEpisodeFileStats(cfg, series.Name, episode.Season, episode.Episode, target)
			}
			alreadyOk = append(alreadyOk, archivePath)
			continue
		}
		logging.Debug("rename file", "series", series.Name, "season", episode.Season, "episode", episode.Episode, "from", path, "to", target, "execute", execute)
		if execute {
			// The renamed file has a new path: update the DB, otherwise the next
			// preview no longer finds the file.
			if err := s.db.SetEpisodeArchivePath(cfg, series.Name, episode.Season, episode.Episode, target); err != nil {
				logging.Warn("rename: failed to update DB path", "error", err)
			}
		}
		items = append(items, map[string]any{
			"season":   episode.Season,
			"episode":  episode.Episode,
			"from":     path,
			"to":       target,
			"executed": execute,
		})
	}

	// Effective archive folder: a per-series `archive_path` when set, otherwise
	// the auto-detected subfolder of the global `archive_root`. Using the raw
	// `series.ArchivePath` here made scanning and cleanup silently no-op for
	// every series configured only through `archive_root`.
	resolvedArchive := ""
	if resolved := cfg.ResolveArchivePath(series); resolved != nil {
		resolvedArchive = *resolved
	}

	// The database may not know about files copied manually or by an older
	// post-processing run. Scan the series archive as well, so those files are
	// renamed and, when an existing episode is better, moved to trash.
	trackedPaths := map[string]bool{}
	for _, episode := range episodes {
		if episode.ArchivePath == nil {
			continue
		}
		candidate := *episode.ArchivePath
		if _, err := os.Stat(candidate); err == nil {
			trackedPaths[filepath.Clean(candidate)] = true
		}
	}
	// Source-restore mode does not scan untracked files: the source is recovered
	// only from the original title in the DB.
	if sourceOnly {
		// no archive scan
	} else if files, err := VideoFiles(resolvedArchive); err == nil {
		nameIndex := bg_archiveEpisodePattern.SubexpIndex("name")
		seasonIndex := bg_archiveEpisodePattern.SubexpIndex("s")
		nsIndex := bg_archiveEpisodePattern.SubexpIndex("ns")
		episodeIndex := bg_archiveEpisodePattern.SubexpIndex("e")
		for _, file := range files {
			if trackedPaths[filepath.Clean(file)] {
				continue
			}
			base := filepath.Base(file)
			captures := bg_archiveEpisodePattern.FindStringSubmatch(base)
			if captures == nil {
				continue
			}
			fileSeries := ""
			if nameIndex >= 0 && nameIndex < len(captures) {
				fileSeries = captures[nameIndex]
			}
			if fileSeries == "" || !SeriesNamesMatch(series.Name, fileSeries) {
				continue
			}
			seasonValue := ""
			if seasonIndex >= 0 && seasonIndex < len(captures) && captures[seasonIndex] != "" {
				seasonValue = captures[seasonIndex]
			} else if nsIndex >= 0 && nsIndex < len(captures) {
				seasonValue = captures[nsIndex]
			}
			season, err := strconv.ParseInt(seasonValue, 10, 64)
			if err != nil {
				continue
			}
			episodeNumber, err := strconv.ParseInt(captures[episodeIndex], 10, 64)
			if err != nil {
				continue
			}
			knownTitle := fmt.Sprintf("Episodio %d", episodeNumber)
			for i := range episodes {
				if episodes[i].Season == season && episodes[i].Episode == episodeNumber {
					knownTitle = episodes[i].Title
					break
				}
			}
			release := models.Release{
				Title:        knownTitle,
				Magnet:       "",
				Source:       "archive-scan",
				Quality:      ParseQuality(base),
				Kind:         "series",
				Series:       bg_copyStrPtr(&series.Name),
				Season:       bg_i64PtrValue(season),
				Episode:      bg_i64PtrValue(episodeNumber),
				IsPack:       false,
				EpisodeRange: []int64{episodeNumber},
				SizeBytes:    0,
				Seeders:      -1,
				Peers:        -1,
				DiscoveredAt: time.Now().UTC(),
			}
			if !force && EpisodeNameConforms(file, &release, cfg) {
				// Already conforming but untracked: link the file to the DB so
				// the next cycle knows the episode exists.
				if execute {
					bg_linkArchiveFile(s.db, series.Name, season, episodeNumber, cfg, file)
				}
				continue
			}
			score := cfg.ReleaseScore(&release)
			if execute {
				discarded, err := DiscardIfInferior(cfg, series.Name, season, episodeNumber, score, file, resolvedArchive)
				if err != nil {
					logging.Warn("archive duplicate check failed", "file", file, "error", err)
					continue
				}
				if discarded {
					_, _ = DiscardSidecars(file, cfg)
					items = append(items, map[string]any{
						"season":    season,
						"episode":   episodeNumber,
						"from":      file,
						"discarded": true,
					})
					continue
				}
			}
			if !execute {
				if target, err := PreviewEpisodeRename(ctx, file, &release, cfg, tmdb); err == nil && target != "" {
					items = append(items, map[string]any{
						"season":   season,
						"episode":  episodeNumber,
						"from":     file,
						"to":       target,
						"executed": false,
					})
				}
				continue
			}
			target, err := RenameEpisode(ctx, file, &release, cfg, tmdb)
			if err != nil {
				// Never surface API keys embedded in URLs (e.g. TMDB) in logs.
				redacted := utils.RedactURLSecrets(err.Error())
				// A TMDB 404 (e.g. a missing special episode) is expected: keep it
				// at debug so it does not pollute the log.
				if strings.Contains(redacted, "404") {
					logging.Debug("archive file rename failed (not found)", "file", file, "error", redacted)
				} else {
					logging.Warn("archive file rename failed", "file", file, "error", redacted)
				}
				continue
			}
			if target == "" {
				// No rename needed: link the existing file.
				bg_linkArchiveFile(s.db, series.Name, season, episodeNumber, cfg, file)
				continue
			}
			_, _ = CleanupOldEpisode(cfg, series.Name, season, episodeNumber, score, target, filepath.Dir(target))
			bg_linkArchiveFile(s.db, series.Name, season, episodeNumber, cfg, target)
			items = append(items, map[string]any{
				"season":   season,
				"episode":  episodeNumber,
				"from":     file,
				"to":       target,
				"executed": true,
			})
		}
	}

	// Clean up clearly inferior (lower resolution) duplicates in the series
	// folder. Covers inherited libraries where the old 480p/720p stayed next to
	// the 1080p. Execute only.
	duplicatesRemoved := 0
	if execute && cfg.CleanupUpgrades && !sourceOnly {
		protected := bg_protectedTorrentPaths(s.activeEngine())
		if removed, err := CleanupInferiorDuplicatesInDir(cfg, series.Name, resolvedArchive, protected); err == nil {
			duplicatesRemoved = removed
		} else {
			logging.Warn("duplicate cleanup failed", "series", series.Name, "error", err)
		}
	}

	alreadyOkCount := len(alreadyOk)
	discardedCount := 0
	errorCount := 0
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if value, ok := entry["discarded"].(bool); ok && value {
			discardedCount++
		}
		if _, ok := entry["error"]; ok {
			errorCount++
		}
	}
	renamedCount := len(items) - discardedCount - errorCount
	if execute {
		// Say what changed, file by file: a bare count does not let the user
		// find the episode afterwards. Failures already have their own WARN.
		for _, item := range items {
			if entry, ok := item.(map[string]any); ok {
				if text := renameItemLogText(series.Name, entry, cfg.CleanupAction); text != "" {
					logging.Info(text)
				}
			}
		}
	}
	note := ""
	if force {
		note = " [force]"
	} else if sourceOnly {
		note = " [source only]"
	}
	if execute {
		// An execution that changes nothing (everything already correct) does not
		// deserve an INFO line every cycle: keep it at debug level.
		changed := renamedCount > 0 || discardedCount > 0 || duplicatesRemoved > 0 || errorCount > 0
		if changed {
			logging.Info(fmt.Sprintf("🗂 rename '%s': %d renamed, %d already correct, %d discarded, %d duplicates removed, %d errors — %d/%d files present%s",
				series.Name, renamedCount, alreadyOkCount, discardedCount, duplicatesRemoved, errorCount, withPath, considered, note))
		} else {
			logging.Debug(fmt.Sprintf("🗂 rename '%s': nothing to change (%d already correct, %d/%d files present)%s",
				series.Name, alreadyOkCount, withPath, considered, note))
		}
	} else if len(items) == 0 {
		logging.Info(fmt.Sprintf("🗂 rename preview '%s': nothing to rename (%d already correct, %d/%d files present)%s",
			series.Name, alreadyOkCount, withPath, considered, note))
	} else {
		logging.Info(fmt.Sprintf("🗂 rename preview '%s': %d file(s) would change, %d already correct — %d/%d files present%s",
			series.Name, len(items), alreadyOkCount, withPath, considered, note))
	}

	return 200, map[string]any{
		"ok":                 true,
		"series":             series.Name,
		"execute":            execute,
		"force":              force,
		"episodes":           considered,
		"with_path":          withPath,
		"items":              items,
		"already_ok":         alreadyOk,
		"already_ok_count":   alreadyOkCount,
		"renamed_count":      renamedCount,
		"discarded_count":    discardedCount,
		"duplicates_removed": duplicatesRemoved,
		"error_count":        errorCount,
	}
}

func bg_i64PtrValue(value int64) *int64 {
	return &value
}

// bg_completedArchivePresent distinguishes a real integrity discrepancy from
// libtorrent's transient paused/zero-progress state immediately after a
// storage relocation. The database path and on-disk size are the authoritative
// evidence that the archived payload is already present.
func bg_completedArchivePresent(db *Database, hash string, totalSize int64) (string, bool) {
	if db == nil || totalSize <= 0 {
		return "", false
	}
	processed, err := db.TorrentProcessed(hash)
	if err != nil || processed == nil || strings.TrimSpace(*processed) == "" {
		return "", false
	}
	path := *processed
	size, err := SizeOfPath(path)
	if err != nil || size < totalSize {
		return path, false
	}
	return path, true
}

// ---------------------------------------------------------------------------
// torrent_event_worker
// ---------------------------------------------------------------------------

// torrentEventWorker implements `torrent_event_worker`: it drives the
// torrent session policy (config reload, metadata promotion, queue/speed
// policy, pin/sequential flags), processes lifecycle events and then applies the
// seed/cleanup policy once per tick.
func torrentEventWorker(configPath string, fallback *Config, state *AppState, db *Database, comics *ComicsDb, eventLog *EventLog) {
	torrents := state.activeEngine()
	// Optional libtorrent-only hooks: nil for any other backend.
	extras, _ := torrents.(torrentEngineEmbeddedExtras)
	moveRequests := map[string]struct{}{}
	// Hashes whose final "completed" notification was already sent by this
	// worker: a later storage_moved/torrent_finished for the same torrent must not
	// announce it again.
	completionNotified := map[string]struct{}{}
	// Completion attempts that failed with a transient error (NAS unavailable,
	// I/O timeout, disk momentarily full, database busy) and wait for a retry.
	completionRetries := map[string]completionRetry{}
	// Completed torrents whose last recovery check found nothing to do: they are
	// not queried again for settledRecheckInterval (each check costs several
	// SQLite queries, at every 750 ms tick, for every seeding torrent).
	settledTorrents := map[string]settledTorrent{}
	storageMoveRetries := map[string]StorageMoveRetry{}
	seedCopyWarnings := map[string]time.Time{}
	// Storage moves are asynchronous. Keep post-seeding moves protected until
	// their success/failure alert arrives, otherwise the seed cleanup can remove
	// the source while libtorrent is still copying it.
	postSeedMoves := map[string]struct{}{}
	// Storage moves survive a restart: the retry schedule and the post-seed
	// protection are saved in the database and restored here.
	persistedMoves := restoreTorrentMoves(db, storageMoveRetries, postSeedMoves, time.Now())
	// Zero time: the first tick runs the completion recovery scan at once.
	var lastRecoveryScan time.Time
	metadataWaitStart := map[string]time.Time{}
	metadataFirstSeen := map[string]time.Time{}
	metadataLastWarning := map[string]time.Time{}
	stallWaitStart := map[string]StallWatch{}
	if restored, err := db.LoadStallWatches(); err != nil {
		logging.Warn("could not restore stalled torrent retry state", "error", err)
	} else if len(restored) > 0 {
		stallWaitStart = restored
		logging.Info("restored stalled torrent retry state", "torrents", len(restored))
	}
	// Periodic RAM-disk reconciliation: the metadata event fires once, so a
	// missed event (restart/race) used to leave an oversized torrent on the
	// tmpfs forever. `ramdiskAttempts` rate-limits retries per hash.
	ramdiskAttempts := map[string]time.Time{}
	lastRamdiskCheck := time.Now().Add(-120 * time.Second)
	lastMetadataPromotion := time.Now().Add(-30 * time.Second)
	lastDynamicAdjustment := time.Now().Add(-90 * time.Second)
	lastResumeSave := time.Now()
	// lastQueueCounts is the last logged "transferring/stuck/waiting" triple.
	lastQueueCounts := ""
	lastSpeedPolicy := time.Now().Add(-60 * time.Second)
	var lastSpeed *bg_speedPair
	lastDebugLog := time.Now().Add(-300 * time.Second)
	var lastPin *string
	var lastSequential *bool
	cfg := fallback
	notifier := FromConfig(cfg).Async()
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	lastConfigReload := time.Now().Add(-5 * time.Second)
	lastConfigForce := time.Now()
	var lastConfigMod time.Time
	var lastConfigGen uint64
	lastFingerprint := ""
	// Visibility for long integrity checks (multi-gigabyte packs on the NAS).
	// Torrents being re-checked: when the check started and when its progress
	// was last logged (at DEBUG; INFO gets only the start and a summary).
	type dataCheck struct{ started, lastLog time.Time }
	checking := map[string]dataCheck{}
	// One-time startup reconciliation. libtorrent's save_resume_data can record
	// a bitfield ahead of the bytes actually flushed to disk (its disk cache is
	// not drained on every shutdown). A torrent that reads as complete but was
	// never archived is therefore re-checked once, so the real progress is
	// established before any storage move, rename or archive step. Without this
	// a hard restart could trust a "100%" that is not on disk and re-download
	// the whole file.
	for _, torrent := range torrents.List() {
		if torrent.Progress < 99.99 {
			continue
		}
		// A torrent that is actively seeding has already verified its data.
		if torrent.State == "seeding" {
			continue
		}
		// Skip a torrent that was already re-checked recently: re-reading a
		// multi-gigabyte pack from the NAS is expensive and gives the same result.
		if extras != nil && extras.recentlyRechecked(torrent.Hash, recheckGuardWindow) {
			continue
		}
		status, statusErr := db.TorrentStatus(torrent.Hash)
		if statusErr == nil && status != nil && *status == "completed" {
			continue
		}
		if ok, err := torrents.ForceRecheck(torrent.Hash); err == nil && ok {
			logging.Info("startup re-check of an unarchived completed torrent",
				"hash", torrent.Hash, "name", torrent.Name)
		}
	}
	// Only torrents restored at startup can be considered "leftover failures".
	// Captured here, before the event loop, so a torrent the user re-adds later
	// is never detached because of a stale error row.
	startupHashes := map[string]struct{}{}
	for _, torrent := range torrents.List() {
		startupHashes[strings.ToLower(torrent.Hash)] = struct{}{}
	}
	for {
		if !state.SleepBackground(750 * time.Millisecond) {
			return
		}
		now := time.Now()
		// The backend can be switched at runtime from the settings/API: pick
		// up the new engine on the next tick so handlers and automation never
		// diverge on which transfer plane they drive.
		if active := state.activeEngine(); active != torrents {
			torrents = active
			extras, _ = torrents.(torrentEngineEmbeddedExtras)
			logging.Info("torrent event worker switched backend", "backend", torrents.Name())
		}
		// A long re-check (a large pack on the NAS) gets one line when it starts
		// and one summary when it ends; the per-minute progress is DEBUG.
		present := map[string]struct{}{}
		for _, torrent := range torrents.List() {
			key := strings.ToLower(torrent.Hash)
			present[key] = struct{}{}
			check, wasChecking := checking[key]
			if torrent.State == "checking_files" {
				if !wasChecking {
					checking[key] = dataCheck{started: now, lastLog: now}
					logging.Info(fmt.Sprintf("🔎 Checking the data already downloaded for «%s»…", torrent.Name))
				} else if now.Sub(check.lastLog) >= 60*time.Second {
					check.lastLog = now
					checking[key] = check
					logging.Debug("data check in progress", "name", torrent.Name, "progress", logPercent(torrent.Progress))
				}
				continue
			}
			if wasChecking {
				delete(checking, key)
				result := "the data is complete"
				if torrent.Progress < 99.99 {
					result = logPercent(torrent.Progress) + " was already downloaded, the rest will be downloaded"
				}
				logging.Info(fmt.Sprintf("✅ «%s» checked in %s: %s", torrent.Name, logDuration(now.Sub(check.started)), result))
			}
		}
		for key := range checking {
			if _, ok := present[key]; !ok {
				delete(checking, key)
			}
		}
		if now.Sub(lastConfigReload) >= 5*time.Second {
			// Reload only when something actually changed (API writes bump the
			// config generation, file edits bump the mtime) instead of opening
			// the config database every 5 seconds. A forced reload every minute
			// still catches out-of-band database edits.
			reload := now.Sub(lastConfigForce) >= 60*time.Second
			if !reload {
				if info, err := os.Stat(configPath); err == nil && info.ModTime().After(lastConfigMod) {
					reload = true
				}
			}
			if !reload && ConfigGeneration() != lastConfigGen {
				reload = true
			}
			if reload {
				lastConfigForce = now
				if info, err := os.Stat(configPath); err == nil {
					lastConfigMod = info.ModTime()
				}
				lastConfigGen = ConfigGeneration()
				if loaded, err := LoadConfig(configPath); err == nil {
					if fallback.DryRun {
						loaded.DryRun = true
					}
					cfg = &loaded
				} else {
					// Keep the last valid configuration: falling back to the startup
					// one would silently revert paths, seed policy and limits.
					logging.Warn("configuration reload failed; keeping the last valid configuration", "error", err)
				}
				// Rebuild the notifier/TMDB client only when the relevant config
				// changed: they used to be rebuilt (with new HTTP clients) every 5s.
				fingerprint := fmt.Sprintf("%s|%s|%s",
					bg_derefStr(cfg.TmdbAPIKey),
					cfg.TmdbLanguage(),
					bg_jsonString(cfg.Settings),
				)
				if fingerprint != lastFingerprint {
					notifier = FromConfig(cfg).Async()
					tmdb = NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
					lastFingerprint = fingerprint
				}
			}
			lastConfigReload = now
		}
		if extras != nil && now.Sub(lastResumeSave) >= resumeSaveInterval {
			if requested := extras.RequestResumeSave(); requested < 0 {
				logging.Debug("periodic resume save request failed")
			} else if requested > 0 {
				logging.Debug("periodic resume save requested", "torrents", requested)
			}
			lastResumeSave = now
		}
		if now.Sub(lastMetadataPromotion) >= 30*time.Second {
			// Torrents without metadata that were just promoted lose
			// `auto_managed`; when metadata arrives it must be re-armed, even if
			// the alert was lost (e.g. fastresume restore). These hooks are
			// libtorrent-specific; other backends handle metadata themselves.
			if extras != nil {
				extras.PromoteMetadata()
				rearmed := extras.EnsureAutoManaged()
				if rearmed > 0 {
					logging.Info("auto-managed flag restored on running torrents", "rearmed", rearmed)
				}
			}
			lastMetadataPromotion = now
		}
		if now.Sub(lastDynamicAdjustment) >= 90*time.Second {
			effectiveDownloadKib, _ := bg_currentSpeedLimits(cfg)
			torrents.AdjustQueue(cfg, effectiveDownloadKib)
			// Queue visibility: log when the counts shown in the line change, not
			// on every tick. Comparing the engine "downloading" state instead
			// re-logged the same counts twice per stall probe (resumed, then
			// parked again ten minutes later).
			snapshot := torrents.List()
			// Apply add-time options that need metadata (first/last pieces) or that
			// pause as soon as metadata arrives (metadata-only adds).
			if extras != nil {
				extras.EnforceDeferredOptions(snapshot)
			}
			queued := 0
			transferring := 0
			idle := 0
			var rateKib uint64
			for _, torrent := range snapshot {
				// A torrent parked by the stall monitor is stuck whatever its
				// engine state: after a restart it comes back merely paused
				// until the monitor parks it again, and must not be reported
				// as waiting in between.
				if entry, ok := stallWaitStart[torrent.Hash]; ok && entry.stalledSince != nil && !TorrentTransferring(torrent) && torrent.Progress < 100 {
					idle++
					rateKib += torrent.DownloadRate
					continue
				}
				if torrent.State == "paused" {
					queued++
				}
				switch {
				case TorrentTransferring(torrent):
					transferring++
				case TorrentIdle(torrent):
					idle++
				}
				rateKib += torrent.DownloadRate
			}
			counts := fmt.Sprintf("%d/%d/%d", transferring, idle, queued)
			if counts != lastQueueCounts {
				rateKib /= 1024
				speed := fmt.Sprintf("speed %d KB/s", rateKib)
				if effectiveDownloadKib > 0 {
					speed += fmt.Sprintf(" (limit %d KB/s)", effectiveDownloadKib)
				}
				// "Active" means really transferring: a download stuck at 0 B/s
				// is counted apart, not as active.
				logging.Info(fmt.Sprintf("📊 Downloads: %d transferring, %d stuck at 0 B/s, %d waiting or paused · %s", transferring, idle, queued, speed))
				lastQueueCounts = counts
			}
			lastDynamicAdjustment = now
		}
		if now.Sub(lastSpeedPolicy) >= 60*time.Second {
			downloadKib, uploadKib, source, tempShadowed := speedPolicy(cfg)
			// Always re-assert, not just on change: "Apply now" and the optimizer
			// set the session limit to the unscheduled value (e.g. 3000 by day)
			// and the policy must correct it on its own within a minute. The log
			// stays only on real changes, of the values or of their source (the
			// schedule starting or ending is logged even with equal values).
			changed := lastSpeed == nil || lastSpeed.download != downloadKib || lastSpeed.upload != uploadKib || lastSpeed.source != source
			if _, err := torrents.SetGlobalSpeedLimits(downloadKib, uploadKib); err != nil {
				logging.Debug("speed policy apply failed", "error", err)
			} else {
				if changed {
					label := map[string]string{
						speedSourceTemp:     "temporary limit",
						speedSourceTempKeep: "temporary limit, until removed",
						speedSourceSchedule: "scheduled limit",
						speedSourceBase:     "standard limit",
					}[source]
					wasScheduled := lastSpeed != nil && lastSpeed.source == speedSourceSchedule
					switch {
					case source == speedSourceSchedule && !wasScheduled:
						label = "schedule started"
					case source != speedSourceSchedule && wasScheduled:
						label += ", schedule ended"
					}
					if source == speedSourceSchedule && tempShadowed {
						label += ", overrides the temporary limit without expiry"
					}
					logging.Info(fmt.Sprintf("🚦 Speed set to %s download, %s upload (%s)", speedLimitLabel(downloadKib), speedLimitLabel(uploadKib), label))
				}
				lastSpeed = &bg_speedPair{download: downloadKib, upload: uploadKib, source: source}
			}
			lastSpeedPolicy = now
		}
		// `debug_enabled`: periodic diagnostics for crashes/RAM/loops.
		if cfg.DebugEnabled() && now.Sub(lastDebugLog) >= 300*time.Second {
			lastDebugLog = now
			logging.Debug("debug diagnostics", "resident_kb", bg_residentKB(), "torrents", len(torrents.List()))
		}
		var pinned *string
		if value, ok := cfg.Settings["libtorrent_pinned_hash"]; ok {
			trimmed := strings.ToLower(strings.TrimSpace(value))
			if trimmed != "" {
				pinned = &trimmed
			}
		}
		if !bg_strPtrEqual(lastPin, pinned) {
			applied := false
			if pinned != nil {
				if _, err := torrents.SetPin(*pinned, true); err != nil {
					logging.Debug("pin apply failed; will retry", "error", err)
				} else {
					applied = true
				}
			} else {
				if _, err := torrents.SetPin("", false); err != nil {
					logging.Debug("unpin apply failed; will retry", "error", err)
				} else {
					applied = true
				}
			}
			if applied {
				lastPin = bg_copyStrPtr(pinned)
			}
		}
		sequential := false
		if value, ok := cfg.Settings["libtorrent_sequential"]; ok {
			sequential = settingTruthy(value)
		}
		if lastSequential == nil || *lastSequential != sequential {
			if _, err := torrents.SetSequential(sequential); err != nil {
				logging.Debug("sequential apply failed; will retry", "error", err)
			} else {
				lastSequential = &sequential
			}
		}
		MonitorMetadata(cfg, torrents, db, notifier, metadataWaitStart, metadataFirstSeen, metadataLastWarning)
		MonitorStalled(cfg, torrents, db, notifier, stallWaitStart)
		if now.Sub(lastRamdiskCheck) >= 30*time.Second {
			ReconcileRamdisk(cfg, torrents, ramdiskAttempts)
			lastRamdiskCheck = now
		}
		RetryStorageMoves(torrents, moveRequests, postSeedMoves, storageMoveRetries)
		torrentEvents := torrents.PollEvents()
		eventHashes := map[string]struct{}{}
		for _, event := range torrentEvents {
			eventHashes[strings.ToLower(event.Hash)] = struct{}{}
			delete(settledTorrents, strings.ToLower(event.Hash))
		}
		// Forget torrents that left the session.
		if len(settledTorrents) > 0 {
			present := map[string]struct{}{}
			for _, torrent := range torrents.List() {
				present[strings.ToLower(torrent.Hash)] = struct{}{}
			}
			for hash := range settledTorrents {
				if _, ok := present[hash]; !ok {
					delete(settledTorrents, hash)
				}
			}
		}
		// A torrent may reach 100% while Gextto is restarting or while its
		// completion event is already gone from libtorrent's alert queue.
		// Recreate the event from the persisted torrent status so postprocess and
		// notifications are not silently skipped. Events are the primary signal:
		// this scan is only a safety net, so it runs at start-up and then every
		// completionRecoveryInterval instead of at every tick.
		if now.Sub(lastRecoveryScan) >= completionRecoveryInterval {
			lastRecoveryScan = now
			for _, torrent := range torrents.List() {
				if !torrent.HasMetadata {
					continue
				}
				hash := strings.ToLower(torrent.Hash)
				if _, ok := eventHashes[hash]; ok {
					continue
				}
				if settled, ok := settledTorrents[hash]; ok {
					if now.Before(settled.until) && settled.savePath == torrent.SavePath {
						continue
					}
					delete(settledTorrents, hash)
				}
				// A completion that failed transiently is retried on its own backoff,
				// not at every tick.
				if retry, waiting := completionRetries[hash]; waiting && now.Before(retry.next) {
					continue
				}
				_, inMoves := moveRequests[hash]
				_, inRetries := storageMoveRetries[hash]
				_, inPostSeed := postSeedMoves[hash]
				if inMoves || inRetries || inPostSeed {
					if meta, metaErr := db.TorrentMeta(hash); metaErr == nil && meta != nil {
						if dest, ok := ConfiguredDestinationFor(&meta.Release, cfg); ok && SamePath(torrent.SavePath, dest) {
							delete(moveRequests, hash)
							delete(storageMoveRetries, hash)
							delete(postSeedMoves, hash)
							inMoves, inRetries, inPostSeed = false, false, false
						}
					}
					if inMoves || inRetries || inPostSeed {
						continue
					}
				}
				// In-progress downloads that are actively downloading (<99.99% and State "downloading" or "queued")
				// cannot possibly need completion recovery; skip immediately to avoid redundant SQLite queries.
				if torrent.Progress < 99.99 && (torrent.State == "downloading" || torrent.State == "queued") {
					continue
				}
				// A completed single whose end-of-seed move already reached the
				// archive but whose import never ran (lost storage_moved alert, or a
				// restart) is finalized through the normal storage_moved path. Marking
				// the raw archive path complete here would skip the rename, the
				// MediaInfo probe and the completion notification.
				if bg_torrentNeedsArchiveImport(cfg, db, &torrent) {
					status, _ := db.TorrentStatus(hash)
					isCompletedDB := status != nil && *status == "completed"
					if !isCompletedDB && torrent.Progress < 99.99 {
						if checked, checkErr := torrents.ForceRecheck(hash); checkErr != nil || !checked {
							logging.Debug("existing archive recheck could not be started", "error", checkErr)
						}
						continue
					}
					logging.Debug("recovering completed torrent without completion event",
						"hash", hash, "name", torrent.Name, "save_path", torrent.SavePath,
						"progress", torrent.Progress, "kind", "storage_moved")
					torrentEvents = append(torrentEvents, models.TorrentEvent{
						Kind:     "storage_moved",
						Hash:     hash,
						Name:     torrent.Name,
						SavePath: torrent.SavePath,
					})
					eventHashes[hash] = struct{}{}
					continue
				}
				if torrent.Progress < 99.99 {
					continue
				}
				pending := false
				if meta, err := db.TorrentMeta(hash); err == nil && meta != nil {
					if status, err := db.TorrentStatus(hash); err == nil && status != nil {
						if *status != "completed" && *status != "error" && *status != "removed" {
							pending = true
						}
					}
				}
				if !pending {
					settledTorrents[hash] = settledTorrent{savePath: torrent.SavePath, until: now.Add(settledRecheckInterval)}
				}
				if pending {
					logging.Debug("recovering completed torrent without completion event",
						"hash", hash, "name", torrent.Name, "save_path", torrent.SavePath, "progress", torrent.Progress)
					torrentEvents = append(torrentEvents, models.TorrentEvent{
						Kind:     "torrent_finished",
						Hash:     hash,
						Name:     torrent.Name,
						SavePath: torrent.SavePath,
					})
					eventHashes[hash] = struct{}{}
				}
			}
		}
		completionSeen := false
		for _, event := range torrentEvents {
			publicEvent := event
			hash := event.Hash
			if event.Kind == "torrent_finished" || event.Kind == "storage_moved" {
				completionSeen = true
			}
			if event.Kind == "torrent_checked" {
				var found *models.TorrentView
				for _, torrent := range torrents.List() {
					if strings.EqualFold(torrent.Hash, event.Hash) {
						candidate := torrent
						found = &candidate
						break
					}
				}
				if found != nil {
					// A routine re-check of an in-progress download is normal and
					// must not spam the log. Surface it only when it reveals a
					// discrepancy: a torrent error, or data that the database
					// already considers completed but that is not actually
					// complete on disk.
					discrepancy := strings.TrimSpace(found.Error) != ""
					if status, statusErr := db.TorrentStatus(event.Hash); statusErr == nil && status != nil &&
						*status == "completed" && found.Progress < 99.99 {
						if archivePath, present := bg_completedArchivePresent(db, event.Hash, found.TotalSize); present && strings.TrimSpace(found.Error) == "" {
							// After MoveStorage, libtorrent may emit torrent_checked while
							// its resumed session still reports paused/0%. The verified
							// archived payload is present, so this is not corruption.
							logging.Debug("integrity check reported a transient state after archive relocation",
								"name", event.Name, "path", archivePath, "state", found.State)
							continue
						}
						discrepancy = true
					}
					if discrepancy {
						logging.Warn("completed torrent needs integrity verification",
							"hash", event.Hash, "name", event.Name, "state", found.State,
							"progress", found.Progress, "checked_bytes", found.TotalDone, "total_bytes", found.TotalSize,
							"error", found.Error)
					} else {
						logging.Debug("torrent integrity check completed",
							"hash", event.Hash, "name", event.Name, "state", found.State,
							"progress", found.Progress, "checked_bytes", found.TotalDone, "total_bytes", found.TotalSize)
					}
				} else {
					logging.Debug("torrent integrity check completed; torrent is no longer in the session",
						"hash", event.Hash, "name", event.Name)
				}
			}
			if comic, err := comics.Torrent(hash); err == nil && comic != nil {
				// A comic torrent always carries the "Comic" tag, so it can be
				// filtered with the other downloads and routed by category.
				_ = db.SetTorrentTag(hash, "Comic")
				if event.Kind == "torrent_finished" || event.Kind == "storage_moved" {
					if err := comics.CompleteTorrent(hash, event.SavePath); err != nil {
						_ = notifier.NotifyEvent("comic_error", map[string]any{
							"hash":  hash,
							"title": comic.Title,
							"error": err.Error(),
						})
						logging.Error("comic torrent completion persistence failed",
							"error", err, "hash", event.Hash, "title", comic.Title)
					} else {
						comicSize, _ := SizeOfPath(CompletionPath(&event))
						if err := notifier.NotifyEvent("comic_completed", map[string]any{
							"hash":       hash,
							"title":      comic.Title,
							"post_url":   comic.PostURL,
							"path":       event.SavePath,
							"size_bytes": comicSize,
							"method":     "torrent",
						}); err != nil {
							logging.Warn("completion notification failed", "hash", hash, "title", comic.Title, "error", err)
						} else {
							logging.Debug("completion notification sent", "hash", hash, "title", comic.Title)
						}
						logging.Info(fmt.Sprintf("comic completed — «%s» · %s", comic.Title, event.SavePath))
					}
				}
				eventLog.Push(publicEvent)
				continue
			}
			// A torrent already archived (completed with a recorded archive copy)
			// has been announced before: a repeated completion event, or a
			// re-process of a missing copy, must not notify a second time.
			alreadyAnnounced := false
			if event.Kind == "torrent_finished" || event.Kind == "storage_moved" {
				if _, sent := completionNotified[strings.ToLower(hash)]; sent {
					alreadyAnnounced = true
				} else if status, statusErr := db.TorrentStatus(hash); statusErr == nil && status != nil && *status == "completed" {
					if archived, archivedErr := db.TorrentProcessed(hash); archivedErr == nil && archived != nil && strings.TrimSpace(*archived) != "" {
						alreadyAnnounced = true
					}
				}
			}
			processed, handleErr := HandleTorrentEvent(cfg, torrents, db, moveRequests, postSeedMoves, storageMoveRetries, event, tmdb, notifier)
			isCompletion := event.Kind == "torrent_finished" || event.Kind == "storage_moved"
			if handleErr != nil && isCompletion && isTransientCompletionError(handleErr) {
				key := strings.ToLower(hash)
				retry := completionRetries[key]
				if retry.attempts < completionRetryMaxAttempts {
					retry.attempts++
					retry.next = now.Add(completionRetryDelay(retry.attempts))
					completionRetries[key] = retry
					logging.Warn("torrent completion failed with a transient error; will retry",
						"hash", hash, "name", event.Name, "attempt", retry.attempts,
						"max_attempts", completionRetryMaxAttempts,
						"retry_in", completionRetryDelay(retry.attempts).String(), "error", handleErr)
					eventLog.Push(publicEvent)
					continue
				}
				logging.Warn("torrent completion still failing after retries; giving up",
					"hash", hash, "name", event.Name, "attempts", retry.attempts)
			}
			if isCompletion {
				delete(completionRetries, strings.ToLower(hash))
			}
			if handleErr != nil {
				restored, _ := db.RestoreUpgrade(hash)
				_ = db.MarkTorrentError(hash, handleErr.Error())
				// Completion errors are terminal for the persisted state; detach
				// the torrent so it cannot remain active forever. Keep its source
				// files in place for manual inspection.
				if isCompletion {
					if removed, err := torrents.Remove(hash, false); err != nil {
						logging.Warn("failed to detach torrent after completion error",
							"hash", hash, "name", event.Name, "error", err)
					} else if removed {
						_ = db.MarkTorrentRemovedAt(hash)
					}
				}
				_ = notifier.NotifyEvent("torrent_error", map[string]any{
					"hash":             hash,
					"name":             event.Name,
					"title":            event.Name,
					"error":            handleErr.Error(),
					"upgrade_restored": restored,
				})
				logging.Error("torrent completion handling failed", "error", handleErr, "hash", hash, "name", event.Name)
				processed = false
			}
			if processed && (event.Kind == "torrent_finished" || event.Kind == "storage_moved") {
				requestMediaLibraryRefresh(cfg, mediaRefreshFolder(db, hash))
			}
			// A completed single episode/movie is moved into the archive and
			// renamed, so the file is no longer at the path libtorrent tracks. It
			// can no longer seed and would re-download the release on the next
			// check (seen as a duplicate). Drop those torrents, but keep
			// no-rename/in-place completions seeding as before.
			//
			// Only when the user asked for automatic removal: with the setting
			// off the torrent is paused and stays listed as completed, and the
			// user clears it with "Pulisci completati".
			if processed && cfg.Libtorrent.AutoRemoveCompleted && (event.Kind == "torrent_finished" || event.Kind == "storage_moved") {
				isPack := false
				if meta, err := db.TorrentMeta(event.Hash); err == nil && meta != nil {
					isPack = meta.Release.IsPack
				}
				if !isPack {
					if _, err := os.Stat(CompletionPath(&event)); err != nil {
						if removed, err := torrents.Remove(event.Hash, false); err == nil && removed {
							_ = db.MarkTorrentRemovedAt(event.Hash)
							logging.Info(fmt.Sprintf("completed torrent removed: file renamed into the archive — «%s»", event.Name))
						}
					}
				}
			}
			if processed && (event.Kind == "torrent_finished" || event.Kind == "storage_moved") {
				completionMeta, _ := db.TorrentMeta(event.Hash)
				isPack := completionMeta != nil && completionMeta.Release.IsPack
				if !isPack {
					processedPath, err := db.TorrentProcessed(event.Hash)
					processed := event.SavePath
					if err == nil && processedPath != nil {
						processed = *processedPath
					}
					sizeBytes, _ := SizeOfPath(processed)
					title := event.Name
					if completionMeta != nil {
						title = completionMeta.Release.Title
					}
					// Download duration and average speed for the notification.
					var durationSeconds *int64
					var averageSpeedBps *int64
					if times, err := db.TorrentTimes(event.Hash); err == nil && times != nil {
						if created, ok := utils.ParseTimestamp(times.CreatedAt); ok {
							completed := time.Now().UTC()
							if times.CompletedAt != nil {
								if parsed, ok := utils.ParseTimestamp(*times.CompletedAt); ok {
									completed = parsed
								}
							}
							seconds := int64(completed.Sub(created).Seconds())
							if seconds < 1 {
								seconds = 1
							}
							speed := int64(float64(sizeBytes) / float64(seconds))
							durationSeconds = &seconds
							averageSpeedBps = &speed
						}
					}
					var kindValue any
					var seriesValue any
					var seasonValue any
					var episodeValue any
					if completionMeta != nil {
						kindValue = completionMeta.Release.Kind
						if completionMeta.Release.Series != nil {
							seriesValue = *completionMeta.Release.Series
						}
						if completionMeta.Release.Season != nil {
							seasonValue = *completionMeta.Release.Season
						}
						if completionMeta.Release.Episode != nil {
							episodeValue = *completionMeta.Release.Episode
						}
					}
					var replacedTitleValue any
					if replacedName, ok := db.UpgradeReplacedInfo(event.Hash); ok {
						replacedTitleValue = replacedName
					}
					if alreadyAnnounced {
						logging.Debug("completion already announced; notification skipped", "hash", event.Hash, "kind", event.Kind)
					} else {
						completionNotified[strings.ToLower(event.Hash)] = struct{}{}
						if err := notifier.NotifyEvent("torrent_completed", map[string]any{
							"hash":              event.Hash,
							"name":              event.Name,
							"title":             title,
							"kind":              kindValue,
							"series":            seriesValue,
							"season":            seasonValue,
							"episode":           episodeValue,
							"path":              processed,
							"size_bytes":        sizeBytes,
							"duration_seconds":  durationSeconds,
							"average_speed_bps": averageSpeedBps,
							"replaced_title":    replacedTitleValue,
						}); err != nil {
							logging.Warn("completion notification failed", "hash", event.Hash, "event", "torrent_completed", "title", title, "error", err)
						} else {
							logging.Debug("completion notification sent", "hash", event.Hash, "event", "torrent_completed", "title", title)
						}
					}
					// Ground-truth media inspection of the placed file. A missing
					// ffprobe just leaves the filename-derived quality in place.
					if completionMeta != nil {
						probePath := processed
						release := completionMeta.Release
						if info, probeErr := ProbeResult(probePath); probeErr != nil {
							logging.Warn("MediaInfo probe failed for completed file", "title", release.Title, "path", probePath, "error", probeErr)
						} else if err := db.SetMediaInfo(&release, &info); err != nil {
							logging.Warn("could not save MediaInfo for completed file", "title", release.Title, "path", probePath, "error", err)
						} else {
							logging.Debug("saved media details for completed file",
								"title", release.Title, "path", probePath, "resolution", info.Resolution(), "hdr", info.HDR, "bit_depth", info.BitDepth)
						}
					}
				}
			}
			eventLog.Push(publicEvent)
		}
		if completionSeen {
			// A finished transfer can leave libtorrent's peak arena resident;
			// release it now instead of waiting for the next allocation burst.
			TrimMemory()
		}
		DetachErrorTorrents(torrents, db, startupHashes)
		DetachCompletedArchivedSingles(cfg, torrents, db)
		RejectActivePackIdentityMismatches(torrents, db)
		// Remove completed torrents that have finished seeding before the seed
		// policy moves them to disk only to delete them. With automatic removal
		// off, completed torrents stay listed as completed until the user runs
		// "Pulisci completati".
		RemoveSeededCompleted(cfg, torrents, db, postSeedMoves)
		// Enforce the seed policy only after handling this tick's events: with a
		// very low seed limit a just-finished torrent could otherwise be removed
		// before its `torrent_finished` event is post-processed and archived.
		EnforceSeedPolicy(cfg, torrents, db, postSeedMoves, storageMoveRetries, seedCopyWarnings)
		syncTorrentMoves(db, persistedMoves, storageMoveRetries, postSeedMoves)
	}
}

// resumeSaveInterval is how often the torrent progress is saved while running,
// so an unclean stop loses at most this much download state.
const resumeSaveInterval = 2 * time.Minute

// completionRecoveryInterval is how often the worker looks for completed
// torrents whose completion event was lost. Transient completion retries
// (completionRetries, first backoff one minute) also go through this scan.
const completionRecoveryInterval = 30 * time.Second

// torrentMoveRestoreGrace is how long a restored storage move waits before it
// is checked again: the engine may still be loading its torrents right after a
// start, and a move whose torrent is not listed yet would be dropped.
const torrentMoveRestoreGrace = 60 * time.Second

// persistedMove is the saved part of a torrent's storage-move state, used to
// write only what changed since the last tick.
type persistedMove struct {
	hasRetry    bool
	destination string
	postSeed    bool
	attempts    uint8
	guard       bool
}

// restoreTorrentMoves loads the saved storage moves into the worker maps and
// returns what is persisted. No move is running after a restart, so every
// restored retry is marked in flight with a short grace: RetryStorageMoves then
// clears it if the torrent already sits in its destination, or issues the move
// again. A post-seed protection without its retry is stale and is dropped.
func restoreTorrentMoves(db *Database, retries map[string]StorageMoveRetry, postSeedMoves map[string]struct{}, now time.Time) map[string]persistedMove {
	persisted := map[string]persistedMove{}
	saved, err := db.LoadTorrentMoves()
	if err != nil {
		logging.Warn("could not restore pending storage moves", "error", err)
		return persisted
	}
	for hash, state := range saved {
		entry := persistedMove{guard: state.PostSeedGuard}
		if state.Retry != nil {
			entry.hasRetry = true
			entry.destination = state.Retry.destination
			entry.postSeed = state.Retry.postSeed
			entry.attempts = state.Retry.attempts
		}
		persisted[hash] = entry
		if state.Retry == nil {
			continue
		}
		retry := *state.Retry
		retry.inFlight = true
		retry.nextAttempt = now.Add(torrentMoveRestoreGrace)
		retries[hash] = retry
		if state.PostSeedGuard {
			postSeedMoves[hash] = struct{}{}
		}
	}
	if len(saved) > 0 {
		logging.Info("restored pending storage moves", "torrents", len(saved))
	}
	return persisted
}

// syncTorrentMoves writes the storage-move state that changed since the last
// call. The worker maps stay the working copy; the database only mirrors them.
func syncTorrentMoves(db *Database, persisted map[string]persistedMove, retries map[string]StorageMoveRetry, postSeedMoves map[string]struct{}) {
	current := make(map[string]persistedMove, len(retries)+len(postSeedMoves))
	for hash, retry := range retries {
		key := strings.ToLower(hash)
		entry := current[key]
		entry.hasRetry = true
		entry.destination = retry.destination
		entry.postSeed = retry.postSeed
		entry.attempts = retry.attempts
		current[key] = entry
	}
	for hash := range postSeedMoves {
		key := strings.ToLower(hash)
		entry := current[key]
		entry.guard = true
		current[key] = entry
	}
	save := func(hash string, entry persistedMove) bool {
		state := TorrentMoveState{PostSeedGuard: entry.guard}
		if entry.hasRetry {
			state.Retry = &StorageMoveRetry{destination: entry.destination, postSeed: entry.postSeed, attempts: entry.attempts}
		}
		if err := db.SaveTorrentMove(hash, state); err != nil {
			logging.Warn("could not save storage move state", "hash", hash, "error", err)
			return false
		}
		return true
	}
	for hash, entry := range current {
		if previous, ok := persisted[hash]; ok && previous == entry {
			continue
		}
		if save(hash, entry) {
			persisted[hash] = entry
		}
	}
	for hash := range persisted {
		if _, ok := current[hash]; ok {
			continue
		}
		if save(hash, persistedMove{}) {
			delete(persisted, hash)
		}
	}
}

// ---------------------------------------------------------------------------
// speed-limit policy helpers (current_speed_limits / scheduled_speed_limits)
// ---------------------------------------------------------------------------

type bg_speedPair struct {
	download int64
	upload   int64
	source   string
}

func bg_scheduledSpeedLimits(cfg *Config) (int64, int64, bool) {
	enabled := false
	if value, ok := cfg.Settings["libtorrent_sched_enabled"]; ok {
		enabled = settingTruthy(value)
	}
	if !enabled {
		return 0, 0, false
	}
	now := time.Now()
	// `%u` is Monday=1..Sunday=7; `-1` makes it Monday=0..Sunday=6.
	weekday := (int(now.Weekday()) + 6) % 7
	activeDays := map[int]bool{}
	for _, day := range strings.Split(cfg.Settings["libtorrent_sched_days"], ",") {
		if parsed, err := strconv.Atoi(strings.TrimSpace(day)); err == nil {
			activeDays[parsed] = true
		}
	}
	dayOK := activeDays[weekday]
	parseTime := func(key string, defaultHour, defaultMinute int) (int, int) {
		if value, ok := cfg.Settings[key]; ok {
			parts := strings.SplitN(value, ":", 2)
			if len(parts) == 2 {
				hour, errHour := strconv.Atoi(strings.TrimSpace(parts[0]))
				minute, errMinute := strconv.Atoi(strings.TrimSpace(parts[1]))
				if errHour == nil && errMinute == nil {
					return hour, minute
				}
			}
		}
		return defaultHour, defaultMinute
	}
	startHour, startMinute := parseTime("libtorrent_sched_start", 23, 0)
	endHour, endMinute := parseTime("libtorrent_sched_end", 8, 0)
	nowMinutes := now.Hour()*60 + now.Minute()
	startMinutes := startHour*60 + startMinute
	endMinutes := endHour*60 + endMinute
	inTime := false
	if startMinutes <= endMinutes {
		inTime = nowMinutes >= startMinutes && nowMinutes < endMinutes
	} else {
		inTime = nowMinutes >= startMinutes || nowMinutes < endMinutes
	}
	if dayOK && inTime {
		return bg_parseSettingInt(cfg, "libtorrent_sched_dl_limit"),
			bg_parseSettingInt(cfg, "libtorrent_sched_ul_limit"), true
	}
	return 0, 0, false
}

func bg_currentSpeedLimits(cfg *Config) (int64, int64) {
	return gh6_currentSpeedLimits(cfg)
}

func bg_residentKB() uint64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * 4
}

// ---------------------------------------------------------------------------
// apply_libtorrent_optimization
// ---------------------------------------------------------------------------

// bg_applyLibtorrentOptimization applies the optimization only when a value
// changed, so the continuous worker does not rewrite the config every tick.
// It returns (changed, detected memory MB).
func bg_applyLibtorrentOptimization(s *AppState) (bool, uint64, error) {
	cfg := latestConfig(s)
	optimization := gh0_libtorrentOptimizationFor(cfg)
	changed := false
	for _, change := range optimization.changes {
		if cfg.Settings[change.key] != change.value {
			changed = true
			break
		}
	}
	if !changed {
		return false, optimization.memoryMB, nil
	}
	for _, change := range optimization.changes {
		if err := SaveSetting(s.cfg.DataDir, change.key, change.value); err != nil {
			return false, optimization.memoryMB, fmt.Errorf("%s: %w", change.key, err)
		}
	}
	optimized := latestConfig(s)
	if _, err := s.torrents.ApplySettings(optimized); err != nil {
		return false, optimization.memoryMB, err
	}
	return true, optimization.memoryMB, nil
}

// ---------------------------------------------------------------------------
// optimize_worker
// ---------------------------------------------------------------------------

// optimizeWorker implements `optimize_worker`. For gx-torrent it re-asserts the
// engine's own optimization (cache sized from the RAM, queue policy) every
// period; for the embedded engine it runs the libtorrent optimization when
// `libtorrent_auto_optimize` is enabled.
func optimizeWorker(state *AppState) {
	const optimizePeriod = 15 * time.Minute
	for {
		if !state.SleepBackground(optimizePeriod) {
			return
		}
		// gx-torrent owns its cache sizing: the daemon keeps it automatic
		// (1/32 read, 1/16 write of the RAM). Re-assert the policy periodically
		// so a machine/RAM change or a settings edit is re-applied; this is the
		// gx-torrent continuous optimization and is always on.
		if gx, ok := state.activeEngine().(*gxTorrentEngine); ok {
			if _, err := gx.ApplyOptimization(latestConfig(state)); err != nil {
				logging.Debug("gx-torrent continuous optimization failed", "error", err.Error())
			}
			continue
		}
		// Read-only use: the shared cached configuration is enough.
		enabled := settingTruthy(latestConfig(state).Settings["libtorrent_auto_optimize"])
		if !enabled {
			continue
		}
		changed, memoryMB, err := bg_applyLibtorrentOptimization(state)
		if err != nil {
			logging.Warn("libtorrent continuous optimization failed", "error", err)
		} else if changed {
			logging.Info("libtorrent continuous optimization applied", "memory_mb", memoryMB)
		} else {
			logging.Debug("libtorrent continuous optimization: values already optimal")
		}
	}
}

// ---------------------------------------------------------------------------
// ipfilter_refresh_worker
// ---------------------------------------------------------------------------

// ipFilterRefreshInterval is how often the configured IP filter is downloaded
// again and reloaded while the daemon stays up. The refresh at service boot is
// separate (the managed gx-torrent gets a fresh list before it starts).
const ipFilterRefreshInterval = 7 * 24 * time.Hour

// ipFilterRefreshWorker keeps the IP filter fresh on a long-running daemon:
// once a week it re-downloads the configured list (when it is a URL) and
// reloads it into the active engine, so a stale blocklist does not survive for
// months. A backend without IP filter support is skipped.
func ipFilterRefreshWorker(state *AppState) {
	for {
		if !state.SleepBackground(ipFilterRefreshInterval) {
			return
		}
		cfg := latestConfig(state)
		if cfg == nil || strings.TrimSpace(cfg.Libtorrent.IpFilterPath) == "" {
			continue
		}
		if _, ok := state.activeEngine().(interface {
			LoadIPFilter(path string) (int, error)
		}); !ok {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		rules, path, err := applyIPFilter(ctx, state)
		cancel()
		if err != nil {
			logging.Warn("IP filter refresh failed", "error", err.Error())
			continue
		}
		logging.Info("IP filter refreshed", "rules", rules, "path", path)
	}
}

// ---------------------------------------------------------------------------
// db_checkpoint_worker
// ---------------------------------------------------------------------------

// dbCheckpointWorker truncates the WALs periodically so heavy writes (archive
// FTS batches, housekeeping deletes, episode updates) do not leave a
// multi-hundred-MB `-wal` file between restarts.
func dbCheckpointWorker(state *AppState) {
	for {
		if !state.SleepBackground(15 * time.Minute) {
			return
		}
		// Periodic housekeeping also returns any freed heap to the OS.
		TrimMemory()
		db := state.db
		archive := state.archive
		comics := state.comics
		i18n := state.i18n
		if db != nil {
			_ = db.Checkpoint()
		}
		if archive != nil {
			_ = archive.Checkpoint()
		}
		if comics != nil {
			_ = comics.Checkpoint()
		}
		if i18n != nil {
			_ = i18n.Checkpoint()
		}
	}
}

// ---------------------------------------------------------------------------
// temp_cleanup_worker
// ---------------------------------------------------------------------------

// bg_dirHasFiles reports whether the directory contains at least one file (also
// in subdirectories).
func bg_dirHasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if bg_dirHasFiles(path) {
				return true
			}
		} else {
			return true
		}
	}
	return false
}

// bg_cleanupEmptyTempDirs removes the EMPTY directories left in
// `libtorrent_temp_dir` (leftovers of completed or removed downloads). It skips
// active torrent directories and too-recent ones, so a just-started download is
// not disturbed.
func bg_cleanupEmptyTempDirs(cfg *Config, torrents TorrentSession) {
	if cfg.LibtorrentTempDir == nil {
		return
	}
	temp := *cfg.LibtorrentTempDir
	live := torrents.List()
	active := map[string]struct{}{}
	activePaths := make([]string, 0, len(live))
	for _, torrent := range live {
		active[torrent.Name] = struct{}{}
		activePaths = append(activePaths, torrent.SavePath)
	}
	entries, err := os.ReadDir(temp)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(temp, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := active[name]; ok {
			continue
		}
		activeChild := false
		for _, activePath := range activePaths {
			if pathStartsWith(activePath, path) {
				activeChild = true
				break
			}
		}
		if activeChild {
			continue
		}
		if bg_dirHasFiles(path) {
			continue
		}
		// Only if it has been there for at least an hour: avoid removing the
		// directory of a download just created and not yet populated.
		if time.Since(info.ModTime()) < time.Hour {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			logging.Debug("could not remove empty temp folder", "dir", path, "error", err)
		} else {
			logging.Info("removed empty temp folder", "dir", path)
		}
	}
}

// tempCleanupWorker periodically cleans empty folders and stale temporary copy
// files left behind by crashes or aborted transfers.
func tempCleanupWorker(state *AppState) {
	const cleanupPeriod = 30 * time.Minute
	// Run initial sweep 60s after startup to clean remnants from previous runs
	if !state.SleepBackground(60 * time.Second) {
		return
	}
	cfg := latestConfig(state)
	bg_cleanupEmptyTempDirs(cfg, state.activeEngine())
	SweepStaleTempFiles(cfg)
	trashOrphanedTempData(cfg, state.activeEngine(), state.db, time.Now())

	for {
		if !state.SleepBackground(cleanupPeriod) {
			return
		}
		cfg := latestConfig(state)
		bg_cleanupEmptyTempDirs(cfg, state.activeEngine())
		SweepStaleTempFiles(cfg)
		trashOrphanedTempData(cfg, state.activeEngine(), state.db, time.Now())
	}
}

// ---------------------------------------------------------------------------
// housekeeping_worker
// ---------------------------------------------------------------------------

// housekeepingWorker runs the periodic data hygiene (bounded tables + database
// compaction). The first pass is delayed so startup stays fast; the interval is
// configurable (`housekeeping_interval_hours`, 0/disabled via
// `housekeeping_enabled`).
func housekeepingWorker(state *AppState) {
	// The last run is remembered across restarts: a restart (an update) must
	// not trigger the cleanup again, it runs once per interval.
	if !state.SleepBackground(housekeepingFirstDelay(latestConfig(state), time.Now())) {
		return
	}
	for {
		cfg := latestConfig(state)
		if err := SaveSetting(cfg.DataDir, "housekeeping_last_run", strconv.FormatInt(time.Now().Unix(), 10)); err != nil {
			logging.Debug("cannot record the housekeeping run", "error", err)
		}
		enabled := true
		if value, ok := cfg.Settings["housekeeping_enabled"]; ok {
			enabled = settingTruthy(value)
		}
		if enabled {
			if report, ok := gh0_runHousekeeping(state); ok && report != nil {
				removed := report.OldCyclesRemoved + report.StaleTorrentsRemoved + report.SeenRemoved +
					report.GapLogsRemoved + report.StaleProvidersRemoved
				if removed > 0 {
					logging.Info(fmt.Sprintf("🧹 Routine cleanup: removed %s", countLabel(removed, "old record", "old records")))
				}
				logging.Debug("housekeeping completed",
					"cycles", report.OldCyclesRemoved,
					"torrents", report.StaleTorrentsRemoved,
					"seen", report.SeenRemoved,
					"gap_logs", report.GapLogsRemoved,
					"providers", report.StaleProvidersRemoved,
				)
			}
		}
		if !state.SleepBackground(housekeepingInterval(cfg)) {
			return
		}
	}
}

// housekeepingInterval is `housekeeping_interval_hours` (default 24, 1–720).
func housekeepingInterval(cfg *Config) time.Duration {
	hours := int64(24)
	if value, ok := cfg.Settings["housekeeping_interval_hours"]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			hours = parsed
		}
	}
	if hours < 1 {
		hours = 1
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	return time.Duration(hours) * time.Hour
}

// housekeepingFirstDelay waits at least 10 minutes after the start, and until
// one interval has passed since the last run recorded before the restart.
func housekeepingFirstDelay(cfg *Config, now time.Time) time.Duration {
	delay := 10 * time.Minute
	if last, err := strconv.ParseInt(strings.TrimSpace(cfg.Settings["housekeeping_last_run"]), 10, 64); err == nil && last > 0 {
		if due := time.Unix(last, 0).Add(housekeepingInterval(cfg)).Sub(now); due > delay {
			delay = due
		}
	}
	return delay
}

// ---------------------------------------------------------------------------
// watched_folders_worker
// ---------------------------------------------------------------------------

type bg_watchedSignature struct {
	length   uint64
	modified uint64
}

type bg_watchedKey struct {
	path     string
	length   uint64
	modified uint64
}

type bg_watchedFailure struct {
	attempts  int
	nextRetry time.Time
	signature bg_watchedSignature
}

// watchedFoldersWorker polls the configured watched folders and adds dropped
// `.torrent`/`.magnet` files, mirroring qBittorrent's "watched folders". Files
// that cannot be added are retried indefinitely with a bounded exponential
// backoff.
func watchedFoldersWorker(state *AppState) {
	const period = 15 * time.Second
	processed := map[bg_watchedKey]struct{}{}
	oversizedWatched := map[string]struct{}{}
	observed := map[string]bg_watchedSignature{}
	failures := map[string]bg_watchedFailure{}
	for {
		if !state.SleepBackground(period) {
			return
		}
		now := time.Now()
		cfg := latestConfig(state)
		if cfg.DryRun {
			continue
		}
		folders := LoadWatchedFolders(cfg.Settings)
		for _, folder := range folders {
			if !folder.Enabled {
				continue
			}
			for _, path := range ScanFolder(folder) {
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				pathKey := path
				// A .torrent/.magnet is a few kilobytes: never load an oversized
				// file dropped by mistake into memory.
				if info.Size() > maxWatchedFileBytes {
					if _, warned := oversizedWatched[pathKey]; !warned {
						oversizedWatched[pathKey] = struct{}{}
						logging.Warn("watched folder file too large; ignored",
							"path", pathKey, "size", info.Size(), "max", maxWatchedFileBytes)
					}
					continue
				}
				length := uint64(info.Size())
				modified := uint64(0)
				if nanos := info.ModTime().UnixNano(); nanos > 0 {
					modified = uint64(nanos)
				}
				signature := bg_watchedSignature{length: length, modified: modified}
				// A producer may copy a .torrent/.magnet directly into the watched
				// directory. Require two identical observations so we never parse a
				// half-written file.
				if previous, ok := observed[pathKey]; !ok || previous != signature {
					observed[pathKey] = signature
					continue
				}
				if failure, ok := failures[pathKey]; ok {
					if failure.signature != signature {
						delete(failures, pathKey)
					} else if now.Before(failure.nextRetry) {
						continue
					}
				}
				key := bg_watchedKey{path: pathKey, length: length, modified: modified}
				if _, ok := processed[key]; ok {
					continue
				}
				var result bool
				var addErr error
				if strings.HasSuffix(strings.ToLower(pathKey), ".magnet") {
					if magnet := MagnetFromFile(path); magnet != nil {
						result, addErr = state.activeEngine().AddWithPath(*magnet, cfg, nil)
					} else {
						addErr = fmt.Errorf("no magnet URI in %s", path)
					}
				} else {
					result, addErr = state.activeEngine().AddFileWithPath(path, cfg, nil)
				}
				if addErr != nil {
					attempts := 0
					if failure, ok := failures[pathKey]; ok {
						attempts = failure.attempts
					}
					attempts++
					shift := attempts - 1
					if shift > 7 {
						shift = 7
					}
					if shift < 0 {
						shift = 0
					}
					backoff := time.Duration(uint64(15)*(uint64(1)<<uint(shift))) * time.Second
					if backoff > 30*time.Minute {
						backoff = 30 * time.Minute
					}
					failures[pathKey] = bg_watchedFailure{
						attempts:  attempts,
						nextRetry: now.Add(backoff),
						signature: signature,
					}
					logging.Warn("watched folder: could not add torrent; retry scheduled",
						"file", path, "error", addErr, "attempts", attempts, "retry_in_secs", uint64(backoff/time.Second))
					continue
				}
				if !result {
					// Session disabled or dry-run: keep the file for later.
					continue
				}
				delete(failures, pathKey)
				processed[key] = struct{}{}
				if folder.DeleteAfter {
					if err := os.Remove(path); err != nil {
						logging.Warn("watched folder: could not remove imported file", "file", path, "error", err)
					}
				} else if err := Consume(path, false); err != nil {
					logging.Warn("watched folder: could not mark imported file", "file", path, "error", err)
				}
				logging.Info("📥 watched folder: torrent added", "file", path, "folder", folder.Path)
			}
		}
		if len(processed) > 4096 {
			processed = map[bg_watchedKey]struct{}{}
		}
		if len(failures) > 4096 {
			failures = map[string]bg_watchedFailure{}
		}
		if len(observed) > 4096 {
			observed = map[string]bg_watchedSignature{}
		}
	}
}

// ---------------------------------------------------------------------------
// cycle_worker
// ---------------------------------------------------------------------------

// cycleWorker runs the scheduled scrape/cycle loop and, every
// `rename_verify_interval` hours, performs the archive rename repair.
func formatScheduledCycleTime(now, due time.Time) string {
	location := due.Location()
	now = now.In(location)
	if now.Year() == due.Year() && now.Month() == due.Month() && now.Day() == due.Day() {
		return "at " + due.Format("15:04")
	}
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, location)
	if due.Year() == tomorrow.Year() && due.Month() == tomorrow.Month() && due.Day() == tomorrow.Day() {
		return "tomorrow at " + due.Format("15:04")
	}
	return "on " + due.Format("02/01 at 15:04")
}

func cycleWorker(state *AppState) {
	// Cancelled on shutdown so a cycle in progress releases the torrent engine
	// before ShutdownEmbedded destroys the native session.
	ctx, cancel := state.BackgroundContext()
	defer cancel()
	lastRenameCheck := time.Now().Add(-6 * time.Hour)
	// Guards against overlapping archive repairs (they can be slow on NFS).
	var renameRepairRunning atomic.Bool
	// lastRenameAllGood is when the "all episodes correctly named" line was
	// last logged (unix seconds): at most once a day when nothing changes.
	var lastRenameAllGood atomic.Int64
	var lastInactiveLog *time.Time
	firstRun := true
	for {
		loaded, err := LoadConfig(state.config_path)
		if err != nil {
			logging.Error("scheduled config reload failed", "error", err)
			if !state.SleepBackground(60 * time.Second) {
				return
			}
			continue
		}
		cfg := loaded
		if state.cfg.DryRun {
			cfg.DryRun = true
		}
		refresh := cfg.RefreshSecs
		if refresh < 1 {
			refresh = 1
		}
		if firstRun {
			firstRun = false
			// Resume the persisted schedule: a restart must not trigger an
			// immediate full scrape when the previous cycle is still within the
			// configured interval.
			if cfg.Active {
				if lastAt, ok := state.db.LastCycleAt(); ok {
					due := lastAt.Add(durationFromSeconds(refresh))
					if remaining := time.Until(due); remaining > 0 {
						logging.Info(fmt.Sprintf("⏰ Next search %s (in %s; searches run every %s)",
							formatScheduledCycleTime(time.Now(), due.Local()), logDuration(remaining),
							logDuration(durationFromSeconds(refresh))))
						for {
							remaining = time.Until(due)
							if remaining <= 0 {
								break
							}
							chunk := 60 * time.Second
							if remaining < chunk {
								chunk = remaining
							}
							if !state.SleepBackground(chunk) {
								return
							}
							if reloaded, err := LoadConfig(state.config_path); err == nil && !reloaded.Active {
								// Carry the reloaded inactive state out of the delay. Without
								// this assignment the stale `cfg.Active` below could still
								// launch exactly one unwanted scheduled cycle.
								cfg = reloaded
								if state.cfg.DryRun {
									cfg.DryRun = true
								}
								break
							}
						}
					}
				}
			}
		}
		if cfg.Active && !state.stopping() {
			state.cycle_lock.Lock()
			// Publish the start time immediately (see the manual path).
			startedAt := time.Now().UTC()
			state.last_cycle.Set(models.CycleStats{LastStartedAt: &startedAt})
			notifier := FromConfig(&cfg).Async()
			stats, runErr := RunCycle(
				ctx,
				&cfg,
				state.engine,
				state.db,
				state.archive,
				state.comics,
				notifier,
				state.activeEngine(),
			)
			if runErr != nil {
				logging.Error("scheduled cycle failed", "error", runErr)
			} else if stats != nil {
				state.last_cycle.Set(*stats)
				// The scrape/evaluation peak can leave freed C++ arena and Go
				// heap pages resident; return them before the long idle window.
				TrimMemory()
			}
			renameHours := uint64(6)
			if value, ok := cfg.Settings["rename_verify_interval"]; ok {
				if parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64); err == nil {
					renameHours = parsed
				}
			}
			if renameHours > 0 && time.Since(lastRenameCheck) >= durationFromHours(renameHours) {
				lastRenameCheck = time.Now()
				if cfg.RenameEpisodes {
					// The archive lives on a (possibly slow) NFS mount, so run the
					// repair on a dedicated goroutine: blocking filesystem I/O there
					// cannot stall the daemon.
					if !renameRepairRunning.Swap(true) {
						names := make([]string, 0, len(cfg.Series))
						for _, series := range cfg.Series {
							names = append(names, series.Name)
						}
						done := state.trackOperation()
						go func(names []string) {
							defer recoverGoroutine("archive rename repair")
							defer done()
							// Deferred so a panic cannot leave the repair marked as
							// running forever.
							defer renameRepairRunning.Store(false)
							var renamed, discarded, duplicates, errs, correct int64
							for _, name := range names {
								_, value := seriesRenameApply(state, name, true, false, false)
								values, ok := value.(map[string]any)
								if !ok {
									continue
								}
								renamed += bg_countKey(values, "renamed_count")
								discarded += bg_countKey(values, "discarded_count")
								duplicates += bg_countKey(values, "duplicates_removed")
								errs += bg_countKey(values, "error_count")
								correct += bg_countKey(values, "already_ok_count")
							}
							if renamed+discarded+duplicates+errs > 0 {
								logging.Info(renameRepairReportText(len(names), correct, renamed, discarded, duplicates, errs))
								lastRenameAllGood.Store(0)
							} else if now := time.Now().Unix(); now-lastRenameAllGood.Load() >= 24*3600 {
								// Nothing to do is still worth one line a day: it shows
								// the library is being checked, not ignored.
								logging.Info(renameRepairReportText(len(names), correct, 0, 0, 0, 0))
								lastRenameAllGood.Store(now)
							} else {
								logging.Debug(fmt.Sprintf("🗂 Archive rename repair: %d series checked, nothing to change", len(names)))
							}
						}(names)
					}
				} else if !cfg.DebugEnabled() {
					// The check below stats every archived file (often on NFS) only
					// to print a debug line: skip it unless diagnostics are on.
				} else if files, err := state.db.ArchivedEpisodeFiles(); err == nil {
					missing := 0
					for _, file := range files {
						if _, statErr := os.Stat(file[1]); statErr != nil {
							missing++
						}
					}
					logging.Debug("rename verify: rename disabled", "missing", missing, "total", len(files))
				}
			}
			state.cycle_lock.Unlock()
		} else if lastInactiveLog == nil || time.Since(*lastInactiveLog) >= 900*time.Second {
			// Avoid an empty log when the daemon is not active or is in dry-run.
			logging.Info("automatic cycle paused: daemon not active (enable active mode to resume)",
				"dry_run", cfg.DryRun, "refresh_secs", cfg.RefreshSecs)
			stamp := time.Now()
			lastInactiveLog = &stamp
		}
		if !state.SleepBackground(durationFromSeconds(refresh)) {
			return
		}
	}
}

// completionRetry tracks the transient failures of one torrent's completion.
type completionRetry struct {
	attempts int
	next     time.Time
}

// completionRetryMaxAttempts bounds the automatic retries of a completion that
// fails with a transient error; afterwards the failure becomes terminal.
const completionRetryMaxAttempts = 6

// completionRetryDelay is the wait before attempt n+1: 1, 2, 4, 8, 16, 32 minutes.
func completionRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<(attempt-1)) * time.Minute
}

// isTransientCompletionError reports errors that can disappear on their own:
// an unavailable or slow volume, a timeout, a momentarily full disk or a busy
// database. Identity mismatches and missing sources stay terminal.
func isTransientCompletionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	for _, errno := range []syscall.Errno{
		syscall.EIO, syscall.ENOSPC, syscall.EBUSY, syscall.EAGAIN, syscall.ETIMEDOUT,
		syscall.ESTALE, syscall.ENOTCONN, syscall.EHOSTDOWN, syscall.EHOSTUNREACH,
		syscall.ENETDOWN, syscall.ENETUNREACH, syscall.ECONNRESET, syscall.ECONNREFUSED,
		syscall.EINTR, syscall.EDQUOT,
	} {
		if errors.Is(err, errno) {
			return true
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "sqlite_busy")
}

var (
	mediaRefreshMu      sync.Mutex
	mediaRefreshRunning bool
	mediaRefreshPending *mediaRefreshRequest
)

// mediaRefreshRequest collects the folders to rescan; full asks for a whole
// library refresh (a completion whose folder is not known).
type mediaRefreshRequest struct {
	cfg     *Config
	folders []string
	full    bool
}

func (r *mediaRefreshRequest) add(cfg *Config, folder string) {
	r.cfg = cfg
	if folder == "" {
		r.full = true
		return
	}
	r.folders = append(r.folders, folder)
}

func (r *mediaRefreshRequest) run() {
	if r.full {
		RefreshMediaLibraries(r.cfg)
		return
	}
	RefreshMediaLibraries(r.cfg, r.folders...)
}

// requestMediaLibraryRefresh asks Jellyfin/Plex to rescan folder (the whole
// library when folder is "") without blocking the torrent event worker (each
// request can take up to 20 seconds). Requests that arrive while a refresh
// runs are coalesced into a single follow-up refresh, so a burst of
// completions (a season pack) does not queue one scan per file.
func requestMediaLibraryRefresh(cfg *Config, folder string) {
	mediaRefreshMu.Lock()
	if mediaRefreshRunning {
		if mediaRefreshPending == nil {
			mediaRefreshPending = &mediaRefreshRequest{}
		}
		mediaRefreshPending.add(cfg, folder)
		mediaRefreshMu.Unlock()
		return
	}
	mediaRefreshRunning = true
	mediaRefreshMu.Unlock()
	first := &mediaRefreshRequest{}
	first.add(cfg, folder)
	go func(current *mediaRefreshRequest) {
		defer func() {
			if r := recover(); r != nil {
				logging.Error("media library refresh panicked; recovered", "panic", fmt.Sprint(r))
				mediaRefreshMu.Lock()
				mediaRefreshRunning = false
				mediaRefreshPending = nil
				mediaRefreshMu.Unlock()
			}
		}()
		for {
			current.run()
			// Clear the running flag under the same lock that checks for a
			// pending request, so a request arriving now is never lost.
			mediaRefreshMu.Lock()
			if mediaRefreshPending == nil {
				mediaRefreshRunning = false
				mediaRefreshMu.Unlock()
				return
			}
			current = mediaRefreshPending
			mediaRefreshPending = nil
			mediaRefreshMu.Unlock()
		}
	}(first)
}

// settledTorrent remembers a completed torrent that needs no recovery.
type settledTorrent struct {
	savePath string
	until    time.Time
}

// settledRecheckInterval is how long a settled torrent is skipped by the
// completion-recovery scan. A storage move (new save path) or any event for the
// torrent ends the skip at once.
const settledRecheckInterval = 60 * time.Second

// maxWatchedFileBytes bounds the .torrent/.magnet files read from watched
// folders (large multi-file torrents stay well below this).
const maxWatchedFileBytes = 32 << 20
