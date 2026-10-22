package gextto

// ed2k_downloads.go follows aMule's download queue: it lists the files for
// the Scarico page, runs pause/resume/remove, adds ed2k:// links and turns a
// completed file into the same "torrent_finished" event a torrent produces,
// so renaming, archiving, MediaInfo checks and notifications are shared.
//
// The files are not part of the torrent engine: they are shown next to the
// torrents, like the HTTP downloads, and their actions go straight to aMule.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// ed2kQueueCache keeps the last read of aMule's queue for a moment: the page
// chrome, the counters and the Scarico list all read it on each render.
var ed2kQueueCache struct {
	sync.Mutex
	key       string
	at        time.Time
	downloads []amule.Download
}

const ed2kQueueCacheTTL = 2 * time.Second

// Ed2kDownloads returns aMule's download queue, or nil when the integration
// is off or the core cannot be reached.
func Ed2kDownloads(cfg *Config) []amule.Download {
	if !ed2kEnabled(cfg) {
		return nil
	}
	settings, _ := ed2kSettingsFromConfig(cfg)
	ed2kQueueCache.Lock()
	if ed2kQueueCache.key == settings.Address && time.Since(ed2kQueueCache.at) < ed2kQueueCacheTTL {
		cached := ed2kQueueCache.downloads
		ed2kQueueCache.Unlock()
		return cached
	}
	ed2kQueueCache.Unlock()
	var downloads []amule.Download
	if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		downloads, callErr = client.Downloads(ctx)
		return callErr
	}); err != nil {
		return nil
	}
	ed2kQueueCache.Lock()
	ed2kQueueCache.key, ed2kQueueCache.at, ed2kQueueCache.downloads = settings.Address, time.Now(), downloads
	ed2kQueueCache.Unlock()
	return downloads
}

// ed2kForgetQueue drops the cached queue after a change made by Gextto.
func ed2kForgetQueue() {
	ed2kQueueCache.Lock()
	ed2kQueueCache.at = time.Time{}
	ed2kQueueCache.Unlock()
}

// AddEd2kLink queues one ed2k:// file link in aMule. A refusal or a timeout
// is checked against aMule's queue: aMule refuses a file it already has, and
// it can stall for a while when it creates a large part file, yet in both
// cases the file is queued and the add has done its job.
func AddEd2kLink(cfg *Config, link string) error {
	parsed, err := ed2k.ParseFileLink(link)
	if err != nil {
		return err
	}
	err = ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		return client.AddLink(ctx, parsed.String(), 0)
	})
	ed2kForgetQueue()
	if err == nil {
		return nil
	}
	if ed2kQueued(cfg, parsed.Hash) {
		return nil
	}
	return err
}

// ed2kQueued reports whether aMule's download queue holds the file. It reads
// the queue directly (with a longer wait) instead of through the fail-fast
// backoff, which a timeout of the add itself may have just armed.
func ed2kQueued(cfg *Config, hash string) bool {
	client, err := ed2kClient(cfg)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	downloads, err := client.Downloads(ctx)
	if err != nil {
		return false
	}
	ed2kState.mu.Lock()
	ed2kState.failedAt = time.Time{}
	ed2kState.mu.Unlock()
	hash = strings.ToLower(hash)
	for _, download := range downloads {
		if download.Hash == hash {
			return true
		}
	}
	return false
}

// errEd2kNotQueued reports a remove of a file aMule no longer has in its
// download queue.
var errEd2kNotQueued = errors.New("the file is not in aMule's download queue")

// addEd2kRelease starts an approved eD2k release found by the automatic gap
// filler. It reports false, nil when aMule already has the file, like a
// torrent engine does for a duplicate.
func addEd2kRelease(cfg *Config, release *models.Release) (bool, error) {
	if link, err := ed2k.ParseFileLink(release.Ed2kLink); err == nil && ed2kQueued(cfg, link.Hash) {
		return false, nil
	}
	if err := AddEd2kLink(cfg, release.Ed2kLink); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Ed2kDownloadAction pauses, resumes or removes a download by MD4 hash. A
// completed file is only taken off the list: its data stays in aMule's
// incoming folder (and in the library, if it was imported).
func Ed2kDownloadAction(cfg *Config, action, hash string) error {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if !ed2k.ValidHash(hash) {
		return errors.New("invalid eD2k hash")
	}
	defer ed2kForgetQueue()
	return ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		switch action {
		case "pause":
			return client.Pause(ctx, hash)
		case "resume":
			return client.Resume(ctx, hash)
		case "remove":
			downloads, err := client.Downloads(ctx)
			if err != nil {
				return err
			}
			for _, download := range downloads {
				if download.Hash != hash {
					continue
				}
				if download.Complete() {
					return client.ClearCompleted(ctx)
				}
				return client.Delete(ctx, hash)
			}
			// Not in the queue any more (aMule 3.x drops finished files at
			// once): nothing to remove.
			return errEd2kNotQueued
		}
		return errors.New("unknown eD2k action")
	})
}

// ed2kDownloadState maps aMule's file status to the states the UI uses.
func ed2kDownloadState(download amule.Download) string {
	switch {
	case download.Complete():
		return "finished"
	case download.Paused():
		return "paused"
	case download.Status == amule.StatusError:
		return "error"
	case download.Status == amule.StatusHashing || download.Status == amule.StatusWaitingHash ||
		download.Status == amule.StatusCompleting || download.Status == amule.StatusAllocating:
		return "checking"
	case download.Speed > 0:
		return "downloading"
	}
	return "queued"
}

// ed2kSession presents aMule's queue to the completion handler as a
// TorrentSession. Only what post-processing needs is implemented; the
// torrent-only operations (recheck, reannounce, storage moves) report false.
type ed2kSession struct {
	nullEngine
	cfg *Config
	// pending lists the eD2k files Gextto queued and has not seen complete
	// (from the database). It may be nil.
	pending func() ([]string, error)
}

var (
	ed2kEventsMu   sync.Mutex
	ed2kLastStatus = map[string]bool{}
	ed2kEventsKey  string
	ed2kLastPoll   time.Time
	// ed2kReported remembers when a completion found among the shared files
	// was reported, so it is not reported again at every poll; a file still
	// pending after ed2kReportRetry is reported again (the completion
	// handler skips what it already archived).
	ed2kReported = map[string]time.Time{}
)

const ed2kReportRetry = 10 * time.Minute

// ed2kPollInterval spaces the completion checks: the event loop ticks far more
// often than an eD2k file can complete, and each check reads aMule's whole
// download queue.
const ed2kPollInterval = 5 * time.Second

// ed2kFileStableFor makes sure aMule has finished moving a completed file from
// its temporary folder into the incoming/category folder before Gextto imports
// it. On a separate disk or Docker mount that move is a copy, and aMule can
// already report the download as complete while the file is still growing.
// The value is a variable so tests do not have to sleep through it.
var ed2kFileStableFor = 2 * time.Second

type ed2kReadyState struct {
	size    int64
	modTime time.Time
	since   time.Time
}

var (
	ed2kReadyMu    sync.Mutex
	ed2kReadySince = map[string]ed2kReadyState{}
)

// ed2kFileReady reports whether the completed file is present with the size
// aMule promised and has not changed for ed2kFileStableFor.
func ed2kFileReady(hash, path string, expected int64) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		ed2kForgetReady(hash)
		return false
	}
	if expected > 0 && info.Size() != expected {
		ed2kForgetReady(hash)
		return false
	}
	if ed2kFileStableFor <= 0 {
		ed2kForgetReady(hash)
		return true
	}
	now := time.Now()
	ed2kReadyMu.Lock()
	previous, seen := ed2kReadySince[hash]
	if !seen || previous.size != info.Size() || !previous.modTime.Equal(info.ModTime()) {
		ed2kReadySince[hash] = ed2kReadyState{size: info.Size(), modTime: info.ModTime(), since: now}
		ed2kReadyMu.Unlock()
		return false
	}
	stable := now.Sub(previous.since) >= ed2kFileStableFor
	ed2kReadyMu.Unlock()
	if stable {
		ed2kForgetReady(hash)
	}
	return stable
}

func ed2kForgetReady(hash string) {
	ed2kReadyMu.Lock()
	delete(ed2kReadySince, hash)
	ed2kReadyMu.Unlock()
}

func (s ed2kSession) List() []models.TorrentView {
	downloads := Ed2kDownloads(s.cfg)
	incoming := ed2kIncomingDir(s.cfg)
	out := make([]models.TorrentView, 0, len(downloads))
	for _, download := range downloads {
		state := ed2kDownloadState(download)
		out = append(out, models.TorrentView{
			Hash:         download.Hash,
			Name:         download.Name,
			Progress:     download.Progress(),
			State:        state,
			Network:      networkEd2k,
			Sources:      int(download.Sources),
			TotalSize:    download.Size,
			TotalDone:    download.Done,
			HasMetadata:  download.Size > 0,
			SavePath:     incoming,
			DownloadRate: uint64(download.Speed),
		})
	}
	return out
}

// PollEvents reports the files that completed since the previous call.
//
// aMule 2.3 keeps a finished file in its download queue with the "complete"
// status; aMule 3.x drops it from the queue at once and only shares it. Both
// are handled: a completed queue entry is reported directly, and a file that
// left the queue (or that the database still lists as pending after a Gextto
// restart) is looked up among aMule's shared files, which also give the
// folder it was saved in.
func (s ed2kSession) PollEvents() []models.TorrentEvent {
	ed2kEventsMu.Lock()
	due := time.Since(ed2kLastPoll) >= ed2kPollInterval
	if due {
		ed2kLastPoll = time.Now()
	}
	ed2kEventsMu.Unlock()
	if !due {
		return nil
	}
	downloads := Ed2kDownloads(s.cfg)
	if downloads == nil {
		return nil
	}
	var pending []string
	if s.pending != nil {
		if hashes, err := s.pending(); err == nil {
			pending = hashes
		}
	}
	settings, _ := ed2kSettingsFromConfig(s.cfg)

	ed2kEventsMu.Lock()
	if ed2kEventsKey != settings.Address {
		ed2kLastStatus = map[string]bool{}
		ed2kReported = map[string]time.Time{}
		ed2kReadyMu.Lock()
		ed2kReadySince = map[string]ed2kReadyState{}
		ed2kReadyMu.Unlock()
		ed2kEventsKey = settings.Address
	}
	var events []models.TorrentEvent
	present := make(map[string]struct{}, len(downloads))
	candidates := map[string]struct{}{}
	incoming := ed2kIncomingDir(s.cfg)
	for _, download := range downloads {
		hash := strings.ToLower(download.Hash)
		name := filepath.Base(strings.TrimSpace(download.Name))
		present[hash] = struct{}{}
		if !download.Complete() {
			ed2kLastStatus[hash] = false
			continue
		}
		// A file that aMule marked complete may still be moving from TEMP to
		// the incoming/category folder. Do not report it until it is there,
		// with the right size, and stable for a moment.
		path := ""
		if incoming != "" && name != "" {
			path = filepath.Join(incoming, name)
		}
		if path != "" && ed2kFileReady(hash, path, download.Size) {
			wasComplete, seen := ed2kLastStatus[hash]
			ed2kLastStatus[hash] = true
			if !seen || !wasComplete {
				events = append(events, models.TorrentEvent{
					Kind:     "torrent_finished",
					Hash:     hash,
					Name:     name,
					SavePath: incoming,
				})
			}
			continue
		}
		// The queue entry is complete but the file is not at the expected
		// path yet. Let the shared-file lookup below find its real folder
		// (a category folder, for example) instead of giving up.
		ed2kLastStatus[hash] = false
		candidates[hash] = struct{}{}
	}
	for hash := range ed2kLastStatus {
		if _, ok := present[hash]; !ok {
			delete(ed2kLastStatus, hash)
			if reportedAt, reported := ed2kReported[hash]; reported && time.Since(reportedAt) < ed2kReportRetry {
				continue
			}
			candidates[hash] = struct{}{}
		}
	}
	pendingSet := make(map[string]struct{}, len(pending))
	for _, hash := range pending {
		hash = strings.ToLower(hash)
		pendingSet[hash] = struct{}{}
		if _, inQueue := present[hash]; inQueue {
			continue
		}
		if reportedAt, ok := ed2kReported[hash]; ok && time.Since(reportedAt) < ed2kReportRetry {
			continue
		}
		candidates[hash] = struct{}{}
	}
	for hash := range ed2kReported {
		if _, ok := pendingSet[hash]; !ok {
			delete(ed2kReported, hash)
		}
	}
	ed2kEventsMu.Unlock()

	if len(candidates) == 0 {
		return events
	}
	var shared []amule.SharedFile
	if err := ed2kCall(s.cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		shared, callErr = client.SharedFiles(ctx)
		return callErr
	}); err != nil {
		return events
	}
	ed2kEventsMu.Lock()
	defer ed2kEventsMu.Unlock()
	for _, file := range shared {
		hash := strings.ToLower(file.Hash)
		if _, wanted := candidates[hash]; !wanted || file.Dir == "" || file.Name == "" {
			continue
		}
		if reportedAt, reported := ed2kReported[hash]; reported && time.Since(reportedAt) < ed2kReportRetry {
			continue
		}
		path := filepath.Join(file.Dir, filepath.Base(file.Name))
		if !ed2kFileReady(hash, path, file.Size) {
			continue
		}
		ed2kReported[hash] = time.Now()
		ed2kLastStatus[hash] = true
		events = append(events, models.TorrentEvent{
			Kind:     "torrent_finished",
			Hash:     hash,
			Name:     filepath.Base(file.Name),
			SavePath: file.Dir,
		})
	}
	return events
}

func (s ed2kSession) Pause(hash string) (bool, error) {
	return true, Ed2kDownloadAction(s.cfg, "pause", hash)
}

// ImportCompletedByCopy reports the configured copy mode for aMule: copy and
// hardlink keep the source in the incoming folder for sharing. In move mode it
// returns false and MoveCompletedImmediately moves the source at completion.
func (s ed2kSession) ImportCompletedByCopy() bool {
	return !settingsBool(s.cfg, "move_episodes", false)
}

// MoveCompletedImmediately reports that aMule cannot defer a storage move to
// the end of a seed: eD2k has no seed policy controlled by Gextto, so with
// move mode on the source is moved when the download completes. With copy
// mode off, the source stays in aMule's incoming folder and keeps being shared.
func (ed2kSession) MoveCompletedImmediately() bool { return true }

func (s ed2kSession) Resume(hash string) (bool, error) {
	return true, Ed2kDownloadAction(s.cfg, "resume", hash)
}

func (s ed2kSession) Remove(hash string, deleteFiles bool) (bool, error) {
	if err := Ed2kDownloadAction(s.cfg, "remove", hash); err != nil {
		if errors.Is(err, errEd2kNotQueued) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ed2kStallDays is how long an automatically chosen eD2k download may go
// without receiving data before Gextto gives up on it (0 = never).
func ed2kStallDays(cfg *Config) int {
	if cfg == nil {
		return 7
	}
	raw := strings.TrimSpace(cfg.Settings["ed2k_stall_days"])
	if raw == "" {
		return 7
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 7
	}
	return value
}

// MonitorEd2kStalled gives up on eD2k downloads Gextto started by itself that
// received nothing for ed2k_stall_days: a rare file can sit in aMule's queue
// for ever, and while it does the episode counts as being downloaded, so the
// gap filler would never look for it elsewhere. The download is cancelled in
// aMule, the release is blocklisted and the gap is searched again at the next
// cycle. Downloads added by hand and paused ones are left alone: the user
// chose to wait for them.
func MonitorEd2kStalled(cfg *Config, db *Database, notifier *Notifier, now time.Time) {
	days := ed2kStallDays(cfg)
	if days <= 0 || db == nil {
		return
	}
	limit := time.Duration(days) * 24 * time.Hour
	pending, err := db.PendingEd2kHashes()
	if err != nil || len(pending) == 0 {
		return
	}
	wanted := make(map[string]struct{}, len(pending))
	for _, hash := range pending {
		wanted[hash] = struct{}{}
	}
	for _, download := range Ed2kDownloads(cfg) {
		if _, ok := wanted[download.Hash]; !ok || download.Complete() || download.Paused() {
			continue
		}
		if reason, err := db.TorrentReason(download.Hash); err != nil || (reason != nil && *reason == "manual") {
			continue
		}
		last := time.Time{}
		if download.LastRecv > 0 {
			last = time.Unix(download.LastRecv, 0)
		} else if times, err := db.TorrentTimes(download.Hash); err == nil && times != nil {
			if created, ok := utils.ParseTimestamp(times.CreatedAt); ok {
				last = created
			}
		}
		if last.IsZero() || now.Sub(last) < limit {
			continue
		}
		metadata, _ := db.TorrentMeta(download.Hash)
		if err := Ed2kDownloadAction(cfg, "remove", download.Hash); err != nil {
			logging.Warn("cannot cancel a stalled eD2k download in aMule", "name", download.Name, "error", err)
			continue
		}
		restored, _ := db.RestoreUpgrade(download.Hash)
		_ = db.MarkTorrentError(download.Hash, "stalled eD2k download")
		_ = db.MarkTorrentRemovedAt(download.Hash)
		title := download.Name
		if metadata != nil {
			title = metadata.Release.Title
			_ = db.Blocklist(&metadata.Release, "stalled_ed2k")
			release := metadata.Release
			if release.Series != nil && release.Season != nil && release.Episode != nil {
				_ = db.ClearGapSearched(*release.Series, *release.Season, *release.Episode)
			}
		}
		logging.Warn(fmt.Sprintf("❌ Gave up on the eD2k download «%s»: nothing received for %s (%s done). It is cancelled in aMule and the next search will look for another version",
			download.Name, logDuration(limit), logPercent(download.Progress())), "title", title, "hash", download.Hash)
		_ = notifier.NotifyEvent("download_failed", map[string]any{
			"hash":             download.Hash,
			"title":            title,
			"error":            "stalled eD2k download",
			"upgrade_restored": restored,
		})
	}
}
