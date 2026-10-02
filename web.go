package gextto

// Core shared definitions for the web layer: application state, background
// state containers, response/query helpers, the RAM-disk helpers, the setup
// helpers and every request/response input struct. This is the Go implementation of
// the daemon (the parts shared by all handler groups).

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// ---------------------------------------------------------------------------
// Response cache ( `RESPONSE_CACHE`)
// ---------------------------------------------------------------------------

type responseCacheEntry struct {
	at    time.Time
	value map[string]any
}

var (
	responseCacheMu sync.Mutex
	responseCache   = map[string]responseCacheEntry{}
)

// CacheGet returns a cached JSON payload younger than ttl.
func CacheGet(key string, ttl time.Duration) (map[string]any, bool) {
	responseCacheMu.Lock()
	defer responseCacheMu.Unlock()
	entry, ok := responseCache[key]
	if !ok || time.Since(entry.at) >= ttl {
		return nil, false
	}
	return entry.value, true
}

// CachePut stores a JSON payload in the short-lived response cache.
func CachePut(key string, value map[string]any) {
	responseCacheMu.Lock()
	defer responseCacheMu.Unlock()
	responseCache[key] = responseCacheEntry{at: time.Now(), value: value}
}

// cacheGet/cachePut are the contract spellings used by the handler files.
func cacheGet(key string, ttl time.Duration) (map[string]any, bool) { return CacheGet(key, ttl) }
func cachePut(key string, value map[string]any)                     { CachePut(key, value) }

// ---------------------------------------------------------------------------
// RAM disk helpers ( lines 68-188)
// ---------------------------------------------------------------------------

// DecodeMountField decodes the octal escaping used for mount points in
// /proc/self/mountinfo.
func DecodeMountField(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace(value)
}

// DetectedRamdiskMounts returns the mounted tmpfs/ramfs roots visible to the
// daemon, sorted by path and de-duplicated.
func DetectedRamdiskMounts() [][2]string {
	mounts := [][2]string{}
	if contents, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		for _, line := range strings.Split(string(contents), "\n") {
			parts := strings.SplitN(line, " - ", 2)
			if len(parts) != 2 {
				continue
			}
			left := strings.Fields(parts[0])
			if len(left) < 5 {
				continue
			}
			rawPath := left[4]
			right := strings.Fields(parts[1])
			if len(right) == 0 {
				continue
			}
			filesystem := right[0]
			if filesystem != "tmpfs" && filesystem != "ramfs" {
				continue
			}
			path := DecodeMountField(rawPath)
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				mounts = append(mounts, [2]string{path, filesystem})
			}
		}
	}
	// /dev/shm is the conventional user-writable tmpfs and is useful even on
	// systems where mountinfo is restricted by a container.
	shm := "/dev/shm"
	present := false
	for _, mount := range mounts {
		if mount[0] == shm {
			present = true
			break
		}
	}
	if info, err := os.Stat(shm); err == nil && info.IsDir() && !present {
		mounts = append(mounts, [2]string{shm, "tmpfs"})
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i][0] < mounts[j][0] })
	deduped := mounts[:0]
	for i, mount := range mounts {
		if i == 0 || mounts[i-1][0] != mount[0] {
			deduped = append(deduped, mount)
		}
	}
	return deduped
}

// DirectoryWritable reports whether the daemon can write to path.
func DirectoryWritable(path string) bool {
	if path == "" {
		return false
	}
	// libc::W_OK == 2.
	return syscall.Access(path, 2) == nil
}

// FilesystemSpace returns (total, free, ok) bytes for the filesystem holding
// path.
func FilesystemSpace(path string) (uint64, uint64, bool) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return 0, 0, false
	}
	blockSize := uint64(stats.Frsize)
	if blockSize == 0 {
		blockSize = uint64(stats.Bsize)
	}
	return stats.Blocks * blockSize, stats.Bavail * blockSize, true
}

func ramdiskSatSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

// RamdiskRecommendation implements `ramdisk_recommendation`.
func RamdiskRecommendation(path string) map[string]any {
	const gib = 1024.0 * 1024.0 * 1024.0
	total, free, _ := FilesystemSpace(path)
	margin := uint64(math.Max(float64(free)*0.10, 0.5*gib))
	if limit := uint64(4.0 * gib); margin > limit {
		margin = limit
	}
	threshold := uint64(math.Min(float64(total)*0.50, float64(ramdiskSatSub(free, margin))))
	if floor := uint64(0.5 * gib); threshold < floor {
		threshold = floor
	}
	roundGib := func(bytes uint64) float64 { return math.Round(float64(bytes)/gib*10.0) / 10.0 }
	return map[string]any{
		"threshold_gb":   fmt.Sprintf("%.1f", roundGib(threshold)),
		"margin_gb":      fmt.Sprintf("%.1f", roundGib(margin)),
		"min_free_bytes": "0",
		"total_bytes":    total,
		"free_bytes":     free,
	}
}

// RamdiskEntry implements `ramdisk_entry`.
func RamdiskEntry(path string, filesystem string, configured *string) map[string]any {
	isDir := false
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		isDir = true
	}
	var free uint64
	if value := FreeSpaceBytes(path); value != nil {
		free = *value
	}
	return map[string]any{
		"path":        path,
		"filesystem":  filesystem,
		"exists":      isDir,
		"writable":    isDir && DirectoryWritable(path),
		"free_bytes":  free,
		"configured":  configured != nil && *configured == path,
		"recommended": RamdiskRecommendation(path),
	}
}

func jsonStringField(value map[string]any, key string, fallback string) string {
	if raw, ok := value[key].(string); ok {
		return raw
	}
	return fallback
}

// saveConfigSetting persists one setting in `gextto_config.db`. It mirrors
// `Config::save_setting` without depending on the exported helper name, which
// the web `save_setting` handler may otherwise shadow.
func saveConfigSetting(dataDir string, key string, value string) error {
	conn, err := OpenConfigDB(filepath.Join(dataDir, "gextto_config.db"))
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := sqlExec(conn, "CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);"); err != nil {
		return err
	}
	if err := sqlExec(conn, "INSERT INTO settings(key,value) VALUES (?1,?2) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value); err != nil {
		return err
	}
	TouchConfigGeneration()
	return nil
}

// SaveRamdiskSettings persists the RAM-disk settings and returns the computed
// recommendation.
func SaveRamdiskSettings(dataDir string, path string) (map[string]any, error) {
	recommendation := RamdiskRecommendation(path)
	steps := []struct {
		key   string
		value string
	}{
		{"libtorrent_ramdisk_dir", path},
		{"libtorrent_ramdisk_enabled", "yes"},
		{"libtorrent_ramdisk_threshold_gb", jsonStringField(recommendation, "threshold_gb", "3.5")},
		{"libtorrent_ramdisk_margin_gb", jsonStringField(recommendation, "margin_gb", "0.5")},
		{"libtorrent_ramdisk_min_free_bytes", "0"},
	}
	for _, step := range steps {
		if err := saveConfigSetting(dataDir, step.key, step.value); err != nil {
			return nil, err
		}
	}
	return recommendation, nil
}

// ---------------------------------------------------------------------------
// JSON defaults
// ---------------------------------------------------------------------------

// DefaultTmdbKind is `default_tmdb_kind`.
func DefaultTmdbKind() string { return "series" }

// DefaultMetadataSource is `default_metadata_source`.
func DefaultMetadataSource() string { return "tmdb" }

// ---------------------------------------------------------------------------
// Setup helpers ( lines 897-953)
// ---------------------------------------------------------------------------

// SetupMarker implements `setup_marker`.
func SetupMarker(cfg *Config) string {
	return filepath.Join(cfg.DataDir, ".gextto-setup.json")
}

// SetupComplete reports whether the setup marker exists.
func SetupComplete(cfg *Config) bool {
	info, err := os.Stat(SetupMarker(cfg))
	return err == nil && !info.IsDir()
}

// CompleteSetup writes the setup marker file.
func CompleteSetup(cfg *Config) error {
	payload, err := json.MarshalIndent(map[string]any{
		"completed_at": time.Now().UTC().Format("2006-01-02T15:04:05+00:00"),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(SetupMarker(cfg), payload, 0o644)
}

// ---------------------------------------------------------------------------
// Config cache ( lines 928-944)
// ---------------------------------------------------------------------------

// ConfigCache holds the loaded configuration together with the generation it
// was loaded at.
type ConfigCache struct {
	mu         sync.Mutex
	generation uint64
	cfg        *Config
	valid      bool
}

// LatestConfig returns the configuration reloaded when the generation changed.
func LatestConfig(s *AppState) *Config {
	generation := ConfigGeneration()
	if s.config_cache != nil {
		s.config_cache.mu.Lock()
		if s.config_cache.valid && s.config_cache.generation == generation {
			cached := s.config_cache.cfg
			s.config_cache.mu.Unlock()
			return cached
		}
		s.config_cache.mu.Unlock()
	}
	loaded, err := LoadConfig(s.config_path)
	if err != nil {
		return s.cfg
	}
	cfg := loaded
	if s.config_cache != nil {
		s.config_cache.mu.Lock()
		s.config_cache.generation = generation
		s.config_cache.cfg = &cfg
		s.config_cache.valid = true
		s.config_cache.mu.Unlock()
	}
	return &cfg
}

// latestConfig is the contract spelling used by the handler files.
func latestConfig(s *AppState) *Config { return LatestConfig(s) }

// ---------------------------------------------------------------------------
// Background state containers
// ---------------------------------------------------------------------------

// EventLog is the capped torrent-event buffer (
// `Arc<Mutex<Vec<TorrentEvent>>>` with a 512-entry cap).
type EventLog struct {
	mu    sync.Mutex
	items []models.TorrentEvent
}

// Push appends one event, dropping the oldest entries past the 512 cap.
func (e *EventLog) Push(event models.TorrentEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.items = append(e.items, event)
	if len(e.items) > 512 {
		e.items = e.items[len(e.items)-512:]
	}
}

// Drain removes and returns every buffered event.
func (e *EventLog) Drain() []models.TorrentEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	items := e.items
	e.items = nil
	return items
}

// Snapshot returns a copy of the buffered events.
func (e *EventLog) Snapshot() []models.TorrentEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	items := make([]models.TorrentEvent, len(e.items))
	copy(items, e.items)
	return items
}

// SnapshotAndClear returns a copy of the buffered events and empties the log.
func (e *EventLog) SnapshotAndClear() []models.TorrentEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	items := make([]models.TorrentEvent, len(e.items))
	copy(items, e.items)
	e.items = nil
	return items
}

// CycleState guards the latest cycle statistics.
type CycleState struct {
	mu   sync.Mutex
	data models.CycleStats
}

// Snapshot returns the current cycle statistics.
func (c *CycleState) Snapshot() models.CycleStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	value := c.data
	if value.ErrorDetails == nil {
		value.ErrorDetails = map[string]int{}
	}
	return value
}

// Set replaces the current cycle statistics.
func (c *CycleState) Set(value models.CycleStats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = value
}

// Mutate runs fn against the current cycle statistics while holding the lock.
func (c *CycleState) Mutate(fn func(*models.CycleStats)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.data)
}

// RenameProgress is the progress of a background rename-all job.
type RenameProgress struct {
	Running bool   `json:"running"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Series  string `json:"series"`
	Message string `json:"message"`
	Errors  int    `json:"errors"`
}

// ---------------------------------------------------------------------------
// Archive import guard ( lines 340-370)
// ---------------------------------------------------------------------------

var (
	archiveImportBusyMu sync.Mutex
	archiveImportBusy   = map[string]struct{}{}
)

// ArchiveImportGuard is an RAII marker for "this series is being imported right
// now".
type ArchiveImportGuard struct {
	series *string
}

// AcquireArchiveImport marks the trimmed series name as busy and returns the
// guard. A nil or empty series yields a no-op guard.
func AcquireArchiveImport(series *string) *ArchiveImportGuard {
	guard := &ArchiveImportGuard{}
	if series != nil {
		name := strings.TrimSpace(*series)
		if name != "" {
			archiveImportBusyMu.Lock()
			archiveImportBusy[name] = struct{}{}
			archiveImportBusyMu.Unlock()
			guard.series = &name
		}
	}
	runtime.SetFinalizer(guard, func(value *ArchiveImportGuard) { value.Release() })
	return guard
}

// Release clears the busy marker. It is safe to call more than once.
func (g *ArchiveImportGuard) Release() {
	if g == nil || g.series == nil {
		return
	}
	archiveImportBusyMu.Lock()
	delete(archiveImportBusy, *g.series)
	archiveImportBusyMu.Unlock()
	g.series = nil
}

// ---------------------------------------------------------------------------
// Application state ( lines 296-320)
// ---------------------------------------------------------------------------

// AppState is shared by every web handler. Field names intentionally mirror the
// struct so the port reads the same.
type AppState struct {
	cfg         *Config // startup snapshot
	config_path string
	i18n        *I18nDb
	db          *Database
	archive     *Archive
	comics      *ComicsDb
	engine      *Engine
	torrents    *LibtorrentClient
	// torrent_engine is the active transfer engine. When nil the daemon uses
	// the embedded libtorrent adapter (see activeEngine in torrent_engine.go).
	// engine_mu guards it because the backend can be switched at runtime while
	// the background worker reads it.
	engine_mu      sync.RWMutex
	torrent_engine TorrentEngine
	torrent_events *EventLog
	notifier       *Notifier
	tmdb           *TmdbClient
	last_cycle     *CycleState
	cycle_lock     *sync.Mutex
	// manualCyclePending coalesces repeated manual cycle requests while one is
	// waiting for, or holding, cycle_lock. Without it a rapid series of clicks
	// could create an unbounded backlog of full monitoring runs.
	manualCyclePending atomic.Bool
	// health_status debounces the top-bar status so "degraded" only appears
	// after it has persisted for a few seconds (see uiShellChromeFrom).
	health_status   healthStatusDebouncer
	rename_progress *RenameProgress
	// rename_progress_mu guards rename_progress: the rename-all job writes its
	// progress from a background goroutine while the UI polls it, and two
	// concurrent rename requests must not both pass the "already running" check.
	rename_progress_mu sync.Mutex
	config_cache       *ConfigCache
	// jobs tracks long-running background operations with an observable state
	// (see docs/revisione-1.md, section 7). Handlers create and query jobs, and
	// stopBackgroundWorkers closes the manager on shutdown.
	jobs *JobManager
	// Background-worker lifecycle. bgStop is closed when the daemon starts
	// shutting down and bgWG tracks every worker, so Serve waits for them before
	// returning and the torrent session is destroyed only when no worker can
	// call into it anymore.
	bgStop     chan struct{}
	bgStopOnce sync.Once
	bgWG       sync.WaitGroup
	bgContext  context.Context
	bgCancel   context.CancelFunc
}

// ---------------------------------------------------------------------------
// Handler plumbing
// ---------------------------------------------------------------------------

// HandlerFunc is the common web handler signature: the state is injected by
// `handle` instead of being a global.
type HandlerFunc func(w http.ResponseWriter, r *http.Request, s *AppState)

// handle registers fn on mux under pattern, installing cache-control middleware
// and the request-body limit shared by all routes.
// maxRequestBodyBytes caps every request body. It is generous enough for
// torrent uploads and large JSON configuration saves, but prevents a client
// from making the daemon allocate an unbounded amount of memory.
const maxRequestBodyBytes = 16 << 20 // 16 MiB

func handle(s *AppState, mux *http.ServeMux, pattern string, fn HandlerFunc) {
	routeMu.Lock()
	registeredRouteList = append(registeredRouteList, pattern)
	routeMu.Unlock()
	core := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		guardHandler(pattern, fn)(w, r, s)
	})
	mux.Handle(pattern, UiNoCache(core))
}

// RegisteredRoutes returns every HTTP route pattern installed by Router. It is
// used by the route smoke test to make sure no endpoint returns a 500.
func RegisteredRoutes() []string {
	routeMu.Lock()
	defer routeMu.Unlock()
	out := make([]string, len(registeredRouteList))
	copy(out, registeredRouteList)
	return out
}

var (
	routeMu             sync.Mutex
	registeredRouteList []string
)

// jsonResponse writes value as a 200 application/json response.
func jsonResponse(w http.ResponseWriter, value any) {
	jsonStatus(w, http.StatusOK, value)
}

// jsonStatus writes value as application/json with the given status.
func jsonStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(normalizeJSON(value))
}

// jsonError writes the canonical `{"ok":false,"error":message}` body.
func jsonError(w http.ResponseWriter, status int, message string) {
	jsonStatus(w, status, map[string]any{"ok": false, "error": message})
}

// pathParam returns the `{key}` path wildcard, or "" when absent.
func pathParam(r *http.Request, key string) string { return r.PathValue(key) }

// pathInt parses the `{key}` path wildcard as an int64.
func pathInt(r *http.Request, key string) (int64, bool) {
	value, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// queryParam returns the query parameter or "".
func queryParam(r *http.Request, key string) string { return r.URL.Query().Get(key) }

// queryInt parses the query parameter, falling back to def.
func queryInt(r *http.Request, key string, def int64) int64 {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return value
}

// decodeJSON decodes the request body into target.
func decodeJSON(r *http.Request, target any) error {
	return json.NewDecoder(r.Body).Decode(target)
}

// ---------------------------------------------------------------------------
// Input structs ( lines 372-955). Fields are CamelCase of the field
// with the exact JSON JSON tag.
// ---------------------------------------------------------------------------

// LogLevel is the input of `set_log_level`.
type LogLevel struct {
	Level string `json:"level"`
}

// ComicInput is the input of `add_comic`.
type ComicInput struct {
	Title       string `json:"title"`
	TagUrl      string `json:"tag_url"`
	FromDate    string `json:"from_date"`
	SavePath    string `json:"save_path"`
	PostUrl     string `json:"post_url"`
	CoverUrl    string `json:"cover_url"`
	Publisher   string `json:"publisher"`
	Description string `json:"description"`
}

// ComicLinksInput is the input of `comic_links`.
type ComicLinksInput struct {
	Url string `json:"url"`
}

// ComicExploreInput is the input of `comic_explore`.
type ComicExploreInput struct {
	Query string `json:"query"`
}

// ComicDownloadInput is the input of `comic_download`.
type ComicDownloadInput struct {
	Url      string `json:"url"`
	Method   string `json:"method"`
	Title    string `json:"title"`
	PostUrl  string `json:"post_url"`
	SavePath string `json:"save_path"`
}

// ComicDownloadTagInput is the input of `comic_download_tag`.
type ComicDownloadTagInput struct {
	Id  string `json:"id"`
	Tag string `json:"tag"`
}

// ComicWeeklyInput is the input of `comic_weekly_links`.
type ComicWeeklyInput struct {
	Date string `json:"date"`
}

// ComicEnabled is the input of `set_comic_enabled`.
type ComicEnabled struct {
	Enabled bool `json:"enabled"`
}

// SettingInput is the input of `save_setting`.
type SettingInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// RamDiskCreateInput is the input of `create_ramdisk`.
type RamDiskCreateInput struct {
	Path *string `json:"path"`
}

// RamDiskSelectInput is the input of `select_ramdisk`.
type RamDiskSelectInput struct {
	Path string `json:"path"`
}

// AuthCode is the input of the trakt/simkl auth poll endpoints.
type AuthCode struct {
	Code string `json:"code"`
}

// LibraryInput is the input of `save_library` / `save_config_root`.
type LibraryInput struct {
	Series []SeriesConfig `json:"series"`
	Movies []MovieConfig  `json:"movies"`
}

// ScrobbleInput is the input of the scrobble endpoints.
type ScrobbleInput struct {
	Action  *string `json:"action"`
	Payload any     `json:"payload"`
}

// I18nInput is the input of `i18n_set`.
type I18nInput struct {
	Lang  string `json:"lang"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LanguageInput is the input of `i18n_language`.
type LanguageInput struct {
	Lang string `json:"lang"`
}

// I18nYamlInput is the input of `i18n_import`.
type I18nYamlInput struct {
	Yaml string `json:"yaml"`
}

// I18nQuery is the query of `i18n_list`.
type I18nQuery struct {
	Lang *string `json:"lang"`
}

// ArchiveQuery is the query of `archive_entries`.
type ArchiveQuery struct {
	Query *string `json:"query"`
	Q     *string `json:"q"`
	Page  *int    `json:"page"`
	Limit *int    `json:"limit"`
}

// SeenQuery is the query of the seen/grouped views.
type SeenQuery struct {
	Page  *int    `json:"page"`
	Limit *int    `json:"limit"`
	Q     *string `json:"q"`
	Key   *string `json:"key"`
	Group *string `json:"group"`
	Kind  *string `json:"kind"`
}

// HistoryQuery is the query of the history endpoints.
type HistoryQuery struct {
	Page  *int    `json:"page"`
	Limit *int    `json:"limit"`
	Q     *string `json:"q"`
}

// LogQuery is the query of `logs` / `logs_stream`.
type LogQuery struct {
	Limit *int `json:"limit"`
}

// RunNowQuery is the query of `run_now`.
type RunNowQuery struct {
	Domain *string `json:"domain"`
}

// AddTorrentInput is the input of `add_torrent`.
type AddTorrentInput struct {
	Magnet         string  `json:"magnet"`
	SavePath       *string `json:"save_path"`
	StartPaused    bool    `json:"start_paused"`
	NoRename       bool    `json:"no_rename"`
	Sequential     bool    `json:"sequential"`
	SeedMode       bool    `json:"seed_mode"`
	QueueTop       bool    `json:"queue_top"`
	FirstLast      bool    `json:"first_last"`
	StopAtMetadata bool    `json:"stop_at_metadata"`
	// Preallocate overrides the global `libtorrent_preallocate` setting for
	// this add; nil means "use the global default".
	Preallocate *bool `json:"preallocate"`
}

// ScorePreviewInput is the input of `score_preview`.
type ScorePreviewInput struct {
	Title string `json:"title"`
}

// NoRenameInput is the input of `set_torrent_no_rename`.
type NoRenameInput struct {
	Value bool `json:"value"`
}

// RemoveTorrentInput is the input of `remove_torrent_legacy`.
type RemoveTorrentInput struct {
	Hash        string `json:"hash"`
	DeleteFiles bool   `json:"delete_files"`
}

// RemoveCompletedInput is the input of `remove_completed_torrents`.
type RemoveCompletedInput struct {
	DeleteFiles bool `json:"delete_files"`
}

// CleanTrashInput is the input of `clean_trash`.
type CleanTrashInput struct {
	Force bool `json:"force"`
}

// RemoveOptionsInput is the input of `remove_torrent_with_options`.
type RemoveOptionsInput struct {
	DeleteFiles bool `json:"delete_files"`
	Blocklist   bool `json:"blocklist"`
}

// TorrentLimitsInput is the input of `set_torrent_limits_legacy`.
type TorrentLimitsInput struct {
	Hash      string   `json:"hash"`
	DlKbps    int64    `json:"dl_kbps"`
	UlKbps    int64    `json:"ul_kbps"`
	SeedRatio *float64 `json:"seed_ratio"`
	SeedDays  *int64   `json:"seed_days"`
}

// TorrentHashInput is the input of the hash-only torrent actions.
type TorrentHashInput struct {
	Hash string `json:"hash"`
}

// SequentialInput is the input of `set_sequential`.
type SequentialInput struct {
	Enabled bool `json:"enabled"`
}

// TorrentTagInput is the input of `set_torrent_tag`.
type TorrentTagInput struct {
	Hash string `json:"hash"`
	Tag  string `json:"tag"`
}

// DownloadTagInput is the input of `add_download_tag`.
type DownloadTagInput struct {
	Tag string `json:"tag"`
}

// SearchInput is the input of `manual_search`.
type SearchInput struct {
	Query string `json:"query"`
}

// ManualSearchQuery is the query of `manual_search_get`.
type ManualSearchQuery struct {
	Q     *string `json:"q"`
	Query *string `json:"query"`
}

// SearchAddInput is the input of `add_search_result`.
type SearchAddInput struct {
	Release models.Release `json:"release"`
}

// ExplainReleaseInput is the input of `explain_release`.
type ExplainReleaseInput struct {
	Release models.Release `json:"release"`
}

// ArchiveAddInput is the input of `add_archive_entry`.
type ArchiveAddInput struct {
	Title  string `json:"title"`
	Magnet string `json:"magnet"`
	Source string `json:"source"`
}

// ArchiveDeleteInput is the input of `delete_archive_entry`.
type ArchiveDeleteInput struct {
	Magnet string  `json:"magnet"`
	Ids    []int64 `json:"ids"`
}

// ArchiveBatchItem is one item of `batch_archive_download`.
type ArchiveBatchItem struct {
	Title  string `json:"title"`
	Magnet string `json:"magnet"`
	Source string `json:"source"`
}

// ArchiveBatchInput is the input of `batch_archive_download`.
type ArchiveBatchInput struct {
	Items []ArchiveBatchItem `json:"items"`
}

// IgnoreEpisodeInput is the input of `ignore_episode`.
type IgnoreEpisodeInput struct {
	Ignored bool   `json:"ignored"`
	Reason  string `json:"reason"`
}

// MissingSearchInput is the input of `search_missing`.
type MissingSearchInput struct {
	Series  string `json:"series"`
	Season  int64  `json:"season"`
	Episode int64  `json:"episode"`
}

// ArchiveScanInput is the input of `scan_all_archives`.
type ArchiveScanInput struct {
	Path *string `json:"path"`
}

// TmdbQuery is the input of `tmdb_search`.
type TmdbQuery struct {
	Query string `json:"query"`
	Kind  string `json:"kind"`
}

// TmdbAddInput is the input of `tmdb_add`.
type TmdbAddInput struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Year        string `json:"year"`
	TmdbId      string `json:"tmdb_id"`
	TvdbId      string `json:"tvdb_id"`
	Quality     string `json:"quality"`
	Language    string `json:"language"`
	Seasons     string `json:"seasons"`
	ArchivePath string `json:"archive_path"`
	Exclude     string `json:"exclude"`
	Subtitle    string `json:"subtitle"`
	Aliases     string `json:"aliases"`
}

// DiscoverInput is the input of `tmdb_discover`.
type DiscoverInput struct {
	Kind   *string `json:"kind"`
	Window *string `json:"window"`
	Mode   *string `json:"mode"`
}

// TorrentLimits is the input of `set_torrent_limits`.
type TorrentLimits struct {
	DownloadLimit int64    `json:"download_limit"`
	UploadLimit   int64    `json:"upload_limit"`
	SeedRatio     *float64 `json:"seed_ratio"`
	SeedDays      *int64   `json:"seed_days"`
	// Per-torrent connection ceilings (nil = leave unchanged, 0 = unlimited).
	MaxConnections *int64 `json:"max_connections"`
	MaxUploads     *int64 `json:"max_uploads"`
}

// StoragePath is the input of `move_torrent_storage`.
type StoragePath struct {
	Path string `json:"path"`
}

// TagDirRule is one tag directory rule.
type TagDirRule struct {
	Tag      string `json:"tag"`
	TempDir  string `json:"temp_dir"`
	FinalDir string `json:"final_dir"`
}

// SeasonToggleInput is the input of `toggle_season`.
type SeasonToggleInput struct {
	Season  int64 `json:"season"`
	Enabled bool  `json:"enabled"`
}

// SeriesPathInput is the input of `set_series_path`.
type SeriesPathInput struct {
	ArchivePath *string `json:"archive_path"`
	Timeframe   *int64  `json:"timeframe"`
}

// MovieUpdateInput is the input of `update_movie`.
type MovieUpdateInput struct {
	Name                 string `json:"name"`
	Year                 string `json:"year"`
	Quality              string `json:"quality"`
	Language             string `json:"language"`
	Subtitle             string `json:"subtitle"`
	Exclude              string `json:"exclude"`
	LanguageRequirements string `json:"language_requirements"`
	SubtitleRequirements string `json:"subtitle_requirements"`
	Enabled              *bool  `json:"enabled"`
	TmdbId               string `json:"tmdb_id"`
	TvdbId               string `json:"tvdb_id"`
}

// MovieMetadataSearchInput is the input of `movie_metadata_search`.
type MovieMetadataSearchInput struct {
	Query  string `json:"query"`
	Source string `json:"source"`
}

// MovieMetadataApplyInput is the input of `apply_movie_metadata`.
type MovieMetadataApplyInput struct {
	Id     string `json:"id"`
	Source string `json:"source"`
}

// PruneInput is the input of `db_prune` / `db_prune_preview`.
type PruneInput struct {
	RetainCycles      *int64 `json:"retain_cycles"`
	ErrorAgeDays      *int64 `json:"error_age_days"`
	SeenRetentionDays *int64 `json:"seen_retention_days"`
	Preview           bool   `json:"preview"`
}

// DbActionInput is the input of `db_action`.
type DbActionInput struct {
	Action string `json:"action"`
}

// TestNotificationInput is the input of `test_notification`.
type TestNotificationInput struct {
	Message *string `json:"message"`
}

// SettingsPatch mirrors `SettingsPatch`, whose only field is the flattened inline field.
type SettingsPatch struct {
	Values map[string]any
}

// UnmarshalJSON captures every key of the flattened settings object.
func (s *SettingsPatch) UnmarshalJSON(data []byte) error {
	values := map[string]any{}
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	s.Values = values
	return nil
}

// SourceFiltersInput is the input of `save_source_filters`.
type SourceFiltersInput struct {
	Filters []SourceFilter `json:"filters"`
}

// PruneByIdsInput is the input of `db_prune_by_ids`.
type PruneByIdsInput struct {
	MovieSeen  []int64 `json:"movie_seen"`
	SeriesSeen []int64 `json:"series_seen"`
}

// ComicCheckLinksInput is the input of `comic_check_links`.
type ComicCheckLinksInput struct {
	Urls []string `json:"urls"`
}
