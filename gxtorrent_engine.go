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
	Listen       string
	Timeout      time.Duration
	PollInterval time.Duration
	Managed      bool
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
		Listen:       gxManagedListenSetting(cfg),
		Timeout:      timeout,
		PollInterval: poll,
		// Gextto always starts and supervises its own gx-torrent daemon. An
		// external daemon already answering on the URL is used as-is.
		Managed:  true,
		stateDir: cfg.StateDir,
		dataDir:  cfg.DataDir,
	}, nil
}

// gxManagedListenSetting returns the configured listen address for the managed
// daemon. The gx-torrent web page and API are meant to be reachable on the
// whole LAN, so the default is 0.0.0.0:8890; set an explicit loopback address
// to keep the page on this host only.
func gxManagedListenSetting(cfg *Config) string {
	if cfg == nil {
		return "0.0.0.0:8890"
	}
	if listen := strings.TrimSpace(cfg.Settings["gxtorrent_listen"]); listen != "" {
		return listen
	}
	return "0.0.0.0:8890"
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
	// replacing: the adopted daemon is being stopped on purpose to start the
	// updated one, so its exit is not a crash.
	replacing bool

	mu               sync.Mutex
	syncMu           sync.Mutex
	cache            map[string]models.TorrentView
	previous         map[string]models.TorrentView
	events           []models.TorrentEvent
	pendingMoves     map[string]string
	lastSync         time.Time
	lastAttempt      time.Time
	lastErr          string
	connected        bool
	queuePushed      string
	sequentialPushed string
	speedPushed      string
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
	if dir := engine.torrentCopyDir(); dir != "" {
		if renamed := renameHashTorrentCopies(dir); renamed > 0 {
			logging.Info(fmt.Sprintf("🏷️ Renamed %d .torrent copies in %s from their hash to the torrent name", renamed, dir))
		}
	}
	health, alreadyRunning := engine.health()
	if settings.Managed && (!alreadyRunning || health.ownedBy(cfg)) {
		if alreadyRunning {
			engine.adoptOrReplace(cfg, health)
			engine.processMu.Lock()
			adopted := engine.process != nil
			engine.processMu.Unlock()
			alreadyRunning = adopted
			if adopted {
				go engine.superviseManagedProcess()
			}
		}
		if !alreadyRunning {
			// Fresh start: refresh the IP filter first, so the daemon begins
			// with a fresh list (a supervisor restart reuses it).
			gxEnsureIPFilter(cfg, true)
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
	if alreadyRunning {
		// A gx-torrent already runs (e.g. an operator-managed service): refresh
		// and reload the IP filter for it too, so boot starts from a fresh list.
		gxEnsureIPFilter(cfg, true)
		if path := gxIPFilterPath(cfg); path != "" {
			if _, err := engine.LoadIPFilter(path); err != nil {
				logging.Debug("gx-torrent IP filter reload skipped", "error", err.Error())
			}
		}
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

// gxHealth is the daemon's /api/v1/health answer.
type gxHealth struct {
	OK          bool   `json:"ok"`
	Version     string `json:"version"`
	Pid         int    `json:"pid"`
	Fingerprint string `json:"fingerprint"`
	DataDir     string `json:"data_dir"`
}

// ownedBy reports whether this daemon is the one Gextto manages for cfg: it
// carries a fingerprint and uses Gextto's own state directory. Any other
// daemon answering on the URL is never adopted or stopped.
func (h gxHealth) ownedBy(cfg *Config) bool {
	if h.Fingerprint == "" || h.DataDir == "" || cfg == nil {
		return false
	}
	return SamePath(h.DataDir, filepath.Join(cfg.DataDir, "gx-torrent"))
}

func (e *gxTorrentEngine) health() (gxHealth, bool) {
	var health gxHealth
	if err := e.do(http.MethodGet, "/api/v1/health", nil, "", &health); err != nil {
		return health, false
	}
	return health, health.OK
}

// adoptOrReplace handles a managed daemon left running by a previous Gextto
// run. Started with the same binary and options, it is adopted as it is: its
// peers, queue and transfers carry on. Otherwise (Gextto or gx-torrent was
// updated, a network option changed) it is stopped so a fresh one starts.
func (e *gxTorrentEngine) adoptOrReplace(cfg *Config, health gxHealth) {
	spec, err := buildManagedGxCommand(cfg, e.settings)
	if err != nil {
		logging.Debug("gx-torrent fingerprint unavailable", "error", err.Error())
		return
	}
	if health.Fingerprint == spec.fingerprint && gxPidAlive(health.Pid) {
		e.process = adoptGxProcess(health.Pid)
		logging.Info(fmt.Sprintf("🔗 gx-torrent %s was already running: kept as it is, transfers were not interrupted", health.Version))
		if gxEnsureIPFilter(cfg, false) {
			if path := gxIPFilterPath(cfg); path != "" {
				if _, err := e.LoadIPFilter(path); err != nil {
					logging.Debug("gx-torrent IP filter reload skipped", "error", err.Error())
				}
			}
		}
		return
	}
	if moving := e.movesInProgress(); moving > 0 && gxPidAlive(health.Pid) {
		// Stopping it now would cut a copy to the library in half: keep the
		// old daemon until its moves are done, then replace it.
		e.process = adoptGxProcess(health.Pid)
		logging.Info(fmt.Sprintf("⏳ gx-torrent %s will restart with its new program or settings once %d move(s) in progress finish", health.Version, moving))
		go e.replaceWhenIdle(health.Pid, health.Version)
		return
	}
	logging.Info(fmt.Sprintf("🔄 Restarting gx-torrent %s: its program or settings changed", health.Version))
	gxStopPid(health.Pid)
}

// movesInProgress returns how many storage moves the daemon is running.
func (e *gxTorrentEngine) movesInProgress() int {
	var stats struct {
		Moving int `json:"moving"`
	}
	if err := e.do(http.MethodGet, "/api/v1/stats", nil, "", &stats); err != nil {
		return 0
	}
	return stats.Moving
}

// gxReplaceCheckEvery is how often a deferred daemon restart checks the moves.
var gxReplaceCheckEvery = 15 * time.Second

// replaceWhenIdle stops an outdated adopted daemon once it has no move in
// progress; the supervisor then starts the current one.
func (e *gxTorrentEngine) replaceWhenIdle(pid int, version string) {
	defer recoverGoroutine("gx-torrent deferred restart")
	ticker := time.NewTicker(gxReplaceCheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-e.supervisorStop:
			return
		case <-ticker.C:
		}
		if !gxPidAlive(pid) {
			return
		}
		if e.movesInProgress() > 0 {
			continue
		}
		e.processMu.Lock()
		e.replacing = true
		e.processMu.Unlock()
		logging.Info(fmt.Sprintf("🔄 Moves finished: restarting gx-torrent %s with its new program or settings", version))
		gxStopPid(pid)
		return
	}
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
	// Per-torrent speed limits in KiB/s (-1 global, 0 unlimited).
	DownloadLimitKib int64  `json:"download_limit_kib"`
	UploadLimitKib   int64  `json:"upload_limit_kib"`
	HasMetadata      bool   `json:"has_metadata"`
	AutoManaged      bool   `json:"auto_managed"`
	Parked           bool   `json:"parked"`
	Error            string `json:"error"`
	CompletedAt      int64  `json:"completed_at"`
	CurrentTracker   string `json:"current_tracker"`
	TorrentVersion   string `json:"torrent_version"`
	// SuperSeeding is BEP 16 super-seeding, a seeding strategy (gextto fork).
	SuperSeeding bool `json:"super_seeding"`
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
		DownloadLimit:     kibToBytes(item.DownloadLimitKib),
		UploadLimit:       kibToBytes(item.UploadLimitKib),
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
		TorrentVersion:    gxTorrentVersion(item.TorrentVersion),
		TotalSize:         item.TotalSize,
		TotalDone:         item.TotalDone,
		IsSeeding:         item.State == "seeding",
		CurrentTracker:    item.CurrentTracker,
		Error:             strings.TrimSpace(item.Error),
		Stalled:           item.State == "stalled",
		SuperSeeding:      item.SuperSeeding,
	}
	if item.CompletedAt > 0 {
		view.FinishedSeconds = int64(now.Sub(time.Unix(item.CompletedAt, 0)).Seconds())
	}
	view.Diagnosis, _, _ = DiagnoseTorrent(&view)
	return view
}

// gxTorrentVersion normalizes the daemon's torrent version. rain handles v1 and
// the v1 side of hybrid (v1+v2) torrents; an unknown report falls back to v1.
func gxTorrentVersion(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hybrid", "v2":
		return "hybrid"
	default:
		return "v1"
	}
}

// gxHealthCheck probes the daemon health endpoint once. It is used by the
// Health page, so showing whether gx-torrent is up does not depend on the
// adapter's poll state.
func gxHealthCheck(settings gxTorrentSettings) bool {
	timeout := settings.Timeout
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, settings.BaseURL+"/api/v1/health", nil)
	if err != nil {
		return false
	}
	if settings.Token != "" {
		req.Header.Set("X-Gx-Token", settings.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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
		copyDir := e.torrentCopyDir()
		for hash, view := range next {
			if !view.HasMetadata {
				continue
			}
			stateMissing := !fileExists(filepath.Join(e.settings.stateDir, hash+".torrent"))
			// Also re-export when only the operator's configured copy is
			// missing (e.g. a .torrent left by libtorrent in the state dir).
			copyMissing := copyDir != "" && !fileExists(filepath.Join(copyDir, safeTorrentCopyName(view.Name, hash)+".torrent"))
			if stateMissing || copyMissing {
				export = append(export, hash)
			}
		}
	}
	e.mu.Unlock()
	if reconnected {
		logging.Debug("gx-torrent connesso", "url", e.settings.BaseURL)
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
		// The Gextto-owned copy already exists (it may predate this engine, e.g.
		// from libtorrent). Still mirror it into the operator's configured copy
		// directory if that one is missing: Gextto's setting must hold for the
		// active engine too.
		e.copyTorrentToConfiguredDir(hash, target)
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
	e.copyTorrentToConfiguredDir(hash, target)
}

// torrentCopyDir is the optional folder Gextto keeps the `.torrent` copies in
// (libtorrent_torrent_copy_dir); empty means none.
func (e *gxTorrentEngine) torrentCopyDir() string {
	if e.cfg == nil {
		return ""
	}
	if dir := e.cfg.LibtorrentTorrentCopyDir(); dir != nil {
		return strings.TrimSpace(*dir)
	}
	return ""
}

// copyTorrentToConfiguredDir mirrors a `.torrent` copy into the operator's
// configured folder, so gx-torrent behaves like the embedded engine there.
func (e *gxTorrentEngine) copyTorrentToConfiguredDir(hash, source string) {
	dir := e.torrentCopyDir()
	if dir == "" || !fileExists(source) {
		return
	}
	target := filepath.Join(dir, gxTorrentCopyName(source, e.cachedName(hash), hash)+".torrent")
	if fileExists(target) {
		return
	}
	if err := copyFileAtomically(source, target); err != nil {
		logging.Debug("cannot copy gx-torrent .torrent to the configured directory", "error", err.Error())
	}
}

func (e *gxTorrentEngine) cachedName(hash string) string {
	view, ok := e.cachedState(hash)
	if !ok {
		return ""
	}
	return view.Name
}

// gxTorrentCopyName is the recognisable name of a .torrent copy in the
// operator's folder: the torrent name (as the embedded engine does), read from
// the file when the session does not know it yet, the hash as a last resort.
func gxTorrentCopyName(source, name, hash string) string {
	if strings.TrimSpace(name) == "" || strings.EqualFold(strings.TrimSpace(name), hash) {
		if data, err := os.ReadFile(source); err == nil {
			if fromFile, ok := utils.TorrentName(data); ok {
				name = fromFile
			}
		}
	}
	return safeTorrentCopyName(name, hash)
}

// renameHashTorrentCopies gives the copies an older gx-torrent adapter left as
// <hash>.torrent in the operator's folder their torrent name. A copy whose
// named file already exists is a duplicate and is removed.
func renameHashTorrentCopies(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	renamed := 0
	for _, entry := range entries {
		base := strings.TrimSuffix(entry.Name(), ".torrent")
		if entry.IsDir() || base == entry.Name() || !isHexHash(base) {
			continue
		}
		source := filepath.Join(dir, entry.Name())
		named := gxTorrentCopyName(source, "", base)
		if strings.EqualFold(named, base) {
			continue
		}
		target := filepath.Join(dir, named+".torrent")
		if fileExists(target) {
			if sameFileContent(source, target) {
				_ = os.Remove(source)
			}
			continue
		}
		if err := os.Rename(source, target); err == nil {
			renamed++
		}
	}
	return renamed
}

func isHexHash(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func sameFileContent(a, b string) bool {
	left, errA := os.ReadFile(a)
	right, errB := os.ReadFile(b)
	return errA == nil && errB == nil && bytes.Equal(left, right)
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
	// gx-torrent's own self-management: dynamic queue and adaptive cache, on by
	// default. Disabling it makes the daemon use static slot limits and a static
	// RAM-based cache; the manual per-value settings still apply.
	auto := settingsBool(cfg, "gxtorrent_auto", true)
	policy := map[string]any{
		"active_downloads": clamp(lt.ActiveDownloads),
		"active_seeds":     clamp(lt.ActiveSeeds),
		"active_limit":     clamp(lt.ActiveLimit),
		"dont_count_slow":  lt.DontCountSlowTorrents,
		"dynamic_queue":    auto,
		"auto":             auto,
		"dynamic_min":      clamp(lt.DynamicQueueMin),
		"dynamic_max":      clamp(lt.DynamicQueueMax),
		"cache_mb":         gxCacheMB(lt.CacheSize),
		"cache_ttl_secs":   max(lt.CacheExpiry, 10),
		"preallocate":      cfg.LibtorrentPreallocate(),
	}
	// rain has no single "connections limit": map the global libtorrent limit
	// onto its outgoing dial and incoming accept budgets, keeping rain's own
	// 4:1 ratio so the total matches what the user configured.
	if dial, accept := gxPeerLimits(lt.ConnectionsLimit); dial > 0 {
		policy["max_peer_dial"] = dial
		policy["max_peer_accept"] = accept
	}
	return policy
}

// gxPeerLimits splits a global connections limit into rain's outgoing dial and
// incoming accept budgets (rain defaults are 80 and 20). Zero means unlimited
// and leaves rain's defaults in place.
func gxPeerLimits(connectionsLimit int64) (dial, accept int) {
	if connectionsLimit <= 0 {
		return 0, 0
	}
	limit := clampPeerLimit(connectionsLimit)
	dial = limit * 4 / 5
	if dial < 1 {
		dial = 1
	}
	accept = limit - dial
	if accept < 1 {
		accept = 1
	}
	return dial, accept
}

// clampPeerLimit bounds a connections limit before narrowing it to int: an
// explicit upper-bound return keeps the conversion from truncating (CodeQL
// go/incorrect-integer-conversion), and MaxInt32*4/5 cannot overflow.
func clampPeerLimit(value int64) int {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value < 1 {
		return 1
	}
	return int(value)
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
	e.mu.Lock()
	pending, moving := e.pendingMoves[hash]
	e.mu.Unlock()
	if moving && SamePath(pending, destination) {
		// The same move is already running (the metadata handler and the RAM
		// disk reconciliation can both ask for it): same answer as the daemon.
		return false, errors.New("gx-torrent: the torrent is already being moved")
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

// MovingStorage returns the torrents the daemon is moving right now, as hash
// -> name; ok is false when the daemon cannot be reached. Gextto uses it to
// wait for a long copy to the NAS instead of re-issuing the move.
func (e *gxTorrentEngine) MovingStorage() (map[string]string, bool) {
	if e.sync() != nil {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	moving := map[string]string{}
	for hash, view := range e.cache {
		if view.State == "moving" {
			moving[strings.ToLower(hash)] = view.Name
		}
	}
	return moving, true
}

func (e *gxTorrentEngine) RamdiskUncommittedBytes(ramdisk string, excludeHash string) uint64 {
	var total uint64
	views := e.List()
	e.mu.Lock()
	leaving := map[string]bool{}
	for hash, destination := range e.pendingMoves {
		if !PathOnRamdisk(destination, ramdisk) {
			leaving[strings.ToLower(hash)] = true
		}
	}
	e.mu.Unlock()
	for _, view := range views {
		if strings.EqualFold(view.Hash, excludeHash) || !PathOnRamdisk(view.SavePath, ramdisk) {
			continue
		}
		// A torrent already being moved off the RAM disk reserves nothing
		// there: counting it would push other downloads off as well.
		if view.State == "moving" || leaving[strings.ToLower(view.Hash)] {
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

// PieceRuns returns the piece states of a torrent as compact runs, for the
// Gextto piece-diagnostics view. Run End is inclusive (gextto fork on the
// daemon side).
func (e *gxTorrentEngine) PieceRuns(hash string) ([]TorrentPieceRun, bool, error) {
	var payload struct {
		Runs []struct {
			Begin int    `json:"begin"`
			End   int    `json:"end"`
			State string `json:"state"`
		} `json:"runs"`
	}
	if err := e.do(http.MethodGet, "/api/v1/torrents/"+url.PathEscape(strings.ToLower(hash))+"/pieces", nil, "", &payload); err != nil {
		var apiErr gxAPIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	runs := make([]TorrentPieceRun, 0, len(payload.Runs))
	for _, run := range payload.Runs {
		runs = append(runs, TorrentPieceRun{Begin: run.Begin, End: run.End, State: run.State})
	}
	return runs, true, nil
}

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

// SetTrackers replaces the torrent's tracker list (gextto fork on rain: an
// empty list removes every tracker), like the embedded engine.
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
	if err := e.action(hash, "set-trackers", url.Values{"urls": {strings.Join(urls, "\n")}}); err != nil {
		return false, err
	}
	return true, nil
}

// WebSeeds adds or removes web seed URLs (one per line) at runtime.
func (e *gxTorrentEngine) WebSeeds(hash, urls string, remove bool) (bool, error) {
	form := url.Values{"urls": {urls}}
	if remove {
		form.Set("remove", "1")
	}
	if err := e.action(hash, "webseeds", form); err != nil {
		return false, err
	}
	return true, nil
}

// SetLimits stores the per-torrent seed policy and speed limits. rain's limits
// are in KiB/s: -1 inherits the global, 0 is unlimited (gextto fork).
func (e *gxTorrentEngine) SetLimits(hash string, downloadLimit, uploadLimit int64, seedRatio float64, seedDays int64) (bool, error) {
	form := url.Values{}
	form.Set("download_limit", strconv.FormatInt(bytesToKib(downloadLimit), 10))
	form.Set("upload_limit", strconv.FormatInt(bytesToKib(uploadLimit), 10))
	if seedRatio >= -1.0 {
		form.Set("seed_ratio", strconv.FormatFloat(seedRatio, 'f', -1, 64))
	}
	if seedDays >= -1 {
		form.Set("seed_days", strconv.FormatInt(seedDays, 10))
	}
	if err := e.action(hash, "seed-limits", form); err != nil {
		return false, err
	}
	e.updateCached(hash, func(view *models.TorrentView) {
		view.DownloadLimit = downloadLimit
		view.UploadLimit = uploadLimit
		view.SeedRatio = seedRatio
		view.SeedDays = seedDays
	})
	return true, nil
}

// bytesToKib maps a Gextto per-torrent limit in bytes to rain's KiB/s (gextto
// fork): negative inherits the global, zero is unlimited, a positive value is
// rounded up to at least 1 KiB.
func bytesToKib(value int64) int64 {
	switch {
	case value < 0:
		return -1
	case value == 0:
		return 0
	default:
		kib := value / 1024
		if kib < 1 {
			kib = 1
		}
		return kib
	}
}

// kibToBytes is the reverse of bytesToKib for the list view.
func kibToBytes(kib int64) int64 {
	if kib <= 0 {
		return kib
	}
	return kib * 1024
}

// SetMaxConnections caps the established peers of this torrent (gextto fork):
// 0 unlimited, -1 restores the session default.
func (e *gxTorrentEngine) SetMaxConnections(hash string, value int) (bool, error) {
	return e.setConnLimit(hash, "max_connections", value)
}

// SetMaxUploads sets the upload slots of this torrent (gextto fork): 0 unchokes
// every interested peer, -1 restores the session default.
func (e *gxTorrentEngine) SetMaxUploads(hash string, value int) (bool, error) {
	return e.setConnLimit(hash, "max_uploads", value)
}

func (e *gxTorrentEngine) setConnLimit(hash, field string, value int) (bool, error) {
	if err := e.action(hash, "conn-limits", url.Values{field: {strconv.Itoa(value)}}); err != nil {
		return false, err
	}
	return true, nil
}

// SetSuperSeeding toggles BEP 16 super-seeding on this torrent (gextto fork):
// while seeding it advertises one piece at a time so the swarm spreads the data.
// It is a seeding strategy: it does not touch the queue, the seed policy or the
// bandwidth limits.
func (e *gxTorrentEngine) SetSuperSeeding(hash string, enabled bool) (bool, error) {
	value := "0"
	if enabled {
		value = "1"
	}
	if err := e.action(hash, "super-seeding", url.Values{"enabled": {value}}); err != nil {
		return false, err
	}
	return true, nil
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

// SetSequential enables or disables sequential download for torrents added
// afterwards. rain fixes the piece order when a torrent is added, so already
// running torrents are not changed.
func (e *gxTorrentEngine) SetSequential(enabled bool) (bool, error) {
	if err := e.pushConfig(map[string]any{"sequential": enabled}, &e.sequentialPushed); err != nil {
		return false, err
	}
	return true, nil
}

// TorrentFilePath returns the .torrent copy Gextto keeps for the hash.
func (e *gxTorrentEngine) TorrentFilePath(hash string) (string, bool) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return "", false
	}
	for _, dir := range []string{e.settings.stateDir, e.settings.dataDir, e.torrentCopyDir()} {
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
	// Delegate to the shared helper so gx-torrent stages downloads exactly like
	// the embedded engine: an explicit valid path wins, otherwise the configured
	// temp/incomplete dir, then the final download dir. The RAM disk is never an
	// implicit default (an unknown-size add must not start on the tmpfs).
	use := cfg
	if use == nil {
		use = e.cfg
	}
	if use == nil {
		return ""
	}
	return resolveSavePath(preferredPath, use)
}

// gxWarnUnsupportedOptions notes the add-time options rain cannot apply, so a
// caller does not believe they were honored. rain has no sequential download,
// first/last-piece priority, seed mode or per-torrent limits.
func gxWarnUnsupportedOptions(options AddOptions) {
	var unsupported []string
	if options.SeedMode {
		unsupported = append(unsupported, "seed_mode")
	}
	if len(unsupported) > 0 {
		logging.Warn("gx-torrent non supporta queste opzioni di aggiunta: verranno ignorate",
			"options", strings.Join(unsupported, ","))
	}
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
	if options.Sequential {
		form.Set("sequential", "1")
	}
	if options.FirstLast {
		form.Set("first_last", "1")
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
	gxWarnUnsupportedOptions(options)
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
	gxWarnUnsupportedOptions(options)
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

// persistTorrentCopy keeps a Gextto-owned copy of the .torrent for export,
// migration and the configured copy directory.
func (e *gxTorrentEngine) persistTorrentCopy(hash, source string) {
	dir := e.settings.stateDir
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logging.Debug("cannot create gx-torrent state directory", "error", err.Error())
		} else {
			target := filepath.Join(dir, hash+".torrent")
			if err := copyFileAtomically(source, target); err != nil {
				logging.Debug("cannot persist gx-torrent .torrent copy", "hash", hash, "error", err.Error())
			} else {
				e.copyTorrentToConfiguredDir(hash, target)
			}
		}
	}
	if copyDir := e.torrentCopyDir(); copyDir != "" && dir == "" {
		_ = copyFileAtomically(source, filepath.Join(copyDir, hash+".torrent"))
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
	// The daemon outlives Gextto: the next start adopts it when nothing
	// changed, and it stops by itself if Gextto does not come back.
	process.Detach()
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
