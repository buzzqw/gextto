package gextto

// web_torrents.go holds the torrent API: the list and the actions on a single
// torrent (add, remove, pause, resume, limits, trackers, files, peers, web
// seeds, magnet and storage), the speed and temporary limits, the download
// history, the IP filter, and the helpers that queue a release by magnet or
// from a search.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

type filePrioritiesInput struct {
	Priorities []int32 `json:"priorities"`
}

func TorrentStats(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{"ok": true, "stats": s.activeEngine().Stats()})
}

func SetFilePriorities(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input filePrioritiesInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Priorities) > 100000 {
		jsonError(w, http.StatusBadRequest, "too many file priorities")
		return
	}
	ok, err := s.activeEngine().SetFilePriorities(hash, input.Priorities)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusConflict, "torrent unavailable in dry-run")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

func ExportTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	path, ok := s.activeEngine().TorrentFilePath(hash)
	if !ok {
		jsonError(w, http.StatusNotFound, "torrent file not available")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	_, _ = w.Write(data)
}

func RemoveTorrentLegacy(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RemoveTorrentInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	removed, err := SafeRemoveTorrent(s, cfg, input.Hash, input.DeleteFiles)
	status, body := torrentActionResult(removed, err)
	jsonStatus(w, status, body)
}

func OptimizeTransferSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	// On qBittorrent the same operational goal is expressed with the backend's
	// own MiB-based disk cache preferences.
	if qb, ok := s.activeEngine().(*qbittorrentEngine); ok {
		result, err := qb.ApplyOptimization(latestConfig(s))
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":          true,
			"applied":     true,
			"backend":     BackendQbittorrent,
			"optimized":   result,
			"explanation": "Cache disco qBittorrent proporzionata alla RAM e limitata; le altre preferenze restano invariate.",
		})
		return
	}
	// gx-torrent: the cache goes back to automatic (sized from the RAM).
	if gx, ok := s.activeEngine().(*gxTorrentEngine); ok {
		if err := SaveSetting(s.cfg.DataDir, "torrent_cache_size", "-1"); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		result, err := gx.ApplyOptimization(latestConfig(s))
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":          true,
			"applied":     true,
			"backend":     BackendGxTorrent,
			"optimized":   result,
			"explanation": "Cache disco di gx-torrent automatica: lettura 1/32 e scrittura 1/16 della RAM, entro limiti sicuri.",
		})
		return
	}
	jsonError(w, http.StatusConflict, "no active torrent engine supports the optimization")
}

func SetTorrentLimitsLegacy(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TorrentLimitsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The rates are always part of this legacy payload (0 = unlimited); the
	// seed limits only when present, so they keep their value otherwise.
	download := saturatingMulInt64(input.DlKbps, 1024)
	upload := saturatingMulInt64(input.UlKbps, 1024)
	ok, err := s.activeEngine().SetLimits(
		input.Hash,
		&download,
		&upload,
		input.SeedRatio,
		input.SeedDays,
	)
	status, body := torrentActionResult(ok, err)
	jsonStatus(w, status, body)
}

// gh1_torrentDisplayName returns the display name of a torrent, falling back to
// "unnamed torrent".
func gh1TorrentDisplayName(torrents TorrentSession, hash string) string {
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

// gh1_mismatchedPackRelease returns the configured release when the torrent's
// real name identifies a different season.
func mismatchedPackRelease(torrents TorrentSession, db *Database, hash string) *models.Release {
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return nil
	}
	release := meta.Release
	if release.Kind != "series" || !release.IsPack {
		return nil
	}
	name := gh1TorrentDisplayName(torrents, hash)
	corrected := ReconcilePackIdentity(&release, name)
	if corrected == nil {
		return nil
	}
	sameSeason := (corrected.Season == nil) == (release.Season == nil)
	if sameSeason && corrected.Season != nil && release.Season != nil {
		sameSeason = *corrected.Season == *release.Season
	}
	if sameSeason {
		return nil
	}
	result := release
	return &result
}

// gh1_blocklistMismatchedPack permanently blocklists a mismatched season pack so
// gap filling cannot pick it again.
func blocklistMismatchedPack(torrents TorrentSession, db *Database, hash string) bool {
	release := mismatchedPackRelease(torrents, db, hash)
	if release == nil {
		return false
	}
	if blocked, _ := db.IsBlocklisted(hash); blocked {
		return true
	}
	if err := db.Blocklist(release, "season_pack_identity_mismatch"); err != nil {
		logging.Warn("could not blocklist mismatched season pack", "hash", hash, "error", err.Error())
		return false
	}
	logging.Warn("mismatched season pack permanently blocklisted", "hash", hash, "title", release.Title)
	return true
}

func PauseTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	status, value := torrentActionResult(s.activeEngine().Pause(hash))
	jsonStatus(w, status, value)
}

func RecheckTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	name := hash
	for _, torrent := range s.activeEngine().List() {
		if strings.EqualFold(torrent.Hash, hash) {
			name = torrent.Name
			break
		}
	}
	logging.Info("manual torrent integrity check requested", "hash", hash, "name", name)
	ok, err := s.activeEngine().ForceRecheck(hash)
	switch {
	case err != nil:
		logging.Error("manual torrent integrity check failed to start", "hash", hash, "name", name, "error", err.Error())
	case ok:
		logging.Info("manual torrent integrity check accepted by the torrent engine", "hash", hash, "name", name)
	default:
		logging.Warn("manual torrent integrity check not started: torrent session unavailable", "hash", hash, "name", name)
	}
	status, value := torrentActionResult(ok, err)
	jsonStatus(w, status, value)
}

func RemoveTorrentWithOptions(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input RemoveOptionsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	identityMismatch := blocklistMismatchedPack(s.activeEngine(), s.db, hash)
	if input.Blocklist && !identityMismatch {
		if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
			_ = s.db.Blocklist(&meta.Release, "manual")
		}
	}
	cfg := latestConfig(s)
	var removed bool
	var removeErr error
	if input.Archive {
		removed, removeErr = ArchiveAndRemoveTorrent(s, cfg, hash)
	} else {
		removed, removeErr = SafeRemoveTorrent(s, cfg, hash, input.DeleteFiles)
	}
	status, value := torrentActionResult(removed, removeErr)
	jsonStatus(w, status, value)
}

func SetTorrentNoRename(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input NoRenameInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.SetTorrentNoRename(hash, input.Value); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "no_rename": input.Value})
}

func TorrentDetails(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	magnet := ""
	if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
		magnet = meta.Release.Magnet
	}
	for _, torrent := range s.activeEngine().List() {
		if !strings.EqualFold(torrent.Hash, hash) {
			continue
		}
		noRename, _ := s.db.TorrentNoRename(hash)
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":        true,
			"torrent":   torrent,
			"magnet":    magnet,
			"no_rename": noRename,
		})
		return
	}
	jsonError(w, http.StatusNotFound, "torrent not found")
}

func TorrentTags(w http.ResponseWriter, r *http.Request, s *AppState) {
	pairs, _ := s.db.TorrentTags()
	items := []map[string]any{}
	for _, pair := range pairs {
		items = append(items, map[string]any{"hash": pair[0], "tag": pair[1]})
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func UnpinTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	ok, err := s.activeEngine().SetPin("", false)
	if err == nil {
		_ = SaveSetting(s.cfg.DataDir, "torrent_pinned_hash", "")
	}
	status, value := torrentActionResult(ok, err)
	jsonStatus(w, status, value)
}

// PinTorrent pins a torrent and saves its hash among the settings.
func PinTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TorrentHashInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash := strings.ToLower(input.Hash)
	result, err := s.activeEngine().SetPin(hash, true)
	if err == nil {
		_ = SaveSetting(s.cfg.DataDir, "torrent_pinned_hash", hash)
	}
	status, value := torrentActionResult(result, err)
	jsonStatus(w, status, value)
}

// SetSequential turns sequential download on or off and saves the choice.
func SetSequential(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SequentialInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	value := "false"
	if input.Enabled {
		value = "true"
	}
	_ = SaveSetting(s.cfg.DataDir, "torrent_sequential", value)
	result, err := s.activeEngine().SetSequential(input.Enabled)
	status, response := torrentActionResult(result, err)
	jsonStatus(w, status, response)
}

// SetTorrentTag sets the tag of a torrent.
func SetTorrentTag(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TorrentTagInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.SetTorrentTag(input.Hash, strings.TrimSpace(input.Tag)); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// TorrentEvents returns the last 100 torrent events.
func TorrentEvents(w http.ResponseWriter, r *http.Request, s *AppState) {
	events := s.torrent_events.Snapshot()
	if len(events) > 100 {
		events = events[len(events)-100:]
	}
	if events == nil {
		events = []models.TorrentEvent{}
	}
	jsonResponse(w, events)
}

// TorrentTrackers returns the trackers of a torrent.
func TorrentTrackers(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	trackers, ok, err := s.activeEngine().Trackers(hash)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusNotFound, "torrent unavailable in dry-run")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "trackers": trackers})
}

// SetTorrentTrackers replaces the trackers of a torrent.
func SetTorrentTrackers(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input TrackersInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Trackers) > 500 {
		jsonError(w, http.StatusBadRequest, "too many trackers")
		return
	}
	trackers := make([]TrackerEntry, 0, len(input.Trackers))
	for i := range input.Trackers {
		tracker := input.Trackers[i]
		if strings.TrimSpace(tracker.Url) == "" {
			continue
		}
		trackers = append(trackers, TrackerEntry{Tier: tracker.Tier, URL: strings.TrimSpace(tracker.Url)})
	}
	ok, err := s.activeEngine().SetTrackers(hash, trackers)
	if err != nil {
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !ok {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "torrent unavailable in dry-run"})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

// TorrentFiles returns the files of a torrent.
func TorrentFiles(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	files, ok, err := s.activeEngine().Files(hash)
	if err != nil {
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !ok {
		jsonStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "torrent unavailable in dry-run"})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "files": files})
}

// Torrents returns the torrent list decorated for the interface; in dry-run,
// when the live session is empty, it shows the stored torrents instead.
func Torrents(w http.ResponseWriter, r *http.Request, s *AppState) {
	live := s.activeEngine().List()
	if len(live) == 0 && s.dryRun() {
		jsonResponse(w, DecorateTorrents(s, DryRunSessionPreview(s)))
		return
	}
	jsonResponse(w, DecorateTorrents(s, live))
}

// DecorateTorrents turns the torrent views into the maps the API returns, adding
// the archive state, the stall information and the progress of the downloads
// that are finished or archived.
func DecorateTorrents(s *AppState, items []models.TorrentView) []map[string]any {
	hashes := make([]string, 0, len(items))
	for i := range items {
		hashes = append(hashes, strings.ToLower(items[i].Hash))
	}
	aux, err := s.db.TorrentAuxBulk(hashes)
	if err != nil {
		aux = map[string][3]string{}
	}
	// Stall state of the torrents set aside by the stall monitor, so a client
	// can say since when a download is stuck and when it is tried again.
	stalls, err := s.db.LoadStallWatches()
	if err != nil {
		stalls = map[string]StallWatch{}
	}
	decorated := make([]map[string]any, 0, len(items))
	for i := range items {
		item := items[i]
		entry := aux[strings.ToLower(item.Hash)]
		archived := strings.TrimSpace(entry[0]) != ""
		value := map[string]any{}
		if raw, err := json.Marshal(item); err == nil {
			if err := json.Unmarshal(raw, &value); err != nil {
				value = map[string]any{}
			}
		}
		value["archived"] = archived
		value["source"] = entry[1]
		value["reason"] = entry[2]
		if stall, ok := stalls[strings.ToLower(item.Hash)]; ok && stall.stalledSince != nil {
			value["stalled_since"] = stall.stalledSince.UTC().Format(time.RFC3339)
			if !stall.nextRetryAt.IsZero() {
				value["next_retry_at"] = stall.nextRetryAt.UTC().Format(time.RFC3339)
			}
		}
		if archived || item.State == "finished" {
			value["progress"] = 100.0
			if item.TotalSize > 0 && item.TotalDone < item.TotalSize {
				value["total_done"] = item.TotalSize
			}
			if item.State == "paused" && archived {
				value["state"] = "Completato"
			}
		}
		decorated = append(decorated, value)
	}
	return decorated
}

// DryRunSessionPreview builds the torrent rows shown in dry-run from the stored
// torrents that still have a fastresume file in the state directory.
func DryRunSessionPreview(s *AppState) []models.TorrentView {
	cfg := LatestConfig(s)
	hashes := map[string]struct{}{}
	if entries, err := os.ReadDir(cfg.StateDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if filepath.Ext(name) != ".fastresume" {
				continue
			}
			stem := strings.TrimSuffix(name, ".fastresume")
			hashes[strings.ToLower(stem)] = struct{}{}
		}
	}
	if len(hashes) == 0 {
		return []models.TorrentView{}
	}
	stored, err := s.db.StoredTorrentsAll(500)
	if err != nil {
		stored = nil
	}
	preview := make([]models.TorrentView, 0, len(stored))
	for i := range stored {
		item := stored[i]
		if _, ok := hashes[strings.ToLower(item.Hash)]; !ok {
			continue
		}
		state := "restored · dry-run"
		if item.Paused {
			state = "restored · in pausa"
		}
		preview = append(preview, models.TorrentView{
			Hash:            item.Hash,
			Name:            item.Name,
			Progress:        ClampFloat(item.Progress*100.0, 0.0, 100.0),
			State:           state,
			DownloadRate:    0,
			UploadRate:      0,
			SavePath:        cfg.DownloadDir,
			DownloadLimit:   -1,
			UploadLimit:     -1,
			AllTimeUpload:   0,
			AllTimeDownload: item.Downloaded,
			SeedingSeconds:  0,
			QueuePosition:   0,
			NumPeers:        0,
			NumSeeds:        0,
			SeedRatio:       -1.0,
			SeedDays:        -1,
			HasMetadata:     true,
			AutoManaged:     false,
			TorrentVersion:  "",
			TotalSize:       0,
			TotalDone:       0,
			Stalled:         false,
		})
	}
	return preview
}

// ClampFloat clamps a float to the inclusive range.
func ClampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// removeFailedTorrent uses the same removal path as the automatic cleanup, so a
// manual failure and an automatic one read the same in the log.
func removeFailedTorrent(torrents TorrentSession, hash string) bool {
	return tevRemoveFailedTorrent(torrents, hash)
}

// AddTorrentHandler adds a torrent with the options carried by the request:
// start paused, sequential, seed mode, queue position, preallocation, a preferred
// save path and the no-rename flag.
func AddTorrentHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
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
	preallocate := cfg.TorrentPreallocate()
	if input.Preallocate != nil {
		preallocate = *input.Preallocate
	}
	options := AddOptions{
		Paused:         input.StartPaused,
		Sequential:     input.Sequential,
		SeedMode:       input.SeedMode,
		QueueTop:       input.QueueTop,
		FirstLast:      input.FirstLast,
		StopAtMetadata: input.StopAtMetadata,
		Preallocate:    preallocate,
	}
	var preferred *string
	if input.SavePath != nil {
		trimmed := strings.TrimSpace(*input.SavePath)
		if trimmed != "" {
			preferred = &trimmed
		}
	}
	added, err := s.activeEngine().AddWithOptions(input.Magnet, cfg, preferred, options)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !added {
		jsonError(w, http.StatusConflict, "duplicate")
		return
	}
	if input.NoRename {
		if hash, ok := utils.MagnetHash(input.Magnet); ok {
			_ = s.db.SetTorrentNoRename(hash, true)
		}
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true})
}

// MarkTorrentFailed blocklists the release of a torrent the user marked as failed
// and drops it from the session.
func MarkTorrentFailed(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	metadata, err := s.db.TorrentMeta(hash)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if metadata != nil {
		_ = s.db.Blocklist(&metadata.Release, "manual_failed")
		// Like an automatic give-up: let the next cycle look for another
		// version of these episodes now, not after the gap re-search delay.
		release := metadata.Release
		if release.Series != nil && release.Season != nil {
			episodes := release.EpisodeRange
			if len(episodes) == 0 && release.Episode != nil {
				episodes = []int64{*release.Episode}
			}
			for _, episode := range episodes {
				_ = s.db.ClearGapSearched(*release.Series, *release.Season, episode)
			}
		}
	}
	restored, _ := s.db.RestoreUpgrade(hash)
	_ = s.db.MarkTorrentError(hash, "manual failure")
	removed := false
	for _, torrent := range s.activeEngine().List() {
		if strings.EqualFold(torrent.Hash, hash) {
			removed = true
			break
		}
	}
	if removeFailedTorrent(s.activeEngine(), hash) {
		_ = s.db.MarkTorrentRemovedAt(hash)
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "removed": removed, "upgrade_restored": restored})
}

// SetWebSeeds adds or removes the web seeds of a torrent.
func SetWebSeeds(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input WebSeedsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	urls := []string{}
	for _, raw := range input.Urls {
		url := strings.TrimSpace(raw)
		if url == "" {
			continue
		}
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "ftp://") {
			urls = append(urls, url)
		}
	}
	joined := strings.Join(urls, "\n")
	if joined == "" {
		jsonError(w, http.StatusBadRequest, "no valid web seed URL")
		return
	}
	ok, err := s.activeEngine().WebSeeds(hash, joined, input.Remove)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusConflict, "torrent unavailable in dry-run")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

// TorrentHistory returns the completed downloads (torrents and comics) newest
// first, paginated.
func TorrentHistory(w http.ResponseWriter, r *http.Request, s *AppState) {
	limit := queryIntVal(r, "limit", 10)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	page := queryIntVal(r, "page", 1)
	if page < 1 {
		page = 1
	}
	query := queryParam(r, "q")
	// Fetch both sources before paginating: otherwise comic rows would be
	// prepended to every torrent page and the reported total would not match
	// the visible rows.
	_, torrentTotal, err := s.db.CompletedTorrents(0, 1, query)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	torrentLimit := int(torrentTotal)
	if torrentLimit < 1 {
		torrentLimit = 1
	}
	if torrentLimit > 2000 {
		torrentLimit = 2000
	}
	torrents, _, err := s.db.CompletedTorrents(0, torrentLimit, query)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type historyItem struct {
		value any
		at    string
	}
	combined := make([]historyItem, 0, len(torrents))
	for _, torrent := range torrents {
		torrent.Name = historyDisplayName(torrent.Name, torrent.Source)
		at := torrent.CompletedAt
		if at == "" {
			at = torrent.UpdatedAt
		}
		combined = append(combined, historyItem{value: torrent, at: at})
	}
	// Comic HTTP downloads are not libtorrent rows, but users expect every
	// completed download in the same Storico download view. Keep their source
	// record in comics_history and expose it with the common history shape.
	if s.comics != nil {
		if history, historyErr := s.comics.History(s.comics.HistoryLimit()); historyErr == nil {
			needle := strings.ToLower(strings.TrimSpace(query))
			for _, item := range history {
				name := fmt.Sprint(item["title"])
				postURL := fmt.Sprint(item["post_url"])
				if needle != "" && !strings.Contains(strings.ToLower(name), needle) && !strings.Contains(strings.ToLower(postURL), needle) {
					continue
				}
				comic := map[string]any{
					"name":           name,
					"kind":           "comic",
					"tag":            "Comic",
					"quality_score":  0,
					"status":         "completed",
					"processed_path": "",
					"completed_at":   item["sent_at"],
					"source":         postURL,
				}
				combined = append(combined, historyItem{value: comic, at: fmt.Sprint(item["sent_at"])})
			}
		}
	}
	sort.SliceStable(combined, func(i, j int) bool {
		return combined[i].at > combined[j].at
	})
	todayOnly := strings.EqualFold(strings.TrimSpace(r.FormValue("day")), "today")
	if todayOnly {
		today := time.Now().Format("2006-01-02")
		filtered := combined[:0]
		for _, item := range combined {
			if at, ok := HistoryTimestampLocal(item.at); ok && at.Format("2006-01-02") == today {
				filtered = append(filtered, item)
			}
		}
		combined = filtered
	}
	total := int64(len(combined))
	if !todayOnly && torrentTotal > int64(len(torrents)) {
		// Keep pagination honest if the database contains more rows than the
		// defensive fetch cap; the visible page is still correctly ordered for
		// the rows we loaded.
		total = torrentTotal + int64(len(combined)-len(torrents))
	}
	limit64 := int64(limit)
	pages := (total + limit64 - 1) / limit64
	if pages < 1 {
		pages = 1
	}
	offset := (page - 1) * limit
	start := offset
	if start > len(combined) {
		start = len(combined)
	}
	end := start + limit
	if end > len(combined) {
		end = len(combined)
	}
	items := make([]any, 0, end-start)
	for _, item := range combined[start:end] {
		items = append(items, item.value)
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":    true,
		"items": items,
		"total": total,
		"page":  page,
		"pages": pages,
	})
}

// HistoryTimestampLocal parses a stored history timestamp and returns it in the
// local timezone. The database stores UTC SQLite timestamps; the UI shows
// local time, so the "Oggi" filter must use the same convention.
func HistoryTimestampLocal(value string) (time.Time, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return time.Time{}, false
	}
	normalized := strings.Replace(raw, " ", "T", 1)
	if !strings.HasSuffix(normalized, "Z") && !strings.ContainsAny(normalized, "+") {
		if !strings.Contains(normalized, "-") || strings.Count(normalized, "-") == 2 {
			normalized += "Z"
		}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, normalized); err == nil {
			return parsed.Local(), true
		}
	}
	return time.Time{}, false
}

// RestartTorrent restarts a torrent in the session.
func RestartTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	result, err := s.activeEngine().Restart(hash)
	torrentAction(w, result, err)
}

// SetSuperSeeding turns super-seeding on or off when the active engine supports
// it.
func SetSuperSeeding(w http.ResponseWriter, r *http.Request, s *AppState) {
	type superSeeder interface {
		SetSuperSeeding(hash string, enabled bool) (bool, error)
	}
	client, ok := s.activeEngine().(superSeeder)
	if !ok {
		jsonError(w, http.StatusConflict, "the active engine does not support super-seeding")
		return
	}
	hash := pathParam(r, "hash")
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := client.SetSuperSeeding(hash, input.Enabled)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !result {
		jsonError(w, http.StatusConflict, "torrent unavailable in dry-run")
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// TorrentNoRenameList returns the torrents marked as not to be renamed.
func TorrentNoRenameList(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.NoRenameTorrents()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	torrents := make([]map[string]any, 0, len(items))
	for _, item := range items {
		torrents = append(torrents, map[string]any{"hash": item[0], "name": item[1]})
	}
	jsonResponse(w, map[string]any{"ok": true, "torrents": torrents})
}

func scheduledSpeedLimits(cfg *Config) (int64, int64, bool) {
	return scheduledSpeedLimitsAt(cfg, time.Now())
}

func applySpeedPolicy(cfg *Config, torrents TorrentEngine, guard *latencyGuard) {
	download, upload, _ := effectiveSpeedLimits(cfg, guard)
	if _, err := torrents.SetGlobalSpeedLimits(download, upload); err != nil {
		logging.Debug("speed policy apply failed", "error", err)
	}
}

func effectiveDownloadForRatio(torrent models.TorrentView) float64 {
	if torrent.AllTimeDownload > 0 {
		return float64(torrent.AllTimeDownload)
	}
	if torrent.TotalSize > 0 {
		return float64(torrent.TotalSize)
	}
	return 0
}

func countIpfilterRules(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "-") {
			continue
		}
		count++
	}
	return count
}

// IpfilterStatus reports whether the IP filter is configured and active and how
// many rules it holds.
func IpfilterStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	target := strings.TrimSpace(cfg.Transfer.IpFilterPath)
	cached := filepath.Join(cfg.DataDir, "ipfilter.dat")
	isURL := strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
	path := cached
	if !isURL && target != "" {
		path = target
	}
	_, statErr := os.Stat(path)
	active := target != "" && statErr == nil
	rules := 0
	if active {
		rules = countIpfilterRules(path)
	}
	jsonResponse(w, map[string]any{
		"ok":         true,
		"configured": target != "",
		"url":        target,
		"path":       path,
		"active":     active,
		"rules":      rules,
	})
}

// archivedCopyFinalized reports whether the DB points at an archived copy
// that already went through renaming and quality checks. A post-seed relocation
// can leave the file in the archive folder under its original torrent name when
// the import step never ran, so that copy must be finalized instead of removed.
func archivedCopyFinalized(db *Database, torrentName, hash string) bool {
	if db == nil {
		return false
	}
	processed, err := db.TorrentProcessed(hash)
	if err != nil || processed == nil || strings.TrimSpace(*processed) == "" {
		return false
	}
	if _, statErr := os.Stat(*processed); statErr != nil {
		return false
	}
	name := strings.TrimSpace(torrentName)
	if name == "" {
		return true
	}
	return !strings.EqualFold(filepath.Base(filepath.Clean(*processed)), filepath.Base(name))
}

// RemoveCompletedTorrents removes the completed torrents whose seed limits are
// reached, finalizing the archive import first when it never ran.
func RemoveCompletedTorrents(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RemoveCompletedInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	removed := []string{}
	removedNames := []string{}
	skipped := 0
	// Skips caused by an error (finalization or engine removal failed), as
	// opposed to the routine ones (seed limit not reached, move in progress,
	// archive pending): only these are worth surfacing in the summary log.
	failed := []string{}
	policy := newSeedPolicy(cfg, s.db)
	for _, torrent := range s.activeEngine().List() {
		label := strings.TrimSpace(torrent.Name)
		if label == "" {
			label = "unnamed torrent"
		}
		status, _ := s.db.TorrentStatus(torrent.Hash)
		processed, _ := s.db.TorrentProcessed(torrent.Hash)
		isArchived := processed != nil && strings.TrimSpace(*processed) != ""
		isCompletedDB := status != nil && *status == "completed"
		completed := torrent.Progress >= 99.99 || torrent.State == "finished" || torrent.State == "seeding" || isCompletedDB || isArchived
		if !completed {
			continue
		}
		// Explicit infinite seeding (ratio 0 or days 0): the operator asked
		// for the torrent to keep seeding, so "Pulisci completati" leaves it
		// in place like the automatic removal does (RemoveSeededCompleted).
		// Only the no-limit branch below would otherwise take it, and that
		// branch is meant for torrents with no policy at all.
		if torrent.SeedRatio == 0.0 || torrent.SeedDays == 0 {
			skipped++
			continue
		}
		// Never interfere with a move/rename in progress: a torrent libtorrent is
		// moving, or a tracked release whose library copy is not in place yet
		// (end-of-seed archive pending), must be left alone. Removing it now would
		// abort the move and could leave the file behind.
		if torrent.State == "moving" {
			skipped++
			continue
		}
		if meta, err := s.db.TorrentMeta(torrent.Hash); err == nil && meta != nil {
			if processed == nil || strings.TrimSpace(*processed) == "" {
				skipped++
				continue
			}
		}
		// Effective limits: the torrent's own when positive, then the matching
		// seed rule, then the globals; none when zero (unlimited). A torrent
		// that only inherits the global limits must still be removable. A seed
		// stopped for inactivity ended like one that reached its time.
		limits := policy.limitsFor(&torrent)
		ratioReached := false
		if limits.Ratio != nil {
			download := effectiveDownloadForRatio(torrent)
			ratioReached = download > 0.0 && float64(torrent.AllTimeUpload)/download >= *limits.Ratio
		}
		timeReached := limits.timeReached(&torrent) || (limits.IdleSeconds != nil && seedIdle.reached(torrent.Hash))
		canRemove := false
		if ratioReached || timeReached {
			canRemove = true
		} else if isArchived && torrent.State == "paused" {
			canRemove = true
		} else if limits.none() && (torrent.State == "paused" || torrent.State == "finished" || isArchived) {
			canRemove = true
		}
		if !canRemove {
			skipped++
			continue
		}
		// A completed torrent whose archive import never ran (post-seed move
		// interrupted, or the file still carries the torrent name) is finalized
		// here: moved into the library, renamed, quality-checked and deduplicated
		// before being dropped from the session. A bare Remove would leave the
		// file unchecked and under the wrong name.
		source := CompletionPath(&models.TorrentEvent{
			Kind:     "torrent_finished",
			Hash:     torrent.Hash,
			Name:     torrent.Name,
			SavePath: torrent.SavePath,
		})
		// An archived torrent whose library copy was later replaced (a better
		// release superseded it and sent it to the trash) has neither the
		// archived file nor its source any more: there is nothing left to
		// finalize, so it is only dropped from the session. Without this the
		// finalization fails on the missing source at every cleanup.
		_, sourceErr := os.Stat(source)
		archivedAndGone := isArchived && sourceErr != nil
		if !archivedAndGone && !archivedCopyFinalized(s.db, torrent.Name, torrent.Hash) {
			logging.Debug("completed download still needs archiving; finalizing it before cleanup",
				"hash", torrent.Hash, "name", torrent.Name)
			if ok, err := ArchiveAndRemoveTorrent(s, cfg, torrent.Hash); err != nil {
				logging.Warn(fmt.Sprintf("⚠️ «%s» could not be moved to the library before the cleanup: %v", torrent.Name, err), "hash", torrent.Hash)
				skipped++
				failed = append(failed, label+" (not moved to the library: "+err.Error()+")")
				continue
			} else if !ok {
				skipped++
				failed = append(failed, label+" (move to the library not finished)")
				continue
			}
			removed = append(removed, torrent.Hash)
			removedNames = append(removedNames, label)
			continue
		}
		// "Pulisci completati" only drops the finished torrents from the session.
		// Deleting or trashing their files can be a huge cross-filesystem copy (a
		// pack can be tens of GB) and blocked the request for minutes, so the
		// button looked like it did nothing. Files are reclaimed by the automatic
		// removal and by an explicit "remove and delete the files".
		deleteFiles := input.DeleteFiles
		if deleteFiles && cfg.TrashPath != nil && strings.TrimSpace(*cfg.TrashPath) != "" && cfg.CleanupAction != "delete" {
			if _, statErr := os.Stat(source); statErr == nil {
				if target, moveErr := MoveToTrash(source, *cfg.TrashPath); moveErr != nil {
					logging.Error(fmt.Sprintf("❌ Could not move «%s» to the trash (%s): %v", torrent.Name, source, moveErr), "hash", torrent.Hash)
				} else {
					logging.Info(fmt.Sprintf("🗑️ «%s» moved to the trash: %s", torrent.Name, target), "hash", torrent.Hash)
				}
			}
			deleteFiles = false
		}
		ok, err := s.activeEngine().Remove(torrent.Hash, deleteFiles)
		if err != nil {
			logging.Warn(fmt.Sprintf("⚠️ Could not remove the completed «%s»: %v", torrent.Name, err), "hash", torrent.Hash)
			skipped++
			failed = append(failed, label+" (removal failed: "+err.Error()+")")
			continue
		}
		if ok {
			_ = s.db.MarkTorrentRemoved(torrent.Hash)
			removed = append(removed, torrent.Hash)
			removedNames = append(removedNames, label)
		} else {
			skipped++
			failed = append(failed, label+" (the torrent engine refused the removal)")
		}
	}
	summary := fmt.Sprintf("🧹 Completed downloads cleaned up: %s removed", countLabel(len(removed), "torrent", "torrents"))
	if len(removedNames) > 0 {
		summary += " (" + strings.Join(removedNames, ", ") + ")"
	}
	logging.Info(summary)
	if len(failed) > 0 {
		logging.Warn(fmt.Sprintf("⚠️ %s could not be removed: %s",
			countLabel(len(failed), "completed download", "completed downloads"), strings.Join(failed, "; ")))
	}
	logging.Debug("completed torrents cleanup skipped torrents", "skipped", skipped, "with_errors", len(failed))
	jsonResponse(w, map[string]any{
		"ok":      true,
		"success": true,
		"removed": len(removed),
		"skipped": skipped,
		// HTTP comic downloads are not torrents and must remain visible until
		// the user removes them explicitly from the unified Scarico page.
		"http_removed": 0,
		"items":        removed,
	})
}

// SetTempLimits sets or clears the temporary speed limit, or switches the "auto
// speed" exception on and off.
func SetTempLimits(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TempLimitsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// "Auto speed": hand the line to the tuning (no cap) or take it back. Turning
	// it on drops any pending temporary limit, since the two are one override with
	// two modes and the interface disables the numeric fields meanwhile.
	if input.Auto != nil {
		value := "0"
		if *input.Auto {
			value = "1"
		}
		// The Auto exception lasts the minutes given (0 = until removed).
		until := int64(0)
		if minutes := input.Minutes; *input.Auto && minutes > 0 {
			if minutes > 24*60 {
				minutes = 24 * 60
			}
			until = time.Now().Unix() + minutes*60
		}
		for _, pair := range [][2]string{
			{"torrent_temp_auto", value},
			{"torrent_temp_limit_enabled", "0"},
			{"torrent_temp_limit_until", strconv.FormatInt(until, 10)},
		} {
			if err := saveConfigSetting(s.cfg.DataDir, pair[0], pair[1]); err != nil {
				jsonError(w, http.StatusInternalServerError, fmt.Sprintf("salvataggio eccezione Auto: %v", err))
				return
			}
		}
		cfg := latestConfig(s)
		applySpeedPolicy(cfg, s.activeEngine(), s.latency_guard)
		jsonResponse(w, map[string]any{"ok": true, "auto": *input.Auto})
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
	minutes := input.Minutes
	if minutes < 0 {
		minutes = 0
	} else if minutes > 24*60 {
		minutes = 24 * 60
	}
	until := int64(0)
	if !input.Clear && minutes != 0 {
		until = time.Now().Unix() + minutes*60
	}
	enabled := "1"
	if input.Clear {
		enabled = "0"
	}
	pairs := [][2]string{
		{"torrent_temp_dl_limit", strconv.FormatInt(download, 10)},
		{"torrent_temp_ul_limit", strconv.FormatInt(upload, 10)},
		{"torrent_temp_limit_enabled", enabled},
		{"torrent_temp_limit_until", strconv.FormatInt(until, 10)},
	}
	// A numeric temporary limit and "auto speed" are one override with two
	// modes: applying real numbers (or removing the limit) turns auto off. An
	// empty "Applica" — the three fields are disabled while auto is on, so they
	// arrive empty — must not silence auto on its own.
	if input.Clear || download > 0 || upload > 0 || minutes > 0 {
		pairs = append(pairs, [2]string{"torrent_temp_auto", "0"})
	}
	for _, pair := range pairs {
		if err := saveConfigSetting(s.cfg.DataDir, pair[0], pair[1]); err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("salvataggio eccezione: %v", err))
			return
		}
	}
	cfg := latestConfig(s)
	applySpeedPolicy(cfg, s.activeEngine(), s.latency_guard)
	jsonResponse(w, map[string]any{
		"ok":           true,
		"until":        until,
		"enabled":      !input.Clear,
		"permanent":    !input.Clear && minutes == 0,
		"download_kib": download,
		"upload_kib":   upload,
	})
}

// GetTempLimits reports the speed policy in force: the temporary limit (with
// its expiry), the bandwidth schedule and the base limits, plus which one is
// applied now. The TUI shows it on the downloads tab.
func GetTempLimits(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	now := time.Now().Unix()
	until := parseSettingInt(cfg, "torrent_temp_limit_until")
	tempActive := settingTruthy(cfg.Settings["torrent_temp_limit_enabled"]) && (until == 0 || until > now)
	remaining := int64(0)
	if tempActive && until > 0 {
		remaining = until - now
	}
	schedDownload, schedUpload, schedActive := scheduledSpeedLimits(cfg)
	download, upload, source := speedPolicy(cfg)
	if source == speedSourceTempKeep {
		source = speedSourceTemp
	}
	jsonResponse(w, map[string]any{
		"source":             source,
		"download_kib":       download,
		"upload_kib":         upload,
		"temp_auto":          settingTruthy(cfg.Settings["torrent_temp_auto"]),
		"temp_active":        tempActive,
		"temp_download_kib":  parseSettingInt(cfg, "torrent_temp_dl_limit"),
		"temp_upload_kib":    parseSettingInt(cfg, "torrent_temp_ul_limit"),
		"temp_until":         until,
		"temp_remaining_sec": remaining,
		"sched_active":       schedActive,
		"sched_download_kib": schedDownload,
		"sched_upload_kib":   schedUpload,
		"base_download_kib":  max(cfg.Transfer.DownloadLimitKib, 0),
		"base_upload_kib":    max(cfg.Transfer.UploadLimitKib, 0),
		// The latency guard of the Auto speed: latency, idle baseline and the
		// caps it is applying (0 = none).
		"latency_guard": s.latency_guard.status(),
		// Shown next to the limits on the TUI downloads tab.
		"auto_remove_completed": cfg.Transfer.AutoRemoveCompleted,
	})
}

// TorrentPeers returns the peers of a torrent.
func TorrentPeers(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	peers, found, err := s.activeEngine().Peers(hash)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !found {
		jsonError(w, http.StatusNotFound, "torrent unavailable in dry-run")
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "peers": peers})
}

func int64PtrEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// torrentDisplayName returns the real torrent name, falling back to "unnamed
// torrent".
func torrentDisplayName(torrents TorrentSession, hash string) string {
	if torrents == nil {
		return "unnamed torrent"
	}
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

// gh7_mismatched_pack_release returns the configured release when the torrent's
// real name identifies a different season.
func gh7MismatchedPackRelease(torrents TorrentSession, db *Database, hash string) *models.Release {
	if db == nil {
		return nil
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return nil
	}
	release := meta.Release
	if release.Kind != "series" || !release.IsPack {
		return nil
	}
	name := torrentDisplayName(torrents, hash)
	corrected := ReconcilePackIdentity(&release, name)
	if corrected == nil {
		return nil
	}
	if !int64PtrEqual(corrected.Season, release.Season) {
		return &release
	}
	return nil
}

// gh7_blocklist_mismatched_pack permanently blocklists a mismatched season
// pack so gap filling cannot pick it again.
func gh7BlocklistMismatchedPack(torrents TorrentSession, db *Database, hash string) bool {
	release := gh7MismatchedPackRelease(torrents, db, hash)
	if release == nil {
		return false
	}
	if db == nil {
		return false
	}
	if blocked, err := db.IsBlocklisted(hash); err == nil && blocked {
		return true
	}
	if err := db.Blocklist(release, "season_pack_identity_mismatch"); err != nil {
		logging.Warn("could not blocklist mismatched season pack", "hash", hash, "error", err.Error())
		return false
	}
	logging.Warn("mismatched season pack permanently blocklisted", "hash", hash, "title", release.Title)
	return true
}

// Magnet tracker parameters are form-encoded: a space becomes '+', unreserved
// characters stay as they are and the rest becomes %XX.
func formUrlencode(value string) string {
	var builder strings.Builder
	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch {
		case ch == ' ':
			builder.WriteByte('+')
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') ||
			ch == '*' || ch == '-' || ch == '.' || ch == '_':
			builder.WriteByte(ch)
		default:
			fmt.Fprintf(&builder, "%%%02X", ch)
		}
	}
	return builder.String()
}

// Reports whether path is root itself or lies inside it, comparing whole
// components so that /media and /media2 do not match.
func gh7PathStartsWith(path, root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if cleanPath == cleanRoot {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator))
}

// ExportMagnet handles GET /api/torrents/{hash}/magnet.
func ExportMagnet(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	magnet := ""
	if s.db != nil {
		if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
			candidate := meta.Release.Magnet
			if strings.HasPrefix(candidate, "magnet:") {
				magnet = candidate
			}
		}
	}
	if magnet == "" {
		magnet = "magnet:?xt=urn:btih:" + hash
	}
	if s.hasTorrentEngine() {
		if trackers, ok, err := s.activeEngine().Trackers(hash); err == nil && ok {
			for _, tracker := range trackers {
				if !strings.Contains(magnet, tracker.URL) {
					magnet += "&tr=" + formUrlencode(tracker.URL)
				}
			}
		}
	}
	jsonResponse(w, map[string]any{"ok": true, "magnet": magnet})
}

// IpfilterUpdate handles POST /api/torrents/ipfilter_update.
func IpfilterUpdate(w http.ResponseWriter, r *http.Request, s *AppState) {
	rules, path, err := applyIPFilter(r.Context(), s)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "rules": rules, "path": path})
}

// MoveTorrentStorage handles POST /api/torrents/{hash}/storage.
func MoveTorrentStorage(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input StoragePath
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Path) == "" || len(input.Path) > 4096 {
		jsonError(w, http.StatusBadRequest, "storage path is required")
		return
	}
	cfg := latestConfig(s)
	destination := strings.TrimSpace(input.Path)
	allowed := gh7PathStartsWith(destination, cfg.DownloadDir) ||
		(cfg.DownloadTempDir != nil && gh7PathStartsWith(destination, *cfg.DownloadTempDir)) ||
		(cfg.ArchiveRoot != nil && gh7PathStartsWith(destination, *cfg.ArchiveRoot)) ||
		isConfiguredArchive(cfg, s.db, hash, destination)
	if !allowed {
		jsonError(w, http.StatusForbidden, "storage path must be inside a configured Gextto directory")
		return
	}
	name := hash
	for _, torrent := range s.activeEngine().List() {
		if strings.EqualFold(torrent.Hash, hash) {
			name = torrent.Name
			break
		}
	}
	target := filepath.Join(destination, name)
	logging.Info("manual torrent storage move requested",
		"hash", hash, "name", name, "destination", destination)
	if _, statErr := os.Stat(target); statErr == nil {
		// The destination already holds the payload: associate it (change the
		// save path and re-check) instead of failing. This turns a common case
		// into an advantage — the torrent seeds from the files already on disk.
		logging.Info("destination already contains the torrent data; associating existing files",
			"hash", hash, "name", name, "destination", destination, "target", target)
		associated, assocErr := s.activeEngine().AssociateStorage(hash, destination)
		if assocErr != nil {
			logging.Error("associate existing torrent data failed", "hash", hash, "name", name, "error", assocErr.Error())
			jsonError(w, http.StatusBadRequest, assocErr.Error())
			return
		}
		if !associated && !cfg.DryRun {
			jsonError(w, http.StatusConflict, "torrent unavailable")
			return
		}
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":         true,
			"associated": true,
			"path":       destination,
			"message":    "data già presente: torrent associato e in ricontrollo",
		})
		return
	}
	result, err := s.activeEngine().MoveStorage(hash, destination)
	switch {
	case err != nil:
		logging.Error("manual torrent storage move failed to start",
			"hash", hash, "name", name, "destination", destination, "error", err.Error())
	case result:
		logging.Info("manual torrent storage move accepted by the torrent engine; waiting for completion",
			"hash", hash, "name", name, "destination", destination)
	default:
		logging.Warn("manual torrent storage move not started: torrent session unavailable",
			"hash", hash, "name", name, "destination", destination)
	}
	torrentAction(w, result, err)
}

// isConfiguredArchive reports whether destination is inside a library
// folder Gextto itself would use: the archive of a configured series, or the
// destination configured for this torrent's release (tag rules, series
// archive). Post-seed moves go there, so a manual move must be allowed too.
func isConfiguredArchive(cfg *Config, db *Database, hash, destination string) bool {
	for index := range cfg.Series {
		if archive := strings.TrimSpace(cfg.Series[index].ArchivePath); archive != "" && gh7PathStartsWith(destination, archive) {
			return true
		}
	}
	if db == nil {
		return false
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return false
	}
	configured, ok := ConfiguredDestinationFor(&meta.Release, cfg)
	return ok && strings.TrimSpace(configured) != "" && gh7PathStartsWith(destination, configured)
}

// ReannounceTorrent handles POST /api/torrents/{hash}/reannounce.
func ReannounceTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	result, err := s.activeEngine().Reannounce(hash)
	torrentAction(w, result, err)
}

// RemoveTorrent handles DELETE /api/torrents/{hash}.
func RemoveTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	gh7BlocklistMismatchedPack(s.activeEngine(), s.db, hash)
	deleteFiles := r.URL.Query().Get("delete_files") == "1" || r.URL.Query().Get("delete_files") == "true"
	cfg := latestConfig(s)
	removed, err := SafeRemoveTorrent(s, cfg, hash, deleteFiles)
	torrentAction(w, removed, err)
}

// ResumeTorrent handles POST /api/torrents/{hash}/resume.
func ResumeTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	result, err := s.activeEngine().Resume(hash)
	torrentAction(w, result, err)
}

// SetTorrentLimits handles POST /api/torrents/{hash}/limits.
func SetTorrentLimits(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input TorrentLimits
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The fields the request does not carry stay as they are: an update of the
	// seed days must not reset the ratio to the global value (or the rates to
	// "unlimited"), which is what a client that edits one field at a time sends.
	result, err := s.activeEngine().SetLimits(hash, input.DownloadLimit, input.UploadLimit, input.SeedRatio, input.SeedDays)
	if err != nil {
		torrentAction(w, result, err)
		return
	}
	if input.MaxConnections != nil || input.MaxUploads != nil {
		maxConnections, maxUploads := int64(-1), int64(-1)
		if input.MaxConnections != nil {
			maxConnections = *input.MaxConnections
		}
		if input.MaxUploads != nil {
			maxUploads = *input.MaxUploads
		}
		if maxConnections >= 0 {
			if _, err := s.activeEngine().SetMaxConnections(hash, int(maxConnections)); err != nil {
				jsonError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if maxUploads >= 0 {
			if _, err := s.activeEngine().SetMaxUploads(hash, int(maxUploads)); err != nil {
				jsonError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	torrentAction(w, result, err)
}

// TorrentPeersLegacy handles POST /api/torrents/peers.
func TorrentPeersLegacy(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TorrentHashInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	legacy := r.Clone(r.Context())
	legacy.URL.Path = "/api/torrents/" + input.Hash + "/peers"
	legacy.URL.RawPath = ""
	legacy.SetPathValue("hash", input.Hash)
	TorrentPeers(w, legacy, s)
}

// AddRelease queues a release for download, falling back to a search of its title
// when its URL cannot be fetched.
func AddRelease(s *AppState, release models.Release) (int, any) {
	if !SetupComplete(LatestConfig(s)) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	source := strings.TrimSpace(release.Magnet)
	if source == "" && release.TorrentURL != nil {
		source = strings.TrimSpace(*release.TorrentURL)
	}
	isURL := isHTTPURL(source)
	status, value := AddParsedRelease(s, release)
	if isURL && status == http.StatusBadRequest {
		if found := ResolveBySearch(s, release.Title); found != nil {
			retryStatus, retryValue := AddParsedRelease(s, *found)
			if retryStatus != http.StatusBadRequest {
				return retryStatus, retryValue
			}
		}
	}
	return status, value
}

// AddParsedRelease queues a release: an eD2k link through aMule, a magnet or a
// .torrent URL through the active torrent engine.
func AddParsedRelease(s *AppState, release models.Release) (int, any) {
	if strings.TrimSpace(release.Ed2kLink) != "" {
		link, err := ed2k.ParseFileLink(release.Ed2kLink)
		if err != nil {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
		}
		if !ed2kEnabled(latestConfig(s)) {
			return http.StatusConflict, map[string]any{
				"ok":    false,
				"error": "eD2k downloads need aMule: enable it in Settings → eD2k and Kad",
				"link":  link.String(),
			}
		}
		if err := AddEd2kLink(latestConfig(s), link.String()); err != nil {
			logging.Warn("manual eD2k download refused by aMule", "title", link.Name, "error", err.Error(), "hash", strings.ToLower(link.Hash))
			status := http.StatusBadGateway
			if strings.Contains(strings.ToLower(err.Error()), "already") {
				status = http.StatusConflict
			}
			return status, map[string]any{"ok": false, "error": err.Error(), "link": link.String()}
		}
		noteEd2kGrab(release.GrabURL)
		if !isVideoExtension(link.Name) {
			// Not a video (a pdf, a program, an archive): it is aMule's
			// download only, like a torrent added by hand, and stays in the
			// incoming folder instead of going through the library import.
			logging.Info("manual eD2k download queued; not a video, it stays in aMule's incoming folder",
				"title", link.Name, "hash", strings.ToLower(link.Hash))
			return http.StatusAccepted, map[string]any{
				"ok":         true,
				"title":      link.Name,
				"hash":       strings.ToLower(link.Hash),
				"link":       link.String(),
				"queued":     true,
				"registered": false,
			}
		}
		release.Network = networkEd2k
		if release.Title == "" {
			release.Title = link.Name
		}
		identifyRelease(&release)
		score := latestConfig(s).ReleaseScore(&release)
		if err := s.db.RegisterTorrentScored(&release, score); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
		_ = s.db.SetTorrentReason(strings.ToLower(link.Hash), "manual")
		// Logged after the registration, so the history row gets the title,
		// series and episode of the download.
		logging.Info("manual eD2k download queued", "title", release.Title, "score", score, "hash", strings.ToLower(link.Hash))
		return http.StatusAccepted, map[string]any{
			"ok":     true,
			"title":  release.Title,
			"hash":   strings.ToLower(link.Hash),
			"link":   link.String(),
			"queued": true,
		}
	}
	source := strings.TrimSpace(release.Magnet)
	if source == "" && release.TorrentURL != nil {
		source = strings.TrimSpace(*release.TorrentURL)
	}
	if source == "" {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": "release has no magnet or torrent URL"}
	}
	if isHTTPURL(source) {
		hash, err := DownloadAndAdd(s, source, AddOptions{})
		if err != nil {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
		}
		if hash == nil {
			return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
		}
		release.Magnet = "magnet:?xt=urn:btih:" + *hash
		score := latestConfig(s).ReleaseScore(&release)
		if err := s.db.RegisterTorrentScored(&release, score); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
		_ = s.db.SetTorrentReason(*hash, "manual")
		return http.StatusAccepted, map[string]any{"ok": true, "title": release.Title}
	}
	score := latestConfig(s).ReleaseScore(&release)
	if err := s.db.RegisterTorrentScored(&release, score); err != nil {
		return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
	}
	if hash, ok := utils.MagnetHash(release.Magnet); ok {
		_ = s.db.SetTorrentReason(hash, "manual")
	}
	title := release.Title
	logHash, _ := utils.MagnetHash(release.Magnet)
	cfg := LatestConfig(s)
	go func() {
		defer recoverGoroutine("g3 background")
		added, err := s.activeEngine().Add(source, cfg)
		switch {
		case err != nil:
			logging.Error(fmt.Sprintf("❌ Could not add «%s» to the downloads: %v", title, err), "hash", logHash)
		case added:
			logging.Info(fmt.Sprintf("📥 «%s» added to the downloads by hand (score %v)", title, score), "hash", logHash)
		default:
			logging.Info(fmt.Sprintf("«%s» is already among the downloads", title), "hash", logHash)
		}
	}()
	return http.StatusAccepted, map[string]any{"ok": true, "title": title, "queued": true}
}

// identifyRelease fills the identity of a release that arrives without one (an
// eD2k search result or a pasted link carries only its file name): kind,
// series, season, episode, pack and quality are parsed from the title, as for
// any archive release. Without them the completion cannot tell an episode from
// a film and has no library folder to import it into. A release already
// identified is left as it is.
func identifyRelease(release *models.Release) {
	if release == nil || release.Kind != "" || strings.TrimSpace(release.Title) == "" {
		return
	}
	// The parser keys a release on a magnet; an eD2k file has none, so its
	// link's hash stands in for it, as in ParseEd2kRelease.
	magnet := release.Magnet
	if magnet == "" && release.Ed2kLink != "" {
		if link, err := ed2k.ParseFileLink(release.Ed2kLink); err == nil {
			magnet = "magnet:?xt=urn:btih:" + link.Hash
		}
	}
	parsed := ParseRelease(release.Title, magnet, release.Source)
	if parsed == nil {
		return
	}
	release.Kind = parsed.Kind
	release.Series = parsed.Series
	release.Season = parsed.Season
	release.Episode = parsed.Episode
	release.IsPack = parsed.IsPack
	release.EpisodeRange = parsed.EpisodeRange
	if release.Quality.Resolution == "" {
		release.Quality = parsed.Quality
	}
}

// DownloadAndAdd adds a magnet link, or fetches and adds a .torrent file; it
// returns the info hash, and nil when the torrent was already there.
func DownloadAndAdd(s *AppState, rawURL string, options AddOptions) (*string, error) {
	cfg := LatestConfig(s)
	rawURL = strings.TrimSpace(rawURL)
	addMagnet := func(m string) (*string, error) {
		hash, ok := utils.MagnetHash(m)
		if !ok {
			return nil, fmt.Errorf("invalid magnet link")
		}
		added, err := s.activeEngine().AddWithOptions(m, cfg, nil, options)
		if err != nil {
			return nil, err
		}
		if !added {
			return nil, nil
		}
		return &hash, nil
	}
	if strings.HasPrefix(rawURL, "magnet:?") {
		return addMagnet(rawURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	payload, magnet, err := s.engine.FetchTorrent(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if magnet != "" {
		return addMagnet(magnet)
	}
	path := filepath.Join(cfg.StateDir, fmt.Sprintf(".manual-%s.torrent", newUUID()))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return nil, err
	}
	defer os.Remove(path)
	return s.activeEngine().AddTorrentFileWithOptions(path, cfg, nil, options)
}

// ResolveBySearch searches for a release whose title starts with the one given
// and whose source can be used.
func ResolveBySearch(s *AppState, title string) *models.Release {
	query := SearchQueryFromTitle(title)
	normalized := normalizeSearch(query)
	if normalized == "" {
		return nil
	}
	results := s.engine.SearchQueryManual(context.Background(), LatestConfig(s), query)
	for i := range results {
		release := &results[i]
		if SourceIsUsable(release.Magnet) && strings.HasPrefix(normalizeSearch(release.Title), normalized) {
			return release
		}
	}
	return nil
}

// SearchQueryFromTitle turns a release title into a search query: it keeps the
// leading words and stops at the year or at the first quality marker.
func SearchQueryFromTitle(title string) string {
	head := title
	if index := strings.Index(title, "["); index >= 0 {
		head = title[:index]
	}
	cleaned := make([]rune, 0, len(head))
	for _, character := range head {
		if (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') {
			cleaned = append(cleaned, character)
		} else {
			cleaned = append(cleaned, ' ')
		}
	}
	markers := map[string]struct{}{
		"4k": {}, "uhd": {}, "bluray": {}, "bdrip": {}, "web": {}, "webdl": {},
		"webrip": {}, "hdtv": {}, "dvdrip": {}, "h264": {}, "h265": {}, "x264": {},
		"x265": {}, "hevc": {}, "avc": {}, "aac": {}, "ac3": {}, "eac3": {},
		"ddp": {}, "dts": {}, "proper": {}, "repack": {},
	}
	words := []string{}
	for _, word := range strings.Fields(string(cleaned)) {
		lower := strings.ToLower(word)
		isYear := len(word) == 4 && allASCIIDigits(word)
		_, isMarker := markers[lower]
		if !isMarker {
			isMarker = strings.Contains(lower, "1080p") ||
				strings.Contains(lower, "720p") ||
				strings.Contains(lower, "2160p") ||
				strings.Contains(lower, "480p")
		}
		if isMarker {
			break
		}
		words = append(words, word)
		if isYear || len(words) >= 6 {
			break
		}
	}
	return strings.Join(words, " ")
}

// SourceIsUsable reports whether the source is an HTTP URL or a magnet link with
// a valid info hash.
func SourceIsUsable(source string) bool {
	source = strings.TrimSpace(source)
	if isHTTPURL(source) {
		return true
	}
	if !strings.HasPrefix(source, "magnet:") {
		return false
	}
	_, ok := utils.MagnetHash(source)
	return ok
}
