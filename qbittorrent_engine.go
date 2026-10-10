package gextto

// qbittorrent_engine.go adapts the qBittorrent-nox Web API (internal/qbittorrent)
// to the TorrentEngine contract. Gextto keeps the queue and the automation;
// this adapter only moves bytes and reports state.
//
// Design decisions for the optional qBittorrent backend:
//   - polling is the source of truth: every List() refreshes from
//     /torrents/info and diffs the previous snapshot to emit lifecycle events,
//     so a lost qBittorrent script or a restart cannot silently drop a
//     completion;
//   - operations are idempotent: a pause on an already-paused torrent is a
//     no-op success, never an oscillating command;
//   - path translation is explicit: the adapter refuses to hand qBittorrent a
//     path it cannot map, and never deletes data when the API is unreachable;
//   - unsupported operations return ErrCapabilityUnavailable instead of
//     pretending success.

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
	"sync"
	"sync/atomic"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/qbittorrent"
	"github.com/buzzqw/gextto/internal/utils"
)

// qbittorrentSettings is the fully-resolved configuration of the adapter.
type qbittorrentSettings struct {
	Client       qbittorrent.Config
	Category     string
	Tag          string
	Managed      bool
	Mappings     []PathMapping
	PollInterval time.Duration
	stateDir     string
	dataDir      string
}

// qbittorrentSettingsFromConfig reads the qBittorrent settings from a Config.
func qbittorrentSettingsFromConfig(cfg *Config) (qbittorrentSettings, error) {
	if cfg == nil {
		return qbittorrentSettings{}, fmt.Errorf("qBittorrent configuration is unavailable")
	}
	timeout := 15 * time.Second
	if value, ok := cfg.Settings["qbittorrent_request_timeout_secs"]; ok {
		raw := strings.TrimSpace(value)
		parsed, err := strconv.Atoi(raw)
		if raw == "" {
			parsed = 15
			err = nil
		}
		if err != nil || parsed < 1 || parsed > 300 {
			return qbittorrentSettings{}, fmt.Errorf("qbittorrent_request_timeout_secs must be between 1 and 300")
		}
		timeout = time.Duration(parsed) * time.Second
	}
	// An unset value preserves the historical eager refresh behavior. Once the
	// user saves a value from the UI, the adapter throttles reads to that
	// interval (including outage retries).
	poll := time.Duration(0)
	if value, ok := cfg.Settings["qbittorrent_poll_interval_ms"]; ok {
		raw := strings.TrimSpace(value)
		parsed, err := strconv.Atoi(raw)
		if raw == "" {
			parsed = 0
			err = nil
		}
		if err != nil || (parsed != 0 && (parsed < 250 || parsed > 60000)) {
			return qbittorrentSettings{}, fmt.Errorf("qbittorrent_poll_interval_ms must be between 250 and 60000")
		}
		poll = time.Duration(parsed) * time.Millisecond
	}
	mappings, err := ParsePathMappings(cfg.Settings["qbittorrent_path_mappings"])
	if err != nil {
		return qbittorrentSettings{}, err
	}
	return qbittorrentSettings{
		Client: qbittorrent.Config{
			BaseURL:        strings.TrimSpace(cfg.Settings["qbittorrent_url"]),
			Username:       strings.TrimSpace(cfg.Settings["qbittorrent_username"]),
			Password:       cfg.Settings["qbittorrent_password"],
			RequestTimeout: timeout,
		},
		Category:     strings.TrimSpace(cfg.Settings["qbittorrent_category"]),
		Tag:          strings.TrimSpace(cfg.Settings["qbittorrent_tag"]),
		Managed:      settingsBool(cfg, "qbittorrent_managed", false),
		Mappings:     mappings,
		PollInterval: poll,
		stateDir:     cfg.StateDir,
		dataDir:      cfg.DataDir,
	}, nil
}

// qbittorrentEngine is a TorrentEngine backed by qBittorrent-nox.
type qbittorrentEngine struct {
	settings qbittorrentSettings
	// firstLastDefault adds firstLastPiecePrio to every new torrent.
	firstLastDefault atomic.Bool
	client           *qbittorrent.Client
	// cfg is the startup snapshot, kept so the watchdog can restart the
	// managed process without an AppState.
	cfg *Config

	// processMu guards the managed process handle and the closed flag, which
	// the watchdog goroutine and Close() both touch.
	processMu      sync.Mutex
	process        *managedQbittorrentProcess
	supervisorStop chan struct{}
	closed         bool

	categoryReady bool
	queueMu       sync.Mutex
	queueManaged  bool
	queueChanged  bool
	queueOriginal bool

	mu           sync.Mutex
	syncMu       sync.Mutex
	cache        map[string]models.TorrentView
	previous     map[string]models.TorrentView
	events       []models.TorrentEvent
	stalled      map[string]struct{}
	policyPaused map[string]struct{}
	pendingMoves map[string]string
	lastSync     time.Time
	lastAttempt  time.Time
	lastErr      string
	connected    bool
}

var _ TorrentEngine = (*qbittorrentEngine)(nil)

// newQbittorrentEngine builds the adapter. It does not connect: a qBittorrent
// that is down at startup must degrade gracefully, not abort the daemon.
func newQbittorrentEngine(cfg *Config) (*qbittorrentEngine, error) {
	settings, err := qbittorrentSettingsFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	client, err := qbittorrent.New(settings.Client)
	if err != nil {
		return nil, err
	}
	process, err := startManagedQbittorrent(cfg, settings)
	if err != nil {
		return nil, err
	}
	engine := &qbittorrentEngine{
		settings:       settings,
		client:         client,
		cfg:            cfg,
		process:        process,
		supervisorStop: make(chan struct{}),
		cache:          map[string]models.TorrentView{},
		previous:       map[string]models.TorrentView{},
		stalled:        map[string]struct{}{},
		policyPaused:   map[string]struct{}{},
		pendingMoves:   map[string]string{},
	}
	if process != nil {
		go engine.superviseManagedProcess()
	}
	return engine, nil
}

// superviseManagedProcess keeps the managed qBittorrent-nox alive: every
// unexpected exit is logged and the process is restarted. After too many
// crashes in a short window Gextto gives up, switches back to the embedded
// libtorrent engine and restarts the service to apply it.
func (e *qbittorrentEngine) superviseManagedProcess() {
	const (
		crashWindow  = 10 * time.Minute
		maxCrashes   = 3
		restartPause = 5 * time.Second
	)
	var crashes []time.Time
	for {
		e.processMu.Lock()
		process := e.process
		closed := e.closed
		e.processMu.Unlock()
		if closed || process == nil {
			return
		}

		err := process.wait()

		e.processMu.Lock()
		closed = e.closed
		e.processMu.Unlock()
		if closed {
			return // intentional stop
		}

		exit := "uscita inattesa"
		if err != nil {
			exit = err.Error()
		}
		now := time.Now()
		logging.Error("qBittorrent gestito terminato in modo inatteso", "error", exit, "restarts_in_window", len(crashes)+1)

		crashes = append(crashes, now)
		kept := crashes[:0]
		for _, at := range crashes {
			if now.Sub(at) <= crashWindow {
				kept = append(kept, at)
			}
		}
		crashes = kept

		if len(crashes) >= maxCrashes {
			logging.Error("qBittorrent non riesce a restare attivo: passo al motore libtorrent e riavvio il servizio",
				"crashes", len(crashes), "window_minutes", int(crashWindow.Minutes()))
			if saveErr := SaveSetting(e.settings.dataDir, "torrent_backend", BackendEmbedded); saveErr != nil {
				logging.Error("impossibile impostare torrent_backend=embedded", "error", saveErr)
			}
			requestServiceActionLater("restart")
			return
		}

		select {
		case <-e.supervisorStop:
			return
		case <-time.After(restartPause):
		}

		restarted, startErr := startManagedQbittorrent(e.cfg, e.settings)
		if startErr != nil {
			logging.Error("riavvio di qBittorrent gestito non riuscito", "error", startErr)
			continue
		}
		e.processMu.Lock()
		e.process = restarted
		closed = e.closed
		e.processMu.Unlock()
		if closed {
			_ = restarted.Close()
			return
		}
		logging.Info("qBittorrent gestito riavviato dopo un'uscita inattesa", "restart", len(crashes))
	}
}

func (e *qbittorrentEngine) Name() string { return BackendQbittorrent }

func (e *qbittorrentEngine) Capabilities() map[string]bool {
	return capabilitiesFor(BackendQbittorrent)
}

func (e *qbittorrentEngine) requestContext() (context.Context, context.CancelFunc) {
	timeout := e.settings.Client.RequestTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

// ---------------------------------------------------------------------------
// state normalization
// ---------------------------------------------------------------------------

// qbmapRatio translates a qBittorrent ratio_limit into Gextto's convention
// (>0 explicit limit, 0 infinite, <0 use the global policy).
func qbmapRatio(limit float64) float64 {
	switch {
	case limit == -2:
		return -1 // global
	case limit == -1:
		return 0 // infinite
	case limit >= 0:
		return limit
	default:
		return -1
	}
}

// qbmapSeedDays translates a qBittorrent seeding-time limit (minutes) into
// Gextto's day convention.
func qbmapSeedDays(minutes int64) int64 {
	switch {
	case minutes == -2:
		return -1
	case minutes == -1:
		return 0
	case minutes > 0:
		return (minutes + 1439) / 1440
	default:
		return -1
	}
}

// toView maps one qBittorrent torrent into the Gextto TorrentView.
func (e *qbittorrentEngine) toView(t qbittorrent.Torrent) models.TorrentView {
	progress := t.Progress
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	totalSize := t.Size
	if totalSize <= 0 {
		totalSize = t.TotalSize
	}
	if totalSize < 0 {
		totalSize = 0
	}
	totalDone := int64(progress * float64(totalSize))
	if t.AmountLeft > 0 && totalSize >= t.AmountLeft {
		totalDone = totalSize - t.AmountLeft
	}
	if totalDone < 0 {
		totalDone = 0
	}
	if totalDone > totalSize && totalSize > 0 {
		totalDone = totalSize
	}
	state := qbittorrent.NormalizeState(t.State)
	savePath := t.SavePath
	if translated, ok := TranslateBackendToGextto(savePath, e.settings.Mappings); ok {
		savePath = translated
	}
	hasMetadata := t.State != "metaDL" && t.State != "forcedMetaDL" && totalSize > 0
	numPeers := t.Peers
	if numPeers == 0 {
		numPeers = t.NumLeechs
	}
	numSeeds := t.Seeds
	if numSeeds == 0 {
		numSeeds = t.NumSeeds
	}
	errMessage := strings.TrimSpace(t.Error)
	if errMessage == "" && (t.State == "error" || t.State == "missingFiles") {
		errMessage = t.State
	}
	torrentVersion := ""
	switch {
	case t.InfohashV2 != "" && t.InfohashV1 != "":
		torrentVersion = "hybrid"
	case t.InfohashV2 != "":
		torrentVersion = "v2"
	case t.InfohashV1 != "":
		torrentVersion = "v1"
	}
	view := models.TorrentView{
		Hash:              strings.ToLower(t.Hash),
		Name:              t.Name,
		SavePath:          savePath,
		Progress:          progress * 100.0,
		State:             state,
		DownloadRate:      uint64(maxInt64(0, t.DownloadRate)),
		UploadRate:        uint64(maxInt64(0, t.UploadRate)),
		DownloadRateTotal: uint64(maxInt64(0, t.DownloadRate)),
		UploadRateTotal:   uint64(maxInt64(0, t.UploadRate)),
		DownloadLimit:     t.DownloadLimit,
		UploadLimit:       t.UploadLimit,
		AllTimeUpload:     t.Uploaded,
		AllTimeDownload:   t.Downloaded,
		SeedingSeconds:    t.SeedingTime,
		QueuePosition:     t.Priority,
		NumPeers:          numPeers,
		NumSeeds:          numSeeds,
		NumComplete:       t.SeedsTotal,
		NumIncomplete:     t.PeersTotal,
		SeedRatio:         qbmapRatio(t.RatioLimit),
		SeedDays:          qbmapSeedDays(t.SeedingLimit),
		HasMetadata:       hasMetadata,
		AutoManaged:       t.AutoManaged,
		TorrentVersion:    torrentVersion,
		TotalSize:         totalSize,
		TotalDone:         totalDone,
		CurrentTracker:    t.Tracker,
		IsSeeding:         t.State == "uploading" || t.State == "forcedUP" || t.State == "stalledUP",
		Sequential:        t.Sequential,
		SuperSeeding:      t.SuperSeeding,
		Error:             errMessage,
		Stalled:           state == "stalled",
	}
	view.Diagnosis, _, _ = DiagnoseTorrent(&view)
	return view
}

// ---------------------------------------------------------------------------
// polling / reconciliation
// ---------------------------------------------------------------------------

// sync refreshes the cache from qBittorrent and emits lifecycle events for
// every transition since the previous snapshot. When the API is unreachable the
// cache is kept (stale) and the error is recorded: no destructive action may
// follow a backend outage.
func (e *qbittorrentEngine) sync() error {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()

	now := time.Now()
	e.mu.Lock()
	if e.settings.PollInterval > 0 && !e.lastSync.IsZero() && now.Sub(e.lastSync) < e.settings.PollInterval {
		e.mu.Unlock()
		return nil
	}
	// Failed requests are throttled too. Without this guard every UI refresh,
	// queue decision and status page would create a request storm while the
	// external daemon is down.
	if e.settings.PollInterval > 0 && !e.lastAttempt.IsZero() && !e.connected && now.Sub(e.lastAttempt) < e.settings.PollInterval {
		e.mu.Unlock()
		return nil
	}
	e.lastAttempt = now
	e.mu.Unlock()

	ctx, cancel := e.requestContext()
	defer cancel()
	torrents, err := e.client.Torrents(ctx)

	e.mu.Lock()
	if err != nil {
		wasConnected := e.connected
		e.connected = false
		e.lastErr = err.Error()
		e.mu.Unlock()
		if wasConnected {
			logging.Warn("qBittorrent non raggiungibile", "url", e.settings.Client.BaseURL, "error", err)
		}
		return err
	}
	reconnected := !e.connected
	e.connected = true
	e.lastErr = ""
	e.lastSync = now

	next := make(map[string]models.TorrentView, len(torrents))
	for _, t := range torrents {
		if !e.managedTorrent(t) {
			continue
		}
		view := e.toView(t)
		if view.Hash == "" {
			continue
		}
		if _, stalled := e.stalled[view.Hash]; stalled && (view.State == "downloading" || view.State == "paused") {
			view.State = "stalled"
			view.Stalled = true
		}
		next[view.Hash] = view
	}
	for hash, view := range next {
		previous, known := e.previous[hash]
		if !known {
			continue
		}
		e.diffLocked(previous, view)
	}
	// A move completes when the reported save path reaches the requested
	// destination (qBittorrent's setLocation is asynchronous).
	for hash, destination := range e.pendingMoves {
		view, ok := next[hash]
		if !ok {
			delete(e.pendingMoves, hash)
			continue
		}
		if SamePath(view.SavePath, destination) {
			e.events = append(e.events, models.TorrentEvent{
				Kind:     "storage_moved",
				Hash:     hash,
				Name:     view.Name,
				SavePath: view.SavePath,
			})
			delete(e.pendingMoves, hash)
		}
	}
	e.previous = next
	e.cache = next
	// Keep a Gextto-owned .torrent for every torrent whose metadata is known, so
	// export and backend migration do not depend on qBittorrent retention. Do
	// not perform the HTTP export while holding e.mu: an old qBittorrent can take
	// seconds to answer and would block every read/control operation.
	var export []string
	for hash, view := range next {
		if view.HasMetadata {
			target := filepath.Join(e.settings.stateDir, hash+".torrent")
			if e.settings.stateDir != "" && !fileExists(target) {
				export = append(export, hash)
			}
		}
	}
	e.mu.Unlock()
	if reconnected {
		logging.Info("qBittorrent connesso", "url", e.settings.Client.BaseURL)
	}
	for _, hash := range export {
		e.ensureTorrentFile(hash)
	}
	return nil
}

// managedTorrent applies the optional ownership boundary. Category and tag
// filters are deliberately opt-in: with both fields empty, the configured
// qBittorrent instance is treated as dedicated to Gextto and all visible
// torrents are synchronized.
func (e *qbittorrentEngine) managedTorrent(t qbittorrent.Torrent) bool {
	if category := e.settings.Category; category != "" && strings.TrimSpace(t.Category) != category {
		return false
	}
	if wanted := e.settings.Tag; wanted != "" {
		for _, tag := range strings.Split(t.Tags, ",") {
			if strings.TrimSpace(tag) == wanted {
				return true
			}
		}
		return false
	}
	return true
}

// ensureTorrentFile downloads and stores the .torrent once. Failures are
// non-fatal (older qBittorrent has no export endpoint).
func (e *qbittorrentEngine) ensureTorrentFile(hash string) {
	if e.settings.stateDir == "" {
		return
	}
	target := filepath.Join(e.settings.stateDir, hash+".torrent")
	if fileExists(target) {
		return
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	data, err := e.client.ExportTorrent(ctx, hash)
	if err != nil || len(data) == 0 {
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

// ensureQueueingLocked disables qBittorrent's own queue once: Gextto owns the
// active-download decision, so the backend must not independently queue.
func (e *qbittorrentEngine) ensureQueueingLocked() {
	e.queueMu.Lock()
	if e.queueManaged {
		e.queueMu.Unlock()
		return
	}
	e.queueMu.Unlock()

	ctx, cancel := e.requestContext()
	defer cancel()
	prefs, err := e.client.Preferences(ctx)
	if err != nil {
		logging.Debug("qbittorrent queueing preference unavailable", "error", err.Error())
		return
	}
	original, ok := prefs["queueing_enabled"].(bool)
	if !ok {
		logging.Debug("qbittorrent queueing preference has no boolean value")
		return
	}
	if original {
		if err := e.client.SetPreferences(ctx, map[string]any{"queueing_enabled": false}); err != nil {
			logging.Debug("qbittorrent queueing disable skipped", "error", err.Error())
			return
		}
	}
	e.queueMu.Lock()
	e.queueManaged = true
	e.queueOriginal = original
	e.queueChanged = original
	e.queueMu.Unlock()
}

// AdjustQueue enforces Gextto's active-download slots above qBittorrent. It
// only touches torrents it paused itself (policyPaused), so a user pause is
// never overridden, and it is idempotent.
func (e *qbittorrentEngine) AdjustQueue(cfg *Config, _ int64) {
	if cfg == nil {
		return
	}
	// The configured value comes from an int64 setting: only narrow it to int
	// inside an explicit range guard, which is what go/incorrect-integer-
	// conversion recognizes (a plain clamp is not accepted).
	limit := 1
	if value := cfg.Libtorrent.ActiveDownloads; value >= 1 && value <= math.MaxInt {
		limit = int(value)
	}
	if limit < 1 {
		limit = 1
	}
	views := e.List()
	e.ensureQueueingLocked()

	e.mu.Lock()
	type candidate struct {
		hash string
		pos  int
		name string
	}
	var downloading []candidate
	var paused []candidate
	for _, view := range views {
		if view.Progress >= 99.99 {
			continue
		}
		switch view.State {
		case "downloading", "downloading_metadata":
			downloading = append(downloading, candidate{view.Hash, view.QueuePosition, view.Name})
		case "paused":
			if _, ok := e.policyPaused[view.Hash]; ok {
				paused = append(paused, candidate{view.Hash, view.QueuePosition, view.Name})
			}
		}
	}
	e.mu.Unlock()

	less := func(items []candidate) {
		sort.Slice(items, func(i, j int) bool {
			if items[i].pos != items[j].pos {
				return items[i].pos < items[j].pos
			}
			return items[i].hash < items[j].hash
		})
	}
	less(downloading)
	less(paused)

	if len(downloading) > limit {
		for _, item := range downloading[limit:] {
			if _, err := e.Pause(item.hash); err != nil {
				logging.Debug("queue pause failed", "hash", item.hash, "error", err.Error())
				continue
			}
			e.mu.Lock()
			e.policyPaused[item.hash] = struct{}{}
			e.mu.Unlock()
			logging.Info("📊 Queue: paused a torrent over the active slot limit", "name", item.name)
		}
		return
	}
	slots := limit - len(downloading)
	for index := 0; index < slots && index < len(paused); index++ {
		item := paused[index]
		if _, err := e.Resume(item.hash); err != nil {
			logging.Debug("queue resume failed", "hash", item.hash, "error", err.Error())
			continue
		}
		e.mu.Lock()
		delete(e.policyPaused, item.hash)
		e.mu.Unlock()
		logging.Info("📊 Queue: resumed a torrent into a free active slot", "name", item.name)
	}
}

// diffLocked compares two snapshots and queues the normalized events.
func (e *qbittorrentEngine) diffLocked(previous, current models.TorrentView) {
	if !previous.HasMetadata && current.HasMetadata {
		e.events = append(e.events, models.TorrentEvent{
			Kind: "metadata_received", Hash: current.Hash, Name: current.Name, SavePath: current.SavePath,
		})
	}
	if previous.Progress < 99.99 && current.Progress >= 99.99 {
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

// List returns the last synchronized snapshot, refreshing it first. A backend
// outage yields the stale cache (or an empty list) instead of an error.
func (e *qbittorrentEngine) List() []models.TorrentView {
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

// PollEvents drains the lifecycle events accumulated by the snapshot diffs.
func (e *qbittorrentEngine) PollEvents() []models.TorrentEvent {
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

// SyncStats exposes adapter health for the UI/API.
func (e *qbittorrentEngine) SyncStats() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	stats := map[string]any{
		"backend":      BackendQbittorrent,
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
	return stats
}

// SessionHealthy reports whether List is backed by a successful qBittorrent
// snapshot. An empty list during an outage is not evidence that torrents were
// removed from the external session.
func (e *qbittorrentEngine) SessionHealthy() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.connected
}

// Close restores qBittorrent's queueing preference when Gextto temporarily
// disabled it. Gextto owns scheduling while running, but must not leave a
// user's external qBittorrent configuration altered after shutdown.
func (e *qbittorrentEngine) Close() error {
	// Stop the watchdog first so it never restarts the process we are about to
	// stop on purpose.
	e.processMu.Lock()
	if !e.closed {
		e.closed = true
		if e.supervisorStop != nil {
			close(e.supervisorStop)
		}
	}
	process := e.process
	e.processMu.Unlock()

	e.queueMu.Lock()
	restoreQueue := e.queueManaged && e.queueChanged
	original := e.queueOriginal
	e.queueMu.Unlock()

	if restoreQueue {
		ctx, cancel := e.requestContext()
		err := e.client.SetPreferences(ctx, map[string]any{"queueing_enabled": original})
		cancel()
		if err != nil {
			if process != nil {
				_ = process.Close()
			}
			return err
		}
		e.queueMu.Lock()
		e.queueChanged = false
		e.queueMu.Unlock()
	}
	if process != nil {
		return process.Close()
	}
	return nil
}

// Stats returns the global transfer statistics in Gextto's shape.
func (e *qbittorrentEngine) Stats() map[string]any {
	ctx, cancel := e.requestContext()
	defer cancel()
	info, err := e.client.TransferInfo(ctx)
	views := e.List()
	stats := map[string]any{
		"backend":        BackendQbittorrent,
		"torrents":       len(views),
		"dry_run":        false,
		"session_loaded": err == nil,
	}
	if err != nil {
		stats["error"] = err.Error()
		return stats
	}
	for key, value := range info {
		stats[key] = value
	}
	return stats
}

// ApplyOptimization applies a backend-appropriate disk-cache profile. It
// deliberately does not copy libtorrent's block-based cache size: qBittorrent
// exposes a MiB-based disk cache, so the same operational goal (a cache
// proportional to RAM, bounded) is expressed with its own settings.
func (e *qbittorrentEngine) ApplyOptimization(cfg *Config) (map[string]any, error) {
	memoryMB := uint64(0)
	if cfg != nil {
		memoryMB = gh0_libtorrentOptimizationFor(cfg).memoryMB
	}
	cacheMB := int64(512)
	switch {
	case memoryMB > 0 && memoryMB < 2048:
		cacheMB = 128
	case memoryMB < 4096:
		cacheMB = 256
	case memoryMB < 8192:
		cacheMB = 512
	case memoryMB < 16384:
		cacheMB = 1024
	default:
		cacheMB = 2048
	}
	prefs := map[string]any{
		"disk_cache":     cacheMB,
		"disk_cache_ttl": 300,
		"use_os_cache":   true,
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.SetPreferences(ctx, prefs); err != nil {
		return nil, err
	}
	return map[string]any{
		"backend":        BackendQbittorrent,
		"memory_mb":      memoryMB,
		"disk_cache_mb":  cacheMB,
		"disk_cache_ttl": 300,
	}, nil
}

// ---------------------------------------------------------------------------
// control operations (idempotent)
// ---------------------------------------------------------------------------

func (e *qbittorrentEngine) cachedState(hash string) (models.TorrentView, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	view, ok := e.cache[strings.ToLower(hash)]
	return view, ok
}

func (e *qbittorrentEngine) Pause(hash string) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if view, ok := e.cachedState(hash); ok && view.State == "paused" {
		return true, nil
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.Pause(ctx, hash); err != nil {
		return false, err
	}
	e.mu.Lock()
	if view, ok := e.cache[hash]; ok {
		view.State = "paused"
		e.cache[hash] = view
	}
	e.mu.Unlock()
	return true, nil
}

func (e *qbittorrentEngine) Resume(hash string) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if view, ok := e.cachedState(hash); ok && view.State != "paused" && view.State != "stalled" {
		return true, nil
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.Resume(ctx, hash); err != nil {
		return false, err
	}
	e.mu.Lock()
	if view, ok := e.cache[hash]; ok {
		view.State = "downloading"
		e.cache[hash] = view
	}
	delete(e.stalled, hash)
	e.mu.Unlock()
	return true, nil
}

// Restart nudges a stalled torrent: reannounce plus resume if paused.
func (e *qbittorrentEngine) Restart(hash string) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.Reannounce(ctx, hash); err != nil {
		return false, err
	}
	if view, ok := e.cachedState(hash); ok && (view.State == "paused" || view.State == "stalled") {
		if err := e.client.Resume(ctx, hash); err != nil {
			return false, err
		}
	}
	e.mu.Lock()
	delete(e.stalled, hash)
	if view, ok := e.cache[hash]; ok {
		view.State = "downloading"
		e.cache[hash] = view
	}
	e.mu.Unlock()
	return true, nil
}

func (e *qbittorrentEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return false, nil
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.Delete(ctx, deleteFiles, hash); err != nil {
		return false, err
	}
	e.mu.Lock()
	delete(e.cache, hash)
	delete(e.previous, hash)
	delete(e.stalled, hash)
	delete(e.pendingMoves, hash)
	e.mu.Unlock()
	return true, nil
}

func (e *qbittorrentEngine) ForceRecheck(hash string) (bool, error) {
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.Recheck(ctx, hash); err != nil {
		return false, err
	}
	e.mu.Lock()
	if view, ok := e.cache[strings.ToLower(hash)]; ok {
		view.State = "checking_files"
		e.cache[strings.ToLower(hash)] = view
	}
	e.mu.Unlock()
	return true, nil
}

func (e *qbittorrentEngine) Reannounce(hash string) (bool, error) {
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.Reannounce(ctx, hash); err != nil {
		return false, err
	}
	return true, nil
}

// MoveStorage asks qBittorrent to relocate a torrent. The API is
// asynchronous: the returned true only means the request was accepted, and a
// storage_moved event is emitted once the reported path reaches the target.
func (e *qbittorrentEngine) MoveStorage(hash, destination string) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" || strings.TrimSpace(destination) == "" {
		return false, nil
	}
	if view, ok := e.cachedState(hash); ok && SamePath(view.SavePath, destination) {
		return false, nil
	}
	backendDest, ok := TranslateGexttoToBackend(destination, e.settings.Mappings)
	if !ok {
		if len(e.settings.Mappings) == 0 {
			backendDest = destination
		} else {
			return false, fmt.Errorf("%w: %s", ErrPathMappingMissing, destination)
		}
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.SetLocation(ctx, backendDest, hash); err != nil {
		return false, err
	}
	e.mu.Lock()
	e.pendingMoves[hash] = destination
	e.mu.Unlock()
	return true, nil
}

func (e *qbittorrentEngine) MarkStalled(hash string) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	// Park the torrent in the backend too: a dead stall must not keep occupying
	// an active download slot (parity with the embedded engine).
	paused, err := e.Pause(hash)
	if err != nil {
		return false, err
	}
	e.mu.Lock()
	e.stalled[hash] = struct{}{}
	if view, ok := e.cache[hash]; ok {
		view.State = "stalled"
		view.Stalled = true
		e.cache[hash] = view
	}
	e.mu.Unlock()
	return paused, nil
}

func (e *qbittorrentEngine) ClearStalled(hash string) {
	e.mu.Lock()
	delete(e.stalled, strings.ToLower(strings.TrimSpace(hash)))
	e.mu.Unlock()
}

func (e *qbittorrentEngine) RamdiskUncommittedBytes(ramdisk string, excludeHash string) uint64 {
	var total uint64
	for _, view := range e.List() {
		if strings.EqualFold(view.Hash, excludeHash) {
			continue
		}
		if !PathOnRamdisk(view.SavePath, ramdisk) {
			continue
		}
		total += RamdiskPendingBytes(view)
	}
	return total
}

// ---------------------------------------------------------------------------
// inspection
// ---------------------------------------------------------------------------

func (e *qbittorrentEngine) Files(hash string) ([]models.FileView, bool, error) {
	ctx, cancel := e.requestContext()
	defer cancel()
	files, err := e.client.Files(ctx, hash)
	if err != nil {
		return nil, false, err
	}
	out := make([]models.FileView, 0, len(files))
	for _, file := range files {
		out = append(out, models.FileView{
			Path:       file.Name,
			Size:       file.Size,
			Downloaded: int64(file.Progress * float64(file.Size)),
			Priority:   file.Priority,
		})
	}
	return out, true, nil
}

func (e *qbittorrentEngine) Peers(hash string) ([]models.PeerView, bool, error) {
	ctx, cancel := e.requestContext()
	defer cancel()
	peers, err := e.client.Peers(ctx, hash)
	if err != nil {
		return nil, false, err
	}
	out := make([]models.PeerView, 0, len(peers))
	for _, peer := range peers {
		out = append(out, models.PeerView{
			Address:      fmt.Sprintf("%s:%d", peer.IP, peer.Port),
			Client:       peer.Client,
			DownloadRate: uint64(maxInt64(0, peer.DownSpeed)),
			UploadRate:   uint64(maxInt64(0, peer.UpSpeed)),
			// qBittorrent reports 0-1; Gextto's views use percent like libtorrent.
			Progress:      peer.Progress * 100,
			Seed:          peer.Progress >= 1.0,
			TotalDownload: peer.Downloaded,
			TotalUpload:   peer.Uploaded,
			Utp:           strings.Contains(strings.ToLower(peer.Connection), "tp") && !strings.EqualFold(peer.Connection, "BT"),
			Encrypted:     strings.ContainsAny(peer.Flags, "Ee"),
			Incoming:      strings.Contains(peer.Flags, "I"),
		})
	}
	return out, true, nil
}

func (e *qbittorrentEngine) Trackers(hash string) ([]models.TrackerView, bool, error) {
	ctx, cancel := e.requestContext()
	defer cancel()
	trackers, err := e.client.Trackers(ctx, hash)
	if err != nil {
		return nil, false, err
	}
	out := make([]models.TrackerView, 0, len(trackers))
	for _, tracker := range trackers {
		// qBittorrent status: 0 disabled, 1 not contacted, 2 working,
		// 3 updating, 4 not working.
		out = append(out, models.TrackerView{
			URL:            tracker.URL,
			Message:        tracker.Message,
			ScrapeComplete: tracker.NumPeers,
			Verified:       tracker.Status == 2 || tracker.Status == 3,
		})
	}
	return out, true, nil
}

// ---------------------------------------------------------------------------
// mutations
// ---------------------------------------------------------------------------

func (e *qbittorrentEngine) SetFilePriorities(hash string, priorities []int32) (bool, error) {
	if len(priorities) == 0 {
		return false, nil
	}
	grouped := map[int][]int{}
	for index, priority := range priorities {
		norm := qbNormalizeFilePriority(int(priority))
		grouped[norm] = append(grouped[norm], index)
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	for priority, ids := range grouped {
		if err := e.client.SetFilePriorities(ctx, hash, ids, priority); err != nil {
			return false, err
		}
	}
	return true, nil
}

func qbNormalizeFilePriority(p int) int {
	switch {
	case p <= 0:
		return qbittorrent.FilePrioritySkip
	case p >= 7:
		return qbittorrent.FilePriorityMaximal
	case p >= 4:
		return qbittorrent.FilePriorityHigh
	default:
		return qbittorrent.FilePriorityNormal
	}
}

func (e *qbittorrentEngine) SetTrackers(hash string, trackers []TrackerEntry) (bool, error) {
	urls := make([]string, 0, len(trackers))
	seen := map[string]struct{}{}
	for _, tracker := range trackers {
		url := strings.TrimSpace(tracker.URL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	if len(urls) == 0 {
		return false, nil
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	// Replace the list like the embedded engine: remove the trackers that are
	// no longer wanted (qBittorrent's own "** [DHT] **" rows are not
	// trackers) and add the new ones.
	current, err := e.client.Trackers(ctx, hash)
	if err != nil {
		return false, err
	}
	existing := map[string]struct{}{}
	var stale []string
	for _, tracker := range current {
		value := strings.TrimSpace(tracker.URL)
		if value == "" || strings.HasPrefix(value, "**") {
			continue
		}
		existing[value] = struct{}{}
		if _, keep := seen[value]; !keep {
			stale = append(stale, value)
		}
	}
	var added []string
	for _, value := range urls {
		if _, ok := existing[value]; !ok {
			added = append(added, value)
		}
	}
	if len(added) > 0 {
		if err := e.client.AddTrackers(ctx, hash, added); err != nil {
			return false, err
		}
	}
	if len(stale) > 0 {
		if err := e.client.RemoveTrackers(ctx, hash, stale); err != nil {
			return false, err
		}
	}
	return true, nil
}

// SetSuperSeeding toggles qBittorrent's super seeding for one torrent.
func (e *qbittorrentEngine) SetSuperSeeding(hash string, enabled bool) (bool, error) {
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.SetSuperSeeding(ctx, enabled, strings.ToLower(strings.TrimSpace(hash))); err != nil {
		return false, err
	}
	return true, nil
}

// WebSeeds is not exposed by the qBittorrent Web API.
func (e *qbittorrentEngine) WebSeeds(hash, urls string, remove bool) (bool, error) {
	return false, backendCapabilityError(BackendQbittorrent, "web_seeds")
}

func (e *qbittorrentEngine) SetLimits(hash string, downloadLimit, uploadLimit int64, seedRatio float64, seedDays int64) (bool, error) {
	ctx, cancel := e.requestContext()
	defer cancel()
	applied := false
	if downloadLimit >= 0 {
		if err := e.client.SetTorrentDownloadLimit(ctx, downloadLimit, hash); err != nil {
			return false, err
		}
		applied = true
	}
	if uploadLimit >= 0 {
		if err := e.client.SetTorrentUploadLimit(ctx, uploadLimit, hash); err != nil {
			return false, err
		}
		applied = true
	}
	if seedRatio >= -1.0 || seedDays >= -1 {
		ratio := -2.0
		switch {
		case seedRatio > 0:
			ratio = seedRatio
		case seedRatio == 0:
			ratio = -1
		}
		minutes := int64(-2)
		switch {
		case seedDays > 0:
			minutes = seedDays * 1440
		case seedDays == 0:
			minutes = -1
		}
		if err := e.client.SetShareLimits(ctx, ratio, minutes, hash); err != nil {
			return false, err
		}
		applied = true
	}
	return applied, nil
}

func (e *qbittorrentEngine) SetGlobalSpeedLimits(downloadKib, uploadKib int64) (bool, error) {
	ctx, cancel := e.requestContext()
	defer cancel()
	if downloadKib >= 0 {
		if err := e.client.SetGlobalDownloadLimit(ctx, downloadKib*1024); err != nil {
			return false, err
		}
	}
	if uploadKib >= 0 {
		if err := e.client.SetGlobalUploadLimit(ctx, uploadKib*1024); err != nil {
			return false, err
		}
	}
	return true, nil
}

// SetMaxConnections is a qBittorrent global preference, not per-torrent.
func (e *qbittorrentEngine) SetMaxConnections(hash string, value int) (bool, error) {
	return false, backendCapabilityError(BackendQbittorrent, "per_torrent_connections")
}

func (e *qbittorrentEngine) SetMaxUploads(hash string, value int) (bool, error) {
	return false, backendCapabilityError(BackendQbittorrent, "per_torrent_uploads")
}

// SetPin maps "pinned" onto qBittorrent's force-start flag.
func (e *qbittorrentEngine) SetPin(hash string, pinned bool) (bool, error) {
	ctx, cancel := e.requestContext()
	defer cancel()
	if pinned {
		hash = strings.ToLower(strings.TrimSpace(hash))
		if hash == "" {
			return false, nil
		}
		if err := e.client.SetForceStart(ctx, true, hash); err != nil {
			return false, err
		}
		return true, nil
	}
	hashes := e.allHashes()
	if strings.TrimSpace(hash) != "" {
		hashes = []string{strings.ToLower(strings.TrimSpace(hash))}
	}
	if len(hashes) == 0 {
		return true, nil
	}
	if err := e.client.SetForceStart(ctx, false, hashes...); err != nil {
		return false, err
	}
	return true, nil
}

func (e *qbittorrentEngine) SetSequential(enabled bool) (bool, error) {
	hashes := e.allHashes()
	if len(hashes) == 0 {
		return true, nil
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	if err := e.client.SetSequentialDownload(ctx, enabled, hashes...); err != nil {
		return false, err
	}
	return true, nil
}

// AssociateStorage cannot be expressed by the qBittorrent Web API.
func (e *qbittorrentEngine) AssociateStorage(hash, destination string) (bool, error) {
	return false, backendCapabilityError(BackendQbittorrent, "associate_storage")
}

// TorrentFilePath returns the .torrent copy Gextto persists for the hash.
func (e *qbittorrentEngine) TorrentFilePath(hash string) (string, bool) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return "", false
	}
	for _, dir := range []string{e.stateDir(), e.dataDir()} {
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

func (e *qbittorrentEngine) allHashes() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	hashes := make([]string, 0, len(e.cache))
	for hash := range e.cache {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	return hashes
}

func (e *qbittorrentEngine) stateDir() string { return e.settings.stateDir }
func (e *qbittorrentEngine) dataDir() string  { return e.settings.dataDir }

// ---------------------------------------------------------------------------
// add
// ---------------------------------------------------------------------------

func (e *qbittorrentEngine) ensureCategory(ctx context.Context) {
	if e.settings.Category == "" {
		return
	}
	e.mu.Lock()
	if e.categoryReady {
		e.mu.Unlock()
		return
	}
	category := e.settings.Category
	e.mu.Unlock()
	// Create the category outside the lock: it is a network call and the
	// idempotent double-create is harmless, while categoryReady is guarded so
	// concurrent adds do not race on the flag.
	if err := e.client.CreateCategory(ctx, category, ""); err != nil {
		logging.Debug("qbittorrent category creation skipped", "category", category, "error", err.Error())
	}
	e.mu.Lock()
	e.categoryReady = true
	e.mu.Unlock()
}

func (e *qbittorrentEngine) resolveSavePath(preferredPath *string, cfg *Config) (string, error) {
	candidate := ""
	if preferredPath != nil && strings.TrimSpace(*preferredPath) != "" {
		candidate = strings.TrimSpace(*preferredPath)
	} else if cfg != nil {
		candidate = cfg.LibtorrentDir
	} else {
		candidate = e.settings.dataDir
	}
	translated, ok := TranslateGexttoToBackend(candidate, e.settings.Mappings)
	if !ok {
		if len(e.settings.Mappings) == 0 {
			// No mapping configured: the backend is assumed to share the
			// namespace (native install).
			return candidate, nil
		}
		return "", fmt.Errorf("%w: %s", ErrPathMappingMissing, candidate)
	}
	return translated, nil
}

func (e *qbittorrentEngine) addOptions(options AddOptions) qbittorrent.AddOptions {
	return qbittorrent.AddOptions{
		Category:   e.settings.Category,
		Tags:       e.settings.Tag,
		Paused:     options.Paused,
		Sequential: options.Sequential,
		FirstLast:  options.FirstLast || e.firstLastDefault.Load(),
	}
}

// SetFirstLastDefault implements TorrentEngine.
func (e *qbittorrentEngine) SetFirstLastDefault(enabled bool) {
	e.firstLastDefault.Store(enabled)
}

func (e *qbittorrentEngine) Add(magnet string, cfg *Config) (bool, error) {
	return e.AddWithOptions(magnet, cfg, nil, AddOptions{})
}

func (e *qbittorrentEngine) AddWithPath(magnet string, cfg *Config, preferredPath *string) (bool, error) {
	return e.AddWithOptions(magnet, cfg, preferredPath, AddOptions{})
}

func (e *qbittorrentEngine) AddWithOptions(magnet string, cfg *Config, preferredPath *string, options AddOptions) (bool, error) {
	if strings.TrimSpace(magnet) == "" {
		return false, nil
	}
	savePath, err := e.resolveSavePath(preferredPath, cfg)
	if err != nil {
		return false, err
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	e.ensureCategory(ctx)
	// Do not report a phantom start when the torrent is already in the session:
	// the embedded backend returns false in the same case, and the caller uses
	// that to avoid registering a download that never started.
	if known, ok := utils.MagnetHash(magnet); ok {
		for _, existing := range e.List() {
			if strings.EqualFold(existing.Hash, known) {
				return false, nil
			}
		}
	}
	addOpts := e.addOptions(options)
	addOpts.SavePath = savePath
	hash, err := e.client.AddMagnet(ctx, magnet, addOpts)
	if err != nil {
		return false, err
	}
	if hash == "" {
		logging.Warn("qbittorrent add returned no infohash (base32 magnet?)", "magnet", utilsRedactMagnet(magnet))
	}
	return true, nil
}

// AddFileWithPath returns the hash through AddTorrentFileWithOptions.
func (e *qbittorrentEngine) AddFileWithPath(torrentPath string, cfg *Config, preferredPath *string) (bool, error) {
	hash, err := e.AddTorrentFileWithOptions(torrentPath, cfg, preferredPath, AddOptions{})
	if err != nil {
		return false, err
	}
	return hash != nil, nil
}

func (e *qbittorrentEngine) AddTorrentFile(torrentPath, savePath string) (*string, error) {
	preferred := savePath
	return e.AddTorrentFileWithOptions(torrentPath, nil, &preferred, AddOptions{})
}

func (e *qbittorrentEngine) AddTorrentFileEx(torrentPath, savePath string, options AddOptions) (*string, error) {
	preferred := savePath
	return e.AddTorrentFileWithOptions(torrentPath, nil, &preferred, options)
}

func (e *qbittorrentEngine) AddTorrentFileWithOptions(torrentPath string, cfg *Config, preferredPath *string, options AddOptions) (*string, error) {
	if !fileExists(torrentPath) {
		return nil, fmt.Errorf("torrent file not found: %s", torrentPath)
	}
	savePath, err := e.resolveSavePath(preferredPath, cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := e.requestContext()
	defer cancel()
	e.ensureCategory(ctx)

	before := e.currentHashSet()
	addOpts := e.addOptions(options)
	addOpts.SavePath = savePath
	if err := e.client.AddTorrentFile(ctx, torrentPath, addOpts); err != nil {
		return nil, err
	}
	// qBittorrent does not return the infohash: find the newly added torrent.
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(150 * time.Millisecond):
			}
		}
		torrents, listErr := e.client.Torrents(ctx)
		if listErr != nil {
			continue
		}
		for _, torrent := range torrents {
			hash := strings.ToLower(torrent.Hash)
			if _, existed := before[hash]; !existed {
				e.persistTorrentCopy(hash, torrentPath)
				return &hash, nil
			}
		}
	}
	// Fallback: if qBittorrent didn't return a new hash (e.g. already added, or slow
	// indexing), extract the infohash directly from the .torrent file metadata.
	if data, readErr := os.ReadFile(torrentPath); readErr == nil {
		if hash, ok := utils.TorrentInfoHash(data); ok {
			hash = strings.ToLower(hash)
			e.persistTorrentCopy(hash, torrentPath)
			return &hash, nil
		}
	}
	return nil, nil
}

func (e *qbittorrentEngine) currentHashSet() map[string]struct{} {
	_ = e.sync()
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]struct{}, len(e.cache))
	for hash := range e.cache {
		out[hash] = struct{}{}
	}
	return out
}

// persistTorrentCopy keeps a Gextto-owned `.torrent` so the hash can be
// re-imported and exported even if qBittorrent is reset.
func (e *qbittorrentEngine) persistTorrentCopy(hash, source string) {
	dir := e.stateDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logging.Warn("cannot create torrent state directory", "dir", dir, "error", err.Error())
		return
	}
	target := filepath.Join(dir, hash+".torrent")
	if err := copyFileAtomically(source, target); err != nil {
		logging.Warn("cannot persist qbittorrent .torrent copy", "hash", hash, "error", err.Error())
	}
}

// ErrPathMappingMissing is returned when a path cannot be translated into the
// backend namespace.
var ErrPathMappingMissing = errors.New("backend path mapping missing")

// utilsRedactMagnet keeps credentials out of logs.
func utilsRedactMagnet(magnet string) string {
	if len(magnet) > 80 {
		return magnet[:80] + "…"
	}
	return magnet
}
