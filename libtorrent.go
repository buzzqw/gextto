package gextto

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// NativeTorrentStatus mirrors the bridge's gextto_lt_status ABI record.
// Text fields are converted to Go strings at the cgo boundary.
type NativeTorrentStatus struct {
	Hash            string
	Name            string
	SavePath        string
	Progress        float64
	State           int32
	Paused          int32
	DownloadRate    int32
	UploadRate      int32
	NumPeers        int32
	NumSeeds        int32
	DownloadLimit   int32
	UploadLimit     int32
	AllTimeUpload   int64
	AllTimeDownload int64
	SeedingSeconds  int64
	QueuePosition   int32
	HasMetadata     int32
	AutoManaged     int32
	TorrentVersion  int32
	TotalSize       int64
	TotalDone       int64
	// Rich status surfaced by libtorrent.
	Error             string
	CurrentTracker    string
	DownloadPayload   int32
	UploadPayload     int32
	NumComplete       int32
	NumIncomplete     int32
	NumConnections    int32
	ConnectCandidates int32
	FinishedSeconds   int64
	ActiveSeconds     int64
	IsSeeding         int32
	Sequential        int32
	SuperSeeding      int32
	UploadMode        int32
	ShareMode         int32
	DistributedCopies float32
}

// NativeTorrentEvent mirrors the bridge's gextto_lt_event ABI record.
type NativeTorrentEvent struct {
	Kind     int32
	Hash     string
	Name     string
	SavePath string
	Message  string
}

// NativePeer mirrors the bridge's gextto_lt_peer ABI record.
type NativePeer struct {
	Address       string
	Client        string
	DownloadRate  int32
	UploadRate    int32
	NumPieces     int32
	Seed          int32
	Flags         int32
	Progress      float32
	TotalUpload   int64
	TotalDownload int64
}

// Peer flag bits (mirror the bridge's gextto_lt_peer.flags).
const (
	PeerFlagSeed      = 1
	PeerFlagIncoming  = 2
	PeerFlagEncrypted = 4
	PeerFlagUtp       = 8
)

// NativeTracker mirrors the bridge's gextto_lt_tracker ABI record.
type NativeTracker struct {
	URL              string
	Message          string
	Tier             int32
	Status           int32
	Fails            int32
	NextAnnounce     int32
	ScrapeIncomplete int32
	ScrapeComplete   int32
	ScrapeDownloaded int32
	Verified         int32
}

// NativeFile mirrors the bridge's gextto_lt_file ABI record.
type NativeFile struct {
	Path       string
	Size       int64
	Downloaded int64
	Priority   int32
}

// AddOptions are the options applied when a torrent is added, based on
// qBittorrent's rich AddTorrentParams.
type AddOptions struct {
	// Paused adds the torrent in pause (no data transfer until resumed).
	Paused bool
	// Sequential enables per-torrent sequential download.
	Sequential bool
	// SeedMode assumes the data is already complete and skips the hash check.
	SeedMode bool
	// QueueTop moves the torrent to the top of the queue.
	QueueTop bool
	// FirstLast prioritises the first and last piece of every file.
	FirstLast bool
	// StopAtMetadata pauses automatically as soon as metadata is received.
	StopAtMetadata bool
	// Preallocate reserves the full size on disk up front (libtorrent
	// `storage_mode_allocate`). The caller resolves the default from the
	// `libtorrent_preallocate` setting.
	Preallocate bool
	// StopWhenReady pauses the torrent as soon as it has finished checking, so
	// metadata/files are present but no data is transferred until resumed.
	StopWhenReady bool
}

// Flags is the bitmask consumed by the native `*_ex` entry points.
func (o AddOptions) Flags() int32 {
	var flags int32
	if o.Paused {
		flags |= 1
	}
	if o.Sequential {
		flags |= 1 << 1
	}
	if o.SeedMode {
		flags |= 1 << 2
	}
	if o.QueueTop {
		flags |= 1 << 3
	}
	if o.Preallocate {
		flags |= 1 << 4
	}
	if o.StopWhenReady {
		flags |= 1 << 5
	}
	return flags
}

// TrackerEntry is one (tier, URL) pair passed to SetTrackers.
type TrackerEntry struct {
	Tier int32
	URL  string
}

// seedLimit is the per-torrent seed stop rule persisted in seed_limits.json.
type seedLimit struct {
	Ratio float64 `json:"ratio"`
	Days  int64   `json:"days"`
}

// LibtorrentClient owns the native session and the live torrent bookkeeping.
type LibtorrentClient struct {
	torrentsMu sync.RWMutex
	torrents   map[string]models.TorrentView

	stalledMu sync.RWMutex
	stalled   map[string]struct{}

	// firstLastPending holds torrents waiting for metadata to apply first/last
	// piece priorities.
	firstLastMu sync.RWMutex
	// firstLastDefault applies first/last to every new torrent.
	firstLastDefault atomic.Bool
	firstLastPending map[string]struct{}

	// stopAtMetadata holds torrents to pause as soon as metadata arrives.
	stopAtMetadataMu sync.RWMutex
	stopAtMetadata   map[string]struct{}

	// sessionMu serialises the native session handle between the hot read path
	// (List) and Shutdown: destroy must wait for the readers, and readers must
	// not call into a handle that was already destroyed (boost aborts the
	// process with "invalid session handle used").
	sessionMu sync.RWMutex
	// sessionUse counts the calls into the native session that are in flight
	// outside List (Add, Remove, MoveStorage, queue and limit changes...).
	// Shutdown first closes it to new calls, then waits for the running ones,
	// so no cgo call can reach a destroyed handle. A counter rather than an
	// RLock: these methods call each other, and a nested RLock deadlocks as
	// soon as Shutdown waits for the write lock.
	sessionUse     sync.Mutex
	sessionActive  int
	sessionClosing bool
	session        unsafe.Pointer
	configDB       string
	stateDir       string

	// listCache memoises the torrent snapshot for a very short TTL so several
	// reads in the same worker tick (and concurrent UI polls) share one
	// libtorrent status query.
	listMu    sync.Mutex
	listAt    time.Time
	listItems []models.TorrentView
	listValid bool

	removedMu sync.RWMutex
	removed   map[string]time.Time

	// recheckMu guards the persisted per-torrent re-check times.
	recheckMu sync.Mutex

	// DryRun is true when no native session was created.
	DryRun bool
}

// extraSettingsLines converts extra libtorrent settings (chiave=valore lines)
// into bridge commands: numbers as `i:`, booleans as `b:`, text as `s:`. Empty
// or `#` lines are ignored.
func extraSettingsLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		lowered := strings.ToLower(value)
		switch lowered {
		case "true", "false", "yes", "no", "on", "off":
			flag := 0
			if lowered == "true" || lowered == "yes" || lowered == "on" {
				flag = 1
			}
			lines = append(lines, fmt.Sprintf("b:%s=%d", key, flag))
		default:
			if number, err := strconv.ParseInt(value, 10, 64); err == nil {
				lines = append(lines, fmt.Sprintf("i:%s=%d", key, number))
			} else {
				lines = append(lines, fmt.Sprintf("s:%s=%s", key, value))
			}
		}
	}
	return lines
}

// LibtorrentVersion returns the libtorrent version string from the bridge.
func LibtorrentVersion() string {
	return cgoLtVersion()
}

// TrimMemory returns freed C++/libtorrent heap pages to the operating system.
// Called after transfers and cycles so the resident size falls back instead of
// staying at the download peak.
func TrimMemory() {
	cgoTrimMemory()
}

// FreeSpaceBytes returns the free space in bytes for the filesystem containing
// path, or nil when it cannot be determined.
func FreeSpaceBytes(path string) *uint64 {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return nil
	}
	available := saturatingMulUint64(stats.Bavail, uint64(stats.Frsize))
	return &available
}

func saturatingMulUint64(a, b uint64) uint64 {
	if a != 0 && b > math.MaxUint64/a {
		return math.MaxUint64
	}
	return a * b
}

// PathOnRamdisk is true when savePath lies inside the ramdisk directory
// (component-wise, following symlinks when possible).
func PathOnRamdisk(savePath, ramdisk string) bool {
	resolve := func(path string) string {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return path
	}
	save := resolve(savePath)
	ram := resolve(ramdisk)
	if save == ram {
		return true
	}
	prefix := ram
	if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
		prefix += string(os.PathSeparator)
	}
	return strings.HasPrefix(save, prefix)
}

// RamdiskFits decides whether a torrent of totalSize bytes fits on the RAM
// disk. uncommittedBytes is the space still to be written there by other
// in-flight torrents.
func RamdiskFits(thresholdBytes, marginBytes, freeBytes, uncommittedBytes, totalSize uint64) error {
	return RamdiskFitsRemaining(thresholdBytes, marginBytes, freeBytes, uncommittedBytes, totalSize, totalSize)
}

// RamdiskFitsRemaining is the space-aware variant: the size threshold still
// applies to the whole torrent (a file larger than the RAM disk must never be
// staged there), but the free-space requirement is checked against the bytes
// that still have to be written (remainingBytes), not the full size.
//
// This matters for a download that is already mostly on the RAM disk: with the
// full size it would be relocated mid-transfer (risking a corrupted or reset
// file) even though only a small tail is left. Relocating on the remaining
// bytes keeps a nearly complete torrent in place.
func RamdiskFitsRemaining(thresholdBytes, marginBytes, freeBytes, uncommittedBytes, totalSize, remainingBytes uint64) error {
	gib := 1024.0 * 1024.0 * 1024.0
	if thresholdBytes > 0 && totalSize > thresholdBytes {
		return fmt.Errorf("it is too big for the RAM disk (%.1f GB, limit %.1f GB)",
			float64(totalSize)/gib, float64(thresholdBytes)/gib)
	}
	effectiveFree := uint64(0)
	if freeBytes > uncommittedBytes {
		effectiveFree = freeBytes - uncommittedBytes
	}
	required := remainingBytes + marginBytes
	if required < remainingBytes {
		required = math.MaxUint64
	}
	if effectiveFree < required {
		return fmt.Errorf("there is not enough room left on the RAM disk (%.1f GB free, %.1f GB already reserved by other downloads, %.1f GB still to download)",
			float64(freeBytes)/gib, float64(uncommittedBytes)/gib, float64(remainingBytes)/gib)
	}
	return nil
}

func nativeState(state int32, paused bool) string {
	if paused {
		return "paused"
	}
	switch state {
	case 1:
		return "checking_files"
	case 2:
		return "downloading_metadata"
	case 3:
		return "downloading"
	case 4:
		return "finished"
	case 5:
		return "seeding"
	case 7:
		return "checking_resume_data"
	default:
		return "unknown"
	}
}

// preferredDownloadPath returns the engine's fallback download directory when no
// explicit path was chosen: the temporary disk, then the final directory. The
// RAM disk is deliberately not a candidate — an add whose size is still unknown
// must not start on the small tmpfs. A release known to fit gets the RAM disk as
// an explicit path (see automaticDownloadPath), and the metadata handler moves
// any other torrent whose real size turns out to fit.
func preferredDownloadPath(cfg *Config) string {
	var candidates []string
	if cfg.LibtorrentTempDir != nil {
		candidates = append(candidates, *cfg.LibtorrentTempDir)
	}
	candidates = append(candidates, cfg.LibtorrentDir)
	explicit := cfg.RamdiskMinFreeBytes()
	minimumFree := explicit
	if explicit == 0 {
		minimumFree = cfg.RamdiskMarginBytes()
	}
	for _, path := range candidates {
		if !pathIsDir(path) {
			continue
		}
		if free := FreeSpaceBytes(path); free != nil && *free >= minimumFree {
			return path
		}
	}
	return cfg.LibtorrentDir
}

// ramdiskDefaultDir returns the RAM disk when it is enabled, mounted and has
// enough free space for the configured margin. It is the explicit target for an
// acquisition whose size is known to fit; it is never the implicit engine
// default, so an unknown-size add cannot land there by accident.
func ramdiskDefaultDir(cfg *Config) (string, bool) {
	if !cfg.RamdiskEnabled() {
		return "", false
	}
	dir := cfg.RamdiskDir()
	if dir == nil || !pathIsDir(*dir) {
		return "", false
	}
	minimumFree := cfg.RamdiskMinFreeBytes()
	if minimumFree == 0 {
		minimumFree = cfg.RamdiskMarginBytes()
	}
	if free := FreeSpaceBytes(*dir); free != nil && *free >= minimumFree {
		return *dir, true
	}
	return "", false
}

// ReleaseFitsRamdisk reports whether a release whose size is already known can
// be admitted to the RAM disk. An unknown or non-positive size is treated as
// fitting here; the add path pairs this with RamdiskNeedsMetadata so an
// unknown-size release is staged off the tmpfs and decided at metadata time.
//
// This lets an automatic acquisition that already knows it is a multi-gigabyte
// season pack skip the tmpfs entirely instead of being moved out moments later.
func ReleaseFitsRamdisk(release *models.Release, cfg *Config) bool {
	if !cfg.RamdiskEnabled() {
		return true
	}
	if release == nil || release.SizeBytes <= 0 {
		return true
	}
	threshold := cfg.RamdiskThresholdBytes()
	if threshold == 0 {
		return true
	}
	return uint64(release.SizeBytes) <= threshold
}

// RamdiskNeedsMetadata reports whether an acquisition must be staged off the
// RAM disk until its metadata reveals the real size. The RAM disk is a small
// tmpfs: an unknown-size release must not be placed there only to be moved out
// moments later. The metadata handler then decides whether it fits
// (`ramdisk_admission`) and moves it onto the tmpfs if so.
//
// This is an engine-independent policy: it only looks at the release size and
// the configuration, never at the download daemon.
func RamdiskNeedsMetadata(release *models.Release, cfg *Config) bool {
	return cfg.RamdiskEnabled() && (release == nil || release.SizeBytes <= 0)
}

// ramdiskOverflowDir is where an oversized or unknown-size acquisition should be
// downloaded instead of the RAM disk: the temporary disk when it exists, the
// final directory otherwise. The returned path always exists as a directory.
func ramdiskOverflowDir(cfg *Config) (string, bool) {
	if cfg.LibtorrentTempDir != nil && pathIsDir(*cfg.LibtorrentTempDir) {
		return *cfg.LibtorrentTempDir, true
	}
	if pathIsDir(cfg.LibtorrentDir) {
		return cfg.LibtorrentDir, true
	}
	return "", false
}

// automaticDownloadPath returns the explicit download path for an automatic
// acquisition, or nil to let the engine choose. A release whose known size fits
// the RAM disk returns nil: the engine stages it on the tmpfs. A release that
// is known to be too big, or whose size is still unknown, returns the overflow
// dir, so a multi-gigabyte payload never lands on the small RAM disk. The
// unknown case is decided later, at metadata time.
//
// This policy is engine-independent: it only looks at the release and the
// configuration, never at the download daemon.
// capabilitiesReporter is implemented by torrent backends that declare what they
// can do (`TorrentEngine.Capabilities`).
type capabilitiesReporter interface {
	Capabilities() map[string]bool
}

// engineSupportsRamdisk reports whether the active backend can stage downloads
// on the RAM disk. qBittorrent reports `ramdisk: none`, so the whole RAM-disk
// policy is a no-op there and every download stays on the default disk. A
// backend that does not declare its capabilities keeps the historical behavior.
func engineSupportsRamdisk(torrents TorrentSession) bool {
	if reporter, ok := torrents.(capabilitiesReporter); ok {
		if supported, known := reporter.Capabilities()["ramdisk"]; known {
			return supported
		}
	}
	return true
}

// automaticDownloadPath returns the explicit download path for an automatic
// acquisition, or nil to let the engine choose. The RAM disk is chosen only when
// the backend supports it and the release's known size fits it. A release known
// to be too big, or whose size is still unknown, returns the overflow dir, so a
// multi-gigabyte payload never lands on the small RAM disk; the unknown case is
// decided later, at metadata time.
//
// This policy is engine-independent: it only looks at the release, the
// configuration and the backend's declared capability, never at a specific
// daemon.
func automaticDownloadPath(release *models.Release, cfg *Config, ramdiskSupported bool) *string {
	if dir, ok := DownloadDirFor(release, cfg); ok {
		return &dir
	}
	if !ramdiskSupported || !cfg.RamdiskEnabled() {
		// No RAM disk tier for this backend: the engine default (temp/final) is
		// the only safe choice.
		return nil
	}
	if !ReleaseFitsRamdisk(release, cfg) || RamdiskNeedsMetadata(release, cfg) {
		// Known to be too big, or still unknown: stage off the tmpfs. An
		// unknown size is decided at metadata time, which moves the torrent
		// onto the RAM disk if it fits.
		if dir, ok := ramdiskOverflowDir(cfg); ok {
			return &dir
		}
		return nil
	}
	// Known to fit: the RAM disk is an explicit choice (the engine default no
	// longer falls back to it), so ask for it when it has room.
	if dir, ok := ramdiskDefaultDir(cfg); ok {
		return &dir
	}
	return nil
}

// pathIsDir and fileExists resolve the path inside an os.Root so a crafted
// value cannot escape its directory through path separators.
func pathIsDir(path string) bool {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Stat(filepath.Base(path))
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Stat(filepath.Base(path))
	return err == nil && info.Mode().IsRegular()
}

// RamdiskUncommittedBytes is the number of bytes still to be written on the RAM
// disk by torrents other than excludeHash.
func (c *LibtorrentClient) RamdiskUncommittedBytes(ramdisk string, excludeHash string) uint64 {
	// Read the live session snapshot: a map of only the torrents added in this
	// process would ignore everything restored from fastresume after a restart.
	var total uint64
	for _, torrent := range c.List() {
		if strings.EqualFold(torrent.Hash, excludeHash) {
			continue
		}
		if !PathOnRamdisk(torrent.SavePath, ramdisk) {
			continue
		}
		remaining := torrent.TotalSize - torrent.TotalDone
		if remaining < 0 {
			remaining = 0
		}
		total += uint64(remaining)
	}
	return total
}

// SessionHealthy reports whether the embedded session holds a real, usable
// snapshot. It is false in dry-run or when no native session exists (libtorrent
// disabled or another backend selected). Callers must treat an empty snapshot
// from an unhealthy session as "unknown", never as "every torrent vanished":
// reconciliation against an empty list would otherwise wipe the tracked queue.
func (c *LibtorrentClient) SessionHealthy() bool {
	return c.session != nil && !c.DryRun
}

// NewLibtorrentClient creates the native session (unless dry-run) and restores
// any fastresume state.
func NewLibtorrentClient(cfg *Config) (*LibtorrentClient, error) {
	// When another torrent backend (gx-torrent or qBittorrent) is selected, the
	// embedded libtorrent session must NOT be created: two engines writing the
	// same files is the single most dangerous failure mode in the plans. The
	// client still exists (so libtorrent-only endpoints can report a clean
	// capability error) but has no native session and restores nothing.
	alternative := alternativeBackendActive(cfg)
	dryRun := cfg.DryRun || !cfg.LibtorrentEnabled || alternative
	if alternative && !cfg.DryRun && cfg.LibtorrentEnabled {
		// Expected when another engine is active: a debug detail, not an INFO
		// line next to the real "engine started" message.
		logging.Debug("embedded libtorrent not started: another torrent backend is active",
			"backend", TorrentBackendName(cfg))
	}
	var session unsafe.Pointer
	if !dryRun {
		connectionsLimit := int64(200)
		if value, ok := cfg.Settings["libtorrent_connections_limit"]; ok {
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
				connectionsLimit = parsed
			}
		}
		raw, errMessage := cgoLtCreate(
			cfg.Libtorrent.PortMin,
			cfg.Libtorrent.PortMax,
			limitBytesFromKib(cfg.Libtorrent.DownloadLimitKib),
			limitBytesFromKib(cfg.Libtorrent.UploadLimitKib),
			clampMin1(cfg.Libtorrent.ActiveDownloads),
			clampMin1(cfg.Libtorrent.ActiveSeeds),
			clampMin1(cfg.Libtorrent.ActiveLimit),
			clampMin1(connectionsLimit),
			boolToUint8(cfg.Libtorrent.Dht),
			boolToUint8(cfg.Libtorrent.Pex),
			boolToUint8(cfg.Libtorrent.Lsd),
			boolToUint8(cfg.Libtorrent.Upnp),
			boolToUint8(cfg.Libtorrent.Natpmp),
		)
		if raw == nil {
			return nil, fmt.Errorf("cannot create libtorrent session: %s", errMessage)
		}
		session = raw
	}
	client := &LibtorrentClient{
		torrents:         make(map[string]models.TorrentView),
		stalled:          make(map[string]struct{}),
		firstLastPending: make(map[string]struct{}),
		stopAtMetadata:   make(map[string]struct{}),
		removed:          make(map[string]time.Time),
		session:          session,
		configDB:         filepath.Join(cfg.DataDir, "gextto_config.db"),
		stateDir:         cfg.StateDir,
		DryRun:           dryRun,
	}
	if session != nil {
		runtime.SetFinalizer(client, func(c *LibtorrentClient) {
			c.sessionMu.Lock()
			defer c.sessionMu.Unlock()
			if c.session != nil {
				cgoLtDestroy(c.session)
				c.session = nil
			}
		})
	}
	if err := client.restore(cfg.StateDir); err != nil {
		return nil, err
	}
	if err := client.applyExtendedSettings(cfg); err != nil {
		return nil, err
	}
	client.RecheckRestoredAtZero()
	client.RecheckRestoredSuspicious()
	return client, nil
}

func boolToUint8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

func clampMin1(value int64) int32 {
	if value < 1 {
		return 1
	}
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(value)
}

func limitBytesFromKib(kib int64) int32 {
	if kib <= 0 {
		return 0
	}
	value := kib
	if value > math.MaxInt64/1024 {
		value = math.MaxInt64
	} else {
		value = value * 1024
	}
	if value > math.MaxInt32 {
		value = math.MaxInt32
	}
	return int32(value)
}

// RecheckRestoredSuspicious forces a re-check of torrents that fastresume
// restored as complete while their data on disk is missing or zero-filled. A
// relocated or externally truncated file would otherwise be treated as present
// and could be archived as a broken copy.
func (c *LibtorrentClient) RecheckRestoredSuspicious() int {
	if !c.enterSession() {
		return 0
	}
	defer c.exitSession()
	checked := 0
	for _, torrent := range c.List() {
		if !torrent.HasMetadata || torrent.Progress < 99.99 {
			continue
		}
		if c.recentlyRechecked(torrent.Hash, recheckGuardWindow) {
			continue
		}
		files, ok, err := c.Files(torrent.Hash)
		if err != nil || !ok {
			continue
		}
		suspicious := false
		for _, file := range files {
			if file.Size <= 0 {
				continue
			}
			path := filepath.Join(torrent.SavePath, filepath.FromSlash(file.Path))
			if fileSuspiciouslyEmpty(path) {
				suspicious = true
				logging.Warn("completed torrent has missing or zero-filled data; forcing a re-check",
					"hash", torrent.Hash, "name", torrent.Name, "file", file.Path)
				break
			}
		}
		if suspicious {
			if ok, err := c.ForceRecheck(torrent.Hash); err == nil && ok {
				checked++
			}
		}
	}
	if checked > 0 {
		logging.Info("re-checking completed torrents with inconsistent data on disk", "checked", checked)
	}
	return checked
}

// RecheckRestoredAtZero verifies paused torrents whose progress reads 0 but
// whose data is on disk, so the real completion percentage is shown again.
// recheckStatePath is where the last automatic re-check time per torrent is
// persisted, so a large pack is not re-read from the NAS at every restart.
func (c *LibtorrentClient) recheckStatePath() string {
	return filepath.Join(c.stateDir, "recheck_state.json")
}

func (c *LibtorrentClient) recheckTimes() map[string]int64 {
	c.recheckMu.Lock()
	defer c.recheckMu.Unlock()
	return c.recheckTimesLocked()
}

func (c *LibtorrentClient) recheckTimesLocked() map[string]int64 {
	times := map[string]int64{}
	if data, err := os.ReadFile(c.recheckStatePath()); err == nil {
		_ = json.Unmarshal(data, &times)
	}
	return times
}

func (c *LibtorrentClient) writeRecheckTimes(times map[string]int64) {
	data, err := json.Marshal(times)
	if err != nil {
		return
	}
	_ = os.MkdirAll(c.stateDir, 0o755)
	_ = utils.AtomicWrite(c.recheckStatePath(), data)
}

// recentlyRechecked reports whether hash was automatically re-checked within the
// given window.
func (c *LibtorrentClient) recentlyRechecked(hash string, window time.Duration) bool {
	at, ok := c.recheckTimes()[strings.ToLower(hash)]
	if !ok {
		return false
	}
	return time.Since(time.Unix(at, 0)) < window
}

// markRechecked records that hash was re-checked now.
func (c *LibtorrentClient) markRechecked(hash string) {
	c.recheckMu.Lock()
	defer c.recheckMu.Unlock()
	times := c.recheckTimesLocked()
	times[strings.ToLower(hash)] = time.Now().Unix()
	// Bound the file.
	if len(times) > 4096 {
		compact := map[string]int64{}
		cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
		for key, value := range times {
			if value >= cutoff {
				compact[key] = value
			}
		}
		times = compact
	}
	c.writeRecheckTimes(times)
}

// recheckGuardWindow is how long an automatic re-check of the same torrent is
// suppressed. Re-reading multi-gigabyte packs from the NAS is expensive and
// usually gives the same result; explicit manual checks always run.
const recheckGuardWindow = 12 * time.Hour

func (c *LibtorrentClient) RecheckRestoredAtZero() int {
	if !c.enterSession() {
		return 0
	}
	defer c.exitSession()
	checked := 0
	for _, torrent := range c.List() {
		state := strings.ToLower(torrent.State)
		paused := strings.Contains(state, "paus") || strings.Contains(state, "coda")
		_, statErr := os.Stat(torrent.SavePath)
		dataExists := statErr == nil
		if !torrent.HasMetadata || !paused || torrent.Progress >= 0.5 || !dataExists {
			continue
		}
		// Do not re-read a large pack from the NAS at every restart.
		if c.recentlyRechecked(torrent.Hash, recheckGuardWindow) {
			continue
		}
		ok, err := c.ForceRecheck(torrent.Hash)
		if err == nil && ok {
			checked++
		}
	}
	if checked > 0 {
		logging.Info(fmt.Sprintf("🔎 Checking the data of %s to recover %s progress",
			countLabel(checked, "paused download", "paused downloads"), plural(checked, "its", "their")))
	}
	return checked
}

// ApplySettings re-applies the extended libtorrent settings to the running
// session.
func (c *LibtorrentClient) ApplySettings(cfg *Config) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	if err := c.applyExtendedSettings(cfg); err != nil {
		return false, err
	}
	return true, nil
}

// LoadIPFilter loads a local ipfilter file into the live session, returning the
// number of rules.
func (c *LibtorrentClient) LoadIPFilter(path string) (int, error) {
	if !c.enterSession() {
		return 0, nil
	}
	defer c.exitSession()
	loaded, rules, errMessage := cgoLtLoadIPFilter(c.session, path)
	if loaded == 0 {
		return 0, fmt.Errorf("libtorrent ip filter not applied: %s", errMessage)
	}
	if rules < 0 {
		rules = 0
	}
	logging.Info("IP filter loaded", "rules", rules, "path", path)
	return int(rules), nil
}

// SetGlobalSpeedLimits applies global download/upload rate limits (KiB/s,
// 0 = unlimited) to the live session.
func (c *LibtorrentClient) SetGlobalSpeedLimits(downloadKib, uploadKib int64) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	dl := saturatingMulInt64(ltMaxInt64(downloadKib, 0), 1024)
	ul := saturatingMulInt64(ltMaxInt64(uploadKib, 0), 1024)
	payload := fmt.Sprintf("i:download_rate_limit=%d\ni:upload_rate_limit=%d", dl, ul)
	applied, errMessage := cgoLtApplySettings(c.session, payload)
	if applied == 0 {
		return false, fmt.Errorf("libtorrent speed limits not applied: %s", errMessage)
	}
	return true, nil
}

func ltMaxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func saturatingMulInt64(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > 0 && b > 0 {
		if a > math.MaxInt64/b {
			return math.MaxInt64
		}
		return a * b
	}
	if a < 0 && b < 0 {
		if a < math.MaxInt64/b {
			return math.MaxInt64
		}
		return a * b
	}
	if a < 0 {
		if a < math.MinInt64/b {
			return math.MinInt64
		}
	} else {
		if b < math.MinInt64/a {
			return math.MinInt64
		}
	}
	return a * b
}

func (c *LibtorrentClient) applyExtendedSettings(cfg *Config) error {
	if !c.enterSession() {
		return nil
	}
	defer c.exitSession()
	lt := &cfg.Libtorrent
	cacheLabel := "auto"
	if lt.CacheSize > 0 {
		cacheLabel = fmt.Sprintf("%d MB", lt.CacheSize*16/1024)
	}
	limitLabel := func(kib int64) string {
		if kib > 0 {
			return fmt.Sprintf("%d KB/s", kib)
		}
		return "unlimited"
	}
	logging.Debug(fmt.Sprintf("🧠 libtorrent memory: cache %s, max connections %d, I/O threads %d, base bandwidth %s down / %s up",
		cacheLabel, lt.ConnectionsLimit, lt.AioThreads, limitLabel(lt.DownloadLimitKib), limitLabel(lt.UploadLimitKib)))
	var lines []string
	addInt := func(key string, value int64) {
		if value >= 0 {
			lines = append(lines, fmt.Sprintf("i:%s=%d", key, value))
		}
	}
	addBool := func(key string, value bool) {
		flag := 0
		if value {
			flag = 1
		}
		lines = append(lines, fmt.Sprintf("b:%s=%d", key, flag))
	}
	addText := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			lines = append(lines, fmt.Sprintf("s:%s=%s", key, strings.TrimSpace(value)))
		}
	}
	addInt("connections_limit", lt.ConnectionsLimit)
	addInt("upload_slots_limit", lt.UploadSlotsLimit)
	addInt("half_open_limit", lt.HalfOpenLimit)
	addInt("alert_queue_size", lt.AlertQueueSize)
	addInt("max_connections_per_torrent", lt.MaxConnectionsPerTorrent)
	addInt("max_uploads_per_torrent", lt.MaxUploadsPerTorrent)
	addInt("aio_threads", lt.AioThreads)
	addInt("cache_size", lt.CacheSize)
	addInt("cache_expiry", lt.CacheExpiry)
	addInt("download_rate_limit", saturatingMulInt64(ltMaxInt64(lt.DownloadLimitKib, 0), 1024))
	addInt("upload_rate_limit", saturatingMulInt64(ltMaxInt64(lt.UploadLimitKib, 0), 1024))
	addInt("announce_interval", lt.AnnounceInterval)
	addInt("torrent_connect_boost", lt.TorrentConnectBoost)
	addBool("enable_utp", lt.Utp)
	addBool("prefer_rc4", lt.PreferRc4)
	addBool("announce_to_all_trackers", lt.AnnounceToAllTrackers)
	addBool("announce_to_all_tiers", lt.AnnounceToAllTiers)
	addBool("allow_multiple_connections_per_ip", lt.AllowMultipleConnectionsPerIp)
	addBool("apply_ip_filter", lt.ApplyIpFilter)
	addBool("dont_count_slow_torrents", lt.DontCountSlowTorrents)
	policy := lt.Encryption
	if policy < 0 {
		policy = 0
	}
	if policy > 2 {
		policy = 2
	}
	addInt("in_enc_policy", policy)
	addInt("out_enc_policy", policy)
	addText("ip_filter_path", lt.IpFilterPath)
	addText("listen_interfaces", effectiveListenInterfaces(lt))
	// Killswitch VPN: forza il traffico in uscita sulla scheda scelta.
	addText("outgoing_interfaces", lt.OutgoingInterface)
	addText("dht_bootstrap_nodes", lt.DhtBootstrapNodes)
	for _, line := range extraSettingsLines(cfg.Settings["libtorrent_extra_settings"]) {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil
	}
	payload := strings.Join(lines, "\n")
	applied, errMessage := cgoLtApplySettings(c.session, payload)
	if applied == 0 {
		logging.Warn("some libtorrent settings were not applied", "error", errMessage)
	}
	return nil
}

// Add adds a magnet using the default options.
func (c *LibtorrentClient) Add(magnet string, cfg *Config) (bool, error) {
	return c.AddWithPath(magnet, cfg, nil)
}

// AddWithPath adds a magnet, preferring preferredPath when it is an existing
// directory.
func (c *LibtorrentClient) AddWithPath(magnet string, cfg *Config, preferredPath *string) (bool, error) {
	return c.AddWithOptions(magnet, cfg, preferredPath, AddOptions{})
}

// resolveSavePath applies the `preferred_path.filter(is_dir).unwrap_or_else`.
func resolveSavePath(preferredPath *string, cfg *Config) string {
	if preferredPath != nil && pathIsDir(*preferredPath) {
		return *preferredPath
	}
	return preferredDownloadPath(cfg)
}

// AddWithOptions adds a magnet with explicit AddOptions.
func (c *LibtorrentClient) AddWithOptions(magnet string, cfg *Config, preferredPath *string, options AddOptions) (bool, error) {
	clean, ok := utils.SanitizeMagnet(magnet, nil)
	if !ok {
		return false, fmt.Errorf("invalid magnet")
	}
	hash, ok := utils.MagnetHash(clean)
	if !ok {
		return false, fmt.Errorf("missing info hash")
	}
	c.torrentsMu.RLock()
	_, exists := c.torrents[hash]
	c.torrentsMu.RUnlock()
	if exists {
		return false, nil
	}
	savePath := resolveSavePath(preferredPath, cfg)
	if c.enterSession() {
		defer c.exitSession()
		if err := os.MkdirAll(savePath, 0o755); err != nil {
			return false, err
		}
		added, errMessage := cgoLtAddEx(c.session, clean, savePath, options.Flags())
		if added == 0 {
			return false, fmt.Errorf("libtorrent add failed: %s", errMessage)
		}
		if err := c.applyStoredLimits(hash); err != nil {
			return false, err
		}
		c.applyStoredConnLimits(hash)
	} else if c.DryRun {
		logging.Info("dry-run: torrent accepted, not started")
	} else {
		return false, fmt.Errorf("libtorrent session is unavailable")
	}
	c.registerDeferredOptions(hash, clean, options)
	c.ClearStalled(hash)
	state := "downloading_metadata"
	if c.DryRun {
		state = "dry-run"
	}
	c.torrentsMu.Lock()
	if c.torrents == nil {
		c.torrents = make(map[string]models.TorrentView)
	}
	c.torrents[hash] = models.TorrentView{
		Hash:            hash,
		Name:            clean,
		Progress:        0.0,
		State:           state,
		DownloadRate:    0,
		UploadRate:      0,
		SavePath:        savePath,
		DownloadLimit:   -1,
		UploadLimit:     -1,
		AllTimeUpload:   0,
		AllTimeDownload: 0,
		SeedingSeconds:  0,
		QueuePosition:   -1,
		NumPeers:        0,
		NumSeeds:        0,
		SeedRatio:       -1.0,
		SeedDays:        -1,
		HasMetadata:     false,
		AutoManaged:     false,
		TorrentVersion:  "",
		TotalSize:       0,
		TotalDone:       0,
		Stalled:         false,
	}
	c.torrentsMu.Unlock()
	c.unmarkRemoved(hash)
	c.invalidateListCache()
	return true, nil
}

// AddTorrentFile adds a `.torrent` file, returning its infohash. A nil hash
// means no native session is available (the `Ok(None)` case).
func (c *LibtorrentClient) AddTorrentFile(torrentPath, savePath string) (*string, error) {
	if !c.enterSession() {
		return nil, nil
	}
	defer c.exitSession()
	added, hash, errMessage := cgoLtAddFile(c.session, torrentPath, savePath)
	if added == 0 {
		return nil, fmt.Errorf("libtorrent torrent-file add failed: %s", errMessage)
	}
	// Only the global defaults: this entry point takes no AddOptions.
	c.registerDeferredOptions(hash, "", AddOptions{})
	return &hash, nil
}

// AddTorrentFileEx is AddTorrentFile with AddOptions applied at add time.
func (c *LibtorrentClient) AddTorrentFileEx(torrentPath, savePath string, options AddOptions) (*string, error) {
	if !c.enterSession() {
		return nil, nil
	}
	defer c.exitSession()
	added, hash, errMessage := cgoLtAddFileEx(c.session, torrentPath, savePath, options.Flags())
	if added == 0 {
		return nil, fmt.Errorf("libtorrent torrent-file add failed: %s", errMessage)
	}
	// A `.torrent` add already has metadata: persist it (and copy it to the
	// configured folder) right away instead of waiting for an alert.
	if err := c.saveTorrentMetadata(hash, ""); err != nil {
		logging.Debug("cannot persist added torrent metadata", "hash", hash, "error", err)
	}
	c.unmarkRemoved(hash)
	c.invalidateListCache()
	// Only the global defaults: callers with AddOptions register them after.
	c.registerDeferredOptions(hash, "", AddOptions{})
	return &hash, nil
}

// AddFileWithPath adds a `.torrent` selecting the folder like AddWithPath.
func (c *LibtorrentClient) AddFileWithPath(torrentPath string, cfg *Config, preferredPath *string) (bool, error) {
	return c.AddFileWithOptions(torrentPath, cfg, preferredPath, AddOptions{})
}

// AddFileWithOptions adds a `.torrent` file with explicit AddOptions.
func (c *LibtorrentClient) AddFileWithOptions(torrentPath string, cfg *Config, preferredPath *string, options AddOptions) (bool, error) {
	savePath := resolveSavePath(preferredPath, cfg)
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return false, err
	}
	hash, err := c.AddTorrentFileEx(torrentPath, savePath, options)
	if err != nil {
		return false, err
	}
	if hash != nil {
		c.registerDeferredOptions(*hash, "", options)
		return true, nil
	}
	if c.DryRun {
		logging.Info("dry-run: torrent file accepted, not started")
		return true, nil
	}
	return false, nil
}

// registerDeferredOptions records options that can only be applied once
// metadata is available.
func (c *LibtorrentClient) registerDeferredOptions(hash, name string, options AddOptions) {
	normalized := strings.ToLower(hash)
	if options.FirstLast || c.firstLastDefault.Load() {
		c.firstLastMu.Lock()
		c.firstLastPending[normalized] = struct{}{}
		c.firstLastMu.Unlock()
	}
	if options.StopAtMetadata {
		c.stopAtMetadataMu.Lock()
		c.stopAtMetadata[normalized] = struct{}{}
		c.stopAtMetadataMu.Unlock()
	}
	if options.Sequential {
		if _, err := c.SetTorrentSequential(normalized, true); err != nil {
			logging.Debug("could not apply sequential option", "error", err, "name", name)
		}
	}
}

// EnforceDeferredOptions applies pending first/last priorities and
// metadata-only pauses.
func (c *LibtorrentClient) EnforceDeferredOptions(torrents []models.TorrentView) {
	c.firstLastMu.RLock()
	pendingFirstLast := make(map[string]struct{}, len(c.firstLastPending))
	for hash := range c.firstLastPending {
		pendingFirstLast[hash] = struct{}{}
	}
	c.firstLastMu.RUnlock()
	c.stopAtMetadataMu.RLock()
	pendingStop := make(map[string]struct{}, len(c.stopAtMetadata))
	for hash := range c.stopAtMetadata {
		pendingStop[hash] = struct{}{}
	}
	c.stopAtMetadataMu.RUnlock()
	if len(pendingFirstLast) == 0 && len(pendingStop) == 0 {
		return
	}
	for _, torrent := range torrents {
		if !torrent.HasMetadata {
			continue
		}
		if _, ok := pendingFirstLast[torrent.Hash]; ok {
			c.firstLastMu.Lock()
			delete(c.firstLastPending, torrent.Hash)
			c.firstLastMu.Unlock()
			if _, err := c.SetFirstLast(torrent.Hash, true); err != nil {
				logging.Debug("first/last piece priorities unavailable", "hash", torrent.Hash, "error", err)
			} else {
				logging.Debug("first/last piece priorities applied", "hash", torrent.Hash)
			}
		}
		if _, ok := pendingStop[torrent.Hash]; ok {
			c.stopAtMetadataMu.Lock()
			delete(c.stopAtMetadata, torrent.Hash)
			c.stopAtMetadataMu.Unlock()
			if torrent.State != "paused" {
				if _, err := c.Pause(torrent.Hash); err != nil {
					logging.Debug("metadata-only pause failed", "hash", torrent.Hash, "error", err)
				} else {
					logging.Info("⏸️ metadata received, torrent paused (metadata-only add)", "hash", torrent.Hash, "name", torrent.Name)
				}
			}
		}
	}
}

// SetTorrentSequential toggles per-torrent sequential download.
func (c *LibtorrentClient) SetTorrentSequential(hash string, enabled bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtSetTorrentSequential(c.session, strings.ToLower(hash), boolToInt32(enabled))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent sequential failed: %s", errMessage)
	}
	return true, nil
}

// QueueTop moves a torrent to the top of the queue.
func (c *LibtorrentClient) QueueTop(hash string) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtQueueTop(c.session, strings.ToLower(hash))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent queue top failed: %s", errMessage)
	}
	return true, nil
}

// SetFirstLastDefault implements TorrentEngine.
func (c *LibtorrentClient) SetFirstLastDefault(enabled bool) {
	if c != nil {
		c.firstLastDefault.Store(enabled)
	}
}

// SetFirstLast prioritises the first and last piece of every file.
func (c *LibtorrentClient) SetFirstLast(hash string, enabled bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtSetFirstLast(c.session, strings.ToLower(hash), boolToInt32(enabled))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent first/last failed: %s", errMessage)
	}
	return true, nil
}

// SetFilePriorities sets the per-file download priority (0 skipped … 7 max).
func (c *LibtorrentClient) SetFilePriorities(hash string, priorities []int32) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtSetFilePriorities(c.session, strings.ToLower(hash), priorities)
	if ok == 0 {
		return false, fmt.Errorf("libtorrent file priorities failed: %s", errMessage)
	}
	return true, nil
}

// WebSeeds adds or removes HTTP/FTP web seeds (one URL per line in urls).
func (c *LibtorrentClient) WebSeeds(hash, urls string, remove bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtAddWebSeeds(c.session, strings.ToLower(hash), urls, boolToInt32(remove))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent web seed failed: %s", errMessage)
	}
	return true, nil
}

// SetTrackers replaces the tracker list.
func (c *LibtorrentClient) SetTrackers(hash string, trackers []TrackerEntry) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	parts := make([]string, 0, len(trackers))
	for _, tracker := range trackers {
		parts = append(parts, fmt.Sprintf("%d|%s", tracker.Tier, strings.TrimSpace(tracker.URL)))
	}
	payload := strings.Join(parts, "\n")
	ok, errMessage := cgoLtSetTrackers(c.session, strings.ToLower(hash), payload)
	if ok == 0 {
		return false, fmt.Errorf("libtorrent tracker update failed: %s", errMessage)
	}
	return true, nil
}

// SetSuperSeeding enables/disables libtorrent super seeding on a torrent.
func (c *LibtorrentClient) SetSuperSeeding(hash string, enabled bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtSetSuperSeeding(c.session, strings.ToLower(hash), boolToInt32(enabled))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent super seeding failed: %s", errMessage)
	}
	return true, nil
}

// infoHashPattern matches a v1 (SHA-1, 40 hex) or v2 (SHA-256, 64 hex) infohash.
var infoHashPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// stateFile returns `<stateDir>/<hash><ext>`. The hash comes from API callers,
// so anything that is not a lowercase hex infohash is refused: a value such as
// "../x" must not reach a path outside the state directory.
func (c *LibtorrentClient) stateFile(hash, ext string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(hash))
	if !infoHashPattern.MatchString(normalized) {
		return "", false
	}
	return filepath.Join(c.stateDir, normalized+ext), true
}

// TorrentFilePath returns the path of the `.torrent` metadata saved on the
// metadata-received event, if any.
func (c *LibtorrentClient) TorrentFilePath(hash string) (string, bool) {
	path, ok := c.stateFile(hash, ".torrent")
	if ok && fileExists(path) {
		return path, true
	}
	return "", false
}

// AddTorrentFileWithPath is AddFileWithPath returning the infohash.
func (c *LibtorrentClient) AddTorrentFileWithPath(torrentPath string, cfg *Config, preferredPath *string) (*string, error) {
	return c.AddTorrentFileWithOptions(torrentPath, cfg, preferredPath, AddOptions{})
}

// AddTorrentFileWithOptions is AddTorrentFileWithPath with AddOptions.
func (c *LibtorrentClient) AddTorrentFileWithOptions(torrentPath string, cfg *Config, preferredPath *string, options AddOptions) (*string, error) {
	savePath := resolveSavePath(preferredPath, cfg)
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return nil, err
	}
	hash, err := c.AddTorrentFileEx(torrentPath, savePath, options)
	if err != nil {
		return nil, err
	}
	if hash != nil {
		c.registerDeferredOptions(*hash, "", options)
	}
	return hash, nil
}

// listCacheTTL bounds how long a torrent snapshot is reused. The background
// worker polls every 750 ms and reads the list several times per tick, and the
// UI polls every few seconds: a short cache collapses those reads into one
// libtorrent status query (and one per-torrent seed-limit DB read) without
// making the data visibly stale.
const listCacheTTL = 500 * time.Millisecond

// List returns the live torrent statuses. Repeated calls within listCacheTTL
// reuse one snapshot to avoid redundant libtorrent status marshalling.
func (c *LibtorrentClient) List() []models.TorrentView {
	c.listMu.Lock()
	if c.listValid && time.Since(c.listAt) < listCacheTTL {
		out := make([]models.TorrentView, len(c.listItems))
		copy(out, c.listItems)
		c.listMu.Unlock()
		return out
	}
	c.listMu.Unlock()

	result := c.listUncached()

	c.listMu.Lock()
	c.listItems = result
	c.listAt = time.Now()
	c.listValid = true
	c.listMu.Unlock()

	out := make([]models.TorrentView, len(result))
	copy(out, result)
	return out
}

func (c *LibtorrentClient) markRemoved(hash string) {
	normalized := strings.ToLower(strings.TrimSpace(hash))
	if normalized == "" {
		return
	}
	c.removedMu.Lock()
	if c.removed == nil {
		c.removed = make(map[string]time.Time)
	}
	c.removed[normalized] = time.Now()
	c.removedMu.Unlock()

	c.listMu.Lock()
	c.listValid = false
	if len(c.listItems) > 0 {
		filtered := make([]models.TorrentView, 0, len(c.listItems))
		for _, item := range c.listItems {
			if strings.ToLower(strings.TrimSpace(item.Hash)) != normalized {
				filtered = append(filtered, item)
			}
		}
		c.listItems = filtered
	}
	c.listMu.Unlock()
}

func (c *LibtorrentClient) isRecentlyRemoved(hash string) bool {
	normalized := strings.ToLower(strings.TrimSpace(hash))
	if normalized == "" {
		return false
	}
	c.removedMu.RLock()
	if c.removed == nil {
		c.removedMu.RUnlock()
		return false
	}
	at, ok := c.removed[normalized]
	c.removedMu.RUnlock()
	if !ok {
		return false
	}
	if time.Since(at) > 30*time.Second {
		c.removedMu.Lock()
		if c.removed != nil {
			delete(c.removed, normalized)
		}
		c.removedMu.Unlock()
		return false
	}
	return true
}

func (c *LibtorrentClient) unmarkRemoved(hash string) {
	normalized := strings.ToLower(strings.TrimSpace(hash))
	if normalized == "" {
		return
	}
	c.removedMu.Lock()
	if c.removed != nil {
		delete(c.removed, normalized)
	}
	c.removedMu.Unlock()
}

func (c *LibtorrentClient) invalidateListCache() {
	c.listMu.Lock()
	c.listValid = false
	c.listMu.Unlock()
}

// listUncached builds a fresh torrent snapshot.
func (c *LibtorrentClient) listUncached() []models.TorrentView {
	c.sessionMu.RLock()
	session := c.session
	if session == nil {
		c.sessionMu.RUnlock()
		c.torrentsMu.RLock()
		result := make([]models.TorrentView, 0, len(c.torrents))
		for _, torrent := range c.torrents {
			if c.isRecentlyRemoved(torrent.Hash) {
				continue
			}
			result = append(result, torrent)
		}
		c.torrentsMu.RUnlock()
		sort.Slice(result, func(i, j int) bool { return result[i].Hash < result[j].Hash })
		return result
	}
	statuses := cgoLtStatuses(session)
	c.sessionMu.RUnlock()
	limits, err := c.seedLimits()
	if err != nil {
		logging.Warn("cannot read per-torrent seed limits", "error", err)
		limits = map[string]seedLimit{}
	}
	result := make([]models.TorrentView, 0, len(statuses))
	for _, status := range statuses {
		hash := strings.ToLower(status.Hash)
		if c.isRecentlyRemoved(hash) {
			continue
		}
		c.stalledMu.RLock()
		_, stalled := c.stalled[hash]
		c.stalledMu.RUnlock()
		limit, hasLimit := limits[hash]
		state := nativeState(status.State, status.Paused != 0)
		if stalled {
			state = "stalled"
		}
		progress := math.Max(0.0, math.Min(100.0, status.Progress))
		// Show the payload rate only: the total rate includes protocol overhead
		// (handshakes, keepalives), so an idle or stalled torrent would otherwise
		// display a small "fake" download speed. Total rates stay available in
		// `download_rate_total`/`upload_rate_total` for diagnostics.
		downloadRate := uint64(0)
		if status.DownloadPayload > 0 {
			downloadRate = uint64(status.DownloadPayload)
		}
		uploadRate := uint64(0)
		if status.UploadPayload > 0 {
			uploadRate = uint64(status.UploadPayload)
		}
		seedRatio := -1.0
		seedDays := int64(-1)
		if hasLimit {
			seedRatio = limit.Ratio
			seedDays = limit.Days
		}
		torrentVersion := ""
		switch status.TorrentVersion {
		case 1:
			torrentVersion = "v1"
		case 2:
			torrentVersion = "v2"
		case 3:
			torrentVersion = "hybrid"
		}
		totalSize := status.TotalSize
		if totalSize < 0 {
			totalSize = 0
		}
		totalDone := status.TotalDone
		if totalDone < 0 {
			totalDone = 0
		}
		view := models.TorrentView{
			Hash:              hash,
			Name:              status.Name,
			SavePath:          status.SavePath,
			Progress:          progress,
			State:             state,
			DownloadRate:      downloadRate,
			UploadRate:        uploadRate,
			DownloadRateTotal: uint64(maxInt64(0, int64(status.DownloadRate))),
			UploadRateTotal:   uint64(maxInt64(0, int64(status.UploadRate))),
			DownloadLimit:     int64(status.DownloadLimit),
			UploadLimit:       int64(status.UploadLimit),
			AllTimeUpload:     status.AllTimeUpload,
			AllTimeDownload:   status.AllTimeDownload,
			SeedingSeconds:    status.SeedingSeconds,
			QueuePosition:     int(status.QueuePosition),
			NumPeers:          int(status.NumPeers),
			NumSeeds:          int(status.NumSeeds),
			SeedRatio:         seedRatio,
			SeedDays:          seedDays,
			HasMetadata:       status.HasMetadata != 0,
			AutoManaged:       status.AutoManaged != 0,
			TorrentVersion:    torrentVersion,
			TotalSize:         totalSize,
			TotalDone:         totalDone,
			Stalled:           stalled,

			Error:             status.Error,
			CurrentTracker:    status.CurrentTracker,
			NumComplete:       int(status.NumComplete),
			NumIncomplete:     int(status.NumIncomplete),
			NumConnections:    int(status.NumConnections),
			ConnectCandidates: int(status.ConnectCandidates),
			FinishedSeconds:   status.FinishedSeconds,
			ActiveSeconds:     status.ActiveSeconds,
			IsSeeding:         status.IsSeeding != 0,
			Sequential:        status.Sequential != 0,
			SuperSeeding:      status.SuperSeeding != 0,
			UploadMode:        status.UploadMode != 0,
			ShareMode:         status.ShareMode != 0,
			DistributedCopies: float64(status.DistributedCopies),
		}
		view.Diagnosis, _, _ = DiagnoseTorrent(&view)
		result = append(result, view)
	}
	return result
}

// DiagnoseTorrent explains a torrent's situation as a short machine code plus a
// human reason and hint. It makes extreme cases explicit (no seeders, a
// leechers-only swarm, missing metadata, dead trackers) in the logs and in the
// GET /api/torrents/{hash}/why endpoint.
func DiagnoseTorrent(torrent *models.TorrentView) (code, reason, hint string) {
	switch {
	case torrent.Progress >= 100.0 || torrent.IsSeeding:
		return "seeding", "Download completo.", "Il torrent sta condividendo secondo la policy di seed."
	case !torrent.HasMetadata:
		return "metadata", "In attesa dei metadati.", "Nessun peer ha ancora fornito l'elenco dei file: serve almeno un peer (anche leecher) o un web seed."
	case strings.TrimSpace(torrent.Error) != "":
		return "error", "libtorrent ha riportato un errore.", torrent.Error
	// A stalled torrent is paused, so it has no peers: check the stall reasons
	// before the "no peers" case.
	case torrent.Stalled && torrent.NumSeeds == 0 && torrent.NumComplete <= 0:
		return "dead_swarm", "Nessun seeder nello swarm.", "I peer connessi sono leecher: attendi un seeder o scegli un'altra release. Il torrent resta in retry senza essere rimosso."
	case torrent.Stalled && torrent.NumSeeds == 0:
		return "no_connected_seed", "Seeder presenti nello swarm ma non connessi.", "Controlla tracker, porta in ascolto e firewall/NAT."
	case torrent.Stalled:
		return "stalled", "Nessun progresso nonostante i peer.", "Verrà ritentato automaticamente."
	case torrent.NumConnections == 0 && torrent.NumPeers == 0:
		return "no_peers", "Nessun peer connesso.", "In attesa che tracker/DHT trovino peer."
	case torrent.NumSeeds == 0 && torrent.NumComplete <= 0 && torrent.DownloadRate == 0:
		// Peers are connected but nobody can serve the missing pieces: no
		// connected seeder and no known swarm seeder.
		return "dead_swarm", "Nessun seeder nello swarm.", "I peer connessi sono leecher: attendi un seeder o scegli un'altra release. Il torrent resta in retry senza essere rimosso."
	default:
		return "downloading", "Download in corso.", ""
	}
}

// PollEvents drains the pending lifecycle events.
func (c *LibtorrentClient) PollEvents() []models.TorrentEvent {
	if !c.enterSession() {
		return nil
	}
	defer c.exitSession()
	native := cgoLtEvents(c.session)
	result := make([]models.TorrentEvent, 0, len(native))
	for _, event := range native {
		hash := strings.ToLower(event.Hash)
		name := event.Name
		kind := "unknown"
		switch event.Kind {
		case 1:
			kind = "metadata_received"
		case 2:
			kind = "torrent_finished"
		case 3:
			kind = "storage_moved"
		case 4:
			kind = "storage_move_failed"
		case 5:
			kind = "torrent_checked"
		case 6:
			kind = "torrent_error"
		case 7:
			kind = "file_error"
		case 8:
			kind = "tracker_error"
		case 9:
			kind = "metadata_failed"
		case 10:
			kind = "hash_failed"
		case 11:
			kind = "resume_save_failed"
		case 12:
			kind = "torrent_removed"
			c.unmarkRemoved(hash)
			c.invalidateListCache()
		case 13:
			kind = "portmap_error"
		case 14:
			kind = "session_error"
		}
		if kind == "metadata_received" {
			if err := c.saveTorrentMetadata(hash, name); err != nil {
				logging.Warn("cannot persist torrent metadata", "hash", hash, "name", name, "error", err)
			}
		}
		result = append(result, models.TorrentEvent{
			Kind:     kind,
			Hash:     hash,
			Name:     name,
			SavePath: event.SavePath,
			Message:  event.Message,
		})
	}
	return result
}

func (c *LibtorrentClient) saveTorrentMetadata(hash, displayName string) error {
	if !c.enterSession() {
		return nil
	}
	defer c.exitSession()
	if err := os.MkdirAll(c.stateDir, 0o755); err != nil {
		return err
	}
	normalized := strings.ToLower(hash)
	path, ok := c.stateFile(normalized, ".torrent")
	if !ok {
		return fmt.Errorf("invalid torrent hash %q", hash)
	}
	saved, errMessage := cgoLtSaveTorrent(c.session, normalized, path)
	if saved == 0 {
		return fmt.Errorf("libtorrent metadata save failed: %s", errMessage)
	}
	c.copyTorrentFileNamed(normalized, path, displayName)
	return nil
}

// torrentCopyDir reads the configured folder where started torrents' .torrent
// files are copied (empty disables it). It reads the setting live so a change
// applies without a restart.
func (c *LibtorrentClient) torrentCopyDir() string {
	if c.configDB == "" || !fileExists(c.configDB) {
		return ""
	}
	conn, err := OpenConfigDB(c.configDB)
	if err != nil {
		return ""
	}
	defer conn.Close()
	var value string
	if err := conn.QueryRow("SELECT value FROM settings WHERE key='libtorrent_torrent_copy_dir'").Scan(&value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// torrentDisplayName returns the cached display name for a hash, if any.
func (c *LibtorrentClient) torrentDisplayName(hash string) string {
	c.torrentsMu.RLock()
	defer c.torrentsMu.RUnlock()
	if torrent, ok := c.torrents[strings.ToLower(hash)]; ok {
		return torrent.Name
	}
	return ""
}

// safeTorrentCopyName turns a torrent display name into a safe file name for
// the `.torrent` copy folder. A magnet URI (seen before metadata arrives) or an
// empty name falls back to the infohash, and overlong names are truncated so
// the resulting file name cannot exceed the filesystem limit.
func safeTorrentCopyName(name, hash string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	cleaned := strings.TrimSpace(name)
	lowered := strings.ToLower(cleaned)
	// A torrent added from a magnet can report the magnet URI as its name until
	// metadata arrives (or even after sanitisation: "magnetxt=urnbtih..."). Use
	// the infohash instead of a giant, invalid file name.
	if cleaned == "" ||
		strings.HasPrefix(lowered, "magnet:") ||
		strings.Contains(lowered, "urn:btih") ||
		strings.Contains(lowered, "urnbtih") {
		return hash
	}
	cleaned = sanitizeInvalid(cleaned)
	if cleaned == "" {
		return hash
	}
	const maxBytes = 150
	if len(cleaned) > maxBytes {
		cleaned = cleaned[:maxBytes]
		for len(cleaned) > 0 && !utf8.ValidString(cleaned) {
			cleaned = cleaned[:len(cleaned)-1]
		}
		suffix := hash
		if len(suffix) > 8 {
			suffix = suffix[:8]
		}
		cleaned = cleaned + "-" + suffix
	}
	return cleaned
}

// copyTorrentFile copies the saved `.torrent` of a started torrent into the
// configured folder so external tools can reuse it.
func (c *LibtorrentClient) copyTorrentFile(hash, source string) {
	c.copyTorrentFileNamed(hash, source, "")
}

// copyTorrentFileNamed is the same operation with the display name captured
// from the metadata event. That name is authoritative at the moment metadata
// arrives; the cached/list snapshots can still contain the original magnet
// name for a short time.
func (c *LibtorrentClient) copyTorrentFileNamed(hash, source, displayName string) {
	dir := c.torrentCopyDir()
	if dir == "" {
		return
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = strings.TrimSpace(c.torrentDisplayName(hash))
	}
	if name == "" {
		// The cache may not be populated yet right after an add: read the live
		// name from the session so the copied file is recognisable.
		for _, torrent := range c.List() {
			if strings.EqualFold(torrent.Hash, hash) {
				name = torrent.Name
				break
			}
		}
	}
	name = safeTorrentCopyName(name, hash)
	target := filepath.Join(dir, name+".torrent")
	if err := copyFileAtomically(source, target); err != nil {
		logging.Warn("cannot copy torrent file", "hash", hash, "target", target, "error", err)
		return
	}
	logging.Debug("torrent file copied", "hash", hash, "path", target)
}

// RequestResumeSave asks libtorrent to save the resume data of the torrents
// changed since the last save. The files are written while events are polled,
// so a crash or a forced kill no longer loses the downloads added or advanced
// since the start (before, resume data was saved only on a clean shutdown).
// Returns the number of torrents asked to save, or -1 on error.
func (c *LibtorrentClient) RequestResumeSave() int {
	if c.DryRun || c.stateDir == "" || !c.enterSession() {
		return 0
	}
	defer c.exitSession()
	return int(cgoLtRequestResumeSave(c.session, c.stateDir))
}

// PromoteMetadata promotes metadata-only torrents out of the queue.
func (c *LibtorrentClient) PromoteMetadata() {
	if c.enterSession() {
		defer c.exitSession()
		cgoLtPromoteMetadata(c.session)
	}
}

// EnsureAutoManaged re-arms the queue's auto-management on torrents that have
// metadata and are not paused.
func (c *LibtorrentClient) EnsureAutoManaged() int {
	if c.enterSession() {
		defer c.exitSession()
		return cgoLtEnsureAutoManaged(c.session)
	}
	return 0
}

// AdjustQueue applies the dynamic queue policy for the effective global
// download limit.
func (c *LibtorrentClient) AdjustQueue(cfg *Config, effectiveDownloadKib int64) {
	if !c.enterSession() {
		return
	}
	defer c.exitSession()
	toBytes := func(kib int64) int32 {
		value := saturatingMulInt64(kib, 1024)
		if value < 0 {
			value = 0
		}
		if value > math.MaxInt32 {
			value = math.MaxInt32
		}
		return int32(value)
	}
	enabled := int32(0)
	if cfg.Libtorrent.DynamicQueue {
		enabled = 1
	}
	cgoLtAdjustQueue(c.session,
		enabled,
		clampMin1(cfg.Libtorrent.ActiveDownloads),
		clampMin1(cfg.Libtorrent.DynamicQueueMin),
		clampMin1(cfg.Libtorrent.DynamicQueueMax),
		clampMin1(cfg.Libtorrent.ActiveSeeds),
		clampMin1(cfg.Libtorrent.ActiveLimit),
		toBytes(effectiveDownloadKib),
	)
}

// MoveStorage moves a torrent's data to destination.
func (c *LibtorrentClient) MoveStorage(hash, destination string) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	moved, errMessage := cgoLtMoveStorage(c.session, strings.ToLower(hash), destination)
	if moved == 0 {
		return false, fmt.Errorf("libtorrent move storage failed: %s", errMessage)
	}
	return true, nil
}

// AssociateStorage changes a torrent's save path without moving files and
// re-checks it there. Used when the destination already contains the data.
func (c *LibtorrentClient) AssociateStorage(hash, destination string) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, message := cgoLtAssociateStorage(c.session, strings.ToLower(hash), destination)
	if ok == 0 {
		return false, fmt.Errorf("libtorrent associate storage failed: %s", message)
	}
	// reset_save_path emits no storage_moved alert: save the new location now
	// so an unclean stop cannot bring the torrent back to its old path.
	cgoLtRequestResumeSave(c.session, c.stateDir)
	return true, nil
}

// Peers returns the peers of a torrent. The boolean is false when no session is
// available (the `Ok(None)` case).
func (c *LibtorrentClient) Peers(hash string) ([]models.PeerView, bool, error) {
	if !c.enterSession() {
		return nil, false, nil
	}
	defer c.exitSession()
	count, native, errMessage := cgoLtPeers(c.session, strings.ToLower(hash))
	if count == 0 && errMessage != "" {
		return nil, false, fmt.Errorf("libtorrent peer query failed: %s", errMessage)
	}
	result := make([]models.PeerView, 0, len(native))
	for _, peer := range native {
		downloadRate := uint64(0)
		if peer.DownloadRate > 0 {
			downloadRate = uint64(peer.DownloadRate)
		}
		uploadRate := uint64(0)
		if peer.UploadRate > 0 {
			uploadRate = uint64(peer.UploadRate)
		}
		result = append(result, models.PeerView{
			Address:       peer.Address,
			Client:        peer.Client,
			DownloadRate:  downloadRate,
			UploadRate:    uploadRate,
			Pieces:        int(peer.NumPieces),
			Seed:          peer.Seed != 0,
			Progress:      float64(peer.Progress),
			TotalUpload:   peer.TotalUpload,
			TotalDownload: peer.TotalDownload,
			Incoming:      peer.Flags&PeerFlagIncoming != 0,
			Encrypted:     peer.Flags&PeerFlagEncrypted != 0,
			Utp:           peer.Flags&PeerFlagUtp != 0,
		})
	}
	return result, true, nil
}

// Trackers returns the trackers of a torrent.
func (c *LibtorrentClient) Trackers(hash string) ([]models.TrackerView, bool, error) {
	if !c.enterSession() {
		return nil, false, nil
	}
	defer c.exitSession()
	count, native, errMessage := cgoLtTrackers(c.session, strings.ToLower(hash))
	if count == 0 && errMessage != "" {
		return nil, false, fmt.Errorf("libtorrent tracker query failed: %s", errMessage)
	}
	result := make([]models.TrackerView, 0, len(native))
	for _, tracker := range native {
		result = append(result, models.TrackerView{
			URL:              tracker.URL,
			Tier:             int(tracker.Tier),
			Message:          tracker.Message,
			Fails:            int(tracker.Fails),
			NextAnnounce:     int(tracker.NextAnnounce),
			Verified:         tracker.Verified != 0,
			ScrapeIncomplete: int(tracker.ScrapeIncomplete),
			ScrapeComplete:   int(tracker.ScrapeComplete),
			ScrapeDownloaded: int(tracker.ScrapeDownloaded),
		})
	}
	return result, true, nil
}

// Files returns the files of a torrent.
func (c *LibtorrentClient) Files(hash string) ([]models.FileView, bool, error) {
	if !c.enterSession() {
		return nil, false, nil
	}
	defer c.exitSession()
	count, native, errMessage := cgoLtFiles(c.session, strings.ToLower(hash))
	if count == 0 && errMessage != "" {
		return nil, false, fmt.Errorf("libtorrent file query failed: %s", errMessage)
	}
	result := make([]models.FileView, 0, len(native))
	for _, file := range native {
		result = append(result, models.FileView{
			Path:       file.Path,
			Size:       file.Size,
			Downloaded: file.Downloaded,
			Priority:   int(file.Priority),
		})
	}
	return result, true, nil
}

// Pause pauses a torrent.
func (c *LibtorrentClient) Pause(hash string) (bool, error) {
	return c.controlPaused(hash, true)
}

// Resume resumes a torrent and clears its stalled marker.
func (c *LibtorrentClient) Resume(hash string) (bool, error) {
	result, err := c.controlPaused(hash, false)
	if err != nil {
		return false, err
	}
	c.ClearStalled(hash)
	return result, nil
}

func (c *LibtorrentClient) controlPaused(hash string, paused bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtSetPaused(c.session, strings.ToLower(hash), boolToInt32(paused))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent action failed: %s", errMessage)
	}
	c.invalidateListCache()
	return true, nil
}

// MarkStalled keeps the local marker and the native state in sync.
func (c *LibtorrentClient) MarkStalled(hash string) (bool, error) {
	paused, err := c.Pause(hash)
	if err != nil {
		return false, err
	}
	if paused {
		c.stalledMu.Lock()
		c.stalled[strings.ToLower(hash)] = struct{}{}
		c.stalledMu.Unlock()
	}
	return paused, nil
}

// ClearStalled removes a torrent from the stalled set.
func (c *LibtorrentClient) ClearStalled(hash string) {
	c.stalledMu.Lock()
	delete(c.stalled, strings.ToLower(hash))
	c.stalledMu.Unlock()
}

// Restart restarts a torrent without removing its handle, data, or resume
// state.
func (c *LibtorrentClient) Restart(hash string) (bool, error) {
	if ok, err := c.Pause(hash); err != nil || !ok {
		return false, err
	}
	if _, err := c.Resume(hash); err != nil {
		return false, err
	}
	return c.Reannounce(hash)
}

// SetPin pins or unpins a torrent from the auto-managed queue.
func (c *LibtorrentClient) SetPin(hash string, pinned bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtSetPin(c.session, strings.ToLower(hash), boolToInt32(pinned))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent pin failed: %s", errMessage)
	}
	return true, nil
}

// SetSequential toggles sequential download for the whole session.
func (c *LibtorrentClient) SetSequential(enabled bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtSetSequential(c.session, boolToInt32(enabled))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent sequential failed: %s", errMessage)
	}
	return true, nil
}

// ForceRecheck forces a full hash check of a torrent.
func (c *LibtorrentClient) ForceRecheck(hash string) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtForceRecheck(c.session, strings.ToLower(hash))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent recheck failed: %s", errMessage)
	}
	c.markRechecked(hash)
	return true, nil
}

// Reannounce forces a tracker announce.
func (c *LibtorrentClient) Reannounce(hash string) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, errMessage := cgoLtReannounce(c.session, strings.ToLower(hash))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent reannounce failed: %s", errMessage)
	}
	return true, nil
}

func boolToInt32(value bool) int32 {
	if value {
		return 1
	}
	return 0
}

// storedLimits reads the per-torrent byte limits persisted in the config DB.
func (c *LibtorrentClient) storedLimits(hash string) (int32, int32, bool, error) {
	if !fileExists(c.configDB) {
		return 0, 0, false, nil
	}
	conn, err := OpenConfigDB(c.configDB)
	if err != nil {
		return 0, 0, false, err
	}
	defer conn.Close()
	var downloadLimit, uploadLimit int64
	err = conn.QueryRow(
		"SELECT dl_bytes,ul_bytes FROM torrent_limits WHERE lower(info_hash)=?1",
		strings.ToLower(hash),
	).Scan(&downloadLimit, &uploadLimit)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return int32(downloadLimit), int32(uploadLimit), true, nil
}

// SaveConnLimits persists the per-torrent connection/upload-slot ceilings so
// they survive a restart (applied again by applyStoredLimits).
func (c *LibtorrentClient) SaveConnLimits(hash string, maxConnections, maxUploads int64) error {
	if !fileExists(c.configDB) {
		return nil
	}
	conn, err := OpenConfigDB(c.configDB)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Exec("CREATE TABLE IF NOT EXISTS torrent_limits (info_hash TEXT PRIMARY KEY, dl_bytes INTEGER NOT NULL DEFAULT -1, ul_bytes INTEGER NOT NULL DEFAULT -1, updated_at TEXT NOT NULL DEFAULT (datetime('now')));"); err != nil {
		return err
	}
	_ = sqlExec(conn, "ALTER TABLE torrent_limits ADD COLUMN max_connections INTEGER NOT NULL DEFAULT -1")
	_ = sqlExec(conn, "ALTER TABLE torrent_limits ADD COLUMN max_uploads INTEGER NOT NULL DEFAULT -1")
	_, err = conn.Exec(
		"INSERT INTO torrent_limits(info_hash,max_connections,max_uploads,updated_at) VALUES (?1,?2,?3,datetime('now')) ON CONFLICT(info_hash) DO UPDATE SET max_connections=excluded.max_connections,max_uploads=excluded.max_uploads,updated_at=datetime('now')",
		strings.ToLower(hash), maxConnections, maxUploads,
	)
	return err
}

// storedConnLimits reads the persisted connection ceilings. ok is false when
// none were stored (or the columns are absent on an older database).
func (c *LibtorrentClient) storedConnLimits(hash string) (int64, int64, bool) {
	if !fileExists(c.configDB) {
		return 0, 0, false
	}
	conn, err := OpenConfigDB(c.configDB)
	if err != nil {
		return 0, 0, false
	}
	defer conn.Close()
	var maxConnections, maxUploads sql.NullInt64
	if err := conn.QueryRow(
		"SELECT max_connections,max_uploads FROM torrent_limits WHERE lower(info_hash)=?1",
		strings.ToLower(hash),
	).Scan(&maxConnections, &maxUploads); err != nil {
		return 0, 0, false
	}
	if !maxConnections.Valid && !maxUploads.Valid {
		return 0, 0, false
	}
	return maxConnections.Int64, maxUploads.Int64, true
}

func (c *LibtorrentClient) seedLimitsPath() string {
	return filepath.Join(c.stateDir, "seed_limits.json")
}

func (c *LibtorrentClient) seedLimits() (map[string]seedLimit, error) {
	path := c.seedLimitsPath()
	if !fileExists(path) {
		return map[string]seedLimit{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	stored := map[string]seedLimit{}
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	result := make(map[string]seedLimit, len(stored))
	for hash, limit := range stored {
		result[strings.ToLower(hash)] = limit
	}
	return result, nil
}

func (c *LibtorrentClient) saveSeedLimit(hash string, ratio float64, days int64) error {
	if err := os.MkdirAll(c.stateDir, 0o755); err != nil {
		return err
	}
	limits, err := c.seedLimits()
	if err != nil {
		return err
	}
	limits[strings.ToLower(hash)] = seedLimit{Ratio: ratio, Days: days}
	data, err := json.MarshalIndent(limits, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWrite(c.seedLimitsPath(), data)
}

func (c *LibtorrentClient) clearSeedLimit(hash string) error {
	limits, err := c.seedLimits()
	if err != nil {
		return err
	}
	if _, ok := limits[strings.ToLower(hash)]; !ok {
		return nil
	}
	delete(limits, strings.ToLower(hash))
	data, err := json.MarshalIndent(limits, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWrite(c.seedLimitsPath(), data)
}

func (c *LibtorrentClient) applyStoredLimits(hash string) error {
	downloadLimit, uploadLimit, ok, err := c.storedLimits(hash)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	applied, errMessage := cgoLtSetLimits(c.session, hash, downloadLimit, uploadLimit)
	if applied == 0 {
		return fmt.Errorf("libtorrent apply saved limits failed: %s", errMessage)
	}
	return nil
}

// applyStoredConnLimits re-applies the persisted per-torrent connection and
// upload-slot ceilings after an add/restore.
func (c *LibtorrentClient) applyStoredConnLimits(hash string) {
	maxConnections, maxUploads, ok := c.storedConnLimits(hash)
	if !ok {
		return
	}
	if maxConnections >= 0 {
		if _, err := c.SetMaxConnections(hash, int(maxConnections)); err != nil {
			logging.Warn("cannot reapply torrent max connections", "hash", hash, "error", err)
		}
	}
	if maxUploads >= 0 {
		if _, err := c.SetMaxUploads(hash, int(maxUploads)); err != nil {
			logging.Warn("cannot reapply torrent max uploads", "hash", hash, "error", err)
		}
	}
}

// SetLimits sets per-torrent byte limits and persists the seed stop rule.
func (c *LibtorrentClient) SetLimits(hash string, downloadLimit, uploadLimit int64, seedRatio float64, seedDays int64) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	if downloadLimit < -1 || uploadLimit < -1 || downloadLimit > math.MaxInt32 || uploadLimit > math.MaxInt32 {
		return false, fmt.Errorf("limits must be between -1 and %d bytes/s", int64(math.MaxInt32))
	}
	if math.IsInf(seedRatio, 0) || math.IsNaN(seedRatio) || seedRatio < -1.0 || seedDays < -1 {
		return false, fmt.Errorf("seed limits must be -1, 0, or a positive value")
	}
	normalized := strings.ToLower(hash)
	ok, errMessage := cgoLtSetLimits(c.session, normalized, int32(downloadLimit), int32(uploadLimit))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent set limits failed: %s", errMessage)
	}
	conn, err := OpenConfigDB(c.configDB)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	if _, err := conn.Exec("CREATE TABLE IF NOT EXISTS torrent_limits (info_hash TEXT PRIMARY KEY, dl_bytes INTEGER NOT NULL DEFAULT -1, ul_bytes INTEGER NOT NULL DEFAULT -1, updated_at TEXT NOT NULL DEFAULT (datetime('now')));"); err != nil {
		return false, err
	}
	if _, err := conn.Exec("INSERT INTO torrent_limits(info_hash,dl_bytes,ul_bytes,updated_at) VALUES (?1,?2,?3,datetime('now')) ON CONFLICT(info_hash) DO UPDATE SET dl_bytes=excluded.dl_bytes,ul_bytes=excluded.ul_bytes,updated_at=excluded.updated_at", normalized, downloadLimit, uploadLimit); err != nil {
		return false, err
	}
	if seedRatio >= 0.0 || seedDays >= 0 {
		if err := c.saveSeedLimit(normalized, seedRatio, seedDays); err != nil {
			return false, err
		}
	} else {
		if err := c.clearSeedLimit(normalized); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (c *LibtorrentClient) restore(stateDir string) error {
	if !c.enterSession() {
		return nil
	}
	defer c.exitSession()
	restored, warning := cgoLtRestore(c.session, stateDir)
	if warning != "" {
		logging.Warn("some fastresume files were not restored", "warning", warning)
	}
	if restored > 0 {
		logging.Info(fmt.Sprintf("♻️ %d %s restored from the previous session", restored, plural(int64(restored), "download", "downloads")))
	}
	return nil
}

// Shutdown saves resume data for the running session.
// tevArchivedCopyPresent reports whether the DB points at an existing archived
// copy for this torrent. A "completed" row whose copy is gone (or was never
// placed) must not suppress post-processing again.
//
// Single releases are archived to a regular file, but a season pack is copied
// into a destination *directory* (usually the series folder). Treating a
// directory as a missing copy made the completion guard fail on every storage
// move, so the same season pack was re-processed and re-archived in a loop.
// A directory is therefore considered present when it still contains at least
// one regular file.
func tevArchivedCopyPresent(db *Database, hash string) bool {
	processed, err := db.TorrentProcessed(hash)
	if err != nil || processed == nil || strings.TrimSpace(*processed) == "" {
		return false
	}
	info, err := os.Stat(*processed)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return true
	}
	return tevDirectoryHasRegularFile(*processed)
}

// tevDirectoryHasRegularFile reports whether the directory contains a regular
// file, descending at most two levels so a season subfolder is covered without
// walking a whole library.
func tevDirectoryHasRegularFile(dir string) bool {
	return tevDirHasFile(dir, 2)
}

func tevDirHasFile(dir string, depth int) bool {
	if depth < 0 {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			return true
		}
		if entry.IsDir() && tevDirHasFile(filepath.Join(dir, entry.Name()), depth-1) {
			return true
		}
	}
	return false
}

// tevIgnoreRepeatedCompletion mirrors the "already completed" guard but only
// when the archived copy is really on disk.
func tevIgnoreRepeatedCompletion(db *Database, hash string) bool {
	status, err := db.TorrentStatus(hash)
	if err != nil || status == nil || *status != "completed" {
		return false
	}
	if tevArchivedCopyPresent(db, hash) {
		logging.Debug("ignoring completion for an already archived torrent")
		return true
	}
	logging.Debug("completed torrent archive is not recorded yet; continuing post-processing")
	return false
}

// Per-torrent flags accepted by SetTorrentFlag (mirror the GEXTTO_TFLAG_* ABI).
const (
	TorrentFlagApplyIPFilter = 1
	TorrentFlagDisableDHT    = 2
	TorrentFlagDisablePEX    = 4
	TorrentFlagDisableLSD    = 8
)

func ltBool(value bool) int32 {
	if value {
		return 1
	}
	return 0
}

// SetMaxConnections caps the concurrent connections for one torrent
// (0 = libtorrent default).
func (c *LibtorrentClient) SetMaxConnections(hash string, value int) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, message := cgoLtSetMaxConnections(c.session, hash, int32(value))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent set max connections failed: %s", message)
	}
	return true, nil
}

// SetMaxUploads caps the concurrently unchoked peers for one torrent.
func (c *LibtorrentClient) SetMaxUploads(hash string, value int) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, message := cgoLtSetMaxUploads(c.session, hash, int32(value))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent set max uploads failed: %s", message)
	}
	return true, nil
}

// SetUploadMode blocks data download while keeping the torrent in the session
// (useful to seed a torrent whose files are already present).
func (c *LibtorrentClient) SetUploadMode(hash string, enabled bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, message := cgoLtSetUploadMode(c.session, hash, ltBool(enabled))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent set upload mode failed: %s", message)
	}
	return true, nil
}

// SetShareMode seeds from files already on disk without rechecking them.
func (c *LibtorrentClient) SetShareMode(hash string, enabled bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, message := cgoLtSetShareMode(c.session, hash, ltBool(enabled))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent set share mode failed: %s", message)
	}
	return true, nil
}

// SetTorrentFlag toggles one per-torrent libtorrent flag (see TorrentFlag*).
func (c *LibtorrentClient) SetTorrentFlag(hash string, flag int, enabled bool) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, message := cgoLtSetTorrentFlag(c.session, hash, int32(flag), ltBool(enabled))
	if ok == 0 {
		return false, fmt.Errorf("libtorrent set torrent flag failed: %s", message)
	}
	return true, nil
}

// ScrapeTracker requests fresh swarm counts from the trackers of a torrent.
func (c *LibtorrentClient) ScrapeTracker(hash string) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, message := cgoLtScrapeTracker(c.session, hash)
	if ok == 0 {
		return false, fmt.Errorf("libtorrent scrape failed: %s", message)
	}
	return true, nil
}

// ForceDhtAnnounce announces a torrent to the DHT immediately.
func (c *LibtorrentClient) ForceDhtAnnounce(hash string) (bool, error) {
	if !c.enterSession() {
		return false, nil
	}
	defer c.exitSession()
	ok, message := cgoLtForceDhtAnnounce(c.session, hash)
	if ok == 0 {
		return false, fmt.Errorf("libtorrent dht announce failed: %s", message)
	}
	return true, nil
}

// SessionStats returns libtorrent's session counters as name -> value.
func (c *LibtorrentClient) SessionStats() (map[string]int64, error) {
	if !c.enterSession() {
		return nil, nil
	}
	defer c.exitSession()
	ok, output, message := cgoLtSessionStats(c.session)
	if ok == 0 {
		return nil, fmt.Errorf("libtorrent session stats unavailable: %s", message)
	}
	values := map[string]int64{}
	if err := json.Unmarshal([]byte(output), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func (c *LibtorrentClient) Shutdown(cfg *Config) error {
	// A torrent whose files libtorrent is moving (to the library after
	// seeding, off the RAM disk) must finish first: destroying the session
	// mid-move leaves partial files in the destination.
	c.waitForStorageMoves()
	// Refuse new session calls and wait for the running ones, then for every
	// in-flight List(), before destroying the handle: a late cgo call on a
	// destroyed session aborts the process.
	c.drainSession(sessionDrainTimeout)
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.session == nil {
		return nil
	}
	stateDir := c.stateDir
	if cfg != nil && cfg.StateDir != "" {
		stateDir = cfg.StateDir
	}
	saved, errMessage := cgoLtSaveResume(c.session, stateDir)
	if saved == 0 {
		// Destroy the session anyway so buffered pieces are flushed.
		cgoLtDestroy(c.session)
		c.session = nil
		return fmt.Errorf("libtorrent fastresume shutdown failed: %s", errMessage)
	}
	logging.Debug("libtorrent shutdown: resume data saved")
	// Close the session explicitly: the destructor drains libtorrent's disk
	// queue and flushes buffered pieces to the files. Without it the saved
	// bitfield can be ahead of the bytes actually on disk, so a restart
	// re-checks, discards them and re-downloads.
	cgoLtDestroy(c.session)
	c.session = nil
	logging.Info("⏹️ Torrent engine stopped cleanly; downloads will resume where they left off")
	return nil
}

// MovingStorage returns the torrents libtorrent is moving right now, as hash
// -> name; ok is false when the session cannot be asked.
func (c *LibtorrentClient) MovingStorage() (moving map[string]string, ok bool) {
	if !c.enterSession() {
		return nil, false
	}
	defer c.exitSession()
	count, moving, message := cgoLtMovingStorage(c.session)
	if count < 0 {
		logging.Warn("libtorrent: cannot tell whether files are being moved", "error", message)
		return nil, false
	}
	return moving, true
}

// waitForStorageMoves blocks, with no time limit, until libtorrent is not
// moving any torrent's files, saying every shutdownNoticeInterval what it is
// waiting for.
func (c *LibtorrentClient) waitForStorageMoves() {
	var lastNotice time.Time
	waited := false
	for {
		moving, ok := c.MovingStorage()
		if !ok || len(moving) == 0 {
			if waited {
				logging.Info("✅ File moves finished; stopping")
			}
			return
		}
		waited = true
		names := make([]string, 0, len(moving))
		for _, name := range moving {
			names = append(names, name)
		}
		sort.Strings(names)
		if time.Since(lastNotice) >= shutdownNoticeInterval {
			logging.Info(fmt.Sprintf("⏳ Stopping: waiting for %d %s to finish moving %s files so nothing is left half-moved: %s",
				len(names), pluralWord(len(names), "download", "downloads"), pluralWord(len(names), "its", "their"), quotedList(names, 3)))
			lastNotice = time.Now()
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// sessionDrainTimeout bounds how long Shutdown waits for in-flight session
// calls. A native call stuck past it must not hang the daemon's exit.
const sessionDrainTimeout = 15 * time.Second

// enterSession reserves the native session for one call. It returns false when
// there is no session or Shutdown has started; the caller then behaves as if
// the session were absent. Every true result must be paired with exitSession.
func (c *LibtorrentClient) enterSession() bool {
	c.sessionUse.Lock()
	defer c.sessionUse.Unlock()
	if c.sessionClosing || c.session == nil {
		return false
	}
	c.sessionActive++
	return true
}

// exitSession releases a reservation taken by enterSession.
func (c *LibtorrentClient) exitSession() {
	c.sessionUse.Lock()
	c.sessionActive--
	c.sessionUse.Unlock()
}

// drainSession closes the session to new calls and waits, up to timeout, for
// the calls already running.
func (c *LibtorrentClient) drainSession(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	c.sessionUse.Lock()
	c.sessionClosing = true
	for c.sessionActive > 0 {
		if time.Now().After(deadline) {
			logging.Warn("libtorrent shutdown: session calls still running; closing anyway",
				"active", c.sessionActive)
			break
		}
		c.sessionUse.Unlock()
		time.Sleep(20 * time.Millisecond)
		c.sessionUse.Lock()
	}
	c.sessionUse.Unlock()
}

// Remove removes a torrent, optionally deleting its files and resume data.
func (c *LibtorrentClient) Remove(hash string, deleteFiles bool) (bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(hash))
	c.markRemoved(normalized)
	if c.enterSession() {
		defer c.exitSession()
		ok, errMessage := cgoLtRemove(c.session, normalized, boolToInt32(deleteFiles))
		if ok == 0 {
			c.unmarkRemoved(normalized)
			c.invalidateListCache()
			return false, fmt.Errorf("libtorrent action failed: %s", errMessage)
		}
	}
	c.ClearStalled(normalized)
	c.torrentsMu.RLock()
	name := "unnamed torrent"
	if torrent, ok := c.torrents[normalized]; ok && strings.TrimSpace(torrent.Name) != "" {
		name = torrent.Name
	}
	c.torrentsMu.RUnlock()
	// Remove resume data as well, otherwise libtorrent restores the torrent on
	// restart and it reappears in Downloads, undoing the removal.
	for _, ext := range []string{".fastresume", ".torrent"} {
		if path, ok := c.stateFile(normalized, ext); ok {
			_ = os.Remove(path)
		}
	}
	if err := c.clearSeedLimit(normalized); err != nil {
		logging.Warn("could not clear removed torrent seed limits", "hash", normalized, "name", name, "error", err)
	}
	c.torrentsMu.Lock()
	_, existed := c.torrents[normalized]
	delete(c.torrents, normalized)
	c.torrentsMu.Unlock()
	if !existed && c.session == nil {
		c.unmarkRemoved(normalized)
		c.invalidateListCache()
		return false, nil
	}
	return existed || c.session != nil, nil
}

// Stats summarises the session state as a JSON-marshalable object.
func (c *LibtorrentClient) Stats() map[string]any {
	list := c.List()
	nativeCount := uint32(0)
	if c.enterSession() {
		defer c.exitSession()
		nativeCount = cgoLtTorrentCount(c.session)
	}
	downloading := 0
	stalled := 0
	seeding := 0
	queued := 0
	metadataPending := 0
	transferring := 0
	idle := 0
	for _, torrent := range list {
		if torrent.State == "downloading" {
			downloading++
		}
		switch {
		case TorrentTransferring(torrent):
			transferring++
		case TorrentIdle(torrent):
			idle++
		}
		if torrent.Stalled {
			stalled++
		}
		if torrent.State == "seeding" {
			seeding++
		}
		if torrent.State == "paused" && torrent.HasMetadata {
			queued++
		}
		if !torrent.HasMetadata {
			metadataPending++
		}
	}
	return map[string]any{
		"count":            len(list),
		"native_count":     nativeCount,
		"downloading":      downloading,
		"stalled":          stalled,
		"seeding":          seeding,
		"queued":           queued,
		"metadata_pending": metadataPending,
		"transferring":     transferring,
		"idle":             idle,
		"integrated":       c.session != nil,
		"dry_run":          c.DryRun,
	}
}

// StateDir returns the configured state directory.
func (c *LibtorrentClient) StateDir(cfg *Config) string {
	return cfg.StateDir
}

// effectiveListenInterfaces is the effective `listen_interfaces`. When an
// outgoing interface (VPN killswitch) is configured and the listen interface
// does not already specify a port, the listener is bound to the same adapter.
func effectiveListenInterfaces(lt *LibtorrentSettings) string {
	listen := strings.TrimSpace(lt.ListenInterfaces)
	outgoing := strings.TrimSpace(lt.OutgoingInterface)
	if listen != "" {
		if !strings.Contains(listen, ":") && !strings.Contains(listen, ",") && !strings.Contains(listen, "[") {
			return fmt.Sprintf("%s:%d", listen, lt.PortMin)
		}
		return listen
	}
	if outgoing != "" {
		return fmt.Sprintf("%s:%d", outgoing, lt.PortMin)
	}
	return ""
}
