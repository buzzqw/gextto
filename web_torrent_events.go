package gextto

// web_torrent_events.go ports the torrent lifecycle machinery of
// `the reference daemon`: the metadata/stall monitors, the storage-move
// retry loop, the RAM-disk reconciliation, the seed/cleanup policy, the
// background detachment sweeps and the actual torrent-event handler.
//
// Exported entry points keep the CamelCase names the sibling group files call
// them by; every private helper carries the unique `tev_` prefix.

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// ---------------------------------------------------------------------------
// helper structs
// ---------------------------------------------------------------------------

// StallWatch tracks one download's progress timer (implementation of `StallWatch`).
type StallWatch struct {
	// lastProgressAt is the last time the completed byte count increased. A
	// connected leecher is not proof of progress: it can stay online forever
	// while owning no piece that is still needed.
	lastProgressAt time.Time
	lastDone       int64
	// stalledSince, once set, means the torrent is stalled but retained in the
	// session; it is periodically reannounced instead of occupying an active
	// download slot.
	stalledSince *time.Time
	nextRetryAt  time.Time
}

// StorageMoveRetry is one pending/ongoing asynchronous storage move (implementation of
// `StorageMoveRetry`).
type StorageMoveRetry struct {
	destination string
	postSeed    bool
	attempts    uint8
	nextAttempt time.Time
	inFlight    bool
}

const tev_maxStorageMoveRetries uint8 = 5

// ---------------------------------------------------------------------------
// small scalar helpers
// ---------------------------------------------------------------------------

func tev_optionalInt64Equal(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func tev_saturatingMulInt64(value, factor int64) int64 {
	if value == 0 || factor == 0 {
		return 0
	}
	if factor > 0 {
		if value > 0 {
			if value > math.MaxInt64/factor {
				return math.MaxInt64
			}
			return value * factor
		}
		if value < math.MinInt64/factor {
			return math.MinInt64
		}
		return value * factor
	}
	// factor < 0 is not used by the implemented call sites; fall back to a plain
	// multiplication guarded against the simple overflow cases.
	if value > 0 {
		if factor < math.MinInt64/value {
			return math.MinInt64
		}
		return value * factor
	}
	if value < math.MaxInt64/factor {
		return math.MaxInt64
	}
	return value * factor
}

func tev_saturatingAddU8(value uint8, delta uint8) uint8 {
	if value > 255-delta {
		return 255
	}
	return value + delta
}

func tev_settingFloatOr(cfg *Config, key string, fallback float64) float64 {
	if value, ok := cfg.Settings[key]; ok {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

// ---------------------------------------------------------------------------
// torrent_display_name / remove_failed_torrent
// ---------------------------------------------------------------------------

// tev_torrentDisplayName implements `torrent_display_name`.
func tev_torrentDisplayName(torrents TorrentSession, hash string) string {
	for _, torrent := range torrents.List() {
		if strings.EqualFold(torrent.Hash, hash) {
			if strings.TrimSpace(torrent.Name) != "" {
				return torrent.Name
			}
			break
		}
	}
	return "unnamed torrent"
}

// tev_removeFailedTorrent implements `remove_failed_torrent`: partial files are
// deleted only when the download was incomplete; a completed/seeding torrent's
// library is left untouched.
func tev_removeFailedTorrent(torrents TorrentSession, hash string) bool {
	name := tev_torrentDisplayName(torrents, hash)
	incomplete := false
	for _, torrent := range torrents.List() {
		if strings.EqualFold(torrent.Hash, hash) {
			incomplete = torrent.Progress < 99.99 && torrent.State != "seeding" && torrent.State != "finished"
			break
		}
	}
	removed, err := torrents.Remove(hash, incomplete)
	if err != nil {
		logging.Warn("failed torrent removal failed", "hash", hash, "name", name, "error", err.Error())
		return false
	}
	if removed {
		logging.Info("failed download removed from the session", "name", name)
	} else {
		logging.Debug("failed torrent already removed", "hash", hash, "name", name)
	}
	return true
}

// ---------------------------------------------------------------------------
// refresh_media_libraries
// ---------------------------------------------------------------------------

// RefreshMediaLibraries implements `refresh_media_libraries`.
func RefreshMediaLibraries(cfg *Config) {
	jellyfinURL := strings.TrimSpace(cfg.Settings["jellyfin_url"])
	jellyfinKey := strings.TrimSpace(cfg.Settings["jellyfin_api_key"])
	if jellyfinURL != "" && jellyfinKey != "" {
		endpoint := strings.TrimRight(jellyfinURL, "/") + "/Library/Refresh"
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		headers := map[string]string{
			"Authorization": fmt.Sprintf("MediaBrowser Token=\"%s\"", jellyfinKey),
			"X-Emby-Token":  jellyfinKey,
		}
		response, err := HTTPRequest(ctx, http.MethodPost, endpoint, headers, nil, "")
		if err != nil {
			logging.Warn("Jellyfin library refresh failed", "error", utils.RedactURLSecrets(err.Error()))
		} else {
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				logging.Debug("Jellyfin library refresh requested")
			} else {
				logging.Warn("Jellyfin library refresh failed", "status", response.Status)
			}
			response.Body.Close()
		}
		cancel()
	}
	plexURL := strings.TrimSpace(cfg.Settings["plex_url"])
	plexToken := strings.TrimSpace(cfg.Settings["plex_token"])
	if plexURL != "" && plexToken != "" {
		endpoint := strings.TrimRight(plexURL, "/") + "/library/sections/all/refresh"
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		response, err := HTTPGet(ctx, endpoint, map[string]string{"X-Plex-Token": plexToken})
		if err != nil {
			logging.Warn("Plex library refresh failed", "error", utils.RedactURLSecrets(err.Error()))
		} else {
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				logging.Info("Plex library refresh requested")
			} else {
				logging.Warn("Plex library refresh failed", "status", response.Status)
			}
			response.Body.Close()
		}
		cancel()
	}
}

// ---------------------------------------------------------------------------
// mismatched season packs
// ---------------------------------------------------------------------------

// tev_mismatchedPackRelease implements `mismatched_pack_release`.
func tev_mismatchedPackRelease(torrents TorrentSession, db *Database, hash string) *models.Release {
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return nil
	}
	release := meta.Release
	if release.Kind != "series" || !release.IsPack {
		return nil
	}
	name := tev_torrentDisplayName(torrents, hash)
	corrected := ReconcilePackIdentity(&release, name)
	if corrected == nil {
		return nil
	}
	if tev_optionalInt64Equal(corrected.Season, release.Season) {
		return nil
	}
	result := release
	return &result
}

// RejectActivePackIdentityMismatches implements `reject_active_pack_identity_mismatches`.
func RejectActivePackIdentityMismatches(torrents TorrentSession, db *Database) {
	for _, torrent := range torrents.List() {
		release := tev_mismatchedPackRelease(torrents, db, torrent.Hash)
		if release == nil {
			continue
		}
		alreadyBlocked, _ := db.IsBlocklisted(torrent.Hash)
		if alreadyBlocked {
			_, _ = torrents.Remove(torrent.Hash, true)
			continue
		}
		errorMessage := "torrent metadata season does not match declared season"
		_ = db.MarkTorrentError(torrent.Hash, errorMessage)
		_ = db.Blocklist(release, "season_pack_identity_mismatch")
		_ = db.ForgetRemovedTorrent(torrent.Hash)
		removed, err := torrents.Remove(torrent.Hash, true)
		if err != nil {
			logging.Warn("could not remove mismatched season pack", "hash", torrent.Hash, "error", err.Error())
		} else if removed {
			logging.Warn("mismatched season pack removed before completion", "hash", torrent.Hash, "name", torrent.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// storage-move retry machinery
// ---------------------------------------------------------------------------

// tev_storageMoveBackoff implements `storage_move_backoff`.
func tev_storageMoveBackoff(attempt uint8) time.Duration {
	switch attempt {
	case 0, 1:
		return 5 * time.Second
	case 2:
		return 15 * time.Second
	case 3:
		return 60 * time.Second
	case 4:
		return 300 * time.Second
	default:
		return 3600 * time.Second
	}
}

// tev_scheduleStorageMoveRetry implements `schedule_storage_move_retry`.
func tev_scheduleStorageMoveRetry(retries map[string]StorageMoveRetry, hash, destination string, postSeed bool, now time.Time) {
	key := strings.ToLower(hash)
	entry, ok := retries[key]
	if !ok {
		entry = StorageMoveRetry{
			destination: destination,
			postSeed:    postSeed,
			nextAttempt: now,
		}
	}
	entry.destination = destination
	entry.postSeed = postSeed
	entry.inFlight = false
	entry.attempts = tev_saturatingAddU8(entry.attempts, 1)
	entry.nextAttempt = now.Add(tev_storageMoveBackoff(entry.attempts))
	retries[key] = entry
}

// RetryStorageMoves implements `retry_storage_moves`.
func RetryStorageMoves(torrents TorrentSession, moveRequests map[string]struct{}, postSeedMoves map[string]struct{}, retries map[string]StorageMoveRetry) {
	now := time.Now()
	hashes := make([]string, 0, len(retries))
	for hash := range retries {
		hashes = append(hashes, hash)
	}
	for _, hash := range hashes {
		retry, ok := retries[hash]
		if !ok {
			continue
		}
		name := tev_torrentDisplayName(torrents, hash)
		if retry.inFlight && now.Before(retry.nextAttempt) {
			continue
		}
		if retry.inFlight {
			logging.Debug("storage move still in flight after wait; checking the move again",
				"hash", hash, "name", name, "destination", retry.destination)
			if entry, ok := retries[hash]; ok {
				entry.inFlight = false
				entry.attempts = tev_saturatingAddU8(entry.attempts, 1)
				entry.nextAttempt = now
				retries[hash] = entry
			}
			delete(moveRequests, hash)
		}
		if retry.attempts >= tev_maxStorageMoveRetries {
			if entry, ok := retries[hash]; ok {
				entry.attempts = 0
				entry.nextAttempt = now.Add(tev_storageMoveBackoff(tev_maxStorageMoveRetries))
				retries[hash] = entry
			}
			logging.Warn("storage move retry limit reached; cooling down before another attempt",
				"hash", hash, "name", name, "destination", retry.destination)
			continue
		}
		var torrent models.TorrentView
		found := false
		for _, candidate := range torrents.List() {
			if strings.EqualFold(candidate.Hash, hash) {
				torrent = candidate
				found = true
				break
			}
		}
		if !found {
			delete(retries, hash)
			delete(postSeedMoves, hash)
			delete(moveRequests, hash)
			continue
		}
		if SamePath(torrent.SavePath, retry.destination) {
			delete(retries, hash)
			delete(postSeedMoves, hash)
			delete(moveRequests, hash)
			continue
		}
		tev_clearEmptyDestination(retry.destination, torrent.Name)
		if err := ValidateDestinationFrom(torrent.SavePath, retry.destination); err != nil {
			tev_scheduleStorageMoveRetry(retries, hash, retry.destination, retry.postSeed, now)
			continue
		}
		if _, exists := moveRequests[hash]; exists {
			continue
		}
		moveRequests[hash] = struct{}{}
		destination := retry.destination
		postSeed := retry.postSeed
		moved, err := torrents.MoveStorage(hash, destination)
		if err != nil {
			delete(moveRequests, hash)
			logging.Warn("storage move retry failed synchronously",
				"hash", hash, "name", torrent.Name, "destination", destination, "error", err.Error())
			tev_scheduleStorageMoveRetry(retries, hash, destination, postSeed, now)
		} else if moved {
			if entry, ok := retries[hash]; ok {
				entry.inFlight = true
				entry.nextAttempt = now.Add(600 * time.Second)
				retries[hash] = entry
			}
			if postSeed {
				postSeedMoves[hash] = struct{}{}
			}
		} else {
			delete(moveRequests, hash)
			tev_scheduleStorageMoveRetry(retries, hash, destination, postSeed, now)
		}
	}
}

// tev_seedCopyWarningDue implements `seed_copy_warning_due`.
func tev_seedCopyWarningDue(warnings map[string]time.Time, hash string) bool {
	const warningInterval = 600 * time.Second
	const entryRetention = 3600 * time.Second
	now := time.Now()
	for key, last := range warnings {
		if now.Sub(last) >= entryRetention {
			delete(warnings, key)
		}
	}
	key := strings.ToLower(hash)
	due := true
	if last, ok := warnings[key]; ok {
		due = now.Sub(last) >= warningInterval
	}
	if due {
		warnings[key] = now
	}
	return due
}

// ---------------------------------------------------------------------------
// detach sweeps
// ---------------------------------------------------------------------------

// DetachErrorTorrents implements `detach_error_torrents`: it removes torrents
// that were already in the session when the daemon started and are persisted as
// `error` (a leftover from a previous run; files are kept). Torrents added later
// are never detached this way, so re-adding a previously failed torrent works.
func DetachErrorTorrents(torrents TorrentSession, db *Database, startupHashes map[string]struct{}) {
	for _, torrent := range torrents.List() {
		if _, known := startupHashes[strings.ToLower(torrent.Hash)]; !known {
			continue
		}
		status, err := db.TorrentStatus(torrent.Hash)
		isError := err == nil && status != nil && *status == "error"
		if !isError {
			continue
		}
		_, removeErr := torrents.Remove(torrent.Hash, false)
		if removeErr != nil {
			logging.Warn("failed to detach leftover error torrent",
				"hash", torrent.Hash, "name", torrent.Name, "error", removeErr.Error())
			continue
		}
		_ = db.MarkTorrentRemovedAt(torrent.Hash)
		logging.Info("🧹 leftover failed torrent removed after restart (files kept)",
			"hash", torrent.Hash, "name", torrent.Name)
	}
}

// DetachCompletedArchivedSingles implements `detach_completed_archived_singles`.
func DetachCompletedArchivedSingles(torrents TorrentSession, db *Database) {
	for _, torrent := range torrents.List() {
		meta, _ := db.TorrentMeta(torrent.Hash)
		status, _ := db.TorrentStatus(torrent.Hash)
		processed, _ := db.TorrentProcessed(torrent.Hash)
		completed := status != nil && *status == "completed"
		isPack := meta != nil && meta.Release.IsPack
		if !completed || isPack {
			continue
		}
		if processed == nil || strings.TrimSpace(*processed) == "" {
			continue
		}
		processedPath := *processed
		source := CompletionPath(&models.TorrentEvent{
			Kind:     "torrent_finished",
			Hash:     torrent.Hash,
			Name:     torrent.Name,
			SavePath: torrent.SavePath,
		})
		_, processedErr := os.Stat(processedPath)
		_, sourceErr := os.Stat(source)
		if processedErr != nil || sourceErr == nil {
			continue
		}
		_, err := torrents.Remove(torrent.Hash, false)
		if err != nil {
			logging.Warn("failed to detach completed single after recovery",
				"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			continue
		}
		_ = db.MarkTorrentRemovedAt(torrent.Hash)
		logging.Info("detached completed single whose archived source is already gone",
			"hash", torrent.Hash, "name", torrent.Name)
	}
}

// ---------------------------------------------------------------------------
// monitor_stalled
// ---------------------------------------------------------------------------

// tev_configuredStallGiveupMinutes implements `configured_stall_giveup_minutes`.
func tev_configuredStallGiveupMinutes(cfg *Config) float64 {
	if value, ok := cfg.Settings["libtorrent_stall_giveup_min"]; ok {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return parsed
		}
	}
	if value, ok := cfg.Settings["libtorrent_stall_timeout_min"]; ok {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			if parsed > 0 {
				if parsed > 10080 {
					return parsed
				}
				return 10080
			}
			return 0
		}
	}
	return 20160.0
}

// tev_stallExpired updates a download's progress timer and reports whether it
// has stalled (implementation of `stall_expired`). Peer presence and the instantaneous
// rate are deliberately ignored.
func tev_stallExpired(lastProgressAt *time.Time, lastDone *int64, now time.Time, done int64, timeout time.Duration) bool {
	if done > *lastDone {
		*lastProgressAt = now
		*lastDone = done
	}
	return now.Sub(*lastProgressAt) >= timeout
}

// MonitorStalled implements `monitor_stalled`.
func MonitorStalled(cfg *Config, torrents TorrentSession, db *Database, notifier *Notifier, watch map[string]StallWatch) {
	stallAfterMinutes := tev_settingFloatOr(cfg, "libtorrent_stall_after_min", 60.0)
	retryMinutes := tev_settingFloatOr(cfg, "libtorrent_stall_retry_min", 60.0)
	giveupMinutes := tev_configuredStallGiveupMinutes(cfg)
	if stallAfterMinutes <= 0 {
		clear(watch)
		return
	}
	stallTimeout := time.Duration(stallAfterMinutes * 60.0 * float64(time.Second))
	retryValue := retryMinutes
	if retryValue < 1.0 {
		retryValue = 1.0
	}
	retryTimeout := time.Duration(retryValue * 60.0 * float64(time.Second))
	giveupTimeout := time.Duration(0)
	hasGiveup := giveupMinutes > 0
	if hasGiveup {
		giveupTimeout = time.Duration(giveupMinutes * 60.0 * float64(time.Second))
	}
	now := time.Now()
	live := map[string]struct{}{}
	for _, torrent := range torrents.List() {
		live[torrent.Hash] = struct{}{}
		if (torrent.State != "downloading" && torrent.State != "stalled") || torrent.Progress >= 100.0 {
			torrents.ClearStalled(torrent.Hash)
			delete(watch, torrent.Hash)
			continue
		}
		entry, ok := watch[torrent.Hash]
		if !ok {
			entry = StallWatch{
				lastProgressAt: now,
				lastDone:       torrent.TotalDone,
				nextRetryAt:    now,
			}
		}
		progressAt := entry.lastProgressAt
		lastDone := entry.lastDone
		hadProgress := torrent.TotalDone > entry.lastDone
		if !tev_stallExpired(&progressAt, &lastDone, now, torrent.TotalDone, stallTimeout) {
			entry.lastProgressAt = progressAt
			entry.lastDone = lastDone
			if hadProgress {
				entry.stalledSince = nil
			}
			entry.nextRetryAt = now
			watch[torrent.Hash] = entry
			torrents.ClearStalled(torrent.Hash)
			continue
		}
		entry.lastProgressAt = progressAt
		entry.lastDone = lastDone
		firstStall := entry.stalledSince == nil
		if entry.stalledSince == nil {
			stamp := now
			entry.stalledSince = &stamp
		}
		stalledSince := *entry.stalledSince
		if _, err := torrents.MarkStalled(torrent.Hash); err != nil {
			logging.Debug("could not park stalled torrent",
				"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			watch[torrent.Hash] = entry
			continue
		}
		if firstStall {
			code, reason, hint := DiagnoseTorrent(&torrent)
			logging.Warn("⏸️ DOWNLOAD STALLED — excluded from active slots; retaining for periodic retry",
				"hash", torrent.Hash,
				"name", torrent.Name,
				"progress", torrent.Progress,
				"num_peers", torrent.NumPeers,
				"num_seeds", torrent.NumSeeds,
				"swarm_seeds", torrent.NumComplete,
				"swarm_peers", torrent.NumIncomplete,
				"reason", code,
				"detail", reason,
				"hint", hint,
				"stall_after_minutes", stallAfterMinutes,
			)
			entry.nextRetryAt = now.Add(retryTimeout)
		}
		if hasGiveup && now.Sub(stalledSince) >= giveupTimeout {
			failedTitle := ""
			if metadata, err := db.TorrentMeta(torrent.Hash); err == nil && metadata != nil {
				failedTitle = metadata.Release.Title
			}
			restored, _ := db.RestoreUpgrade(torrent.Hash)
			_ = db.MarkTorrentError(torrent.Hash, "stalled download")
			logging.Warn("❌ DOWNLOAD FAILED — stalled beyond the configured retry window",
				"hash", torrent.Hash,
				"name", torrent.Name,
				"title", failedTitle,
				"progress", torrent.Progress,
				"giveup_minutes", giveupMinutes,
			)
			if tev_removeFailedTorrent(torrents, torrent.Hash) {
				_ = db.MarkTorrentRemovedAt(torrent.Hash)
			}
			_ = notifier.NotifyEvent("download_failed", map[string]any{
				"hash":             torrent.Hash,
				"title":            failedTitle,
				"error":            "stalled download",
				"upgrade_restored": restored,
			})
			delete(watch, torrent.Hash)
			torrents.ClearStalled(torrent.Hash)
			continue
		}
		if !now.Before(entry.nextRetryAt) {
			restarted := false
			value, err := torrents.Restart(torrent.Hash)
			if err != nil {
				logging.Debug("stalled torrent restart failed",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			} else if value {
				logging.Info("🔁 stalled torrent resumed and reannounced",
					"hash", torrent.Hash, "name", torrent.Name, "retry_minutes", retryMinutes)
				restarted = true
			} else {
				logging.Debug("stalled torrent restart unavailable in current mode",
					"hash", torrent.Hash, "name", torrent.Name)
			}
			if restarted {
				entry.lastProgressAt = now
				entry.lastDone = torrent.TotalDone
				entry.nextRetryAt = now.Add(retryTimeout)
			} else {
				entry.nextRetryAt = now.Add(15 * time.Second)
			}
		}
		watch[torrent.Hash] = entry
	}
	for hash := range watch {
		if _, ok := live[hash]; !ok {
			delete(watch, hash)
		}
	}
}

// ---------------------------------------------------------------------------
// monitor_metadata
// ---------------------------------------------------------------------------

// MonitorMetadata implements `monitor_metadata`.
func MonitorMetadata(cfg *Config, torrents TorrentSession, db *Database, notifier *Notifier, waitStart, firstSeen map[string]time.Time) {
	const timeout = 600 * time.Second
	giveupMinutes := tev_settingFloatOr(cfg, "libtorrent_metadata_giveup_min", 1440.0)
	if giveupMinutes <= 0 {
		clear(waitStart)
		clear(firstSeen)
		return
	}
	giveup := time.Duration(giveupMinutes * 60.0 * float64(time.Second))
	now := time.Now()
	live := map[string]struct{}{}
	for _, torrent := range torrents.List() {
		live[torrent.Hash] = struct{}{}
		if torrent.HasMetadata {
			delete(waitStart, torrent.Hash)
			delete(firstSeen, torrent.Hash)
			continue
		}
		// A paused magnet is waiting for a download slot and must not age out.
		if torrent.State == "paused" {
			delete(waitStart, torrent.Hash)
			delete(firstSeen, torrent.Hash)
			continue
		}
		if torrent.State != "downloading_metadata" {
			delete(waitStart, torrent.Hash)
			delete(firstSeen, torrent.Hash)
			continue
		}
		started, ok := firstSeen[torrent.Hash]
		if !ok {
			started = now
			firstSeen[torrent.Hash] = now
		}
		retryAt, ok := waitStart[torrent.Hash]
		if !ok {
			retryAt = now
			waitStart[torrent.Hash] = now
		}
		if now.Sub(started) >= giveup {
			metadata, _ := db.TorrentMeta(torrent.Hash)
			if metadata != nil {
				_ = db.Blocklist(&metadata.Release, "dead_magnet")
			}
			restored, _ := db.RestoreUpgrade(torrent.Hash)
			_ = db.MarkTorrentError(torrent.Hash, "metadata timeout")
			logging.Warn("❌ DOWNLOAD FAILED — no metadata (dead magnet) within the give-up window",
				"hash", torrent.Hash, "name", torrent.Name, "giveup_minutes", giveupMinutes)
			if tev_removeFailedTorrent(torrents, torrent.Hash) {
				_ = db.MarkTorrentRemovedAt(torrent.Hash)
			}
			_ = notifier.NotifyEvent("torrent_error", map[string]any{
				"hash":             torrent.Hash,
				"error":            "metadata timeout",
				"upgrade_restored": restored,
			})
			delete(waitStart, torrent.Hash)
			delete(firstSeen, torrent.Hash)
		} else if now.Sub(retryAt) >= timeout {
			value, err := torrents.Reannounce(torrent.Hash)
			if err != nil {
				logging.Debug("metadata reannounce failed",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			} else if value {
				logging.Warn("torrent metadata still unavailable; reannouncing",
					"hash", torrent.Hash, "name", torrent.Name)
			} else {
				logging.Debug("metadata reannounce unavailable in current mode",
					"hash", torrent.Hash, "name", torrent.Name)
			}
			waitStart[torrent.Hash] = now
		}
	}
	for hash := range waitStart {
		if _, ok := live[hash]; !ok {
			delete(waitStart, hash)
		}
	}
	for hash := range firstSeen {
		if _, ok := live[hash]; !ok {
			delete(firstSeen, hash)
		}
	}
}

// ---------------------------------------------------------------------------
// seed-limit helpers
// ---------------------------------------------------------------------------

// tev_needsInfiniteSeedResume implements `needs_infinite_seed_resume`.
func tev_needsInfiniteSeedResume(torrent *models.TorrentView) bool {
	return (torrent.SeedRatio == 0.0 || torrent.SeedDays == 0) &&
		torrent.Progress >= 100.0 &&
		torrent.State == "paused" &&
		!torrent.AutoManaged
}

// tev_effectiveDownloadForRatio implements `effective_download_for_ratio`.
func tev_effectiveDownloadForRatio(torrent *models.TorrentView) float64 {
	if torrent.AllTimeDownload > 0 {
		return float64(torrent.AllTimeDownload)
	}
	totalSize := torrent.TotalSize
	if totalSize < 0 {
		totalSize = 0
	}
	return float64(totalSize)
}

// tev_seedLimitsReached implements `seed_limits_reached`.
func tev_seedLimitsReached(cfg *Config, torrent *models.TorrentView) (bool, bool) {
	globalRatio := 0.0
	globalRatioSet := false
	if cfg.Libtorrent.StopAtRatio && cfg.Libtorrent.SeedRatio > 0.0 {
		globalRatio = cfg.Libtorrent.SeedRatio
		globalRatioSet = true
	}
	globalTime := int64(0)
	globalTimeSet := false
	if cfg.Libtorrent.SeedTimeDays > 0 {
		globalTime = tev_saturatingMulInt64(cfg.Libtorrent.SeedTimeDays, 86_400)
		globalTimeSet = true
	} else if cfg.Libtorrent.SeedTimeMinutes > 0 {
		globalTime = tev_saturatingMulInt64(cfg.Libtorrent.SeedTimeMinutes, 60)
		globalTimeSet = true
	}
	ratioLimit := 0.0
	ratioLimitSet := false
	if torrent.SeedRatio > 0.0 {
		ratioLimit = torrent.SeedRatio
		ratioLimitSet = true
	} else if torrent.SeedRatio < 0.0 {
		ratioLimit = globalRatio
		ratioLimitSet = globalRatioSet
	}
	timeLimit := int64(0)
	timeLimitSet := false
	if torrent.SeedDays > 0 {
		timeLimit = tev_saturatingMulInt64(torrent.SeedDays, 86_400)
		timeLimitSet = true
	} else if torrent.SeedDays < 0 {
		timeLimit = globalTime
		timeLimitSet = globalTimeSet
	}
	ratioReached := false
	if ratioLimitSet {
		download := tev_effectiveDownloadForRatio(torrent)
		ratioReached = download > 0.0 && (float64(torrent.AllTimeUpload)/download) >= ratioLimit
	}
	timeReached := timeLimitSet && torrent.SeedingSeconds >= timeLimit
	return ratioReached, timeReached
}

// tev_archivedPackSourceDisposable implements `archived_pack_source_disposable`.
func tev_archivedPackSourceDisposable(db *Database, hash, savePath string) bool {
	meta, _ := db.TorrentMeta(hash)
	status, _ := db.TorrentStatus(hash)
	processed, _ := db.TorrentProcessed(hash)
	isPack := meta != nil && meta.Release.IsPack
	if !isPack || status == nil || *status != "completed" {
		return false
	}
	if processed == nil || strings.TrimSpace(*processed) == "" {
		return false
	}
	return !pathStartsWith(*processed, savePath)
}

// tev_completedSourceDisposable implements `completed_source_disposable`.
func tev_completedSourceDisposable(db *Database, hash, savePath string) bool {
	status, _ := db.TorrentStatus(hash)
	processed, _ := db.TorrentProcessed(hash)
	if processed == nil || strings.TrimSpace(*processed) == "" {
		return false
	}
	processedPath := *processed
	outside := false
	if processedRoot, err := canonicalizePath(processedPath); err == nil {
		if saveRoot, err := canonicalizePath(savePath); err == nil {
			outside = !pathStartsWith(processedRoot, saveRoot)
		}
	}
	_, statErr := os.Stat(processedPath)
	return statErr == nil && outside && status != nil && *status == "completed"
}

// ---------------------------------------------------------------------------
// remove_seeded_completed
// ---------------------------------------------------------------------------

// RemoveSeededCompleted implements `remove_seeded_completed`.
func RemoveSeededCompleted(cfg *Config, torrents TorrentSession, db *Database, postSeedMoves map[string]struct{}) {
	for _, torrent := range torrents.List() {
		if _, ok := postSeedMoves[torrent.Hash]; ok {
			continue
		}
		// Explicit infinite seeding: the torrent must remain seeding.
		if torrent.SeedRatio == 0.0 || torrent.SeedDays == 0 {
			continue
		}
		// Real completeness, not just a momentary libtorrent state: a torrent
		// that was relocated mid-download can briefly report "finished" while
		// its file is still incomplete.
		completed := torrent.Progress >= 99.99 && torrent.TotalSize > 0 && torrent.TotalDone >= torrent.TotalSize
		if !completed {
			continue
		}
		ratioReached, timeReached := tev_seedLimitsReached(cfg, &torrent)
		if !ratioReached && !timeReached {
			continue
		}
		archivedPack := tev_archivedPackSourceDisposable(db, torrent.Hash, torrent.SavePath)
		archivedCopy := tev_completedSourceDisposable(db, torrent.Hash, torrent.SavePath)
		if !archivedCopy || (!archivedPack && !cfg.Libtorrent.AutoRemoveCompleted) {
			continue
		}
		removed, err := torrents.Remove(torrent.Hash, archivedCopy)
		if err != nil {
			logging.Warn("seeded torrent removal failed",
				"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			continue
		}
		if !removed {
			continue
		}
		_ = db.MarkTorrentRemovedAt(torrent.Hash)
		if archivedPack {
			logging.Info(fmt.Sprintf("🗑️ Season pack seeded — source removed, copy kept on NAS: «%s»", torrent.Name))
		} else {
			logging.Info(fmt.Sprintf("🗑️ Seeding done — removed from the session: «%s»", torrent.Name))
		}
	}
}

// ---------------------------------------------------------------------------
// post-seeding relocation / RAM-disk helpers
// ---------------------------------------------------------------------------

// tev_clearEmptyDestination implements `clear_empty_destination`.
func tev_clearEmptyDestination(destination, name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	target := filepath.Join(destination, name)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		return
	}
	if err := os.Remove(target); err != nil {
		logging.Warn("cannot remove empty destination directory", "target", target, "error", err.Error())
	} else {
		logging.Info("removed empty destination directory before storage move", "target", target)
	}
}

// tev_postSeedRelocate implements `post_seed_relocate`.
func tev_postSeedRelocate(cfg *Config, torrents TorrentSession, torrent *models.TorrentView, postSeedMoves map[string]struct{}, storageMoveRetries map[string]StorageMoveRetry) bool {
	if retry, ok := storageMoveRetries[strings.ToLower(torrent.Hash)]; ok && retry.postSeed {
		return true
	}
	current := torrent.SavePath
	inRamdisk := false
	if ramdisk := cfg.RamdiskDir(); ramdisk != nil {
		inRamdisk = PathOnRamdisk(current, *ramdisk)
	}
	inTemp := false
	if cfg.LibtorrentTempDir != nil {
		inTemp = SamePath(current, *cfg.LibtorrentTempDir)
	}
	if !inRamdisk && !inTemp {
		return false
	}
	destination := cfg.LibtorrentDir
	if SamePath(current, destination) {
		return false
	}
	if err := ValidateDestinationFrom(current, destination); err != nil {
		logging.Warn("post-seeding relocation refused",
			"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
		return false
	}
	tev_clearEmptyDestination(destination, torrent.Name)
	moved, err := torrents.MoveStorage(torrent.Hash, destination)
	if err != nil {
		logging.Warn("post-seeding relocation failed",
			"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
		tev_scheduleStorageMoveRetry(storageMoveRetries, torrent.Hash, destination, true, time.Now())
		return false
	}
	if !moved {
		logging.Debug("post-seeding relocation was not applied", "hash", torrent.Hash, "name", torrent.Name)
		return false
	}
	logging.Debug("📁 MOVING TO NAS — post-seeding relocation",
		"hash", torrent.Hash,
		"name", torrent.Name,
		"from", current,
		"to", destination,
		"size", logging.HumanBytesI64(torrent.TotalSize),
	)
	postSeedMoves[torrent.Hash] = struct{}{}
	storageMoveRetries[strings.ToLower(torrent.Hash)] = StorageMoveRetry{
		destination: destination,
		postSeed:    true,
		attempts:    0,
		nextAttempt: time.Now().Add(600 * time.Second),
		inFlight:    true,
	}
	return true
}

// tev_ramdiskRelocation implements `ramdisk_relocation`. It returns the reason and
// destination when a torrent currently on the RAM disk should move to disk.
func tev_ramdiskRelocation(cfg *Config, torrents TorrentSession, hash, savePath string) (string, string, bool) {
	if !cfg.RamdiskEnabled() {
		return "", "", false
	}
	ramdisk := cfg.RamdiskDir()
	if ramdisk == nil {
		return "", "", false
	}
	if !PathOnRamdisk(savePath, *ramdisk) {
		return "", "", false
	}
	var torrent models.TorrentView
	found := false
	for _, candidate := range torrents.List() {
		if strings.EqualFold(candidate.Hash, hash) {
			torrent = candidate
			found = true
			break
		}
	}
	if !found {
		return "", "", false
	}
	free := FreeSpaceBytes(*ramdisk)
	if free == nil {
		return "", "", false
	}
	uncommitted := torrents.RamdiskUncommittedBytes(*ramdisk, hash)
	totalSize := torrent.TotalSize
	if totalSize < 0 {
		totalSize = 0
	}
	// Space is required for what is still to be written, not for the whole
	// torrent: a nearly complete download must not be relocated mid-transfer.
	remaining := torrent.TotalSize - torrent.TotalDone
	if remaining < 0 {
		remaining = 0
	}
	if err := RamdiskFitsRemaining(cfg.RamdiskThresholdBytes(), cfg.RamdiskMarginBytes(), *free, uncommitted, uint64(totalSize), uint64(remaining)); err == nil {
		return "", "", false
	} else {
		reason := err.Error()
		destination := cfg.LibtorrentDir
		if cfg.LibtorrentTempDir != nil {
			destination = *cfg.LibtorrentTempDir
		}
		if SamePath(savePath, destination) {
			return "", "", false
		}
		tev_clearEmptyDestination(destination, torrent.Name)
		return reason, destination, true
	}
}

// tev_enforceRamdiskCapacity implements `enforce_ramdisk_capacity`.
func tev_enforceRamdiskCapacity(cfg *Config, torrents TorrentSession, event *models.TorrentEvent) {
	reason, destination, ok := tev_ramdiskRelocation(cfg, torrents, event.Hash, event.SavePath)
	if !ok {
		return
	}
	moved, err := torrents.MoveStorage(event.Hash, destination)
	if err != nil {
		logging.Warn("RAM disk relocation failed",
			"hash", event.Hash, "name", event.Name, "error", err.Error())
	} else if moved {
		logging.Info(fmt.Sprintf("«%s» does not fit on the RAM disk, moving to disk (%s) → %s", event.Name, reason, destination))
	} else {
		logging.Warn("RAM disk relocation was not applied",
			"hash", event.Hash, "name", event.Name, "reason", reason)
	}
}

// tev_reconcilePackIdentityFromFiles implements `reconcile_pack_identity_from_files`.
func tev_reconcilePackIdentityFromFiles(release *models.Release, source string) *models.Release {
	if release.Kind != "series" || !release.IsPack {
		return nil
	}
	files, err := VideoFiles(source)
	if err != nil {
		return nil
	}
	seasons := map[int64][]int64{}
	for _, file := range files {
		name := filepath.Base(file)
		parsed := ParseRelease(name, release.Magnet, release.Source)
		if parsed == nil || parsed.Kind != "series" {
			continue
		}
		releaseSeries := ""
		if release.Series != nil {
			releaseSeries = *release.Series
		}
		parsedSeries := ""
		if parsed.Series != nil {
			parsedSeries = *parsed.Series
		}
		if !SeriesNamesMatch(releaseSeries, parsedSeries) {
			continue
		}
		if parsed.Season == nil || parsed.Episode == nil {
			continue
		}
		if *parsed.Episode > 0 {
			seasons[*parsed.Season] = append(seasons[*parsed.Season], *parsed.Episode)
		}
	}
	if len(seasons) == 0 {
		return nil
	}
	season := int64(0)
	episodes := []int64{}
	for candidateSeason, candidateEpisodes := range seasons {
		if len(candidateEpisodes) > len(episodes) {
			season = candidateSeason
			episodes = candidateEpisodes
		}
	}
	if release.Season != nil && *release.Season == season {
		return nil
	}
	if len(episodes) == 0 {
		return nil
	}
	sort.Slice(episodes, func(i, j int) bool { return episodes[i] < episodes[j] })
	deduped := episodes[:0]
	for index, episode := range episodes {
		if index == 0 || episode != episodes[index-1] {
			deduped = append(deduped, episode)
		}
	}
	episodes = deduped
	corrected := *release
	corrected.Season = &season
	if len(episodes) > 0 {
		first := episodes[0]
		corrected.Episode = &first
	}
	corrected.EpisodeRange = episodes
	corrected.IsPack = true
	return &corrected
}

// ramdiskOrphanStaleAfter is how long an unreferenced RAM-disk entry must be
// idle before the sweep may delete it.
const ramdiskOrphanStaleAfter = time.Hour

// ramdiskOrphanIsStale reports whether an unreferenced RAM-disk entry is old
// enough to be considered an orphan.
func ramdiskOrphanIsStale(modTime, now time.Time) bool {
	return now.Sub(modTime) >= ramdiskOrphanStaleAfter
}

// tev_cleanupOrphanedRamdisk implements `cleanup_orphaned_ramdisk`, hardened: it must
// never delete data that belongs to a torrent whose metadata (and therefore
// file name) is not loaded yet, and it only removes entries that have been idle
// for a while. Without these guards a restart could wipe valid in-progress
// downloads from the tmpfs and force a full re-download.
func tev_cleanupOrphanedRamdisk(ramdisk string, torrents TorrentSession) {
	resolved, err := canonicalizePath(ramdisk)
	if err != nil {
		logging.Debug("RAM disk orphan sweep skipped", "path", ramdisk, "error", err.Error())
		return
	}
	list := torrents.List()
	// A torrent without metadata has an unknown file name: its in-progress data
	// would look orphaned. Skip the whole sweep until every torrent is known.
	for _, torrent := range list {
		if !torrent.HasMetadata {
			logging.Debug("RAM disk orphan sweep skipped: a torrent is still fetching metadata")
			return
		}
	}
	protected := []string{}
	for _, torrent := range list {
		savePath := torrent.SavePath
		if !PathOnRamdisk(savePath, resolved) {
			continue
		}
		canonical := savePath
		if value, err := canonicalizePath(savePath); err == nil {
			canonical = value
		}
		if canonical != resolved {
			protected = append(protected, canonical)
		}
		if strings.TrimSpace(torrent.Name) != "" {
			protected = append(protected, filepath.Join(canonical, torrent.Name))
		}
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		path := filepath.Join(resolved, entry.Name())
		isProtected := false
		for _, root := range protected {
			if pathStartsWith(path, root) {
				isProtected = true
				break
			}
		}
		if isProtected {
			continue
		}
		// Only remove entries that have been idle for a long time. A freshly
		// written partial file (or one just placed by libtorrent) must never be
		// deleted as an "orphan".
		if info, err := entry.Info(); err != nil || !ramdiskOrphanIsStale(info.ModTime(), now) {
			continue
		}
		var result error
		if entry.IsDir() {
			result = os.RemoveAll(path)
		} else {
			result = os.Remove(path)
		}
		if result != nil {
			logging.Warn("could not remove orphaned RAM disk data", "path", path, "error", result.Error())
		} else {
			logging.Info("removed orphaned RAM disk data", "path", path)
		}
	}
}

// ReconcileRamdisk implements `reconcile_ramdisk`.
func ReconcileRamdisk(cfg *Config, torrents TorrentSession, attempts map[string]time.Time) {
	ramdisk := cfg.RamdiskDir()
	if ramdisk == nil {
		clear(attempts)
		return
	}
	tev_cleanupOrphanedRamdisk(*ramdisk, torrents)
	if !cfg.RamdiskEnabled() {
		clear(attempts)
		return
	}
	now := time.Now()
	moved := 0
	for _, torrent := range torrents.List() {
		if moved >= 3 {
			break
		}
		hash := strings.ToLower(torrent.Hash)
		if torrent.TotalSize <= 0 || torrent.State == "seeding" || torrent.State == "finished" || !PathOnRamdisk(torrent.SavePath, *ramdisk) {
			continue
		}
		if at, ok := attempts[hash]; ok && now.Sub(at) < 600*time.Second {
			continue
		}
		reason, destination, ok := tev_ramdiskRelocation(cfg, torrents, hash, torrent.SavePath)
		if !ok {
			delete(attempts, hash)
			continue
		}
		attempts[hash] = now
		moved++
		value, err := torrents.MoveStorage(hash, destination)
		if err != nil {
			logging.Warn("RAM disk reconciliation failed",
				"hash", hash, "name", torrent.Name, "error", err.Error())
		} else if value {
			logging.Info(fmt.Sprintf("🔁 RAM disk reconciliation: moving a torrent to disk (%s) → %s", reason, destination),
				"name", torrent.Name)
		} else {
			logging.Warn("RAM disk reconciliation: relocation not applied",
				"hash", hash, "name", torrent.Name, "reason", reason)
		}
	}
}

// ---------------------------------------------------------------------------
// enforce_seed_policy
// ---------------------------------------------------------------------------

// EnforceSeedPolicy implements `enforce_seed_policy`.
func EnforceSeedPolicy(cfg *Config, torrents TorrentSession, db *Database, postSeedMoves map[string]struct{}, retries map[string]StorageMoveRetry, seedCopyWarnings map[string]time.Time) {
	ratioLimit := 0.0
	ratioLimitSet := false
	if cfg.Libtorrent.StopAtRatio && cfg.Libtorrent.SeedRatio > 0.0 {
		ratioLimit = cfg.Libtorrent.SeedRatio
		ratioLimitSet = true
	}
	// legacy semantics: days is the authoritative long-duration setting;
	// minutes remains as a backward-compatible fallback for sub-day limits.
	timeLimit := int64(0)
	timeLimitSet := false
	if cfg.Libtorrent.SeedTimeDays > 0 {
		timeLimit = tev_saturatingMulInt64(cfg.Libtorrent.SeedTimeDays, 86_400)
		timeLimitSet = true
	} else if cfg.Libtorrent.SeedTimeMinutes > 0 {
		timeLimit = tev_saturatingMulInt64(cfg.Libtorrent.SeedTimeMinutes, 60)
		timeLimitSet = true
	}
	// An infinite-seed override means the torrent must keep seeding: if a
	// previous seed limit left it paused after completion, resume it.
	for _, torrent := range torrents.List() {
		if !tev_needsInfiniteSeedResume(&torrent) {
			continue
		}
		value, err := torrents.Resume(torrent.Hash)
		if err != nil {
			logging.Debug("resume for infinite seeding failed",
				"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
		} else if value {
			logging.Info(fmt.Sprintf("seeding resumed (infinite) — «%s»", torrent.Name))
		}
	}
	for _, torrent := range torrents.List() {
		// Never apply the seed policy to an incomplete torrent: relocating and
		// pausing a still-downloading file corrupts its state.
		if torrent.Progress < 99.99 || torrent.TotalSize <= 0 {
			continue
		}
		eligible := torrent.State == "seeding" || torrent.State == "finished" ||
			(torrent.State == "paused" && torrent.Progress >= 99.99 && !torrent.AutoManaged)
		if !eligible {
			continue
		}
		hasOverride := torrent.SeedRatio >= 0.0 || torrent.SeedDays >= 0
		// legacy treats either explicit zero as an infinite per-torrent seed rule.
		if hasOverride && (torrent.SeedRatio == 0.0 || torrent.SeedDays == 0) {
			continue
		}
		torrentRatioLimit := 0.0
		torrentRatioLimitSet := false
		if torrent.SeedRatio > 0.0 {
			torrentRatioLimit = torrent.SeedRatio
			torrentRatioLimitSet = true
		} else if torrent.SeedRatio < 0.0 {
			torrentRatioLimit = ratioLimit
			torrentRatioLimitSet = ratioLimitSet
		}
		torrentTimeLimit := int64(0)
		torrentTimeLimitSet := false
		if torrent.SeedDays > 0 {
			torrentTimeLimit = tev_saturatingMulInt64(torrent.SeedDays, 86_400)
			torrentTimeLimitSet = true
		} else if torrent.SeedDays < 0 {
			torrentTimeLimit = timeLimit
			torrentTimeLimitSet = timeLimitSet
		}
		ratioReached := false
		if torrentRatioLimitSet {
			download := tev_effectiveDownloadForRatio(&torrent)
			ratioReached = download > 0.0 && (float64(torrent.AllTimeUpload)/download) >= torrentRatioLimit
		}
		timeReached := torrentTimeLimitSet && torrent.SeedingSeconds >= torrentTimeLimit
		if !ratioReached && !timeReached {
			continue
		}
		stopped := torrent.State == "paused" && !torrent.AutoManaged
		if !stopped {
			value, err := torrents.Pause(torrent.Hash)
			if err != nil {
				logging.Warn("failed to stop torrent at seed limit",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			} else if value {
				stopped = true
				reason := "seed time reached"
				if ratioReached && timeReached {
					reason = "ratio and time reached"
				} else if ratioReached {
					reason = "ratio reached"
				}
				logging.Info(fmt.Sprintf("⏸️ Seeding done (%s) — pausing «%s»", reason, torrent.Name))
			} else {
				logging.Warn("torrent seed limit could not be applied in current mode",
					"hash", torrent.Hash, "name", torrent.Name)
			}
		}
		if !stopped {
			continue
		}
		if tev_postSeedRelocate(cfg, torrents, &torrent, postSeedMoves, retries) {
			continue
		}
		if cfg.Libtorrent.AutoRemoveCompleted {
			if !tev_completedSourceDisposable(db, torrent.Hash, torrent.SavePath) {
				if tev_seedCopyWarningDue(seedCopyWarnings, torrent.Hash) {
					logging.Debug("seed limit reached but archived copy is not verified; keeping torrent files",
						"hash", torrent.Hash, "name", torrent.Name)
				}
				continue
			}
			removed, err := torrents.Remove(torrent.Hash, true)
			if err != nil {
				logging.Warn("failed to remove torrent at seed limit",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			} else if removed {
				_ = db.MarkTorrentRemovedAt(torrent.Hash)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// discard_completed_source
// ---------------------------------------------------------------------------

// tev_writeRejectionMarker implements `write_rejection_marker`.
func tev_writeRejectionMarker(source, reason string) {
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return
	}
	content := fmt.Sprintf(
		"Rifiutato per minore qualità.\nMotivo: %s\nData: %s\n",
		reason,
		time.Now().Format("2006-01-02 15:04"),
	)
	if err := os.WriteFile(filepath.Join(source, "RIFIUTATO.txt"), []byte(content), 0o644); err != nil {
		logging.Warn("could not write rejection marker", "error", err.Error(), "source", source)
	}
}

// tev_discardCompletedSource implements `discard_completed_source`.
func tev_discardCompletedSource(cfg *Config, db *Database, torrents TorrentSession, event *models.TorrentEvent, reason string) {
	source := CompletionPath(event)
	removed, err := torrents.Remove(event.Hash, false)
	if err != nil {
		logging.Warn("rejected torrent removal failed",
			"hash", event.Hash, "name", event.Name, "error", err.Error())
		// Never move/delete files while libtorrent may still be writing them.
		return
	}
	if removed {
		logging.Info("rejected completed torrent removed from the session",
			"hash", event.Hash, "name", event.Name)
	} else {
		logging.Debug("rejected torrent was already removed",
			"hash", event.Hash, "name", event.Name)
	}
	_ = db.MarkTorrentRemovedAt(event.Hash)
	if _, statErr := os.Stat(source); statErr != nil {
		return
	}
	tev_writeRejectionMarker(source, reason)
	if cfg.TrashPath != nil {
		target, err := MoveToTrash(source, *cfg.TrashPath)
		if err != nil {
			logging.Error("could not move rejected completed download to trash",
				"hash", event.Hash, "name", event.Name, "source", source, "error", err.Error())
		} else {
			logging.Info(fmt.Sprintf("rejected completed download moved to trash — «%s» · %s → %s", event.Name, source, target))
		}
		return
	}
	info, statErr := os.Stat(source)
	var result error
	if statErr == nil && info.IsDir() {
		result = os.RemoveAll(source)
	} else {
		result = os.Remove(source)
	}
	if result != nil {
		logging.Error("could not remove rejected completed download",
			"hash", event.Hash, "name", event.Name, "source", source, "error", result.Error())
	} else {
		logging.Info(fmt.Sprintf("rejected completed download removed — «%s» · %s", event.Name, source))
	}
}

// ---------------------------------------------------------------------------
// complete_torrent
// ---------------------------------------------------------------------------

// tev_completeTorrent implements `complete_torrent`.
// normalizeReleaseSeries rewrites a series release's name to the configured
// series it matches. Without it a release parsed as e.g. "CIA 2026" is recorded
// under a phantom series, while the monitored "CIA" still looks empty and keeps
// re-downloading the episode.
func normalizeReleaseSeries(cfg *Config, release *models.Release) {
	if release == nil || release.Kind != "series" || release.Series == nil {
		return
	}
	if series := cfg.FindSeriesMatch(*release.Series, release.Season); series != nil {
		canonical := series.Name
		release.Series = &canonical
	}
}

func tev_completeTorrent(cfg *Config, db *Database, torrents TorrentSession, event *models.TorrentEvent, release *models.Release, tmdb *TmdbClient) (bool, error) {
	// Record the episode under the configured series, not the raw parsed name.
	normalizeReleaseSeries(cfg, release)
	guard := AcquireArchiveImport(release.Series)
	defer guard.Release()

	path := CompletionPath(event)
	recoveredExisting := false
	if _, statErr := os.Stat(path); statErr != nil {
		resolved := ""
		found := false
		if release.Kind == "series" {
			if release.Series != nil {
				if series := cfg.FindSeriesByName(*release.Series); series != nil {
					if dir := cfg.ResolveArchivePath(series); dir != nil && release.Season != nil && release.Episode != nil {
						if value, ok := FindEpisodeFile(*dir, *release.Season, *release.Episode); ok {
							resolved = value
							found = true
						} else if value, ok := BestEpisodeFile(*dir, *release.Season, *release.Episode); ok {
							resolved = value
							found = true
						}
					}
				}
			}
		} else if release.Kind == "movie" {
			files, _ := VideoFiles(event.SavePath)
			if len(files) == 1 {
				resolved = files[0]
				found = true
			}
		}
		if !found {
			logging.Warn("completed torrent file not found (already moved or renamed); marking completion as failed",
				"hash", event.Hash, "name", event.Name, "path", path)
			return false, fmt.Errorf("completed torrent file not found: %s", path)
		}
		logging.Info("completed file already renamed — using the archived file",
			"name", event.Name, "title", release.Title, "path", resolved)
		path = resolved
		recoveredExisting = true
	}
	// Refuse to archive a broken download: a zero-filled (preallocated but
	// never written) or unrecognised video file must not replace a good copy.
	if err := validateCompletedFile(path); err != nil {
		quarantined := quarantineCorruptFile(path, cfg)
		logging.Error("completed file failed integrity validation; not archiving",
			"name", event.Name, "path", path, "error", err.Error(), "quarantined", quarantined)
		return false, fmt.Errorf("integrity validation failed: %w", err)
	}
	size, err := SizeOfPath(path)
	if err != nil {
		return false, err
	}
	noRename, _ := db.TorrentNoRename(event.Hash)
	renamed := ""
	renamedSet := false
	if noRename {
		logging.Info(fmt.Sprintf("rename skipped — torrent marked no-rename: «%s»", event.Name),
			"name", event.Name, "title", release.Title)
	} else if release.Kind == "movie" {
		value, err := RenameMovie(context.Background(), path, release, cfg, tmdb)
		if err != nil {
			return false, err
		}
		if value != "" {
			renamed = value
			renamedSet = true
		}
	} else {
		value, err := RenameEpisode(context.Background(), path, release, cfg, tmdb)
		if err != nil {
			return false, err
		}
		if value != "" {
			renamed = value
			renamedSet = true
		}
	}
	processedPath := path
	if renamedSet {
		processedPath = renamed
	}
	discarded := false
	if release.Kind == "series" && !release.IsPack {
		if release.Series != nil && release.Season != nil && release.Episode != nil {
			newFile := renamed
			haveNewFile := renamedSet
			if !haveNewFile {
				files, _ := VideoFiles(path)
				if len(files) == 1 {
					newFile = files[0]
					haveNewFile = true
				}
			}
			archive := path
			if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
				archive = filepath.Dir(path)
			}
			if haveNewFile {
				score := cfg.ReleaseScore(release)
				value, err := DiscardIfInferior(cfg, *release.Series, *release.Season, *release.Episode, score, newFile, archive)
				if err != nil {
					return false, err
				}
				if value {
					discarded = true
				} else {
					removed, err := CleanupOldEpisodeWithQuality(cfg, *release.Series, *release.Season, *release.Episode, score, newFile, archive, release.Quality)
					if err != nil {
						return false, err
					}
					if removed > 0 {
						logging.Info(fmt.Sprintf("🗑️ Upgrade cleanup — %d obsolete episode file(s) moved to trash", removed))
					}
				}
			}
		}
	} else if release.Kind == "movie" {
		newFile := renamed
		haveNewFile := renamedSet
		if !haveNewFile {
			files, _ := VideoFiles(path)
			if len(files) == 1 {
				newFile = files[0]
				haveNewFile = true
			}
		}
		archive := path
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			archive = filepath.Dir(path)
		}
		if haveNewFile {
			score := cfg.ReleaseScore(release)
			value, err := DiscardIfInferiorMovie(cfg, release.Title, release.Year, score, newFile, archive)
			if err != nil {
				return false, err
			}
			if value {
				discarded = true
			} else if _, err := CleanupOldMovie(cfg, release.Title, release.Year, score, newFile, archive); err != nil {
				return false, err
			}
		}
	}
	if discarded {
		restored, err := db.RestoreUpgrade(event.Hash)
		if err != nil {
			return false, err
		}
		if !restored {
			if err := db.RollbackRelease(release); err != nil {
				return false, err
			}
		}
		if err := db.MarkTorrentError(event.Hash, "release inferior to existing file"); err != nil {
			return false, err
		}
		tev_discardCompletedSource(cfg, db, torrents, event, "release inferior to existing file")
		logging.Warn("completed release discarded as inferior",
			"hash", event.Hash, "name", event.Name, "title", release.Title)
		return false, nil
	}
	if err := db.MarkReleaseCompleted(release, processedPath, size); err != nil {
		return false, err
	}
	durationSeconds := int64(0)
	averageSpeed := int64(0)
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
			dividend := size
			if dividend < 1 {
				dividend = 1
			}
			durationSeconds = seconds
			averageSpeed = dividend / seconds
		}
	}
	suffix := " (kept original name)"
	if renamedSet {
		suffix = ""
	}
	logging.Info(fmt.Sprintf("🎉 Download complete — «%s» · %s · downloaded in %s at %s · saved to %s%s",
		release.Title,
		logging.HumanBytesI64(size),
		logging.HumanDuration(durationSeconds),
		logging.HumanRate(averageSpeed),
		processedPath,
		suffix,
	))
	if recoveredExisting || (renamedSet && !SamePath(processedPath, path)) {
		removed, err := torrents.Remove(event.Hash, false)
		if err != nil {
			logging.Warn("renamed torrent removal failed",
				"hash", event.Hash, "name", event.Name, "error", err.Error())
		} else if removed {
			_ = db.MarkTorrentRemovedAt(event.Hash)
			logging.Debug("torrent removed after rename: archived under a different path",
				"hash", event.Hash, "name", event.Name)
		} else {
			logging.Debug("renamed torrent already removed", "hash", event.Hash, "name", event.Name)
		}
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// handle_torrent_event
// ---------------------------------------------------------------------------

// HandleTorrentEvent implements `handle_torrent_event`.
func HandleTorrentEvent(cfg *Config, torrents TorrentSession, db *Database, moveRequests map[string]struct{}, postSeedMoves map[string]struct{}, retries map[string]StorageMoveRetry, event models.TorrentEvent, tmdb *TmdbClient, notifier *Notifier) (bool, error) {
	logging.Debug("torrent completion processing started",
		"hash", event.Hash, "kind", event.Kind, "name", event.Name, "save_path", event.SavePath)
	// libtorrent error alerts are handled before the metadata lookup: some of
	// them (tracker/file errors) can arrive before a release row exists, and
	// session-level errors have no hash at all.
	//
	// Tracker errors are routine (dead or unreachable trackers are common and
	// libtorrent retries them) so they stay at DEBUG and never change a
	// torrent's state. A torrent/file error is a real problem: WARN + a
	// notification, but the state is left to the normal completion handling so
	// a transient error cannot silently stop an acquisition.
	switch event.Kind {
	case "tracker_error":
		logging.Debug("libtorrent tracker error",
			"hash", event.Hash, "name", event.Name, "message", event.Message)
		return false, nil
	case "torrent_error", "file_error", "hash_failed", "metadata_failed", "resume_save_failed":
		logging.Warn("libtorrent reported an error",
			"kind", event.Kind, "hash", event.Hash, "name", event.Name, "message", event.Message)
		_ = notifier.NotifyEvent("torrent_error", map[string]any{
			"hash":    event.Hash,
			"kind":    event.Kind,
			"name":    event.Name,
			"error":   event.Message,
			"message": event.Message,
		})
		return false, nil
	case "portmap_error", "session_error":
		logging.Warn("libtorrent session error", "kind", event.Kind, "message", event.Message)
		return false, nil
	}
	metadata, err := db.TorrentMeta(event.Hash)
	if err != nil {
		return false, err
	}
	if metadata == nil {
		// Expected for manually added or foreign torrents: not an error. Still
		// mark the row completed so it does not stay "downloading" forever, but
		// without an archive path (the files are not moved).
		if event.Kind == "torrent_finished" {
			if err := db.MarkTorrentCompletedUnarchived(event.Hash); err != nil {
				logging.Debug("cannot mark foreign torrent completed",
					"hash", event.Hash, "error", err.Error())
			}
			logging.Info(fmt.Sprintf("torrent completed (no release metadata, kept in place) — «%s»", event.Name))
		}
		logging.Debug("torrent alert has no registered release metadata",
			"hash", event.Hash, "kind", event.Kind, "name", event.Name)
		return false, nil
	}
	switch event.Kind {
	// legacy parity: at `add()` the size is unknown and the RAM disk is used
	// first; once metadata arrives the real size decides if it still fits.
	case "metadata_received":
		// The indexer title can disagree with the torrent's real name (for
		// example S06 in Jackett while the payload is S05). Reject this before
		// pieces are downloaded; completion-time validation is still kept as a
		// second line of defence.
		if metadata.Release.IsPack {
			corrected := ReconcilePackIdentity(&metadata.Release, event.Name)
			if corrected != nil && !tev_optionalInt64Equal(corrected.Season, metadata.Release.Season) {
				errorMessage := "torrent metadata season does not match declared season"
				logging.Warn("rejecting season pack before download: metadata identity mismatch",
					"hash", event.Hash,
					"declared_season", metadata.Release.Season,
					"torrent_season", corrected.Season,
					"name", event.Name,
				)
				if err := db.MarkTorrentError(event.Hash, errorMessage); err != nil {
					return false, err
				}
				if err := db.Blocklist(&metadata.Release, "season_pack_identity_mismatch"); err != nil {
					return false, err
				}
				if _, err := torrents.Remove(event.Hash, true); err != nil {
					logging.Warn("failed to remove mismatched season pack",
						"hash", event.Hash, "error", err.Error())
				}
				return false, nil
			}
		}
		for _, torrent := range torrents.List() {
			if strings.EqualFold(torrent.Hash, event.Hash) {
				logging.Info(fmt.Sprintf("📦 Download metadata received — «%s» · %s (torrent: %s)",
					metadata.Release.Title, logging.HumanBytesI64(torrent.TotalSize), torrent.Name))
				break
			}
		}
		tev_enforceRamdiskCapacity(cfg, torrents, &event)
		return false, nil
	case "torrent_finished":
		// Completion can be triggered twice (a recovered event after a
		// restart, or storage_moved): never post-process an already archived
		// release again. A "completed" row whose file is missing is re-processed
		// instead of being skipped forever.
		if tevIgnoreRepeatedCompletion(db, event.Hash, event.Name) {
			return false, nil
		}
		release := metadata.Release
		source := CompletionPath(&event)
		corrected := ReconcilePackIdentity(&release, event.Name)
		if corrected == nil {
			corrected = tev_reconcilePackIdentityFromFiles(&release, source)
		}
		if corrected != nil {
			logging.Warn("correcting season-pack identity from the actual torrent name",
				"hash", event.Hash,
				"title", release.Title,
				"torrent_name", event.Name,
				"declared_season", release.Season,
				"torrent_season", corrected.Season,
			)
			if err := db.ReconcilePackRelease(event.Hash, corrected, cfg); err != nil {
				return false, err
			}
			release = *corrected
		}
		normalizeReleaseSeries(cfg, &release)
		destination, hasDestination := DestinationFor(&release, cfg)
		current := event.SavePath
		if release.IsPack {
			if !hasDestination {
				return tev_completeTorrent(cfg, db, torrents, &event, &release, tmdb)
			}
			size, err := SizeOfPath(source)
			if err != nil {
				return false, err
			}
			matching, err := MatchingPackFiles(source, &release)
			if err != nil {
				return false, err
			}
			if len(matching) == 0 {
				errorMessage := "season pack filenames do not match declared season"
				declaredSeason := "unknown"
				if release.Season != nil {
					declaredSeason = strconv.FormatInt(*release.Season, 10)
				}
				logging.Warn("season pack rejected: files do not match the declared season",
					"hash", event.Hash,
					"name", event.Name,
					"title", release.Title,
					"declared_season", declaredSeason,
					"source", source,
				)
				if err := db.MarkTorrentError(event.Hash, errorMessage); err != nil {
					return false, err
				}
				// A completed pack whose files contradict its declared season
				// is a bad indexer identity, not a transient download failure.
				if err := db.Blocklist(&release, "season_pack_identity_mismatch"); err != nil {
					return false, err
				}
				tev_discardCompletedSource(cfg, db, torrents, &event, errorMessage)
				return false, nil
			}
			// Serialize against the periodic/manual rename repair for this
			// series: it scans the archive and would otherwise rename/trash
			// files while they are still being copied.
			guard := AcquireArchiveImport(release.Series)
			defer guard.Release()
			processed := []PackFileResult{}
			for index := range matching {
				file := matching[index]
				placed, ok, err := StagePackFile(&file, source, destination, cfg, cfg.ReleaseScore(&release))
				if err != nil {
					return false, err
				}
				if !ok {
					continue
				}
				partial, err := ProcessPackFiles(context.Background(), []PackInput{{Path: placed, Source: file}}, &release, cfg, tmdb)
				if err != nil {
					return false, err
				}
				processed = append(processed, partial...)
			}
			allDiscarded := len(processed) > 0
			for _, item := range processed {
				if !item.Discarded {
					allDiscarded = false
					break
				}
			}
			if allDiscarded {
				restored, err := db.RestoreUpgrade(event.Hash)
				if err != nil {
					return false, err
				}
				if !restored {
					if err := db.RollbackRelease(&release); err != nil {
						return false, err
					}
				}
				if err := db.MarkTorrentError(event.Hash, "season pack inferior to existing files"); err != nil {
					return false, err
				}
				// Esito definitivo: il pack è stato scartato per intero. Esce
				// dalla sessione e la sorgente va nel cestino.
				tev_discardCompletedSource(cfg, db, torrents, &event, "season pack inferior to existing files")
				return false, nil
			}
			entries := []PackEpisode{}
			for _, item := range processed {
				if item.Discarded {
					continue
				}
				entries = append(entries, PackEpisode{
					Episode:   item.Episode,
					Path:      item.Path,
					SizeBytes: item.SizeBytes,
					Score:     item.QualityScore,
				})
			}
			if err := db.MarkPackCompleted(&release, entries, destination, size); err != nil {
				return false, err
			}
			episodes := []any{}
			for _, item := range processed {
				if item.Discarded {
					continue
				}
				episodes = append(episodes, map[string]any{
					"series":  release.Series,
					"season":  release.Season,
					"episode": item.Episode,
					"path":    item.Path,
				})
			}
			logging.Info(fmt.Sprintf("🎉 Season pack complete — «%s» · %d episodes · %s · archived to %s",
				release.Title, len(episodes), logging.HumanBytesI64(size), destination))
			discardedCount := 0
			for _, item := range processed {
				if item.Discarded {
					discardedCount++
				}
			}
			notificationErr := notifier.NotifyEvent("season_pack_completed", map[string]any{
				"series":          release.Series,
				"season":          release.Season,
				"title":           release.Title,
				"path":            destination,
				"size_bytes":      size,
				"new_count":       len(episodes),
				"discarded_count": discardedCount,
				"episodes":        episodes,
			})
			if notificationErr != nil {
				logging.Warn("completion notification failed",
					"hash", event.Hash, "name", event.Name, "event", "season_pack_completed", "error", notificationErr.Error())
			} else {
				logging.Debug("completion notification sent",
					"hash", event.Hash, "name", event.Name, "event", "season_pack_completed")
			}
			// A pack is copied into the library, never moved out of its torrent
			// storage here: libtorrent must retain the exact original tree to
			// seed and verify it.
			logging.Info(fmt.Sprintf("📁 Season pack copied to NAS, source kept for seeding · %s · %s",
				logging.HumanBytesI64(size), destination))
			return true, nil
		}
		if !hasDestination {
			return tev_completeTorrent(cfg, db, torrents, &event, &release, tmdb)
		}
		if SamePath(current, destination) {
			return tev_completeTorrent(cfg, db, torrents, &event, &release, tmdb)
		}
		if _, exists := moveRequests[event.Hash]; exists {
			return false, nil
		}
		moveRequests[event.Hash] = struct{}{}
		if err := ValidateDestinationFrom(current, destination); err != nil {
			return false, err
		}
		logging.Debug("📁 MOVING TO NAS — completed download leaves the work folder",
			"hash", event.Hash,
			"name", event.Name,
			"title", release.Title,
			"from", current,
			"to", destination,
		)
		moved, err := torrents.MoveStorage(event.Hash, destination)
		if err != nil {
			delete(moveRequests, event.Hash)
			fallbackDestination := cfg.LibtorrentDir
			if alternative, ok := DestinationFor(&release, cfg); ok {
				fallbackDestination = alternative
			}
			tev_scheduleStorageMoveRetry(retries, event.Hash, fallbackDestination, false, time.Now())
			logging.Warn("completed torrent storage move failed synchronously",
				"hash", event.Hash, "name", event.Name, "error", err.Error())
		} else if moved {
			retries[strings.ToLower(event.Hash)] = StorageMoveRetry{
				destination: destination,
				postSeed:    false,
				attempts:    0,
				nextAttempt: time.Now().Add(600 * time.Second),
				inFlight:    true,
			}
		} else {
			delete(moveRequests, event.Hash)
		}
		return false, nil
	case "storage_move_failed":
		delete(moveRequests, event.Hash)
		existing, hasExisting := retries[strings.ToLower(event.Hash)]
		statusAtFailure, _ := db.TorrentStatus(event.Hash)
		wasPostSeedMove := false
		if hasExisting && existing.postSeed {
			wasPostSeedMove = true
		}
		if !wasPostSeedMove {
			if _, ok := postSeedMoves[event.Hash]; ok {
				delete(postSeedMoves, event.Hash)
				wasPostSeedMove = true
			}
		}
		if !wasPostSeedMove {
			wasPostSeedMove = statusAtFailure != nil && *statusAtFailure == "completed"
		}
		logging.Warn("storage move failed (destination may already exist); torrent kept in place",
			"hash", event.Hash, "name", event.Name, "save_path", event.SavePath)
		// Unknown move failures belong to RAM-disk/manual relocation, not to
		// completion import. Never guess the archive destination for a
		// still-downloading torrent.
		var destination *string
		if hasExisting {
			value := existing.destination
			destination = &value
		} else {
			statusNow, _ := db.TorrentStatus(event.Hash)
			if wasPostSeedMove || (statusNow != nil && *statusNow == "completed") {
				value := cfg.LibtorrentDir
				destination = &value
			}
		}
		if destination != nil {
			tev_scheduleStorageMoveRetry(retries, event.Hash, *destination, wasPostSeedMove, time.Now())
			if wasPostSeedMove {
				postSeedMoves[event.Hash] = struct{}{}
			}
		}
		return false, nil
	case "storage_moved":
		logging.Info("torrent storage move completed",
			"hash", event.Hash, "name", event.Name, "save_path", event.SavePath)
		delete(postSeedMoves, event.Hash)
		delete(retries, strings.ToLower(event.Hash))
		// A post-seeding relocation happens after the release was already
		// committed. Do not run rename/copy/pack processing a second time, but
		// do re-process a "completed" torrent whose archived copy is missing.
		if tevIgnoreRepeatedCompletion(db, event.Hash, event.Name) {
			delete(moveRequests, event.Hash)
			return false, nil
		}
		var current *models.TorrentView
		for _, torrent := range torrents.List() {
			if strings.EqualFold(torrent.Hash, event.Hash) {
				candidate := torrent
				current = &candidate
				break
			}
		}
		done := current != nil && current.Progress >= 99.99 && current.TotalSize > 0 && current.TotalDone >= current.TotalSize
		if !done {
			delete(moveRequests, event.Hash)
			// libtorrent pauses a torrent while it moves its data and does not
			// always resume it; a relocation forced by the RAM-disk policy must
			// not leave an incomplete download stopped forever.
			if current != nil && current.State == "paused" && current.AutoManaged && current.Progress < 99.99 {
				if resumed, rerr := torrents.Resume(event.Hash); rerr == nil && resumed {
					logging.Info("download resumed after storage move",
						"hash", event.Hash, "name", event.Name, "progress", current.Progress)
				}
				_, _ = torrents.Reannounce(event.Hash)
			}
			logging.Debug("ignoring storage move before torrent completion",
				"hash", event.Hash, "name", event.Name)
			return false, nil
		}
		delete(moveRequests, event.Hash)
		return tev_completeTorrent(cfg, db, torrents, &event, &metadata.Release, tmdb)
	default:
		return false, nil
	}
}
