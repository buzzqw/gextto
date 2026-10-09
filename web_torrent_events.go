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
	"errors"
	"fmt"
	"math"
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
	// retryStep counts the retries scheduled so far: each one waits longer
	// than the previous (see tev_stallRetryIntervals), so a dead swarm is not
	// probed, and logged, every hour forever.
	retryStep uint8
	// probeUntil is set while a parked torrent is resumed for a retry; when it
	// passes without byte progress the torrent is parked again. It is not
	// persisted: after a restart a parked torrent is simply parked again.
	probeUntil time.Time
	// persisted reports whether the entry has a database row: parked torrents
	// always do, downloads only once idle for tev_stallPersistAfter, so a
	// restart does not reset the clock of a download that is already stuck.
	persisted bool
	// savedState is what the database row holds (see stallPersistedState):
	// the row is rewritten only when it changes, not at every worker tick.
	savedState string
}

// stallPersistedState is the part of a StallWatch stored in its row.
func stallPersistedState(entry StallWatch) string {
	since := ""
	if entry.stalledSince != nil {
		since = entry.stalledSince.UTC().Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%d|%d|%s|%d|%d", entry.lastProgressAt.UnixNano(), entry.lastDone, since, entry.nextRetryAt.UnixNano(), entry.retryStep)
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

// ramdiskAdmitMaxProgress is the completion (percent) below which a torrent
// whose metadata just arrived is still worth moving onto the RAM disk. Beyond
// it the payload has really started and stays where it is.
const ramdiskAdmitMaxProgress = 1.0

const (
	// Keep the existing metadata recovery cadence separate from the stalled
	// download state machine below.
	tev_metadataRetryInterval = 10 * time.Minute
	// Report a continuing metadata problem periodically, not on every retry.
	tev_metadataWarnInterval = time.Hour
)

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

// tev_packFileNames renders a compact, bounded list of the outcomes of a season
// pack so the log names what was kept and what was discarded without flooding.
func tev_packFileNames(items []PackFileResult) string {
	if len(items) == 0 {
		return "none"
	}
	const limit = 8
	names := make([]string, 0, len(items))
	for index, item := range items {
		if index >= limit {
			names = append(names, fmt.Sprintf("… and %d more", len(items)-limit))
			break
		}
		names = append(names, filepath.Base(item.Path))
	}
	return strings.Join(names, ", ")
}

// tev_archivedFileNames renders the names currently present in an archived
// season-pack destination. The list is deliberately bounded because a library
// folder can contain more files than the pack being reported.
func tev_archivedFileNames(paths []string) string {
	if len(paths) == 0 {
		return "none"
	}
	const limit = 8
	names := make([]string, 0, len(paths))
	for index, path := range paths {
		if index >= limit {
			names = append(names, fmt.Sprintf("… and %d more", len(paths)-limit))
			break
		}
		names = append(names, filepath.Base(path))
	}
	return strings.Join(names, ", ")
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
		logging.Info(fmt.Sprintf("🧹 Failed download removed from the list: «%s»", name), "hash", hash)
	} else {
		logging.Debug("failed torrent already removed", "hash", hash, "name", name)
	}
	return true
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

// storageMoveReporter is implemented by engines that can say which torrents
// are still moving their files (the embedded libtorrent).
type storageMoveReporter interface {
	MovingStorage() (map[string]string, bool)
}

// RetryStorageMoves implements `retry_storage_moves`.
func RetryStorageMoves(torrents TorrentSession, moveRequests map[string]struct{}, postSeedMoves map[string]struct{}, retries map[string]StorageMoveRetry) {
	now := time.Now()
	// Asked once per tick, and only when an in-flight move reached its check.
	var moving map[string]string
	movingKnown := false
	stillMoving := func(hash string) bool {
		if !movingKnown {
			movingKnown = true
			if reporter, ok := torrents.(storageMoveReporter); ok {
				if current, ok := reporter.MovingStorage(); ok {
					moving = current
				}
			}
		}
		_, found := moving[strings.ToLower(hash)]
		return found
	}
	hashes := make([]string, 0, len(retries))
	for hash := range retries {
		hashes = append(hashes, hash)
	}
	for _, hash := range hashes {
		retry, ok := retries[hash]
		if !ok {
			continue
		}
		// Both an in-flight move and a failed one wait for nextAttempt: without
		// the second check a move that keeps failing (destination already
		// occupied) was re-issued on every worker tick, several times a second.
		if now.Before(retry.nextAttempt) {
			continue
		}
		name := tev_torrentDisplayName(torrents, hash)
		// A big move to a NAS can take longer than the in-flight wait. While
		// libtorrent is still copying, re-issuing the move would only fail
		// ("destination exists") and be reported as an error: check again later.
		if retry.inFlight && stillMoving(hash) {
			if entry, ok := retries[hash]; ok {
				entry.nextAttempt = now.Add(time.Minute)
				retries[hash] = entry
			}
			logging.Debug("storage move still copying; waiting for it to finish",
				"hash", hash, "name", name, "destination", retry.destination)
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
			logging.Warn(fmt.Sprintf("⚠️ «%s» still cannot be moved to %s after several attempts; trying again later", name, retry.destination), "hash", hash)
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
		if err != nil && tev_moveAlreadyRunning(err) {
			// Restored after a Gextto restart while the engine was still
			// copying: the move is running, wait for it like any in-flight one.
			if entry, ok := retries[hash]; ok {
				entry.inFlight = true
				entry.nextAttempt = now.Add(time.Minute)
				retries[hash] = entry
			}
			if postSeed {
				postSeedMoves[hash] = struct{}{}
			}
			logging.Debug("storage move already running in the engine; waiting for it", "name", torrent.Name, "destination", destination)
		} else if err != nil {
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
		logging.Info(fmt.Sprintf("🧹 Failed download «%s» removed from the list after the restart (its files are kept)", torrent.Name), "hash", torrent.Hash)
	}
}

// DetachCompletedArchivedSingles implements `detach_completed_archived_singles`.
func DetachCompletedArchivedSingles(cfg *Config, torrents TorrentSession, db *Database) {
	// Completed singles stay in the session (shown as completed) unless the
	// user asked for automatic removal; this recovery pass must not remove
	// them behind the user's back.
	if cfg != nil && !cfg.Libtorrent.AutoRemoveCompleted {
		return
	}
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

// tev_stallProbeWindow is how long a parked torrent stays resumed after each
// retry, waiting for a seeder to answer the reannounce.
const tev_stallProbeWindow = 10 * time.Minute

// tev_stallPersistAfter is how long a download must go without a byte before
// its progress clock is saved, so it survives a restart.
const tev_stallPersistAfter = 5 * time.Minute

// tev_stallRetryIntervals is the wait before each retry of a parked torrent:
// one hour, then three, six, twelve, and every 24 hours thereafter.
var tev_stallRetryIntervals = [...]time.Duration{
	time.Hour,
	3 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
}

// tev_stallRetryDelay is the wait before retry number step (0-based). The
// first one honours the configured retry interval (libtorrent_stall_retry_min);
// the later ones follow the backoff, never shorter than that setting.
func tev_stallRetryDelay(step uint8, base time.Duration) time.Duration {
	if step == 0 {
		return base
	}
	if step >= uint8(len(tev_stallRetryIntervals)) {
		step = uint8(len(tev_stallRetryIntervals) - 1)
	}
	if delay := tev_stallRetryIntervals[step]; delay > base {
		return delay
	}
	return base
}

// tev_scheduleStallRetry sets when a parked torrent is retried next and
// advances the backoff.
func tev_scheduleStallRetry(entry *StallWatch, now time.Time, base time.Duration) time.Duration {
	delay := tev_stallRetryDelay(entry.retryStep, base)
	entry.nextRetryAt = now.Add(delay)
	if entry.retryStep < uint8(len(tev_stallRetryIntervals)-1) {
		entry.retryStep++
	}
	return delay
}

// MonitorStalled implements `monitor_stalled`.
func MonitorStalled(cfg *Config, torrents TorrentSession, db *Database, notifier *Notifier, watch map[string]StallWatch) {
	stallAfterMinutes := tev_settingFloatOr(cfg, "libtorrent_stall_after_min", 60.0)
	retryMinutes := tev_settingFloatOr(cfg, "libtorrent_stall_retry_min", 60.0)
	giveupMinutes := tev_configuredStallGiveupMinutes(cfg)
	// A dead swarm (zero seeders) is very unlikely to revive, so it gets its own
	// shorter give-up window; when it expires the gap is retried immediately.
	deadSwarmGiveupMinutes := tev_settingFloatOr(cfg, "libtorrent_dead_swarm_giveup_min", 4320.0)
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
	probeTimeout := tev_stallProbeWindow
	if probeTimeout > retryTimeout/2 {
		probeTimeout = retryTimeout / 2
	}
	now := time.Now()
	live := map[string]struct{}{}
	for _, torrent := range torrents.List() {
		live[torrent.Hash] = struct{}{}
		entry, watched := watch[torrent.Hash]
		parked := watched && entry.stalledSince != nil
		finished := torrent.Progress >= 100.0 || torrent.State == "finished" || torrent.State == "seeding"
		// A data check or metadata fetch (typically right after a restart) is
		// not a verdict: keep the clock as it is until the torrent settles.
		transient := !finished && torrent.State != "downloading" && torrent.State != "stalled" && torrent.State != "paused"
		if transient {
			continue
		}
		// A parked torrent shows up as stalled, paused or downloading (during a
		// probe): none of these ends the stall, so its clock and give-up
		// deadline survive. A torrent that is not parked is timed only while it
		// is really downloading; user-paused torrents never enter watch.
		if finished || (!parked && torrent.State == "paused") {
			if watched {
				torrents.ClearStalled(torrent.Hash)
				// Only idle or stalled entries were persisted: do not issue a DELETE
				// for every seeding/paused torrent at every tick.
				if entry.persisted {
					_ = db.DeleteStallWatch(torrent.Hash)
				}
				delete(watch, torrent.Hash)
			}
			continue
		}
		if !parked {
			if !watched {
				entry = StallWatch{lastProgressAt: now, lastDone: torrent.TotalDone}
			}
			progressAt := entry.lastProgressAt
			lastDone := entry.lastDone
			expired := tev_stallExpired(&progressAt, &lastDone, now, torrent.TotalDone, stallTimeout)
			entry.lastProgressAt = progressAt
			entry.lastDone = lastDone
			if !expired {
				idle := now.Sub(entry.lastProgressAt) >= tev_stallPersistAfter
				switch {
				case idle && !entry.persisted:
					if err := db.SaveStallWatch(torrent.Hash, entry); err == nil {
						entry.persisted = true
						entry.savedState = stallPersistedState(entry)
					}
				case !idle && entry.persisted:
					_ = db.DeleteStallWatch(torrent.Hash)
					entry.persisted = false
				}
				watch[torrent.Hash] = entry
				continue
			}
			if _, err := torrents.MarkStalled(torrent.Hash); err != nil {
				logging.Debug("could not park stalled torrent",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
				watch[torrent.Hash] = entry
				continue
			}
			stamp := now
			entry.stalledSince = &stamp
			code, reason, hint := DiagnoseTorrent(&torrent)
			logging.Warn(fmt.Sprintf("⏸️ «%s» is stuck at %s: %s. It is set aside so other downloads can proceed, and retried in %s, then less and less often (up to once a day)",
				torrent.Name, logPercent(torrent.Progress), stallReasonForLog(code, &torrent),
				logDuration(tev_stallRetryDelay(0, retryTimeout))), "hash", torrent.Hash)
			logging.Debug("download stalled",
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
			tev_scheduleStallRetry(&entry, now, retryTimeout)
			if err := db.SaveStallWatch(torrent.Hash, entry); err != nil {
				logging.Debug("could not persist stalled torrent retry state", "hash", torrent.Hash, "error", err.Error())
			} else {
				entry.persisted = true
				entry.savedState = stallPersistedState(entry)
			}
			watch[torrent.Hash] = entry
			continue
		}

		probing := !entry.probeUntil.IsZero()
		if torrent.TotalDone > entry.lastDone {
			// Only bytes received during a probe prove the swarm is back: while
			// parked the torrent is paused, so a higher count there comes from a
			// data check and merely moves the baseline.
			if probing {
				logging.Info(fmt.Sprintf("▶️ «%s» is downloading again (%s)", torrent.Name, logPercent(torrent.Progress)), "hash", torrent.Hash)
				logging.Debug("stalled torrent made byte progress",
					"hash", torrent.Hash, "recovered_bytes", torrent.TotalDone-entry.lastDone, "progress", torrent.Progress)
				torrents.ClearStalled(torrent.Hash)
				_ = db.DeleteStallWatch(torrent.Hash)
				watch[torrent.Hash] = StallWatch{lastProgressAt: now, lastDone: torrent.TotalDone}
				continue
			}
			entry.lastDone = torrent.TotalDone
		}
		stalledSince := *entry.stalledSince
		entryGiveup := giveupTimeout
		entryGiveupMinutes := giveupMinutes
		hasEntryGiveup := hasGiveup
		if deadSwarmGiveupMinutes > 0 {
			if code, _, _ := DiagnoseTorrent(&torrent); code == "dead_swarm" {
				entryGiveup = time.Duration(deadSwarmGiveupMinutes * 60.0 * float64(time.Second))
				entryGiveupMinutes = deadSwarmGiveupMinutes
				hasEntryGiveup = true
			}
		}
		if hasEntryGiveup && now.Sub(stalledSince) >= entryGiveup {
			var metadata *models.TorrentMeta
			failedTitle := ""
			if value, err := db.TorrentMeta(torrent.Hash); err == nil && value != nil {
				metadata = value
				failedTitle = value.Release.Title
			}
			restored, _ := db.RestoreUpgrade(torrent.Hash)
			_ = db.MarkTorrentError(torrent.Hash, "stalled download")
			logging.Warn(fmt.Sprintf("❌ Gave up on «%s»: stuck at %s for %s. It is removed and the next search will look for another version",
				torrent.Name, logPercent(torrent.Progress),
				logDuration(time.Duration(entryGiveupMinutes*float64(time.Minute)))),
				"title", failedTitle, "hash", torrent.Hash)
			if tev_removeFailedTorrent(torrents, torrent.Hash) {
				_ = db.MarkTorrentRemovedAt(torrent.Hash)
			}
			// 2b: after a dead-swarm give-up, retry an alternative release on the
			// next cycle instead of waiting for the gap-search throttle to expire.
			if metadata != nil && metadata.Release.Series != nil &&
				metadata.Release.Season != nil && metadata.Release.Episode != nil {
				_ = db.ClearGapSearched(*metadata.Release.Series, *metadata.Release.Season, *metadata.Release.Episode)
			}
			_ = notifier.NotifyEvent("download_failed", map[string]any{
				"hash":             torrent.Hash,
				"title":            failedTitle,
				"error":            "stalled download",
				"upgrade_restored": restored,
			})
			delete(watch, torrent.Hash)
			torrents.ClearStalled(torrent.Hash)
			_ = db.DeleteStallWatch(torrent.Hash)
			continue
		}
		switch {
		case probing && !now.Before(entry.probeUntil):
			// The probe brought no bytes: park it again until the next retry.
			if _, err := torrents.MarkStalled(torrent.Hash); err != nil {
				logging.Debug("could not park stalled torrent after its retry",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			} else {
				entry.probeUntil = time.Time{}
			}
		case probing:
			// Still probing: give the swarm time to answer the reannounce.
		case !now.Before(entry.nextRetryAt):
			value, err := torrents.Restart(torrent.Hash)
			if err != nil {
				logging.Debug("stalled torrent restart failed",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
				entry.nextRetryAt = now.Add(15 * time.Second)
			} else if value {
				next := tev_scheduleStallRetry(&entry, now, retryTimeout)
				logging.Info(fmt.Sprintf("🔁 «%s» is still stuck; looking for other users to download from again (next attempt in %s)",
					torrent.Name, logDuration(next)), "hash", torrent.Hash)
				entry.probeUntil = now.Add(probeTimeout)
			} else {
				logging.Debug("stalled torrent restart unavailable in current mode",
					"hash", torrent.Hash, "name", torrent.Name)
				entry.nextRetryAt = now.Add(15 * time.Second)
			}
		case torrent.State != "stalled":
			// After a daemon restart a parked torrent comes back merely paused, or
			// running if it was probing: park it again.
			if _, err := torrents.MarkStalled(torrent.Hash); err != nil {
				logging.Debug("could not park stalled torrent",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			}
		}
		if state := stallPersistedState(entry); !entry.persisted || state != entry.savedState {
			if err := db.SaveStallWatch(torrent.Hash, entry); err != nil {
				logging.Debug("could not persist stalled torrent retry state", "hash", torrent.Hash, "error", err.Error())
			} else {
				entry.persisted = true
				entry.savedState = state
			}
		}
		watch[torrent.Hash] = entry
	}
	for hash, entry := range watch {
		if _, ok := live[hash]; !ok {
			delete(watch, hash)
			if stallWatchPersisted(entry) {
				_ = db.DeleteStallWatch(hash)
			}
		}
	}
}

// stallWatchPersisted reports whether a monitor entry has a database row.
func stallWatchPersisted(entry StallWatch) bool {
	return entry.persisted
}

// ---------------------------------------------------------------------------
// monitor_metadata
// ---------------------------------------------------------------------------

func tev_metadataRetryWarningDue(lastWarning, now time.Time) bool {
	return lastWarning.IsZero() || now.Sub(lastWarning) >= tev_metadataWarnInterval
}

// MonitorMetadata implements `monitor_metadata`. The retry cadence and give-up
// policy are intentionally unchanged; only the repeated warning is rate-limited.
func MonitorMetadata(cfg *Config, torrents TorrentSession, db *Database, notifier *Notifier, waitStart, firstSeen, lastWarning map[string]time.Time) {
	giveupMinutes := tev_settingFloatOr(cfg, "libtorrent_metadata_giveup_min", 1440.0)
	if giveupMinutes <= 0 {
		clear(waitStart)
		clear(firstSeen)
		clear(lastWarning)
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
			delete(lastWarning, torrent.Hash)
			continue
		}
		// A paused magnet is waiting for a download slot and must not age out.
		if torrent.State == "paused" {
			delete(waitStart, torrent.Hash)
			delete(firstSeen, torrent.Hash)
			delete(lastWarning, torrent.Hash)
			continue
		}
		if torrent.State != "downloading_metadata" {
			delete(waitStart, torrent.Hash)
			delete(firstSeen, torrent.Hash)
			delete(lastWarning, torrent.Hash)
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
			logging.Warn(fmt.Sprintf("❌ Gave up on «%s»: in %s nobody provided its list of files (the link is probably dead). It is removed",
				torrent.Name, logDuration(time.Duration(giveupMinutes*float64(time.Minute)))), "hash", torrent.Hash)
			if tev_removeFailedTorrent(torrents, torrent.Hash) {
				_ = db.MarkTorrentRemovedAt(torrent.Hash)
			}
			_ = notifier.NotifyEvent("torrent_error", map[string]any{
				"hash":             torrent.Hash,
				"name":             torrent.Name,
				"title":            torrent.Name,
				"error":            "metadata timeout",
				"upgrade_restored": restored,
			})
			delete(waitStart, torrent.Hash)
			delete(firstSeen, torrent.Hash)
			delete(lastWarning, torrent.Hash)
		} else if now.Sub(retryAt) >= tev_metadataRetryInterval {
			value, err := torrents.Reannounce(torrent.Hash)
			if err != nil {
				logging.Debug("metadata reannounce failed",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
			} else if value {
				previousWarning := lastWarning[torrent.Hash]
				if tev_metadataRetryWarningDue(previousWarning, now) {
					logging.Warn(fmt.Sprintf("⏳ «%s»: after %s nobody has provided its list of files yet; still trying",
						torrent.Name, logDuration(now.Sub(started))), "hash", torrent.Hash)
					lastWarning[torrent.Hash] = now
				} else {
					logging.Debug("torrent metadata retry still pending",
						"hash", torrent.Hash,
						"name", torrent.Name,
						"elapsed_minutes", int(now.Sub(started).Minutes()))
				}
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
			delete(lastWarning, hash)
		}
	}
	for hash := range firstSeen {
		if _, ok := live[hash]; !ok {
			delete(firstSeen, hash)
			delete(lastWarning, hash)
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

// tev_seedsForever reports whether a torrent's seed policy never ends: an
// explicit per-torrent zero, or the absence of any global limit. A move that is
// deferred until "end of seed" would then never happen, so the caller should
// import the file immediately instead.
func tev_seedsForever(cfg *Config, torrents TorrentSession, hash string) bool {
	globalSet := cfg != nil && (cfg.Libtorrent.SeedRatio > 0.0 ||
		cfg.Libtorrent.SeedTimeDays > 0 || cfg.Libtorrent.SeedTimeMinutes > 0)
	for _, torrent := range torrents.List() {
		if !strings.EqualFold(torrent.Hash, hash) {
			continue
		}
		if torrent.SeedRatio >= 0.0 || torrent.SeedDays >= 0 {
			return torrent.SeedRatio == 0.0 || torrent.SeedDays == 0
		}
		return !globalSet
	}
	return !globalSet
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
	if cfg.Libtorrent.SeedRatio > 0.0 {
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
		// its file is still incomplete. Also recognize torrents already completed
		// and archived in the database whose download files were moved to NAS.
		status, _ := db.TorrentStatus(torrent.Hash)
		processed, _ := db.TorrentProcessed(torrent.Hash)
		isArchived := processed != nil && strings.TrimSpace(*processed) != ""
		isCompletedDB := status != nil && *status == "completed"
		completed := (torrent.Progress >= 99.99 && torrent.TotalSize > 0 && torrent.TotalDone >= torrent.TotalSize) || isArchived || isCompletedDB
		if !completed {
			continue
		}
		ratioReached, timeReached := tev_seedLimitsReached(cfg, &torrent)
		if !ratioReached && !timeReached && (!isArchived || torrent.State != "paused") {
			continue
		}
		archivedPack := tev_archivedPackSourceDisposable(db, torrent.Hash, torrent.SavePath)
		archivedCopy := tev_completedSourceDisposable(db, torrent.Hash, torrent.SavePath)
		source := CompletionPath(&models.TorrentEvent{
			Kind:     "torrent_finished",
			Hash:     torrent.Hash,
			Name:     torrent.Name,
			SavePath: torrent.SavePath,
		})
		sourceIsFolder := false
		if info, statErr := os.Stat(source); statErr == nil {
			sourceIsFolder = info.IsDir()
		}
		archiveFolder := archivedPack || (archivedCopy && sourceIsFolder)
		if !archivedCopy {
			continue
		}
		if !cfg.Libtorrent.AutoRemoveCompleted {
			// The user wants completed torrents to stay listed (shown as
			// completed) and to clear them manually with "Pulisci completati".
			continue
		}
		trashConfigured := cfg.TrashPath != nil && strings.TrimSpace(*cfg.TrashPath) != "" && cfg.CleanupAction != "delete"
		if archiveFolder && trashConfigured {
			removed, err := torrents.Remove(torrent.Hash, false)
			if err != nil {
				logging.Warn("seeded archived folder removal failed",
					"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
				continue
			}
			if removed {
				_ = db.MarkTorrentRemovedAt(torrent.Hash)
			}
			if target, moveErr := MoveToTrash(source, *cfg.TrashPath); moveErr != nil {
				logging.Error("could not move seeded archived folder to trash",
					"hash", torrent.Hash, "name", torrent.Name, "source", source, "error", moveErr.Error())
			} else {
				logging.Info("seeded archived folder moved to trash",
					"hash", torrent.Hash, "name", torrent.Name, "source", source, "trash", target)
			}
			continue
		}
		deleteFiles := archivedCopy
		if !archiveFolder && !settingsBool(cfg, "move_episodes", false) {
			// Copy mode keeps the download source; move mode already moved it
			// into the library at the end of the seed.
			deleteFiles = false
		}
		if archiveFolder && cfg.CleanupAction != "delete" {
			// The library copy is in place but no usable trash destination is
			// configured: never delete the only other copy. Remove the torrent
			// from the session and keep the files.
			deleteFiles = false
			logging.Warn("seeded archived folder has no trash destination; keeping the source files",
				"hash", torrent.Hash, "name", torrent.Name, "source", source)
		}
		removed, err := torrents.Remove(torrent.Hash, deleteFiles)
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
			archivePath := "path unavailable"
			archiveName := "name unavailable"
			archiveFiles := "unavailable"
			if processed, processedErr := db.TorrentProcessed(torrent.Hash); processedErr == nil && processed != nil && strings.TrimSpace(*processed) != "" {
				archivePath = *processed
				archiveName = filepath.Base(filepath.Clean(archivePath))
				if files, filesErr := VideoFiles(archivePath); filesErr == nil {
					archiveFiles = tev_archivedFileNames(files)
				}
			}
			logging.Info(fmt.Sprintf("🗑️ Seeding finished for «%s»: download copy removed, the library copy stays in %s", torrent.Name, archivePath), "hash", torrent.Hash)
			logging.Debug("season pack seeded", "folder", archiveName, "files", archiveFiles)
		} else {
			logging.Info(fmt.Sprintf("🗑️ Seeding finished for «%s»: removed from the download list", torrent.Name), "hash", torrent.Hash)
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
// seedReason, when not empty, is why seeding just ended: the relocation line
// then also announces it (one line instead of two) and clears it.
func tev_postSeedRelocate(cfg *Config, torrents TorrentSession, db *Database, torrent *models.TorrentView, postSeedMoves map[string]struct{}, storageMoveRetries map[string]StorageMoveRetry, seedReason *string) bool {
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
	// A torrent that seeded from RAM/tmp must be moved directly to its
	// configured archive destination when one exists. LibtorrentDir is only the
	// work/download directory and is the fallback for releases without an
	// archive destination. Moving to LibtorrentDir first makes the completion
	// handler scan the whole work tree (including unrelated backups) and leaves
	// the completed file outside its library.
	// A release whose library copy is already in place (a season pack or copy
	// mode import) only has to leave RAM/tmp: moving its torrent tree into the
	// archive would add a second copy as a subfolder of the series.
	destination := cfg.LibtorrentDir
	if db != nil && !tev_completedSourceDisposable(db, torrent.Hash, current) {
		if meta, err := db.TorrentMeta(torrent.Hash); err == nil && meta != nil {
			if configured, ok := ConfiguredDestinationFor(&meta.Release, cfg); ok && strings.TrimSpace(configured) != "" {
				destination = configured
			}
		}
	}
	if SamePath(current, destination) {
		return false
	}
	if err := ValidateDestinationFrom(current, destination); err != nil {
		logging.Warn("post-seeding relocation refused",
			"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
		return false
	}
	tev_clearEmptyDestination(destination, torrent.Name)
	if associated, err := tev_associateExistingPostSeedStorage(torrents, db, torrent, destination); associated || err != nil {
		if err != nil {
			logging.Warn("existing post-seed archive could not be associated",
				"hash", torrent.Hash, "name", torrent.Name, "destination", destination, "error", err.Error())
			return false
		}
		logging.Info(fmt.Sprintf("📁 %s: its library copy is already in %s", seedFinishedLead(torrent.Name, seedReason), destination), "hash", torrent.Hash)
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
	logging.Info(fmt.Sprintf("📁 %s: moving it to the library in %s (%s)",
		seedFinishedLead(torrent.Name, seedReason), destination, logging.HumanBytesI64(torrent.TotalSize)), "hash", torrent.Hash)
	logging.Debug("post-seeding relocation", "hash", torrent.Hash, "from", current)
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

// tev_associateExistingPostSeedStorage recovers a post-seed move interrupted
// after the destination was populated but before the storage_moved event was
// persisted. The normal move must not be retried in that case: libtorrent
// correctly refuses to overwrite the already existing archive copy.
func tev_associateExistingPostSeedStorage(torrents TorrentSession, db *Database, torrent *models.TorrentView, destination string) (bool, error) {
	associator, ok := torrents.(interface {
		AssociateStorage(hash, destination string) (bool, error)
	})
	if !ok || torrent == nil || torrent.TotalSize <= 0 {
		return false, nil
	}
	target := CompletionPath(&models.TorrentEvent{
		Hash:     torrent.Hash,
		Name:     torrent.Name,
		SavePath: destination,
	})
	if _, err := os.Stat(target); err != nil {
		return false, nil
	}
	size, err := SizeOfPath(target)
	if err != nil || size < torrent.TotalSize {
		return false, nil
	}
	associated, err := associator.AssociateStorage(torrent.Hash, destination)
	if err != nil {
		return false, err
	}
	if !associated {
		return false, nil
	}
	// reset_save_path deliberately does not emit libtorrent's
	// storage_moved alert. Persist the already archived payload immediately so
	// the completion history and "Pulisci completati" can see it, rather than
	// leaving the torrent forever in the unarchived state.
	if err := db.MarkTorrentCompleted(torrent.Hash, target, size); err != nil {
		return false, err
	}
	return true, nil
}

// tev_storageMoveAlreadySatisfied reports whether a reported storage-move
// failure is moot because the payload already sits in `destination`. It is true
// when libtorrent already updated the save path, or when the complete payload
// is present there (the archive import won the race). In the second case it
// also re-associates the storage, because a move that libtorrent reports as
// failed must not be retried onto the existing copy.
func tev_storageMoveAlreadySatisfied(torrents TorrentSession, db *Database, hash, destination string) bool {
	for _, item := range torrents.List() {
		if !strings.EqualFold(item.Hash, hash) {
			continue
		}
		torrent := item
		if SamePath(torrent.SavePath, destination) {
			return true
		}
		associated, err := tev_associateExistingPostSeedStorage(torrents, db, &torrent, destination)
		return err == nil && associated
	}
	return false
}

// bg_torrentNeedsArchiveImport reports whether a completed single has already
// been moved into its archive destination but the import phase (rename,
// MediaInfo probe and completion notification) never ran. That happens when the
// end-of-seed storage move reached the archive after its `storage_moved` alert
// was lost (restart) or before the event was consumed. Such a torrent must be
// finalized through the normal storage_moved path instead of being marked
// complete with the raw archive path: the latter used to skip the rename and the
// completion notification (Marshals regression).
func bg_torrentNeedsArchiveImport(cfg *Config, db *Database, torrent *models.TorrentView) bool {
	if cfg == nil || db == nil || torrent == nil {
		return false
	}
	if !torrent.HasMetadata || torrent.TotalSize <= 0 {
		return false
	}
	status, err := db.TorrentStatus(torrent.Hash)
	if err != nil || status == nil || *status != "completed" {
		return false
	}
	processed, err := db.TorrentProcessed(torrent.Hash)
	if err != nil || (processed != nil && strings.TrimSpace(*processed) != "") {
		return false
	}
	meta, err := db.TorrentMeta(torrent.Hash)
	if err != nil || meta == nil {
		return false
	}
	destination, ok := ConfiguredDestinationFor(&meta.Release, cfg)
	if !ok || strings.TrimSpace(destination) == "" || !SamePath(torrent.SavePath, destination) {
		return false
	}
	target := CompletionPath(&models.TorrentEvent{
		Kind:     "storage_moved",
		Hash:     torrent.Hash,
		Name:     torrent.Name,
		SavePath: torrent.SavePath,
	})
	info, statErr := os.Stat(target)
	if statErr != nil {
		return false
	}
	if info.IsDir() {
		if !tevDirectoryHasRegularFile(target) {
			return false
		}
	} else if info.Size() <= 0 {
		return false
	}
	return true
}

// tev_moveAlreadyRunning recognises the engine refusing a move because the
// same torrent is already being moved: the relocation is under way, not failed.
func tev_moveAlreadyRunning(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already being moved")
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

// tev_ramdiskAdmission decides whether a torrent staged off the RAM disk because
// its size was unknown should now be moved onto it: the metadata handler calls
// it once the real size is known. It is the inverse of tev_ramdiskRelocation
// and, like it, depends only on the configuration and the engine-agnostic
// TorrentSession — never on the download daemon.
func tev_ramdiskAdmission(cfg *Config, torrents TorrentSession, event *models.TorrentEvent) (string, bool) {
	if !engineSupportsRamdisk(torrents) || !cfg.RamdiskEnabled() {
		return "", false
	}
	ramdisk := cfg.RamdiskDir()
	if ramdisk == nil {
		return "", false
	}
	var torrent models.TorrentView
	found := false
	for _, candidate := range torrents.List() {
		if strings.EqualFold(candidate.Hash, event.Hash) {
			torrent = candidate
			found = true
			break
		}
	}
	if !found || torrent.TotalSize <= 0 {
		return "", false
	}
	if PathOnRamdisk(torrent.SavePath, *ramdisk) {
		// Already there: tev_ramdiskRelocation owns the other direction.
		return "", false
	}
	// This is the metadata decision point, before the payload is written. If a
	// few pieces slipped in, moving is still cheap; a transfer that has really
	// started is left where it is.
	if torrent.TotalDone > 0 && torrent.Progress >= ramdiskAdmitMaxProgress {
		return "", false
	}
	free := FreeSpaceBytes(*ramdisk)
	if free == nil {
		return "", false
	}
	uncommitted := torrents.RamdiskUncommittedBytes(*ramdisk, event.Hash)
	// The whole payload ends up on the RAM disk, so require room for all of it
	// (not just the bytes still missing): the already-written part is copied.
	if err := RamdiskFits(cfg.RamdiskThresholdBytes(), cfg.RamdiskMarginBytes(), *free, uncommitted, uint64(torrent.TotalSize)); err != nil {
		return "", false
	}
	return *ramdisk, true
}

// tev_admitToRamdisk moves a torrent onto the RAM disk once its metadata shows
// that it fits.
func tev_admitToRamdisk(cfg *Config, torrents TorrentSession, event *models.TorrentEvent) {
	destination, ok := tev_ramdiskAdmission(cfg, torrents, event)
	if !ok {
		return
	}
	moved, err := torrents.MoveStorage(event.Hash, destination)
	if err != nil && tev_moveAlreadyRunning(err) {
		logging.Debug("RAM disk admission: move already in progress", "name", event.Name)
	} else if err != nil {
		logging.Warn("RAM disk admission failed",
			"hash", event.Hash, "name", event.Name, "error", err.Error())
	} else if moved {
		logging.Info(fmt.Sprintf("💾 «%s» moved onto the RAM disk", event.Name), "hash", event.Hash)
	} else {
		logging.Debug("RAM disk admission was not applied", "hash", event.Hash, "name", event.Name)
	}
}

// tev_enforceRamdiskCapacity implements `enforce_ramdisk_capacity`.
func tev_enforceRamdiskCapacity(cfg *Config, torrents TorrentSession, event *models.TorrentEvent) {
	reason, destination, ok := tev_ramdiskRelocation(cfg, torrents, event.Hash, event.SavePath)
	if !ok {
		return
	}
	moved, err := torrents.MoveStorage(event.Hash, destination)
	if err != nil && tev_moveAlreadyRunning(err) {
		logging.Debug("RAM disk relocation: move already in progress", "name", event.Name)
	} else if err != nil {
		logging.Warn("RAM disk relocation failed",
			"hash", event.Hash, "name", event.Name, "error", err.Error())
	} else if moved {
		logging.Info(fmt.Sprintf("💾 «%s» moved from the RAM disk to %s: %s", event.Name, destination, reason), "hash", event.Hash)
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
		if err != nil && tev_moveAlreadyRunning(err) {
			logging.Debug("RAM disk reconciliation: move already in progress", "name", torrent.Name)
		} else if err != nil {
			logging.Warn("RAM disk reconciliation failed",
				"hash", hash, "name", torrent.Name, "error", err.Error())
		} else if value {
			logging.Info(fmt.Sprintf("💾 «%s» moved from the RAM disk to %s: %s", torrent.Name, destination, reason), "hash", torrent.Hash)
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
	if cfg.Libtorrent.SeedRatio > 0.0 {
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
			logging.Info(fmt.Sprintf("🔁 «%s» is sharing again (set to seed without limits)", torrent.Name), "hash", torrent.Hash)
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
		// Announced together with the move to the library when one follows,
		// otherwise on its own below.
		seedReason := ""
		if !stopped {
			value, err := torrents.Pause(torrent.Hash)
			if err != nil {
				if torrentNotFoundError(err) {
					// The torrent vanished between the listing and the pause
					// (already removed elsewhere): nothing to stop, not a warning.
					logging.Debug("seed-limit pause skipped: torrent already removed from the session",
						"hash", torrent.Hash, "name", torrent.Name)
				} else {
					logging.Warn("failed to stop torrent at seed limit",
						"hash", torrent.Hash, "name", torrent.Name, "error", err.Error())
				}
			} else if value {
				stopped = true
				reason := "seed time reached"
				if ratioReached && timeReached {
					reason = "ratio and time reached"
				} else if ratioReached {
					reason = "ratio reached"
				}
				seedReason = reason
			} else {
				logging.Warn("torrent seed limit could not be applied in current mode",
					"hash", torrent.Hash, "name", torrent.Name)
			}
		}
		if !stopped {
			continue
		}
		if tev_postSeedRelocate(cfg, torrents, db, &torrent, postSeedMoves, retries, &seedReason) {
			if seedReason != "" {
				logging.Info(fmt.Sprintf("⏸️ %s", seedFinishedLead(torrent.Name, &seedReason)), "hash", torrent.Hash)
			}
			continue
		}
		// Archive at the end of the seed: a completed torrent that kept its
		// source for seeding is moved into its library destination now, then
		// renamed by the storage_moved handler. Removal happens after the move,
		// so the file is never deleted before it is archived. `postSeedMoves`
		// protects the source while libtorrent copies it, and the guard on
		// `completed_source_disposable` skips torrents whose library copy is
		// already in place (copy mode / folder and pack import).
		if _, archiving := postSeedMoves[torrent.Hash]; !archiving {
			if _, retrying := retries[strings.ToLower(torrent.Hash)]; !retrying {
				if !tev_completedSourceDisposable(db, torrent.Hash, torrent.SavePath) {
					if meta, metaErr := db.TorrentMeta(torrent.Hash); metaErr == nil && meta != nil {
						release := meta.Release
						if dest, ok := ConfiguredDestinationFor(&release, cfg); ok && !SamePath(torrent.SavePath, dest) {
							if moved, moveErr := torrents.MoveStorage(torrent.Hash, dest); moveErr == nil && moved {
								postSeedMoves[torrent.Hash] = struct{}{}
								retries[strings.ToLower(torrent.Hash)] = StorageMoveRetry{
									destination: dest,
									postSeed:    true,
									attempts:    0,
									nextAttempt: time.Now().Add(600 * time.Second),
									inFlight:    true,
								}
								logging.Info(fmt.Sprintf("📁 %s: moving it to the library in %s (%s)",
									seedFinishedLead(torrent.Name, &seedReason), dest, logging.HumanBytesI64(torrent.TotalSize)), "hash", torrent.Hash)
								logging.Debug("post-seed archive move", "hash", torrent.Hash, "from", torrent.SavePath)
								continue
							}
						}
					}
				}
			}
		}
		if seedReason != "" {
			logging.Info(fmt.Sprintf("⏸️ %s", seedFinishedLead(torrent.Name, &seedReason)), "hash", torrent.Hash)
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
	if cfg.TrashPath != nil && cfg.CleanupAction != "delete" {
		target, err := MoveToTrash(source, *cfg.TrashPath)
		if err != nil {
			logging.Error("could not move rejected completed download to trash",
				"hash", event.Hash, "name", event.Name, "source", source, "error", err.Error())
		} else {
			logging.Info(fmt.Sprintf("rejected completed download moved to trash — «%s» · %s → %s", event.Name, source, target), "hash", event.Hash)
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
		logging.Info(fmt.Sprintf("rejected completed download removed — «%s» · %s", event.Name, source), "hash", event.Hash)
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

// tev_resolveEpisodeCompletedFile resolves a single episode stored below a
// torrent directory. It is intentionally strict when there are several video
// files: the wrong file must never be moved into the series archive.
func tev_resolveEpisodeCompletedFile(path string, release *models.Release) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat completed episode path %s: %w", path, err)
	}
	if !info.IsDir() {
		return path, nil
	}
	files, err := VideoFiles(path)
	if err != nil {
		return "", fmt.Errorf("scan completed episode folder %s: %w", path, err)
	}
	if len(files) == 1 {
		return files[0], nil
	}
	if release.Season != nil && release.Episode != nil {
		matches := make([]string, 0, len(files))
		for _, file := range files {
			if filenameMatchesEpisode(filepath.Base(file), *release.Season, *release.Episode) {
				matches = append(matches, file)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf("completed episode folder %s contains %d ambiguous video files", path, len(files))
}

// tev_completeEpisodeFolderWithArchive copies only the episode video to the
// configured series archive, for both a single episode file and a folder that
// contains it. The source stays in the download folder while the torrent seeds;
// RemoveSeededCompleted drops the torrent after the configured seed limit when
// automatic removal is enabled, and keeps the source in copy mode.
func tev_completeEpisodeFolderWithArchive(cfg *Config, db *Database, torrents TorrentSession, event *models.TorrentEvent, release *models.Release, destination string, tmdb *TmdbClient) (bool, error) {
	source := CompletionPath(event)
	video, err := tev_resolveEpisodeCompletedFile(source, release)
	if err != nil {
		return false, err
	}
	if err := ValidateDestinationFrom(video, destination); err != nil {
		return false, err
	}
	target := filepath.Join(destination, filepath.Base(video))
	sourceHandled := false
	sourcePreserved := false
	linked := false
	seriesName := ""
	if release.Series != nil {
		seriesName = *release.Series
	}
	if resolved, resolveErr := ResolveExistingTarget(video, target, cfg.ReleaseScore(release), cfg, "series", seriesName); resolveErr != nil {
		return false, resolveErr
	} else if resolved {
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
	} else {
		value, err := linkOrCopyFile(cfg, video, target)
		if err != nil {
			return false, err
		}
		linked = value
		sourceHandled = true
		sourcePreserved = true
	}
	processedEvent := *event
	processedEvent.SavePath = destination
	processedEvent.Name = filepath.Base(target)
	processed, err := tev_completeTorrentOptions(cfg, db, torrents, &processedEvent, release, tmdb, sourcePreserved)
	if err != nil {
		return false, err
	}
	if processed && sourceHandled {
		if linked {
			logging.Info(fmt.Sprintf("📁 «%s» linked into the library (hardlink, no extra space); the download stays to keep sharing it", event.Name),
				"library_file", target, "hash", event.Hash)
		} else {
			logging.Info(fmt.Sprintf("📁 «%s» copied to the library; the download stays to keep sharing it", event.Name),
				"library_file", target, "hash", event.Hash)
		}
	}
	return processed, nil
}

func tev_completeTorrent(cfg *Config, db *Database, torrents TorrentSession, event *models.TorrentEvent, release *models.Release, tmdb *TmdbClient) (bool, error) {
	return tev_completeTorrentOptions(cfg, db, torrents, event, release, tmdb, false)
}

// tevSeedingCompletionAlreadyRecorded suppresses duplicate finished alerts for
// a single episode deliberately retained in its download directory while it
// seeds. It does not apply to storage_moved, which is the later event that
// imports the file into the archive at the end of seeding.
func tevSeedingCompletionAlreadyRecorded(db *Database, hash string) bool {
	status, err := db.TorrentStatus(hash)
	if err != nil || status == nil || *status != "completed" {
		return false
	}
	processed, err := db.TorrentProcessed(hash)
	return err == nil && (processed == nil || strings.TrimSpace(*processed) == "")
}

// tev_notifySeeding announces a completed download that is now seeding from the
// download folder and will be moved into the library at the end of the seed.
// The same `torrent_completed` event is used, flagged with `seeding` so the
// message differs from the final "archived" notification.
func tev_notifySeeding(db *Database, notifier *Notifier, event *models.TorrentEvent, release *models.Release, path string, sizeBytes int64) {
	if notifier == nil || event == nil {
		return
	}
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
	var kindValue, seriesValue, seasonValue, episodeValue any
	title := event.Name
	if release != nil {
		kindValue = release.Kind
		title = release.Title
		if release.Series != nil {
			seriesValue = *release.Series
		}
		if release.Season != nil {
			seasonValue = *release.Season
		}
		if release.Episode != nil {
			episodeValue = *release.Episode
		}
	}
	if err := notifier.NotifyEvent("torrent_completed", map[string]any{
		"hash":              event.Hash,
		"name":              event.Name,
		"title":             title,
		"kind":              kindValue,
		"series":            seriesValue,
		"season":            seasonValue,
		"episode":           episodeValue,
		"path":              path,
		"size_bytes":        sizeBytes,
		"duration_seconds":  durationSeconds,
		"average_speed_bps": averageSpeedBps,
		"seeding":           true,
	}); err != nil {
		logging.Warn("seeding notification failed", "hash", event.Hash, "event", "torrent_completed", "title", title, "error", err)
	} else {
		logging.Debug("seeding notification sent", "hash", event.Hash, "title", title)
	}
}

// tev_markReleaseCompleted records a completed release. A season pack is
// recorded file by file: MarkReleaseCompleted would give every episode of the
// season the pack folder as its archive path.
func tev_markReleaseCompleted(cfg *Config, db *Database, release *models.Release, path string, size int64) error {
	if release.Kind != "series" || !release.IsPack {
		return db.MarkReleaseCompleted(release, path, size)
	}
	matching, err := MatchingPackFiles(path, release)
	if err != nil {
		return err
	}
	entries := make([]PackEpisode, 0, len(matching))
	for _, file := range matching {
		fileSize, err := SizeOfPath(file.Path)
		if err != nil {
			return err
		}
		entries = append(entries, PackEpisode{
			Episode:   file.Episode,
			Path:      file.Path,
			SizeBytes: fileSize,
			Score:     cfg.ReleaseScore(release),
		})
	}
	return db.MarkPackCompleted(release, entries, path, size)
}

// tev_completeTorrentOptions controls whether the original torrent source must
// remain available for seeding. Single episodes are imported by copying their
// video to the library, so they use preserveSource=true.
// logLibraryImport records the archive/import phase of a completed release. When
// the release superseded existing files it emits a single "updated" line that
// also names the replaced file(s), instead of the import line plus one
// "replaced" line per removed file.
func logLibraryImport(cfg *Config, hash, target string, size int64, path, suffix string, replaced []string) {
	if len(replaced) == 0 {
		logging.Info(fmt.Sprintf("📁 %s added to the library (%s): %s%s",
			target, logging.HumanBytesI64(size), path, suffix), "hash", hash)
		return
	}
	action := "moved to the trash"
	if cfg.CleanupAction == "delete" {
		action = "deleted"
	}
	names := make([]string, len(replaced))
	for i, file := range replaced {
		names[i] = "«" + filepath.Base(file) + "»"
	}
	logging.Info(fmt.Sprintf("♻️ %s updated (%s): %s · %s %s %s",
		target, logging.HumanBytesI64(size), path,
		plural(len(replaced), "previous version", "previous versions"),
		strings.Join(names, ", "), action), "hash", hash)
}

func tev_completeTorrentOptions(cfg *Config, db *Database, torrents TorrentSession, event *models.TorrentEvent, release *models.Release, tmdb *TmdbClient, preserveSource bool) (bool, error) {
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
		logging.Debug("completed file already renamed — using the archived file",
			"name", event.Name, "title", release.Title, "path", resolved)
		path = resolved
		recoveredExisting = true
	}
	if release.Kind == "movie" {
		resolved, resolveErr := resolveMovieCompletedFile(path)
		if resolveErr != nil {
			return false, resolveErr
		}
		if !SamePath(resolved, path) {
			logging.Debug("completed movie folder resolved to its video file",
				"hash", event.Hash,
				"folder", path,
				"video", resolved,
			)
			path = resolved
		}
	}
	// Refuse to archive a broken download: a zero-filled (preallocated but
	// never written) or unrecognised video file must not replace a good copy.
	if err := validateCompletedFile(path); err != nil {
		quarantined := quarantineCorruptFile(path, cfg)
		logging.Error("completed file failed integrity validation; not archiving",
			"name", event.Name, "path", path, "error", err.Error(), "quarantined", quarantined)
		return false, fmt.Errorf("integrity validation failed: %w", err)
	}
	logging.Debug("completed file passed integrity check", "name", event.Name, "path", path)
	size, err := SizeOfPath(path)
	if err != nil {
		return false, err
	}
	noRename, _ := db.TorrentNoRename(event.Hash)
	renamed := ""
	renamedSet := false
	discarded := false
	if noRename {
		logging.Info(fmt.Sprintf("rename skipped — torrent marked no-rename: «%s»", event.Name),
			"name", event.Name, "title", release.Title, "hash", event.Hash)
	} else if release.Kind == "movie" {
		value, err := RenameMovie(context.Background(), path, release, cfg, tmdb)
		if errors.Is(err, ErrInferiorDuplicate) {
			discarded = true
		} else if err != nil {
			return false, err
		} else if value != "" {
			renamed = value
			renamedSet = true
		}
	} else {
		value, err := RenameEpisode(context.Background(), path, release, cfg, tmdb)
		if errors.Is(err, ErrInferiorDuplicate) {
			discarded = true
		} else if err != nil {
			return false, err
		} else if value != "" {
			renamed = value
			renamedSet = true
		}
	}
	processedPath := path
	if renamedSet {
		processedPath = renamed
	}
	// Files the new release supersedes, to log a single upgrade line.
	var replaced []string
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
				value, err := DiscardIfInferiorWithQuality(cfg, *release.Series, *release.Season, *release.Episode, score, newFile, archive, &release.Quality)
				if err != nil {
					return false, err
				}
				if value {
					discarded = true
				} else {
					replaced, err = cleanupOldEpisode(cfg, *release.Series, *release.Season, *release.Episode, score, newFile, archive, &release.Quality)
					if err != nil {
						return false, err
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
			} else {
				replaced, err = cleanupOldMovie(cfg, release.Title, release.Year, score, newFile, archive)
				if err != nil {
					return false, err
				}
			}
		}
	}
	// A movie that arrived inside a torrent folder is moved up into the archive
	// root as a flat file (with its subtitles and artwork) unless the operator
	// disabled `movies_flat_files`.
	if release.Kind == "movie" && !discarded {
		flat, flatErr := flattenMovieFolder(cfg, release, processedPath)
		if flatErr != nil {
			logging.Warn("movie folder flatten failed",
				"name", event.Name, "path", processedPath, "error", flatErr.Error())
		} else if !SamePath(flat, processedPath) {
			processedPath = flat
			renamedSet = true
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
	if err := tev_markReleaseCompleted(cfg, db, release, processedPath, size); err != nil {
		return false, err
	}
	suffix := " (kept original name)"
	if renamedSet {
		suffix = ""
	}
	// Download completion is logged when the payload first reaches 100%. This
	// later handler records the separate archive/import phase (often after a
	// seed period), so the two lifecycle messages retain their real order. When
	// the release replaced existing files, one combined "updated" line is
	// emitted instead of the import line plus one line per removed file.
	logLibraryImport(cfg, event.Hash, logTarget(release), size, processedPath, suffix, replaced)
	if !preserveSource && cfg.Libtorrent.AutoRemoveCompleted && (recoveredExisting || (renamedSet && !SamePath(processedPath, path))) {
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

// tev_logDownloadComplete logs the moment the torrent payload becomes complete,
// before a deferred seeding relocation can start. In move mode the final
// library path does not exist yet, so it deliberately reports the seeding path
// rather than claiming that the archive import already happened.
func tev_logDownloadComplete(db *Database, event *models.TorrentEvent, release *models.Release, path string, size int64) {
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
	logging.Info(fmt.Sprintf("🎉 %s downloaded (%s in %s, %s on average). It stays in the download folder while it is shared, then moves to the library",
		logTarget(release),
		logging.HumanBytesI64(size),
		logDuration(time.Duration(durationSeconds)*time.Second),
		logging.HumanRate(averageSpeed),
	), "folder", path, "hash", event.Hash)
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
	case "hash_failed":
		// A damaged piece is discarded and downloaded again automatically:
		// routine, and alarming if reported one by one. Only a torrent that
		// keeps receiving damaged data is worth a warning in the log; it is
		// self-healing, so it never sends a Telegram/e-mail notification.
		failures, alert := recordHashFailure(event.Hash)
		logging.Debug("piece failed its hash check; it will be downloaded again",
			"name", event.Name, "failures", failures, "message", event.Message)
		if alert {
			logging.Warn(fmt.Sprintf("⚠️ «%s» keeps receiving damaged data (%d pieces so far). Gextto downloads them again automatically, but if it never finishes, remove it and choose another version",
				event.Name, failures), "hash", event.Hash)
		}
		return false, nil
	case "resume_save_failed":
		// The progress is saved again at the next periodic save and on a
		// clean shutdown (which reports its own failure): not worth a warning.
		logging.Debug("torrent resume data could not be saved", "name", event.Name, "message", event.Message)
		return false, nil
	case "torrent_error", "file_error", "metadata_failed":
		if !recordTorrentErrorNotice(event.Hash, event.Kind, time.Now()) {
			logging.Debug("repeated torrent error; already reported",
				"name", event.Name, "kind", event.Kind, "message", event.Message)
			return false, nil
		}
		logging.Warn(fmt.Sprintf("⚠️ Problem with «%s»: %s", event.Name, torrentErrorText(event.Kind, event.Message)), "hash", event.Hash)
		_ = notifier.NotifyEvent("torrent_error", map[string]any{
			"hash":    event.Hash,
			"kind":    event.Kind,
			"name":    event.Name,
			"error":   event.Message,
			"message": event.Message,
		})
		return false, nil
	case "portmap_error", "session_error":
		if event.Kind == "portmap_error" {
			logging.Warn("⚠️ The router did not open the listening port automatically (UPnP/NAT-PMP); downloads work but may find fewer users",
				"detail", event.Message)
		} else {
			logging.Warn("⚠️ Torrent engine problem: " + event.Message)
		}
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
			firstCompletion, err := db.MarkTorrentCompletedUnarchived(event.Hash, event.Name)
			if err != nil {
				logging.Debug("cannot mark foreign torrent completed",
					"hash", event.Hash, "error", err.Error())
				return false, err
			}
			if firstCompletion {
				path := CompletionPath(&event)
				sizeBytes, _ := SizeOfPath(path)
				if err := notifier.NotifyEvent("torrent_completed", map[string]any{
					"hash":       event.Hash,
					"name":       event.Name,
					"title":      event.Name,
					"path":       path,
					"size_bytes": sizeBytes,
					"manual":     true,
				}); err != nil {
					logging.Warn("manual torrent completion notification failed",
						"hash", event.Hash, "title", event.Name, "error", err)
				} else {
					logging.Debug("manual torrent completion notification sent",
						"hash", event.Hash, "title", event.Name)
				}
			}
			// A foreign torrent can emit this lifecycle alert again after a
			// session refresh/restart. Completion is not an actionable change for
			// the user; the INFO log is reserved for the later removal from the
			// sharing session.
			logging.Debug(fmt.Sprintf("torrent completed (no release metadata, kept in place) — «%s»", event.Name))
		}
		logging.Debug("torrent alert has no registered release metadata",
			"hash", event.Hash, "kind", event.Kind, "name", event.Name)
		return false, nil
	}
	switch event.Kind {
	// A release whose size is unknown at `add()` is staged off the RAM disk;
	// once metadata arrives the real size decides: it is moved onto the RAM
	// disk when it fits, and out of it when it no longer does.
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
				logging.Debug(fmt.Sprintf("📦 Download metadata received — «%s» · %s (torrent: %s)",
					metadata.Release.Title, logging.HumanBytesI64(torrent.TotalSize), torrent.Name))
				break
			}
		}
		tev_enforceRamdiskCapacity(cfg, torrents, &event)
		tev_admitToRamdisk(cfg, torrents, &event)
		return false, nil
	case "torrent_finished":
		// Completion can be triggered twice (a recovered event after a
		// restart, or storage_moved): never post-process an already archived
		// release again. A "completed" row whose file is missing is re-processed
		// instead of being skipped forever.
		if tevIgnoreRepeatedCompletion(db, event.Hash) {
			return false, nil
		}
		if tevSeedingCompletionAlreadyRecorded(db, event.Hash) {
			logging.Debug("ignoring duplicate completion while single episode is seeding", "name", event.Name)
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
		archiveDestination, hasArchiveDestination := ConfiguredDestinationFor(&release, cfg)
		destination, _ := DestinationFor(&release, cfg)
		current := event.SavePath
		if release.Kind == "series" && !release.IsPack {
			if sourceInfo, sourceErr := os.Stat(source); sourceErr == nil {
				if !hasArchiveDestination {
					size, sizeErr := SizeOfPath(source)
					if sizeErr != nil {
						return false, sizeErr
					}
					if err := db.MarkReleaseCompleted(&release, source, size); err != nil {
						return false, err
					}
					logging.Info(fmt.Sprintf("🎉 «%s» downloaded; no library folder is set for it, so it stays in %s", event.Name, source), "hash", event.Hash)
					return true, nil
				}
				if sourceInfo.IsDir() || !settingsBool(cfg, "move_episodes", false) || tev_seedsForever(cfg, torrents, event.Hash) {
					// Folder episodes, single files in copy mode, and any torrent
					// configured to seed forever are imported by copying the video
					// while the source stays in the download folder for seeding.
					return tev_completeEpisodeFolderWithArchive(cfg, db, torrents, &event, &release, archiveDestination, tmdb)
				}
				// Move mode: keep the source in the download folder so the torrent
				// can actually seed; the move and rename into the library happen at
				// the end of the seed (see EnforceSeedPolicy). The episode is marked
				// downloaded now (without an archive path, so the library still looks
				// empty) so the gap filler does not fetch it again while it seeds.
				// When the archive destination IS the download folder there is
				// nothing to move: fall through to the in-place rename below.
				if !SamePath(filepath.Dir(source), archiveDestination) {
					size, sizeErr := SizeOfPath(source)
					if sizeErr != nil {
						return false, sizeErr
					}
					if err := db.MarkReleaseCompleted(&release, "", size); err != nil {
						return false, err
					}
					tev_logDownloadComplete(db, &event, &release, source, size)
					tev_notifySeeding(db, notifier, &event, &release, source, size)
					return false, nil
				}
			}
		}
		if release.IsPack {
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
			if !hasArchiveDestination {
				entries := make([]PackEpisode, 0, len(matching))
				for _, file := range matching {
					fileSize, sizeErr := SizeOfPath(file.Path)
					if sizeErr != nil {
						return false, sizeErr
					}
					entries = append(entries, PackEpisode{
						Episode:   file.Episode,
						Path:      file.Path,
						SizeBytes: fileSize,
						Score:     cfg.ReleaseScore(&release),
					})
				}
				if err := db.MarkPackCompleted(&release, entries, source, size); err != nil {
					return false, err
				}
				logging.Info(fmt.Sprintf("🎉 «%s» downloaded (%s); no library folder is set for it, so it stays in %s",
					event.Name, countLabel(len(entries), "episode", "episodes"), source), "hash", event.Hash)
				return true, nil
			}
			// Serialize against the periodic/manual rename repair for this
			// series: it scans the archive and would otherwise rename/trash
			// files while they are still being copied.
			guard := AcquireArchiveImport(release.Series)
			defer guard.Release()
			processed := []PackFileResult{}
			// Enumerate the destination once and reuse it for every episode:
			// each episode's "is there a better existing file?" check walks the
			// directory, and a full-season pack would otherwise re-walk it N
			// times on the (possibly slow) archive mount.
			destinationFiles, _ := VideoFiles(destination)
			for index := range matching {
				file := matching[index]
				placed, ok, err := StagePackFile(&file, source, destination, destinationFiles, cfg, cfg.ReleaseScore(&release))
				if err != nil {
					return false, err
				}
				if !ok {
					processed = append(processed, PackFileResult{
						Episode:   file.Episode,
						Path:      file.Path,
						Discarded: true,
					})
					continue
				}
				partial, err := ProcessPackFiles(context.Background(), []PackInput{{Path: placed, Source: file}}, &release, cfg, tmdb)
				if err != nil {
					return false, err
				}
				processed = append(processed, partial...)
			}
			allDiscarded := tev_allPackFilesDiscarded(processed)
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
				logging.Warn(fmt.Sprintf("🗑️ Season pack rejected — «%s» · no episodes kept · discarded %d: %s",
					release.Title, len(processed), tev_packFileNames(processed)), "hash", event.Hash)
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
			logging.Info(fmt.Sprintf("🎉 %s complete: %s (%s) added to the library in %s",
				logTarget(&release), countLabel(len(episodes), "episode", "episodes"), logging.HumanBytesI64(size), destination), "hash", event.Hash)
			// Detail per file: which episodes were kept and which were discarded.
			// The Python original logged this per file; the Rust port only kept
			// the counts, which made a rejected episode impossible to trace.
			keptDetail := []PackFileResult{}
			discardedDetail := []PackFileResult{}
			upgradedDetail := []PackFileResult{}
			trashCount := 0
			for _, item := range processed {
				if item.Discarded {
					discardedDetail = append(discardedDetail, item)
				} else {
					keptDetail = append(keptDetail, item)
					if item.Upgrade {
						upgradedDetail = append(upgradedDetail, item)
					}
					trashCount += item.TrashCount
				}
			}
			cleanupLabel := "moved to trash"
			if cfg.CleanupAction == "delete" {
				cleanupLabel = "deleted"
			}
			logging.Debug(fmt.Sprintf("season pack detail — kept %d: %s · upgrades %d: %s · %s: %d file(s) · discarded %d: %s",
				len(keptDetail), tev_packFileNames(keptDetail),
				len(upgradedDetail), tev_packFileNames(upgradedDetail), cleanupLabel, trashCount,
				len(discardedDetail), tev_packFileNames(discardedDetail)))
			discardedCount := len(discardedDetail)
			discardedList := []any{}
			for _, item := range discardedDetail {
				discardedList = append(discardedList, map[string]any{
					"episode": item.Episode,
					"path":    item.Path,
					"name":    filepath.Base(item.Path),
				})
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
				"discarded":       discardedList,
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
			logging.Debug(fmt.Sprintf("season pack copied to NAS, source kept for seeding · %s · %s",
				logging.HumanBytesI64(size), destination))
			return true, nil
		}
		if SamePath(current, destination) {
			return tev_completeTorrent(cfg, db, torrents, &event, &release, tmdb)
		}
		// Movies are always relocated to the configured destination (or to the
		// default download volume when no archive is set); unlike episodes they
		// do not have a separate copy-mode deferral.
		if release.Kind == "movie" && hasArchiveDestination && settingsBool(cfg, "move_episodes", false) {
			// Move mode: keep the movie in the download folder so the torrent can
			// seed; the move and rename into the library happen at the end of the
			// seed (see EnforceSeedPolicy). The release is marked downloaded now
			// (without an archive path) so the gap filler does not fetch it again.
			size, sizeErr := SizeOfPath(source)
			if sizeErr != nil {
				return false, sizeErr
			}
			if err := db.MarkReleaseCompleted(&release, "", size); err != nil {
				return false, err
			}
			tev_notifySeeding(db, notifier, &event, &release, source, size)
			logging.Info(fmt.Sprintf("🎉 %s downloaded. It stays in the download folder while it is shared, then moves to the library", logTarget(&release)),
				"folder", source, "hash", event.Hash)
			return false, nil
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
		// A move that fails once the payload is already in the destination is
		// not a real failure: the post-seed relocation and the archive import
		// race, so libtorrent refuses to overwrite the archived copy. Treat it
		// as done instead of warning and scheduling a retry that can only fail
		// again. The rename itself already ran on storage_moved.
		if destination != nil && tev_storageMoveAlreadySatisfied(torrents, db, event.Hash, *destination) {
			logging.Debug("storage move already satisfied; not retrying",
				"hash", event.Hash, "name", event.Name, "save_path", event.SavePath)
			delete(retries, strings.ToLower(event.Hash))
			delete(postSeedMoves, event.Hash)
			return false, nil
		}
		logging.Warn(fmt.Sprintf("⚠️ Could not move «%s» to the library; it stays where it is and will be retried", event.Name),
			"folder", event.SavePath, "error", strings.TrimSpace(event.Message), "hash", event.Hash)
		if destination != nil {
			tev_scheduleStorageMoveRetry(retries, event.Hash, *destination, wasPostSeedMove, time.Now())
			if wasPostSeedMove {
				postSeedMoves[event.Hash] = struct{}{}
			}
		}
		return false, nil
	case "storage_moved":
		logging.Debug("torrent storage move completed",
			"hash", event.Hash, "name", event.Name, "save_path", event.SavePath)
		// Save the new location now instead of at the next periodic save: if
		// Gextto stopped uncleanly in between, libtorrent would restore the
		// torrent at its old path, recreate empty files there and download
		// again what is already in the library.
		if saver, ok := torrents.(interface{ RequestResumeSave() int }); ok {
			saver.RequestResumeSave()
		}
		delete(postSeedMoves, event.Hash)
		delete(retries, strings.ToLower(event.Hash))
		// A post-seeding relocation happens after the release was already
		// committed. Do not run rename/copy/pack processing a second time, but
		// do re-process a "completed" torrent whose archived copy is missing.
		if tevIgnoreRepeatedCompletion(db, event.Hash) {
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
		isCompletedDB := false
		if st, _ := db.TorrentStatus(event.Hash); st != nil && *st == "completed" {
			isCompletedDB = true
		}
		done := isCompletedDB || (current != nil && current.Progress >= 99.99 && current.TotalSize > 0 && current.TotalDone >= current.TotalSize)
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

// tev_allPackFilesDiscarded is true when staging/post-processing retained no
// importable episode. An empty result is discarded too: marking such a pack
// completed would suppress its gaps even though no library file was kept.
func tev_allPackFilesDiscarded(processed []PackFileResult) bool {
	for _, item := range processed {
		if !item.Discarded {
			return false
		}
	}
	return true
}
