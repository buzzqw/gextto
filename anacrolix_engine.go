//go:build anacrolix

package gextto

// anacrolix_engine.go implements AnacrolixBackend, the native-Go torrent engine
// behind the `anacrolix` build tag. The default build never compiles this file,
// so the official libtorrent path and its dependency tree are untouched.
//
// Scope of this optional backend implementation:
//   - one `*torrent.Client` per process, file storage plus a persistent piece
//     completion store;
//   - magnet and `.torrent` add with per-torrent storage directories;
//   - snapshot polling with the same normalized events as the qBittorrent
//     adapter (completion, metadata, checks, errors);
//   - resume after restart through the persisted `.torrent` + completion store;
//   - explicit capability errors for the operations anacrolix cannot apply
//     (per-torrent limits, storage move, sequential flag, global rate changes).
//     Storage move and the richer piece-level features are the documented next
//     phases, not silently faked.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

func init() {
	newAnacrolixEngine = newAnacrolixEngineImpl
}

// anacrolixSettings is the resolved configuration of the adapter. Every field
// has a dedicated `anacrolix_*` setting with a fallback to the shared/libtorrent
// value, so the backend is configurable on its own without changing the default
// behavior.
type anacrolixSettings struct {
	DataDir       string
	MetadataDir   string
	CompletionDB  string
	ManifestPath  string
	ListenPort    int
	DHT           bool
	PEX           bool
	UTP           bool
	TCP           bool
	Trackers      bool
	UPnP          bool
	Hashers       int
	MaxUnverified int64
	DownloadLimit int64 // KiB/s, applied at creation only
	UploadLimit   int64 // KiB/s, applied at creation only
	Mappings      []PathMapping

	DhtBootstrapNodes  string
	IpFilterPath       string
	ApplyIpFilter      bool
	ProxyType          int64
	ProxyHost          string
	ProxyPort          int64
	ProxyUser          string
	ProxyPassword      string
	MaxConnsPerTorrent int
}

func anacrolixSettingsFromConfig(cfg *Config) anacrolixSettings {
	dataDir := settingOrDefault(cfg.Settings, "anacrolix_data_dir", cfg.LibtorrentDir)
	metadataDir := filepath.Join(cfg.DataDir, "anacrolix")
	mappings, _ := ParsePathMappings(cfg.Settings["anacrolix_path_mappings"])
	maxUnverifiedMB := settingsParseInt(cfg, "anacrolix_max_unverified_mb", 64)
	if maxUnverifiedMB <= 0 {
		maxUnverifiedMB = 64
	}
	hashers := settingsParseInt(cfg, "anacrolix_piece_hashers", 2)
	if hashers <= 0 {
		hashers = 2
	}
	return anacrolixSettings{
		DataDir:       dataDir,
		MetadataDir:   metadataDir,
		CompletionDB:  metadataDir,
		ManifestPath:  filepath.Join(metadataDir, "manifest.json"),
		ListenPort:    int(settingsParseInt(cfg, "anacrolix_listen_port", int64(cfg.Libtorrent.PortMin))),
		DHT:           settingsBool(cfg, "anacrolix_dht", cfg.Libtorrent.Dht),
		PEX:           settingsBool(cfg, "anacrolix_pex", cfg.Libtorrent.Pex),
		UTP:           settingsBool(cfg, "anacrolix_utp", cfg.Libtorrent.Utp),
		TCP:           settingsBool(cfg, "anacrolix_tcp", true),
		Trackers:      settingsBool(cfg, "anacrolix_trackers", true),
		UPnP:          settingsBool(cfg, "anacrolix_upnp", cfg.Libtorrent.Upnp),
		Hashers:       int(hashers),
		MaxUnverified: maxUnverifiedMB << 20,
		DownloadLimit: settingsParseInt(cfg, "anacrolix_download_limit_kib", cfg.Libtorrent.DownloadLimitKib),
		UploadLimit:   settingsParseInt(cfg, "anacrolix_upload_limit_kib", cfg.Libtorrent.UploadLimitKib),
		Mappings:      mappings,

		DhtBootstrapNodes:  settingOrDefault(cfg.Settings, "anacrolix_dht_bootstrap_nodes", cfg.Libtorrent.DhtBootstrapNodes),
		IpFilterPath:       settingOrDefault(cfg.Settings, "anacrolix_ipfilter_path", cfg.Libtorrent.IpFilterPath),
		ApplyIpFilter:      settingsBool(cfg, "anacrolix_apply_ip_filter", cfg.Libtorrent.ApplyIpFilter),
		ProxyType:          settingsParseInt(cfg, "anacrolix_proxy_type", 0),
		ProxyHost:          settingOrDefault(cfg.Settings, "anacrolix_proxy_host", ""),
		ProxyPort:          settingsParseInt(cfg, "anacrolix_proxy_port", 0),
		ProxyUser:          settingOrDefault(cfg.Settings, "anacrolix_proxy_user", ""),
		ProxyPassword:      settingOrDefault(cfg.Settings, "anacrolix_proxy_password", ""),
		MaxConnsPerTorrent: int(settingsParseInt(cfg, "anacrolix_max_conns_per_torrent", cfg.Libtorrent.MaxConnectionsPerTorrent)),
	}
}

// anacrolixTorrent is the adapter's per-torrent bookkeeping.
type anacrolixTorrent struct {
	handle     *torrent.Torrent
	savePath   string
	paused     bool
	stalled    bool
	uploadOnly bool
}

// anacrolixManifestEntry is deliberately separate from the live torrent
// handle. The manifest is the durable contract used to restore a session.
type anacrolixManifestEntry struct {
	SavePath   string `json:"save_path"`
	Paused     bool   `json:"paused,omitempty"`
	UploadOnly bool   `json:"upload_only,omitempty"`
}

type anacrolixSample struct {
	done int64
	at   time.Time
}

// anacrolixEngine is a TorrentEngine backed by the in-process anacrolix client.
type anacrolixEngine struct {
	settings   anacrolixSettings
	client     *torrent.Client
	completion storage.PieceCompletion

	mu           sync.Mutex
	state        map[string]*anacrolixTorrent
	previous     map[string]models.TorrentView
	events       []models.TorrentEvent
	manifest     map[string]anacrolixManifestEntry
	samples      map[string]anacrolixSample
	policyPaused map[string]struct{}
	closed       bool
}

var _ TorrentEngine = (*anacrolixEngine)(nil)

func newAnacrolixEngineImpl(cfg *Config) (TorrentEngine, error) {
	if err := validateAnacrolixConfig(cfg); err != nil {
		return nil, err
	}
	settings := anacrolixSettingsFromConfig(cfg)
	if err := os.MkdirAll(settings.MetadataDir, 0o755); err != nil {
		return nil, fmt.Errorf("anacrolix: create metadata dir: %w", err)
	}
	completion, err := storage.NewDefaultPieceCompletionForDir(settings.CompletionDB)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: piece completion store: %w", err)
	}
	if persistent, ok := completion.(storage.PieceCompletionPersistenter); ok && !persistent.Persistent() {
		logging.Warn("anacrolix piece completion is not persistent; a restart will re-check data")
	}

	clientCfg := torrent.NewDefaultClientConfig()
	clientCfg.DataDir = settings.DataDir
	clientCfg.ListenPort = settings.ListenPort
	clientCfg.DisableUTP = !settings.UTP
	clientCfg.DisableTCP = !settings.TCP
	clientCfg.DisablePEX = !settings.PEX
	clientCfg.NoDHT = !settings.DHT
	clientCfg.NoDefaultPortForwarding = !settings.UPnP
	clientCfg.DisableTrackers = !settings.Trackers
	clientCfg.PieceHashersPerTorrent = settings.Hashers
	clientCfg.MaxUnverifiedBytes = settings.MaxUnverified
	clientCfg.DefaultStorage = storage.NewFileWithCompletion(settings.DataDir, completion)
	if settings.MaxConnsPerTorrent > 0 {
		clientCfg.EstablishedConnsPerTorrent = settings.MaxConnsPerTorrent
	}
	// Per-client rate limits are only applied at creation: anacrolix has no
	// runtime setter, so a change requires a restart (the API says so).
	if settings.DownloadLimit > 0 {
		bytes := settings.DownloadLimit * 1024
		clientCfg.DownloadRateLimiter = rate.NewLimiter(rate.Limit(bytes), int(clampBurst(bytes)))
	}
	if settings.UploadLimit > 0 {
		bytes := settings.UploadLimit * 1024
		clientCfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(bytes), int(clampBurst(bytes)))
	}

	// Map the network settings that anacrolix can honour.
	if settings.ApplyIpFilter {
		if blocklist := anacrolixBlocklist(settings); blocklist != nil {
			clientCfg.IPBlocklist = blocklist
		}
	}
	if proxy := anacrolixHTTPProxy(settings); proxy != nil {
		clientCfg.HTTPProxy = http.ProxyURL(proxy)
		logging.Info("anacrolix HTTP proxy configured", "proxy", proxy.Redacted())
	}

	client, err := torrent.NewClient(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: create client: %w", err)
	}
	if nodes := anacrolixDhtNodes(settings); len(nodes) > 0 {
		client.AddDhtNodes(nodes)
	}
	engine := &anacrolixEngine{
		settings:     settings,
		client:       client,
		completion:   completion,
		state:        map[string]*anacrolixTorrent{},
		previous:     map[string]models.TorrentView{},
		manifest:     map[string]anacrolixManifestEntry{},
		samples:      map[string]anacrolixSample{},
		policyPaused: map[string]struct{}{},
	}
	engine.loadManifest()
	engine.restore()
	logging.Info("anacrolix backend started", "data_dir", settings.DataDir, "listen_port", settings.ListenPort)
	return engine, nil
}

func (e *anacrolixEngine) Name() string { return BackendAnacrolix }

func (e *anacrolixEngine) Capabilities() map[string]bool {
	return capabilitiesFor(BackendAnacrolix)
}

// ---------------------------------------------------------------------------
// manifest
// ---------------------------------------------------------------------------

func (e *anacrolixEngine) loadManifest() {
	raw, err := os.ReadFile(e.settings.ManifestPath)
	if err != nil {
		return
	}
	// Older builds wrote {"hash":"/path"}. Accept that format forever so a
	// backend upgrade never strands active downloads.
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return
	}
	for hash, value := range values {
		var entry anacrolixManifestEntry
		if len(value) > 0 && value[0] == '"' {
			if json.Unmarshal(value, &entry.SavePath) == nil {
				e.manifest[hash] = entry
			}
			continue
		}
		if json.Unmarshal(value, &entry) == nil && strings.TrimSpace(entry.SavePath) != "" {
			e.manifest[hash] = entry
		}
	}
}

func (e *anacrolixEngine) saveManifest() {
	encoded, err := json.MarshalIndent(e.manifest, "", "  ")
	if err != nil {
		return
	}
	tmp := e.settings.ManifestPath + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o644); err != nil {
		logging.Warn("anacrolix manifest write failed", "error", err.Error())
		return
	}
	_ = os.Rename(tmp, e.settings.ManifestPath)
}

// restore re-adds the torrents known from a previous run. The piece completion
// store supplies the verified pieces, so a restart does not re-download data.
func (e *anacrolixEngine) restore() {
	for hash, manifest := range e.manifest {
		path := filepath.Join(e.settings.MetadataDir, hash+".torrent")
		if !fileExists(path) {
			continue
		}
		options := AddOptions{Paused: manifest.Paused, SeedMode: manifest.UploadOnly}
		if _, err := e.addTorrentFileInternal(path, manifest.SavePath, options, nil); err != nil {
			logging.Warn("anacrolix restore failed", "hash", hash, "error", err.Error())
		}
	}
}

// persistTorrent keeps the .torrent for restart and re-export.
func (e *anacrolixEngine) persistTorrent(hash string, mi *metainfo.MetaInfo) {
	if mi == nil {
		return
	}
	target := filepath.Join(e.settings.MetadataDir, hash+".torrent")
	if fileExists(target) {
		return
	}
	tmp := target + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		logging.Warn("anacrolix .torrent write failed", "hash", hash, "error", err.Error())
		return
	}
	if err := mi.Write(file); err != nil {
		file.Close()
		_ = os.Remove(tmp)
		logging.Warn("anacrolix .torrent encode failed", "hash", hash, "error", err.Error())
		return
	}
	file.Close()
	_ = os.Rename(tmp, target)
}

// ---------------------------------------------------------------------------
// path handling
// ---------------------------------------------------------------------------

func (e *anacrolixEngine) resolveSavePath(preferredPath *string, cfg *Config) (string, error) {
	candidate := ""
	if preferredPath != nil && strings.TrimSpace(*preferredPath) != "" {
		candidate = strings.TrimSpace(*preferredPath)
	} else if cfg != nil && strings.TrimSpace(cfg.LibtorrentDir) != "" {
		candidate = cfg.LibtorrentDir
	} else {
		candidate = e.settings.DataDir
	}
	if _, ok := TranslateGexttoToBackend(candidate, e.settings.Mappings); !ok && len(e.settings.Mappings) > 0 {
		return "", fmt.Errorf("%w: %s", ErrPathMappingMissing, candidate)
	}
	return candidate, nil
}

// ---------------------------------------------------------------------------
// add
// ---------------------------------------------------------------------------

func (e *anacrolixEngine) addOptionsFrom(options AddOptions) torrent.AddTorrentOpts {
	opts := torrent.AddTorrentOpts{
		DisallowDataDownload: options.Paused,
		DisallowDataUpload:   options.Paused,
	}
	return opts
}

func (e *anacrolixEngine) register(handle *torrent.Torrent, savePath string, options AddOptions) *anacrolixTorrent {
	entry := &anacrolixTorrent{
		handle:     handle,
		savePath:   savePath,
		paused:     options.Paused,
		uploadOnly: options.SeedMode,
	}
	hash := handle.InfoHash().HexString()
	if options.SeedMode {
		handle.DisallowDataDownload()
	}
	e.mu.Lock()
	e.state[hash] = entry
	e.manifest[hash] = anacrolixManifestEntry{
		SavePath:   savePath,
		Paused:     options.Paused,
		UploadOnly: options.SeedMode,
	}
	e.mu.Unlock()
	e.saveManifest()
	return entry
}

func (e *anacrolixEngine) Add(magnet string, cfg *Config) (bool, error) {
	return e.AddWithOptions(magnet, cfg, nil, AddOptions{})
}

func (e *anacrolixEngine) AddWithPath(magnet string, cfg *Config, preferredPath *string) (bool, error) {
	return e.AddWithOptions(magnet, cfg, preferredPath, AddOptions{})
}

func (e *anacrolixEngine) AddWithOptions(magnet string, cfg *Config, preferredPath *string, options AddOptions) (bool, error) {
	if strings.TrimSpace(magnet) == "" {
		return false, nil
	}
	savePath, err := e.resolveSavePath(preferredPath, cfg)
	if err != nil {
		return false, err
	}
	spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
	if err != nil {
		return false, fmt.Errorf("anacrolix: invalid magnet: %w", err)
	}
	opts := e.addOptionsFrom(options)
	spec.AddTorrentOpts.DisallowDataDownload = opts.DisallowDataDownload
	spec.AddTorrentOpts.DisallowDataUpload = opts.DisallowDataUpload
	spec.AddTorrentOpts.Storage = storage.NewFileWithCompletion(savePath, e.completion)
	handle, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return false, fmt.Errorf("anacrolix: add magnet: %w", err)
	}
	e.register(handle, savePath, options)
	return true, nil
}

func (e *anacrolixEngine) AddFileWithPath(torrentPath string, cfg *Config, preferredPath *string) (bool, error) {
	hash, err := e.AddTorrentFileWithOptions(torrentPath, cfg, preferredPath, AddOptions{})
	if err != nil {
		return false, err
	}
	return hash != nil, nil
}

func (e *anacrolixEngine) AddTorrentFile(torrentPath, savePath string) (*string, error) {
	preferred := savePath
	return e.AddTorrentFileWithOptions(torrentPath, nil, &preferred, AddOptions{})
}

func (e *anacrolixEngine) AddTorrentFileEx(torrentPath, savePath string, options AddOptions) (*string, error) {
	preferred := savePath
	return e.AddTorrentFileWithOptions(torrentPath, nil, &preferred, options)
}

func (e *anacrolixEngine) AddTorrentFileWithOptions(torrentPath string, cfg *Config, preferredPath *string, options AddOptions) (*string, error) {
	if !fileExists(torrentPath) {
		return nil, fmt.Errorf("torrent file not found: %s", torrentPath)
	}
	savePath, err := e.resolveSavePath(preferredPath, cfg)
	if err != nil {
		return nil, err
	}
	return e.addTorrentFileInternal(torrentPath, savePath, options, cfg)
}

func (e *anacrolixEngine) addTorrentFileInternal(torrentPath, savePath string, options AddOptions, cfg *Config) (*string, error) {
	meta, err := metainfo.LoadFromFile(torrentPath)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: load .torrent: %w", err)
	}
	spec, err := torrent.TorrentSpecFromMetaInfoErr(meta)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: parse metainfo: %w", err)
	}
	opts := e.addOptionsFrom(options)
	spec.AddTorrentOpts.DisallowDataDownload = opts.DisallowDataDownload
	spec.AddTorrentOpts.DisallowDataUpload = opts.DisallowDataUpload
	spec.AddTorrentOpts.Storage = storage.NewFileWithCompletion(savePath, e.completion)
	handle, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: add torrent: %w", err)
	}
	e.register(handle, savePath, options)
	hash := handle.InfoHash().HexString()
	e.persistTorrent(hash, meta)
	return &hash, nil
}

// ---------------------------------------------------------------------------
// polling / events
// ---------------------------------------------------------------------------

func (e *anacrolixEngine) toView(entry *anacrolixTorrent, now time.Time) models.TorrentView {
	handle := entry.handle
	hash := strings.ToLower(handle.InfoHash().HexString())
	done := handle.BytesCompleted()
	total := int64(0)
	hasMetadata := false
	if info := handle.Info(); info != nil {
		total = info.TotalLength()
		hasMetadata = true
	}
	progress := 0.0
	if total > 0 {
		progress = float64(done) / float64(total) * 100.0
	}
	state := "downloading"
	switch {
	case entry.paused:
		state = "paused"
	case !hasMetadata:
		state = "downloading_metadata"
	case handle.Seeding():
		state = "seeding"
	case entry.stalled:
		state = "stalled"
	}
	if entry.uploadOnly {
		state = "seeding"
	}
	sample := e.samples[hash]
	downloadRate := uint64(0)
	if !sample.at.IsZero() {
		elapsed := now.Sub(sample.at).Seconds()
		if elapsed > 0 && done >= sample.done {
			downloadRate = uint64(float64(done-sample.done) / elapsed)
		}
	}
	e.samples[hash] = anacrolixSample{done: done, at: now}
	stats := handle.Stats()
	view := models.TorrentView{
		Hash:              hash,
		Name:              handle.Name(),
		SavePath:          entry.savePath,
		Progress:          progress,
		State:             state,
		DownloadRate:      downloadRate,
		DownloadRateTotal: downloadRate,
		TotalSize:         total,
		TotalDone:         done,
		HasMetadata:       hasMetadata,
		IsSeeding:         handle.Seeding(),
		NumPeers:          stats.ActivePeers,
		NumSeeds:          stats.ConnectedSeeders,
		NumComplete:       stats.ConnectedSeeders,
		NumIncomplete:     stats.TotalPeers,
		AutoManaged:       true,
		SeedRatio:         -1,
		SeedDays:          -1,
	}
	view.Diagnosis, _, _ = DiagnoseTorrent(&view)
	return view
}

// sync rebuilds the snapshot and queues lifecycle events for transitions.
func (e *anacrolixEngine) sync() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("anacrolix: client closed")
	}
	now := time.Now()
	next := map[string]models.TorrentView{}
	for hash, entry := range e.state {
		view := e.toView(entry, now)
		// Persist metadata as soon as it is available so a restart is safe.
		if view.HasMetadata {
			if info := entry.handle.Info(); info != nil && !fileExists(filepath.Join(e.settings.MetadataDir, hash+".torrent")) {
				mi := entry.handle.Metainfo()
				e.persistTorrent(hash, &mi)
			}
		}
		next[hash] = view
	}
	for hash, view := range next {
		if previous, ok := e.previous[hash]; ok {
			e.diffLocked(previous, view)
		}
	}
	e.previous = next
	return nil
}

func (e *anacrolixEngine) diffLocked(previous, current models.TorrentView) {
	if !previous.HasMetadata && current.HasMetadata {
		e.events = append(e.events, models.TorrentEvent{Kind: "metadata_received", Hash: current.Hash, Name: current.Name, SavePath: current.SavePath})
	}
	if previous.Progress < 99.99 && current.Progress >= 99.99 {
		e.events = append(e.events, models.TorrentEvent{Kind: "torrent_finished", Hash: current.Hash, Name: current.Name, SavePath: current.SavePath})
	}
	if previous.State != "error" && current.State == "error" {
		e.events = append(e.events, models.TorrentEvent{Kind: "torrent_error", Hash: current.Hash, Name: current.Name, SavePath: current.SavePath, Message: current.Error})
	}
}

func (e *anacrolixEngine) List() []models.TorrentView {
	_ = e.sync()
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]models.TorrentView, 0, len(e.previous))
	for _, view := range e.previous {
		result = append(result, view)
	}
	return result
}

func (e *anacrolixEngine) PollEvents() []models.TorrentEvent {
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

func (e *anacrolixEngine) Stats() map[string]any {
	views := e.List()
	stats := map[string]any{
		"backend":        BackendAnacrolix,
		"torrents":       len(views),
		"session_loaded": e.client != nil,
	}
	if e.client != nil {
		clientStats := e.client.Stats()
		stats["active_peers"] = clientStats.ActivePeers
		stats["total_peers"] = clientStats.TotalPeers
		stats["bytes_read"] = clientStats.BytesReadData.Int64()
		stats["bytes_written"] = clientStats.BytesWrittenData.Int64()
	}
	return stats
}

// ---------------------------------------------------------------------------
// control
// ---------------------------------------------------------------------------

func (e *anacrolixEngine) lookup(hash string) (*anacrolixTorrent, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	entry, ok := e.state[strings.ToLower(strings.TrimSpace(hash))]
	return entry, ok
}

func (e *anacrolixEngine) Pause(hash string) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	entry.handle.DisallowDataDownload()
	entry.handle.DisallowDataUpload()
	e.mu.Lock()
	entry.paused = true
	e.mu.Unlock()
	return true, nil
}

func (e *anacrolixEngine) Resume(hash string) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	entry.handle.AllowDataDownload()
	entry.handle.AllowDataUpload()
	e.mu.Lock()
	entry.paused = false
	entry.stalled = false
	e.mu.Unlock()
	return true, nil
}

func (e *anacrolixEngine) Restart(hash string) (bool, error) {
	return e.Resume(hash)
}

// AdjustQueue enforces Gextto's active-download slots on anacrolix. Only the
// torrents this scheduler paused are resumed again, so a user pause is kept.
func (e *anacrolixEngine) AdjustQueue(cfg *Config, _ int64) {
	if cfg == nil {
		return
	}
	limit := int(cfg.Libtorrent.ActiveDownloads)
	if limit < 1 {
		limit = 1
	}
	type candidate struct {
		hash string
		name string
	}
	var active []candidate
	var paused []candidate
	for _, view := range e.List() {
		if view.Progress >= 99.99 {
			continue
		}
		switch view.State {
		case "downloading", "downloading_metadata", "stalled":
			active = append(active, candidate{view.Hash, view.Name})
		case "paused":
			e.mu.Lock()
			_, policy := e.policyPaused[view.Hash]
			e.mu.Unlock()
			if policy {
				paused = append(paused, candidate{view.Hash, view.Name})
			}
		}
	}
	less := func(items []candidate) {
		sort.Slice(items, func(i, j int) bool { return items[i].hash < items[j].hash })
	}
	less(active)
	less(paused)
	if len(active) > limit {
		for _, item := range active[limit:] {
			if _, err := e.Pause(item.hash); err != nil {
				continue
			}
			e.mu.Lock()
			e.policyPaused[item.hash] = struct{}{}
			e.mu.Unlock()
			logging.Info("📊 Queue: paused a torrent over the active slot limit", "name", item.name)
		}
		return
	}
	slots := limit - len(active)
	for index := 0; index < slots && index < len(paused); index++ {
		item := paused[index]
		if _, err := e.Resume(item.hash); err != nil {
			continue
		}
		e.mu.Lock()
		delete(e.policyPaused, item.hash)
		e.mu.Unlock()
		logging.Info("📊 Queue: resumed a torrent into a free active slot", "name", item.name)
	}
}

func (e *anacrolixEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	normalized := strings.ToLower(strings.TrimSpace(hash))
	name := entry.handle.Name()
	entry.handle.Drop()
	if deleteFiles {
		content := filepath.Join(entry.savePath, name)
		if name == "" || !pathWithin(content, entry.savePath) {
			content = entry.savePath
		}
		if info, err := os.Stat(content); err == nil {
			if info.IsDir() {
				_ = os.RemoveAll(content)
			} else {
				_ = os.Remove(content)
			}
		}
	}
	e.mu.Lock()
	delete(e.state, normalized)
	delete(e.previous, normalized)
	delete(e.samples, normalized)
	delete(e.manifest, normalized)
	e.mu.Unlock()
	_ = os.Remove(filepath.Join(e.settings.MetadataDir, normalized+".torrent"))
	e.saveManifest()
	return true, nil
}

func (e *anacrolixEngine) ForceRecheck(hash string) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	if err := entry.handle.VerifyData(); err != nil {
		return false, err
	}
	return true, nil
}

func (e *anacrolixEngine) Reannounce(hash string) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	handle := entry.handle
	mi := handle.Metainfo()
	if len(mi.AnnounceList) == 0 {
		return false, nil
	}
	handle.AddTrackers(mi.UpvertedAnnounceList())
	return true, nil
}

// MoveStorage relocates a torrent's data and re-adds it with the new storage
// base. anacrolix binds the storage directory when a torrent is added, so the
// move is a quiesce (drop, which closes the file handles), a filesystem move,
// then a re-add from the persisted .torrent. The shared piece-completion store
// keeps the verified pieces, so no data is re-downloaded.
func (e *anacrolixEngine) MoveStorage(hash, destination string) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" || strings.TrimSpace(destination) == "" {
		return false, nil
	}
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	if SamePath(entry.savePath, destination) {
		return false, nil
	}
	if _, ok := TranslateGexttoToBackend(destination, e.settings.Mappings); !ok && len(e.settings.Mappings) > 0 {
		return false, fmt.Errorf("%w: %s", ErrPathMappingMissing, destination)
	}
	name := entry.handle.Name()
	if strings.TrimSpace(name) == "" {
		return false, fmt.Errorf("anacrolix: cannot move a torrent without metadata")
	}
	torrentFile := filepath.Join(e.settings.MetadataDir, hash+".torrent")
	if !fileExists(torrentFile) {
		return false, fmt.Errorf("anacrolix: move requires a persisted .torrent for %s", hash)
	}
	source := filepath.Join(entry.savePath, name)
	target := filepath.Join(destination, name)
	if !pathWithin(source, entry.savePath) {
		return false, fmt.Errorf("anacrolix: refusing unsafe source path %s", source)
	}
	info, err := os.Stat(source)
	if err != nil {
		return false, fmt.Errorf("anacrolix: source data missing: %w", err)
	}
	if info.IsDir() {
		if entries, readErr := os.ReadDir(source); readErr == nil && len(entries) > 0 {
			return false, fmt.Errorf("anacrolix: destination already populated: %s", target)
		}
	} else if fileExists(target) {
		return false, fmt.Errorf("anacrolix: destination file already exists: %s", target)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return false, err
	}

	// Quiesce and detach the torrent before touching its files. Removing it from
	// the state map (under lock) keeps a concurrent List/sync from dereferencing
	// a dropped handle.
	wasPaused := entry.paused
	wasUploadOnly := entry.uploadOnly
	entry.handle.DisallowDataDownload()
	entry.handle.DisallowDataUpload()
	e.mu.Lock()
	delete(e.state, hash)
	delete(e.policyPaused, hash)
	e.mu.Unlock()
	entry.handle.Drop()

	readd := func(path string) {
		if _, addErr := e.addTorrentFileInternal(torrentFile, path, AddOptions{Paused: wasPaused, SeedMode: wasUploadOnly}, nil); addErr != nil {
			logging.Error("anacrolix move: re-add failed", "hash", hash, "path", path, "error", addErr.Error())
		}
	}
	if err := anacrolixMoveTree(source, target); err != nil {
		// Put the torrent back where it was so it is never lost.
		readd(entry.savePath)
		return false, err
	}
	if _, err := e.addTorrentFileInternal(torrentFile, destination, AddOptions{Paused: wasPaused, SeedMode: wasUploadOnly}, nil); err != nil {
		// Try to restore the previous location rather than leaving it detached.
		if moveErr := anacrolixMoveTree(target, source); moveErr == nil {
			readd(entry.savePath)
		}
		return false, err
	}
	if !wasPaused {
		if _, err := e.Resume(hash); err != nil {
			logging.Debug("anacrolix move: resume after move failed", "hash", hash, "error", err.Error())
		}
	}
	logging.Info("📁 anacrolix storage moved", "hash", hash, "from", entry.savePath, "to", destination)
	return true, nil
}

// anacrolixMoveTree moves a file or directory, falling back to a copy+delete
// across filesystems.
func anacrolixMoveTree(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := copyTree(source, target); err != nil {
			return err
		}
		return os.RemoveAll(source)
	}
	if err := migrateCopyFile(source, target); err != nil {
		return err
	}
	return os.Remove(source)
}

func (e *anacrolixEngine) MarkStalled(hash string) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	e.mu.Lock()
	entry.stalled = true
	e.mu.Unlock()
	return true, nil
}

func (e *anacrolixEngine) ClearStalled(hash string) {
	if entry, ok := e.lookup(hash); ok {
		e.mu.Lock()
		entry.stalled = false
		e.mu.Unlock()
	}
}

func (e *anacrolixEngine) RamdiskUncommittedBytes(ramdisk string, excludeHash string) uint64 {
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

func (e *anacrolixEngine) Files(hash string) ([]models.FileView, bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return nil, false, nil
	}
	files := entry.handle.Files()
	out := make([]models.FileView, 0, len(files))
	for _, file := range files {
		out = append(out, models.FileView{
			Path:       file.Path(),
			Size:       file.Length(),
			Downloaded: file.BytesCompleted(),
			Priority:   anacrolixPriorityToInt(file.Priority()),
		})
	}
	return out, true, nil
}

func (e *anacrolixEngine) Peers(hash string) ([]models.PeerView, bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return nil, false, nil
	}
	conns := entry.handle.PeerConns()
	out := make([]models.PeerView, 0, len(conns))
	for _, conn := range conns {
		stats := conn.Stats()
		out = append(out, models.PeerView{
			Address:      fmt.Sprintf("%s", conn.RemoteAddr),
			Client:       conn.Network,
			DownloadRate: uint64(stats.DownloadRate),
			UploadRate:   uint64(stats.LastWriteUploadRate),
			Pieces:       stats.RemotePieceCount,
		})
	}
	return out, true, nil
}

func (e *anacrolixEngine) Trackers(hash string) ([]models.TrackerView, bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return nil, false, nil
	}
	mi := entry.handle.Metainfo()
	list := mi.UpvertedAnnounceList()
	out := []models.TrackerView{}
	for tier, urls := range list {
		for _, url := range urls {
			out = append(out, models.TrackerView{URL: url, Tier: tier, Verified: true})
		}
	}
	return out, true, nil
}

// PieceRuns exposes compact per-piece diagnostics for the piece inspector.
func (e *anacrolixEngine) PieceRuns(hash string) ([]TorrentPieceRun, bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return nil, false, nil
	}
	runs := entry.handle.PieceStateRuns()
	out := make([]TorrentPieceRun, 0, len(runs))
	begin := 0
	for _, run := range runs {
		length := run.Length
		if length <= 0 {
			continue
		}
		out = append(out, TorrentPieceRun{
			Begin: begin,
			End:   begin + length - 1,
			State: anacrolixPieceState(run.PieceState),
		})
		begin += length
	}
	return out, true, nil
}

func anacrolixPieceState(state torrent.PieceState) string {
	switch {
	case state.Complete && state.Ok:
		return "complete"
	case state.Hashing || state.QueuedForHash:
		return "checking"
	case state.Partial:
		return "partial"
	default:
		return "missing"
	}
}

// ---------------------------------------------------------------------------
// mutations not fully supported by anacrolix
// ---------------------------------------------------------------------------

func (e *anacrolixEngine) SetFilePriorities(hash string, priorities []int32) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	files := entry.handle.Files()
	if len(priorities) != len(files) {
		return false, fmt.Errorf("anacrolix: %d priorities for %d files", len(priorities), len(files))
	}
	for index, file := range files {
		file.SetPriority(intToAnacrolixPriority(int(priorities[index])))
	}
	return true, nil
}

func (e *anacrolixEngine) SetTrackers(hash string, trackers []TrackerEntry) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	urls := make([]string, 0, len(trackers))
	for _, tracker := range trackers {
		if url := strings.TrimSpace(tracker.URL); url != "" {
			urls = append(urls, url)
		}
	}
	if len(urls) == 0 {
		return false, nil
	}
	entry.handle.AddTrackers([][]string{urls})
	return true, nil
}

func (e *anacrolixEngine) WebSeeds(hash, urls string, remove bool) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	if remove {
		return false, backendCapabilityError(BackendAnacrolix, "web_seeds_remove")
	}
	list := strings.FieldsFunc(urls, func(r rune) bool { return r == ',' || r == '\n' })
	entry.handle.AddWebSeeds(list)
	return true, nil
}

func (e *anacrolixEngine) SetLimits(hash string, downloadLimit, uploadLimit int64, seedRatio float64, seedDays int64) (bool, error) {
	return false, backendCapabilityError(BackendAnacrolix, "per_torrent_limits")
}

func (e *anacrolixEngine) SetGlobalSpeedLimits(downloadKib, uploadKib int64) (bool, error) {
	return false, backendCapabilityError(BackendAnacrolix, "runtime_global_limits")
}

func (e *anacrolixEngine) SetMaxConnections(hash string, value int) (bool, error) {
	entry, ok := e.lookup(hash)
	if !ok {
		return false, nil
	}
	entry.handle.SetMaxEstablishedConns(value)
	return true, nil
}

func (e *anacrolixEngine) SetMaxUploads(hash string, value int) (bool, error) {
	return false, backendCapabilityError(BackendAnacrolix, "per_torrent_uploads")
}

func (e *anacrolixEngine) SetPin(hash string, pinned bool) (bool, error) {
	return false, backendCapabilityError(BackendAnacrolix, "pin")
}

func (e *anacrolixEngine) SetSequential(enabled bool) (bool, error) {
	return false, backendCapabilityError(BackendAnacrolix, "sequential")
}

func (e *anacrolixEngine) AssociateStorage(hash, destination string) (bool, error) {
	return false, backendCapabilityError(BackendAnacrolix, "associate_storage")
}

func (e *anacrolixEngine) TorrentFilePath(hash string) (string, bool) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return "", false
	}
	path := filepath.Join(e.settings.MetadataDir, hash+".torrent")
	if fileExists(path) {
		return path, true
	}
	return "", false
}

// Close releases the client and the completion store.
func (e *anacrolixEngine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()
	errs := e.client.Close()
	if closer, ok := e.completion.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// priority conversion
// ---------------------------------------------------------------------------

func anacrolixPriorityToInt(priority torrent.PiecePriority) int {
	switch {
	case priority <= torrent.PiecePriorityNone:
		return 0
	case priority >= torrent.PiecePriorityNow:
		return 7
	case priority >= torrent.PiecePriorityHigh:
		return 6
	default:
		return 1
	}
}

func intToAnacrolixPriority(priority int) torrent.PiecePriority {
	switch {
	case priority <= 0:
		return torrent.PiecePriorityNone
	case priority >= 7:
		return torrent.PiecePriorityNow
	case priority >= 5:
		return torrent.PiecePriorityHigh
	default:
		return torrent.PiecePriorityNormal
	}
}

// anacrolixBlocklist loads the configured local ipfilter file (eMule format).
// URLs are ignored: fetching them at startup would add a network dependency;
// a remote filter can be refreshed by Gextto into a local file.
func anacrolixBlocklist(settings anacrolixSettings) iplist.Ranger {
	path := strings.TrimSpace(settings.IpFilterPath)
	if path == "" || !fileExists(path) {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		logging.Warn("anacrolix ipfilter open failed", "path", path, "error", err.Error())
		return nil
	}
	defer file.Close()
	ranger, err := iplist.NewFromReader(file)
	if err != nil {
		logging.Warn("anacrolix ipfilter parse failed", "path", path, "error", err.Error())
		return nil
	}
	logging.Info("anacrolix ipfilter loaded", "path", path)
	return ranger
}

// anacrolixHTTPProxy maps an HTTP proxy (type 3/4) onto the anacrolix HTTP
// proxy hook. SOCKS proxies are not supported by anacrolix and are reported
// instead of silently ignored.
func anacrolixHTTPProxy(settings anacrolixSettings) *url.URL {
	switch settings.ProxyType {
	case 3, 4: // http, http_pw
	default:
		if settings.ProxyType == 1 || settings.ProxyType == 2 || settings.ProxyType == 5 {
			logging.Warn("anacrolix does not support SOCKS proxies; proxy ignored",
				"proxy_type", settings.ProxyType)
		}
		return nil
	}
	host := strings.TrimSpace(settings.ProxyHost)
	if host == "" || settings.ProxyPort <= 0 {
		return nil
	}
	proxy := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, strconv.FormatInt(settings.ProxyPort, 10)),
	}
	if user := strings.TrimSpace(settings.ProxyUser); user != "" {
		proxy.User = url.UserPassword(user, settings.ProxyPassword)
	}
	return proxy
}

// anacrolixDhtNodes splits the configured DHT bootstrap list.
func anacrolixDhtNodes(settings anacrolixSettings) []string {
	raw := strings.TrimSpace(settings.DhtBootstrapNodes)
	if raw == "" {
		return nil
	}
	nodes := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', '\n', '\r', ' ', '\t':
			return true
		}
		return false
	})
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if trimmed := strings.TrimSpace(node); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// clampBurst keeps a rate limiter burst within a sane range.
func clampBurst(bytes int64) int64 {
	const minimum = 256 * 1024
	if bytes < minimum {
		return minimum
	}
	if bytes > math.MaxInt32 {
		return math.MaxInt32
	}
	return bytes
}

// context is referenced by future piece-check scheduling; keep the import used.
var _ = context.Background
