package gextto

// gxtorrent_engine.go adapts the gx-torrent daemon (cmd/gx-torrent, built on
// rain) to the TorrentEngine contract.
//
// Unlike qBittorrent, gx-torrent owns its queue: Gextto pushes the slot policy
// (active downloads/seeds, hard cap, slow-torrent handling, dynamic queue,
// global limits) and the daemon starts and stops torrents itself. Gextto keeps
// the stall monitor (park with MarkStalled, probe with Restart, give up), the
// seed policy and the post-processing.
//
// As with qBittorrent:
//   - polling is the source of truth: every List() refreshes the snapshot and
//     diffs the previous one to emit lifecycle events;
//   - an unreachable daemon yields the stale cache and SessionHealthy=false,
//     never an empty list that could be mistaken for removed torrents;
//   - unsupported operations return ErrCapabilityUnavailable.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// gxTorrentSettings is the resolved adapter configuration.
type gxTorrentSettings struct {
	BaseURL      string
	Token        string
	Timeout      time.Duration
	PollInterval time.Duration
	Managed      bool
	Binary       string
	stateDir     string
	dataDir      string
}

func gxTorrentSettingsFromConfig(cfg *Config) (gxTorrentSettings, error) {
	if cfg == nil {
		return gxTorrentSettings{}, fmt.Errorf("gx-torrent configuration is unavailable")
	}
	timeout := 15 * time.Second
	if raw := strings.TrimSpace(cfg.Settings["gxtorrent_request_timeout_secs"]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 300 {
			return gxTorrentSettings{}, fmt.Errorf("gxtorrent_request_timeout_secs must be between 1 and 300")
		}
		timeout = time.Duration(parsed) * time.Second
	}
	poll := 1500 * time.Millisecond
	if raw := strings.TrimSpace(cfg.Settings["gxtorrent_poll_interval_ms"]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || (parsed != 0 && (parsed < 250 || parsed > 60000)) {
			return gxTorrentSettings{}, fmt.Errorf("gxtorrent_poll_interval_ms must be between 250 and 60000")
		}
		poll = time.Duration(parsed) * time.Millisecond
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Settings["gxtorrent_url"]), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8890"
	}
	if parsed, err := url.Parse(baseURL); err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return gxTorrentSettings{}, fmt.Errorf("gxtorrent_url must be a complete http(s) URL")
	}
	return gxTorrentSettings{
		BaseURL:      baseURL,
		Token:        strings.TrimSpace(cfg.Settings["gxtorrent_token"]),
		Timeout:      timeout,
		PollInterval: poll,
		Managed:      settingsBool(cfg, "gxtorrent_managed", true),
		Binary:       strings.TrimSpace(cfg.Settings["gxtorrent_binary"]),
		stateDir:     cfg.StateDir,
		dataDir:      cfg.DataDir,
	}, nil
}

// gxTorrentEngine is a TorrentEngine backed by the gx-torrent daemon.
type gxTorrentEngine struct {
	settings gxTorrentSettings
	client   *http.Client
	cfg      *Config

	processMu      sync.Mutex
	process        *gxManagedProcess
	supervisorStop chan struct{}
	closed         bool

	mu           sync.Mutex
	syncMu       sync.Mutex
	cache        map[string]models.TorrentView
	previous     map[string]models.TorrentView
	events       []models.TorrentEvent
	pendingMoves map[string]string
	lastSync     time.Time
	lastAttempt  time.Time
	lastErr      string
	connected    bool
	queuePushed  string
	speedPushed  string
}

var _ TorrentEngine = (*gxTorrentEngine)(nil)

// newGxTorrentEngine builds the adapter and, in managed mode, starts the
// daemon. It does not require the daemon to answer: an outage degrades to a
// stale view and the adapter keeps retrying.
func newGxTorrentEngine(cfg *Config) (*gxTorrentEngine, error) {
	settings, err := gxTorrentSettingsFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	engine := &gxTorrentEngine{
		settings:       settings,
		client:         &http.Client{Timeout: settings.Timeout},
		cfg:            cfg,
		supervisorStop: make(chan struct{}),
		cache:          map[string]models.TorrentView{},
		previous:       map[string]models.TorrentView{},
		pendingMoves:   map[string]string{},
	}
	if settings.Managed && !engine.ping() {
		process, err := startManagedGxTorrent(cfg, settings)
		if err != nil {
			return nil, err
		}
		engine.process = process
		go engine.superviseManagedProcess()
		engine.waitReady(15 * time.Second)
	}
	return engine, nil
}

func (e *gxTorrentEngine) Name() string { return BackendGxTorrent }

func (e *gxTorrentEngine) Capabilities() map[string]bool {
	return capabilitiesFor(BackendGxTorrent)
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

// ErrTorrentV2Unsupported is returned when gx-torrent refuses a BitTorrent
// v2-only torrent (rain handles v1 and hybrid torrents).
var ErrTorrentV2Unsupported = errors.New("BitTorrent v2-only torrent: not supported by gx-torrent")

// gxAPIError is a non-2xx answer from the daemon.
type gxAPIError struct {
	Status  int
	Message string
}

func (e gxAPIError) Unwrap() error {
	if strings.HasPrefix(e.Message, "v2_unsupported") {
		return ErrTorrentV2Unsupported
	}
	return nil
}

func (e gxAPIError) Error() string {
	if e.Status == http.StatusNotFound {
		return "gx-torrent: torrent not found"
	}
	if e.Message != "" {
		return "gx-torrent: " + e.Message
	}
	return fmt.Sprintf("gx-torrent: HTTP %d", e.Status)
}

func (e *gxTorrentEngine) do(method, path string, body io.Reader, contentType string, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), e.settings.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, e.settings.BaseURL+path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if e.settings.Token != "" {
		req.Header.Set("X-Gx-Token", e.settings.Token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error string `json:"error"`
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = json.Unmarshal(data, &payload)
		return gxAPIError{Status: resp.StatusCode, Message: payload.Error}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw, err = io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *gxTorrentEngine) postForm(path string, form url.Values) error {
	return e.do(http.MethodPost, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil)
}

func (e *gxTorrentEngine) action(hash, action string, form url.Values) error {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return fmt.Errorf("gx-torrent: missing hash")
	}
	if form == nil {
		form = url.Values{}
	}
	return e.postForm("/api/v1/torrents/"+url.PathEscape(hash)+"/"+action, form)
}

func (e *gxTorrentEngine) ping() bool {
	var health struct {
		OK bool `json:"ok"`
	}
	return e.do(http.MethodGet, "/api/v1/health", nil, "", &health) == nil && health.OK
}

func (e *gxTorrentEngine) waitReady(limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if e.ping() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// ---------------------------------------------------------------------------
// state normalization
// ---------------------------------------------------------------------------

// gxTorrentItem mirrors cmd/gx-torrent torrentInfo.
type gxTorrentItem struct {
	Hash           string  `json:"hash"`
	Name           string  `json:"name"`
	State          string  `json:"state"`
	SavePath       string  `json:"save_path"`
	Progress       float64 `json:"progress"`
	TotalSize      int64   `json:"total_size"`
	TotalDone      int64   `json:"total_done"`
	DownloadRate   int64   `json:"download_rate"`
	UploadRate     int64   `json:"upload_rate"`
	Downloaded     int64   `json:"downloaded"`
	Uploaded       int64   `json:"uploaded"`
	SeedingSeconds int64   `json:"seeding_seconds"`
	ActiveSeconds  int64   `json:"active_seconds"`
	QueuePosition  int     `json:"queue_position"`
	NumPeers       int     `json:"num_peers"`
	NumSeeds       int     `json:"num_seeds"`
	NumComplete    int     `json:"num_complete"`
	NumIncomplete  int     `json:"num_incomplete"`
	SeedRatio      float64 `json:"seed_ratio"`
	SeedDays       int64   `json:"seed_days"`
	HasMetadata    bool    `json:"has_metadata"`
	AutoManaged    bool    `json:"auto_managed"`
	Parked         bool    `json:"parked"`
	Error          string  `json:"error"`
	CompletedAt    int64   `json:"completed_at"`
	CurrentTracker string  `json:"current_tracker"`
}

func (e *gxTorrentEngine) toView(item gxTorrentItem, now time.Time) models.TorrentView {
	progress := math.Max(0, math.Min(100, item.Progress))
	view := models.TorrentView{
		Hash:              strings.ToLower(item.Hash),
		Name:              item.Name,
		SavePath:          item.SavePath,
		Progress:          progress,
		State:             item.State,
		DownloadRate:      uint64(maxInt64(0, item.DownloadRate)),
		UploadRate:        uint64(maxInt64(0, item.UploadRate)),
		DownloadRateTotal: uint64(maxInt64(0, item.DownloadRate)),
		UploadRateTotal:   uint64(maxInt64(0, item.UploadRate)),
		DownloadLimit:     -1,
		UploadLimit:       -1,
		AllTimeUpload:     item.Uploaded,
		AllTimeDownload:   item.Downloaded,
		SeedingSeconds:    item.SeedingSeconds,
		ActiveSeconds:     item.ActiveSeconds,
		QueuePosition:     item.QueuePosition,
		NumPeers:          item.NumPeers,
		NumSeeds:          item.NumSeeds,
		NumConnections:    item.NumPeers,
		NumComplete:       item.NumComplete,
		NumIncomplete:     item.NumIncomplete,
		SeedRatio:         item.SeedRatio,
		SeedDays:          item.SeedDays,
		HasMetadata:       item.HasMetadata,
		AutoManaged:       item.AutoManaged,
		TorrentVersion:    "v1",
		TotalSize:         item.TotalSize,
		TotalDone:         item.TotalDone,
		IsSeeding:         item.State == "seeding",
		CurrentTracker:    item.CurrentTracker,
		Error:             strings.TrimSpace(item.Error),
		Stalled:           item.State == "stalled",
	}
	if item.CompletedAt > 0 {
		view.FinishedSeconds = int64(now.Sub(time.Unix(item.CompletedAt, 0)).Seconds())
	}
	view.Diagnosis, _, _ = DiagnoseTorrent(&view)
	return view
}

// ---------------------------------------------------------------------------
// polling / reconciliation
// ---------------------------------------------------------------------------

// sync refreshes the snapshot (throttled by the poll interval) and emits
// lifecycle events for the transitions since the previous one.
func (e *gxTorrentEngine) sync() error {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()

	now := time.Now()
	e.mu.Lock()
	if e.settings.PollInterval > 0 && !e.lastAttempt.IsZero() && now.Sub(e.lastAttempt) < e.settings.PollInterval {
		e.mu.Unlock()
		return nil
	}
	e.lastAttempt = now
	e.mu.Unlock()

	var items []gxTorrentItem
	err := e.do(http.MethodGet, "/api/v1/torrents", nil, "", &items)

	e.mu.Lock()
	if err != nil {
		wasConnected := e.connected
		e.connected = false
		e.lastErr = err.Error()
		e.mu.Unlock()
		if wasConnected {
			logging.Warn("gx-torrent non raggiungibile", "url", e.settings.BaseURL, "error", err)
		}
		return err
	}
	reconnected := !e.connected
	e.connected = true
	e.lastErr = ""
	e.lastSync = now

	next := make(map[string]models.TorrentView, len(items))
	for _, item := range items {
		view := e.toView(item, now)
		if view.Hash != "" {
			next[view.Hash] = view
		}
	}
	for hash, view := range next {
		if previous, known := e.previous[hash]; known {
			e.diffLocked(previous, view)
		}
	}
	// A move completes when the reported save path reaches the destination;
	// it fails when the daemon reports the move error.
	for hash, destination := range e.pendingMoves {
		view, ok := next[hash]
		switch {
		case !ok:
			delete(e.pendingMoves, hash)
		case view.State == "moving":
		case SamePath(view.SavePath, destination):
			e.events = append(e.events, models.TorrentEvent{
				Kind: "storage_moved", Hash: hash, Name: view.Name, SavePath: view.SavePath,
			})
			delete(e.pendingMoves, hash)
		case strings.HasPrefix(view.Error, "move failed"):
			e.events = append(e.events, models.TorrentEvent{
				Kind: "storage_move_failed", Hash: hash, Name: view.Name, SavePath: view.SavePath, Message: view.Error,
			})
			delete(e.pendingMoves, hash)
		}
	}
	e.previous = next
	e.cache = next
	var export []string
	if e.settings.stateDir != "" {
		for hash, view := range next {
			if view.HasMetadata && !fileExists(filepath.Join(e.settings.stateDir, hash+".torrent")) {
				export = append(export, hash)
			}
		}
	}
	e.mu.Unlock()
	if reconnected {
		logging.Info("gx-torrent connesso", "url", e.settings.BaseURL)
		e.mu.Lock()
		// Re-push the queue policy and the limits: a restarted daemon must not
		// run with stale values.
		e.queuePushed = ""
		e.speedPushed = ""
		e.mu.Unlock()
	}
	for _, hash := range export {
		e.ensureTorrentFile(hash)
	}
	return nil
}

// diffLocked turns snapshot transitions into Gextto's lifecycle events.
func (e *gxTorrentEngine) diffLocked(previous, current models.TorrentView) {
	if !previous.HasMetadata && current.HasMetadata {
		e.events = append(e.events, models.TorrentEvent{
			Kind: "metadata_received", Hash: current.Hash, Name: current.Name, SavePath: current.SavePath,
		})
	}
	if previous.Progress < 99.99 && current.Progress >= 99.99 && current.TotalSize > 0 {
		e.events = append(e.events, models.TorrentEvent{
			Kind: "torrent_finished", Hash: current.Hash, Name: current.Name, SavePath: current.SavePath,
		})
	}
	if previous.State == "checking_files" && current.State != "checking_files" {
		e.events = append(e.events, models.TorrentEvent{
			Kind: "torrent_checked", Hash: current.Hash, Name: current.Name, SavePath: current.SavePath,
		})
	}
	if previous.State != "error" && current.State == "error" {
		e.events = append(e.events, models.TorrentEvent{
			Kind: "torrent_error", Hash: current.Hash, Name: current.Name,
			SavePath: current.SavePath, Message: current.Error,
		})
	}
}

// ensureTorrentFile keeps a Gextto-owned .torrent for export and migration.
func (e *gxTorrentEngine) ensureTorrentFile(hash string) {
	target := filepath.Join(e.settings.stateDir, hash+".torrent")
	if fileExists(target) {
		return
	}
	var data []byte
	if err := e.do(http.MethodGet, "/api/v1/torrents/"+hash+"/torrent-file", nil, "", &data); err != nil || len(data) == 0 {
		return
	}
	if err := os.MkdirAll(e.settings.stateDir, 0o755); err != nil {
		return
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, target)
}

// List returns the last snapshot, refreshing it first.
func (e *gxTorrentEngine) List() []models.TorrentView {
	_ = e.sync()
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]models.TorrentView, 0, len(e.cache))
	for _, view := range e.cache {
		result = append(result, view)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Hash < result[j].Hash })
	return result
}

// PollEvents drains the lifecycle events accumulated by the diffs.
func (e *gxTorrentEngine) PollEvents() []models.TorrentEvent {
	_ = e.sync()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.events) == 0 {
		return nil
	}
	out := e.events
	e.events = nil
	return out
}

// refresh forces the next List to hit the daemon (after a mutation).
func (e *gxTorrentEngine) refresh() {
	e.mu.Lock()
	e.lastAttempt = time.Time{}
	e.mu.Unlock()
}

// SessionHealthy reports whether List is backed by a live snapshot.
func (e *gxTorrentEngine) SessionHealthy() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.connected
}

// SyncStats exposes adapter health for the UI/API.
func (e *gxTorrentEngine) SyncStats() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	stats := map[string]any{
		"backend":      BackendGxTorrent,
		"connected":    e.connected,
		"torrents":     len(e.cache),
		"poll_ms":      e.settings.PollInterval.Milliseconds(),
		"pending_move": len(e.pendingMoves),
	}
	if !e.lastSync.IsZero() {
		stats["last_sync"] = e.lastSync.UTC().Format(time.RFC3339)
	}
	if e.lastErr != "" {
		stats["last_error"] = e.lastErr
	}
	e.processMu.Lock()
	stats["managed"] = e.process != nil
	e.processMu.Unlock()
	return stats
}

// Stats returns the daemon's transfer and queue counters.
func (e *gxTorrentEngine) Stats() map[string]any {
	stats := map[string]any{
		"backend":  BackendGxTorrent,
		"dry_run":  false,
		"torrents": len(e.List()),
	}
	var daemon map[string]any
	if err := e.do(http.MethodGet, "/api/v1/stats", nil, "", &daemon); err != nil {
		stats["session_loaded"] = false
		stats["error"] = err.Error()
		return stats
	}
	stats["session_loaded"] = true
	for key, value := range daemon {
		stats[key] = value
	}
	return stats
}

// ---------------------------------------------------------------------------
// queue and limits
// ---------------------------------------------------------------------------

// gxQueuePolicy translates Gextto's libtorrent queue settings for the daemon.
func gxQueuePolicy(cfg *Config) map[string]any {
	clamp := func(value int64) int {
		if value < 1 {
			return 1
		}
		if value > 10000 {
			return 10000
		}
		return int(value)
	}
	lt := cfg.Libtorrent
	return map[string]any{
		"active_downloads": clamp(lt.ActiveDownloads),
		"active_seeds":     clamp(lt.ActiveSeeds),
		"active_limit":     clamp(lt.ActiveLimit),
		"dont_count_slow":  lt.DontCountSlowTorrents,
		"dynamic_queue":    lt.DynamicQueue,
		"dynamic_min":      clamp(lt.DynamicQueueMin),
		"dynamic_max":      clamp(lt.DynamicQueueMax),
		"cache_mb":         gxCacheMB(lt.CacheSize),
		"cache_ttl_secs":   max(lt.CacheExpiry, 10),
		"preallocate":      cfg.LibtorrentPreallocate(),
	}
}

// gxCacheMB converts libtorrent_cache_size (16 KiB blocks, -1 automatic)
// into the daemon's MiB value (-1 automatic: sized from the RAM).
func gxCacheMB(blocks int64) int64 {
	if blocks <= 0 {
		return -1
	}
	return max((blocks*16+1023)/1024, 1)
}

func (e *gxTorrentEngine) pushConfig(values map[string]any, last *string) error {
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	e.mu.Lock()
	unchanged := *last == string(encoded)
	e.mu.Unlock()
	if unchanged {
		return nil
	}
	if err := e.do(http.MethodPost, "/api/v1/config", bytes.NewReader(encoded), "application/json", nil); err != nil {
		return err
	}
	e.mu.Lock()
	*last = string(encoded)
	e.mu.Unlock()
	return nil
}

// AdjustQueue pushes Gextto's queue policy to the daemon, which applies it
// by itself every few seconds (slots, slow torrents, stall rotation, dynamic
// queue). The push is skipped when nothing changed.
func (e *gxTorrentEngine) AdjustQueue(cfg *Config, effectiveDownloadKib int64) {
	if cfg == nil {
		return
	}
	policy := gxQueuePolicy(cfg)
	if err := e.pushConfig(policy, &e.queuePushed); err != nil {
		logging.Debug("gx-torrent queue policy push failed", "error", err.Error())
	}
	_ = effectiveDownloadKib // the daemon reads the global limit pushed by SetGlobalSpeedLimits
}

// SetGlobalSpeedLimits applies the global limits (KiB/s; 0 = unlimited).
func (e *gxTorrentEngine) SetGlobalSpeedLimits(downloadKib, uploadKib int64) (bool, error) {
	values := map[string]any{}
	if downloadKib >= 0 {
		values["speed_limit_download"] = downloadKib
	}
	if uploadKib >= 0 {
		values["speed_limit_upload"] = uploadKib
	}
	if len(values) == 0 {
		return true, nil
	}
	if err := e.pushConfig(values, &e.speedPushed); err != nil {
		return false, err
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// control operations
// ---------------------------------------------------------------------------

func (e *gxTorrentEngine) cachedState(hash string) (models.TorrentView, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	view, ok := e.cache[strings.ToLower(strings.TrimSpace(hash))]
	return view, ok
}

func (e *gxTorrentEngine) updateCached(hash string, update func(*models.TorrentView)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	hash = strings.ToLower(strings.TrimSpace(hash))
	if view, ok := e.cache[hash]; ok {
		update(&view)
		e.cache[hash] = view
	}
}

func (e *gxTorrentEngine) Pause(hash string) (bool, error) {
	if view, ok := e.cachedState(hash); ok && view.State == "paused" && !view.AutoManaged {
		return true, nil
	}
	if err := e.action(hash, "pause", nil); err != nil {
		return false, err
	}
	e.updateCached(hash, func(view *models.TorrentView) {
		view.State = "paused"
		view.AutoManaged = false
	})
	e.refresh()
	return true, nil
}

// Resume hands the torrent back to the daemon's queue: it starts at once
// when a slot is free, otherwise it waits its turn.
func (e *gxTorrentEngine) Resume(hash string) (bool, error) {
	if err := e.action(hash, "resume", nil); err != nil {
		return false, err
	}
	e.updateCached(hash, func(view *models.TorrentView) {
		view.AutoManaged = true
		view.Stalled = false
		if view.State == "stalled" {
			view.State = "paused"
		}
	})
	e.refresh()
	return true, nil
}

// Restart is the stall probe: the daemon runs the torrent outside the queue
// for a probe window and announces it again.
func (e *gxTorrentEngine) Restart(hash string) (bool, error) {
	if err := e.action(hash, "restart", nil); err != nil {
		return false, err
	}
	e.updateCached(hash, func(view *models.TorrentView) {
		view.Stalled = false
		if view.Progress >= 100 {
			view.State = "seeding"
		} else {
			view.State = "downloading"
		}
	})
	e.refresh()
	return true, nil
}

func (e *gxTorrentEngine) Reannounce(hash string) (bool, error) {
	if err := e.action(hash, "reannounce", nil); err != nil {
		return false, err
	}
	return true, nil
}

func (e *gxTorrentEngine) ForceRecheck(hash string) (bool, error) {
	if err := e.action(hash, "verify", nil); err != nil {
		return false, err
	}
	e.updateCached(hash, func(view *models.TorrentView) { view.State = "checking_files" })
	e.refresh()
	return true, nil
}

// MarkStalled parks the torrent in the daemon: stopped, out of the queue, and
// reported as "stalled" until Gextto probes or clears it.
func (e *gxTorrentEngine) MarkStalled(hash string) (bool, error) {
	if err := e.action(hash, "park", nil); err != nil {
		return false, err
	}
	e.updateCached(hash, func(view *models.TorrentView) {
		view.State = "stalled"
		view.Stalled = true
	})
	e.refresh()
	return true, nil
}

// ClearStalled returns a parked torrent to the queue.
func (e *gxTorrentEngine) ClearStalled(hash string) {
	if err := e.action(hash, "unpark", nil); err != nil && !torrentNotFoundError(err) {
		logging.Debug("gx-torrent unpark failed", "hash", hash, "error", err.Error())
		return
	}
	e.refresh()
}

// Remove drops the torrent. The daemon never deletes the payload unless
// deleteFiles is set, and then only the torrent's own file or folder.
func (e *gxTorrentEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return false, nil
	}
	path := "/api/v1/torrents/" + url.PathEscape(hash)
	if deleteFiles {
		path += "?delete_files=1"
	}
	if err := e.do(http.MethodDelete, path, nil, "", nil); err != nil {
		return false, err
	}
	e.mu.Lock()
	delete(e.cache, hash)
	delete(e.previous, hash)
	delete(e.pendingMoves, hash)
	e.mu.Unlock()
	e.refresh()
	return true, nil
}

// MoveStorage asks the daemon to relocate the payload. The move runs in the
// background; storage_moved is emitted once the reported path matches.
func (e *gxTorrentEngine) MoveStorage(hash, destination string) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" || strings.TrimSpace(destination) == "" {
		return false, nil
	}
	if view, ok := e.cachedState(hash); ok && SamePath(view.SavePath, destination) {
		return false, nil
	}
	if err := e.action(hash, "move", url.Values{"destination": {destination}}); err != nil {
		return false, err
	}
	e.mu.Lock()
	e.pendingMoves[hash] = destination
	e.mu.Unlock()
	e.refresh()
	return true, nil
}

// AssociateStorage points the torrent at data already in destination and
// verifies it there, without moving files.
func (e *gxTorrentEngine) AssociateStorage(hash, destination string) (bool, error) {
	if strings.TrimSpace(destination) == "" {
		return false, nil
	}
	if err := e.action(hash, "associate", url.Values{"destination": {destination}}); err != nil {
		return false, err
	}
	e.refresh()
	return true, nil
}

func (e *gxTorrentEngine) RamdiskUncommittedBytes(ramdisk string, excludeHash string) uint64 {
	var total uint64
	for _, view := range e.List() {
		if strings.EqualFold(view.Hash, excludeHash) || !PathOnRamdisk(view.SavePath, ramdisk) {
			continue
		}
		if remaining := view.TotalSize - view.TotalDone; remaining > 0 {
			total += uint64(remaining)
		}
	}
	return total
}

// ---------------------------------------------------------------------------
// inspection
// ---------------------------------------------------------------------------

func (e *gxTorrentEngine) Files(hash string) ([]models.FileView, bool, error) {
	var files []struct {
		Path       string `json:"path"`
		Size       int64  `json:"size"`
		Downloaded int64  `json:"downloaded"`
		Priority   int    `json:"priority"`
	}
	if err := e.do(http.MethodGet, "/api/v1/torrents/"+url.PathEscape(strings.ToLower(hash))+"/files", nil, "", &files); err != nil {
		return nil, false, err
	}
	out := make([]models.FileView, 0, len(files))
	for _, file := range files {
		out = append(out, models.FileView{Path: file.Path, Size: file.Size, Downloaded: file.Downloaded, Priority: file.Priority})
	}
	return out, true, nil
}

func (e *gxTorrentEngine) Peers(hash string) ([]models.PeerView, bool, error) {
	var peers []struct {
		Address      string  `json:"address"`
		Client       string  `json:"client"`
		DownloadRate int64   `json:"download_rate"`
		UploadRate   int64   `json:"upload_rate"`
		Incoming     bool    `json:"incoming"`
		Encrypted    bool    `json:"encrypted"`
		UTP          bool    `json:"utp"`
		Progress     float64 `json:"progress"`
		Seed         bool    `json:"seed"`
	}
	if err := e.do(http.MethodGet, "/api/v1/torrents/"+url.PathEscape(strings.ToLower(hash))+"/peers", nil, "", &peers); err != nil {
		return nil, false, err
	}
	out := make([]models.PeerView, 0, len(peers))
	for _, peer := range peers {
		out = append(out, models.PeerView{
			Address:      peer.Address,
			Client:       peer.Client,
			DownloadRate: uint64(maxInt64(0, peer.DownloadRate)),
			UploadRate:   uint64(maxInt64(0, peer.UploadRate)),
			Incoming:     peer.Incoming,
			Encrypted:    peer.Encrypted,
			Utp:          peer.UTP,
			Progress:     peer.Progress,
			Seed:         peer.Seed,
		})
	}
	return out, true, nil
}

func (e *gxTorrentEngine) Trackers(hash string) ([]models.TrackerView, bool, error) {
	var trackers []struct {
		URL          string `json:"url"`
		Status       string `json:"status"`
		Seeders      int    `json:"seeders"`
		Leechers     int    `json:"leechers"`
		Message      string `json:"message"`
		NextAnnounce int    `json:"next_announce"`
	}
	if err := e.do(http.MethodGet, "/api/v1/torrents/"+url.PathEscape(strings.ToLower(hash))+"/trackers", nil, "", &trackers); err != nil {
		return nil, false, err
	}
	out := make([]models.TrackerView, 0, len(trackers))
	for _, tracker := range trackers {
		view := models.TrackerView{
			URL:              tracker.URL,
			Message:          tracker.Message,
			NextAnnounce:     tracker.NextAnnounce,
			Verified:         tracker.Status == "working",
			ScrapeComplete:   tracker.Seeders,
			ScrapeIncomplete: tracker.Leechers,
		}
		if tracker.Status == "not_working" {
			view.Fails = 1
		}
		out = append(out, view)
	}
	return out, true, nil
}

// ---------------------------------------------------------------------------
// mutations
// ---------------------------------------------------------------------------

// SetFilePriorities selects the files to download (0 = skip). gx-torrent has
// no priority levels: any value above 0 means "download". Skipped files stay
// out of the save path; changing the selection briefly restarts the torrent.
func (e *gxTorrentEngine) SetFilePriorities(hash string, priorities []int32) (bool, error) {
	if len(priorities) == 0 {
		return false, nil
	}
	values := make([]string, len(priorities))
	for i, priority := range priorities {
		values[i] = strconv.Itoa(int(priority))
	}
	if err := e.action(hash, "file-priorities", url.Values{"priorities": {strings.Join(values, ",")}}); err != nil {
		return false, err
	}
	e.refresh()
	return true, nil
}

// SetTrackers adds the given trackers (rain cannot remove one).
func (e *gxTorrentEngine) SetTrackers(hash string, trackers []TrackerEntry) (bool, error) {
	var urls []string
	seen := map[string]struct{}{}
	for _, tracker := range trackers {
		value := strings.TrimSpace(tracker.URL)
		if value == "" {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		urls = append(urls, value)
	}
	if len(urls) == 0 {
		return false, nil
	}
	if err := e.action(hash, "trackers", url.Values{"urls": {strings.Join(urls, "\n")}}); err != nil {
		return false, err
	}
	return true, nil
}

func (e *gxTorrentEngine) WebSeeds(hash, urls string, remove bool) (bool, error) {
	return false, backendCapabilityError(BackendGxTorrent, "web_seeds")
}

// SetLimits stores the per-torrent seed policy (enforced by Gextto). rain has
// no per-torrent rate limit: a positive rate limit is refused explicitly.
func (e *gxTorrentEngine) SetLimits(hash string, downloadLimit, uploadLimit int64, seedRatio float64, seedDays int64) (bool, error) {
	if downloadLimit > 0 || uploadLimit > 0 {
		return false, backendCapabilityError(BackendGxTorrent, "per_torrent_rate_limit")
	}
	form := url.Values{}
	if seedRatio >= -1.0 {
		form.Set("seed_ratio", strconv.FormatFloat(seedRatio, 'f', -1, 64))
	}
	if seedDays >= -1 {
		form.Set("seed_days", strconv.FormatInt(seedDays, 10))
	}
	if len(form) == 0 {
		return false, nil
	}
	if err := e.action(hash, "seed-limits", form); err != nil {
		return false, err
	}
	e.updateCached(hash, func(view *models.TorrentView) {
		view.SeedRatio = seedRatio
		view.SeedDays = seedDays
	})
	return true, nil
}

func (e *gxTorrentEngine) SetMaxConnections(hash string, value int) (bool, error) {
	return false, backendCapabilityError(BackendGxTorrent, "per_torrent_connections")
}

func (e *gxTorrentEngine) SetMaxUploads(hash string, value int) (bool, error) {
	return false, backendCapabilityError(BackendGxTorrent, "per_torrent_uploads")
}

// SetPin forces a torrent to run outside the queue; an empty hash unpins all.
func (e *gxTorrentEngine) SetPin(hash string, pinned bool) (bool, error) {
	if strings.TrimSpace(hash) == "" {
		if pinned {
			return false, nil
		}
		if err := e.postForm("/api/v1/pins/clear", url.Values{}); err != nil {
			return false, err
		}
		return true, nil
	}
	value := "0"
	if pinned {
		value = "1"
	}
	if err := e.action(hash, "pin", url.Values{"pinned": {value}}); err != nil {
		return false, err
	}
	e.refresh()
	return true, nil
}

// SetSequential: rain always downloads rarest-first. Turning it off is a
// no-op success; turning it on is refused.
func (e *gxTorrentEngine) SetSequential(enabled bool) (bool, error) {
	if !enabled {
		return true, nil
	}
	return false, backendCapabilityError(BackendGxTorrent, "sequential")
}

// TorrentFilePath returns the .torrent copy Gextto keeps for the hash.
func (e *gxTorrentEngine) TorrentFilePath(hash string) (string, bool) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return "", false
	}
	for _, dir := range []string{e.settings.stateDir, e.settings.dataDir} {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, hash+".torrent")
		if fileExists(path) {
			return path, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// add
// ---------------------------------------------------------------------------

func (e *gxTorrentEngine) resolveSavePath(preferredPath *string, cfg *Config) string {
	if preferredPath != nil && strings.TrimSpace(*preferredPath) != "" {
		return strings.TrimSpace(*preferredPath)
	}
	if cfg != nil && strings.TrimSpace(cfg.LibtorrentDir) != "" {
		return cfg.LibtorrentDir
	}
	if e.cfg != nil {
		return e.cfg.LibtorrentDir
	}
	return ""
}

func gxAddForm(savePath string, options AddOptions) url.Values {
	form := url.Values{}
	if savePath != "" {
		if absolute, err := filepath.Abs(savePath); err == nil {
			savePath = absolute
		}
		form.Set("destination", savePath)
	}
	if options.Paused || options.StopWhenReady {
		form.Set("paused", "1")
	}
	if options.QueueTop {
		form.Set("top", "1")
	}
	if options.StopAtMetadata {
		form.Set("stop_at_metadata", "1")
	}
	return form
}

func (e *gxTorrentEngine) Add(magnet string, cfg *Config) (bool, error) {
	return e.AddWithOptions(magnet, cfg, nil, AddOptions{})
}

func (e *gxTorrentEngine) AddWithPath(magnet string, cfg *Config, preferredPath *string) (bool, error) {
	return e.AddWithOptions(magnet, cfg, preferredPath, AddOptions{})
}

// AddWithOptions adds a magnet. Like the embedded engine it returns false
// when the torrent is already in the session.
func (e *gxTorrentEngine) AddWithOptions(magnet string, cfg *Config, preferredPath *string, options AddOptions) (bool, error) {
	if strings.TrimSpace(magnet) == "" {
		return false, nil
	}
	form := gxAddForm(e.resolveSavePath(preferredPath, cfg), options)
	form.Set("magnet", magnet)
	var result struct {
		Hash     string `json:"hash"`
		Existing bool   `json:"existing"`
	}
	if err := e.do(http.MethodPost, "/api/v1/add", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &result); err != nil {
		return false, err
	}
	e.refresh()
	return !result.Existing, nil
}

func (e *gxTorrentEngine) AddFileWithPath(torrentPath string, cfg *Config, preferredPath *string) (bool, error) {
	hash, err := e.AddTorrentFileWithOptions(torrentPath, cfg, preferredPath, AddOptions{})
	if err != nil {
		return false, err
	}
	return hash != nil, nil
}

func (e *gxTorrentEngine) AddTorrentFile(torrentPath, savePath string) (*string, error) {
	return e.AddTorrentFileWithOptions(torrentPath, nil, &savePath, AddOptions{})
}

func (e *gxTorrentEngine) AddTorrentFileEx(torrentPath, savePath string, options AddOptions) (*string, error) {
	return e.AddTorrentFileWithOptions(torrentPath, nil, &savePath, options)
}

func (e *gxTorrentEngine) AddTorrentFileWithOptions(torrentPath string, cfg *Config, preferredPath *string, options AddOptions) (*string, error) {
	data, err := os.ReadFile(torrentPath)
	if err != nil {
		return nil, fmt.Errorf("torrent file not readable: %w", err)
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, values := range gxAddForm(e.resolveSavePath(preferredPath, cfg), options) {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				return nil, err
			}
		}
	}
	part, err := writer.CreateFormFile("torrent", filepath.Base(torrentPath))
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	var result struct {
		Hash string `json:"hash"`
	}
	if err := e.do(http.MethodPost, "/api/v1/add-file", body, writer.FormDataContentType(), &result); err != nil {
		return nil, err
	}
	hash := strings.ToLower(strings.TrimSpace(result.Hash))
	if hash == "" {
		if fromFile, ok := utils.TorrentInfoHash(data); ok {
			hash = strings.ToLower(fromFile)
		}
	}
	if hash == "" {
		return nil, errors.New("gx-torrent did not return the info hash")
	}
	e.persistTorrentCopy(hash, torrentPath)
	e.refresh()
	return &hash, nil
}

func (e *gxTorrentEngine) persistTorrentCopy(hash, source string) {
	dir := e.settings.stateDir
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	if err := copyFileAtomically(source, filepath.Join(dir, hash+".torrent")); err != nil {
		logging.Debug("cannot persist gx-torrent .torrent copy", "hash", hash, "error", err.Error())
	}
}

// ---------------------------------------------------------------------------
// lifecycle
// ---------------------------------------------------------------------------

// Close stops the managed daemon (if Gextto started it).
func (e *gxTorrentEngine) Close() error {
	e.processMu.Lock()
	if !e.closed {
		e.closed = true
		close(e.supervisorStop)
	}
	process := e.process
	e.processMu.Unlock()
	if process != nil {
		return process.Close()
	}
	return nil
}

// LoadIPFilter makes the daemon reload a local IP filter file.
func (e *gxTorrentEngine) LoadIPFilter(path string) (int, error) {
	var result struct {
		Rules int `json:"rules"`
	}
	form := url.Values{"path": {path}}
	if err := e.do(http.MethodPost, "/api/v1/ipfilter", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &result); err != nil {
		return 0, err
	}
	logging.Info("IP filter loaded", "rules", result.Rules, "path", path)
	return result.Rules, nil
}

// SessionStats returns the daemon's session counters (GET
// /api/libtorrent/session-stats). rain has fewer counters than libtorrent.
func (e *gxTorrentEngine) SessionStats() (map[string]int64, error) {
	var stats struct {
		Session map[string]int64 `json:"session"`
	}
	if err := e.do(http.MethodGet, "/api/v1/stats", nil, "", &stats); err != nil {
		return nil, err
	}
	return stats.Session, nil
}

// ApplyOptimization pushes the automatic disk cache and returns the sizes the
// daemon computed from the RAM.
func (e *gxTorrentEngine) ApplyOptimization(cfg *Config) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration unavailable")
	}
	if err := e.pushConfig(gxQueuePolicy(cfg), &e.queuePushed); err != nil {
		return nil, err
	}
	var stats map[string]any
	if err := e.do(http.MethodGet, "/api/v1/stats", nil, "", &stats); err != nil {
		return nil, err
	}
	return map[string]any{
		"backend":        BackendGxTorrent,
		"cache_auto":     stats["cache_auto"],
		"cache_read_mb":  stats["cache_read_mb"],
		"cache_write_mb": stats["cache_write_mb"],
		"preallocate":    stats["preallocate"],
	}, nil
}
