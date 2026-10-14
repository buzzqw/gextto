package gextto

// transfer_policy.go holds the transfer policy that is shared by every engine:
// download-path selection (including the RAM disk), the free-space budget, the
// torrent diagnosis and the small path helpers. It used to live in
// libtorrent.go next to the embedded libtorrent client; the client is gone and
// these helpers stayed.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

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

// TrackerEntry is one (tier, URL) pair passed to SetTrackers.
type TrackerEntry struct {
	Tier int32
	URL  string
}

// TrimMemory returns freed C++/libtorrent heap pages to the operating system.
// Called after transfers and cycles so the resident size falls back instead of
// staying at the download peak.
// TrimMemory is a no-op: it used to return the embedded libtorrent session's
// C++ heap to the operating system. The pure-Go build always stubbed it, and
// the embedded engine is gone, so it is kept only for its callers.
func TrimMemory() {}

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

// RamdiskPendingBytes is the space a torrent on the RAM disk still has to take
// there: the bytes not downloaded yet, minus what is already allocated. A
// payload copied onto the tmpfs from a preallocated file on disk occupies its
// whole size from the start, so the free space already accounts for it;
// counting the missing bytes again would require twice the torrent size and
// push a fitting download off the RAM disk.
func RamdiskPendingBytes(view models.TorrentView) uint64 {
	remaining := view.TotalSize - view.TotalDone
	if remaining <= 0 {
		return 0
	}
	if view.TotalSize > 0 {
		path := CompletionPath(&models.TorrentEvent{Hash: view.Hash, Name: view.Name, SavePath: view.SavePath})
		unallocated := view.TotalSize - diskUsage(path)
		if unallocated < remaining {
			remaining = unallocated
		}
	}
	if remaining <= 0 {
		return 0
	}
	return uint64(remaining)
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

// ramdiskHostMemoryAvailable reports the host memory that is actually available
// (MemAvailable) in bytes; ok is false when it cannot be determined. It is a
// variable so tests can pin it; the default reads /proc/meminfo.
var ramdiskHostMemoryAvailable = func() (uint64, bool) {
	_, available := memory_info()
	return available, available > 0
}

// ramdiskEffectiveFreeBytes returns the RAM disk's usable free space: the lesser
// of the tmpfs free space and the host memory that is really available. A tmpfs
// is RAM-backed, so its statfs free space can report several GB on a 6 GB mount
// while the machine has almost no memory left; trusting it would stage a
// download that then fails or drives the host into swap. When the host figure
// cannot be read the tmpfs one is used unchanged (the historical behavior).
// A nil result means the space could not be determined.
func ramdiskEffectiveFreeBytes(ramdisk string) *uint64 {
	free := FreeSpaceBytes(ramdisk)
	if free == nil {
		return nil
	}
	available, ok := ramdiskHostMemoryAvailable()
	if !ok || available >= *free {
		return free
	}
	return &available
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
	if free := ramdiskEffectiveFreeBytes(*dir); free != nil && *free >= minimumFree {
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
	// Known to fit the size threshold: the RAM disk is an explicit choice (the
	// engine default no longer falls back to it), so ask for it only when it
	// has room for the payload. The size threshold alone is not enough: the
	// tmpfs may be free while the host has little memory left, or the tmpfs
	// itself may be nearly full, so the payload must fit the effective free
	// space too. Otherwise it falls back to the overflow dir like an oversized
	// release.
	if dir, ok := ramdiskDefaultDir(cfg); ok {
		if free := ramdiskEffectiveFreeBytes(dir); free != nil {
			if err := RamdiskFits(cfg.RamdiskThresholdBytes(), cfg.RamdiskMarginBytes(), *free, 0, uint64(release.SizeBytes)); err != nil {
				if overflow, ok := ramdiskOverflowDir(cfg); ok {
					return &overflow
				}
				return nil
			}
		}
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

// resolveSavePath applies the `preferred_path.filter(is_dir).unwrap_or_else`.
func resolveSavePath(preferredPath *string, cfg *Config) string {
	if preferredPath != nil && pathIsDir(*preferredPath) {
		return *preferredPath
	}
	return preferredDownloadPath(cfg)
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
