package gextto

// Compatibility and diagnostics endpoints. They are small, read-mostly
// handlers that complement the main API without duplicating functionality that
// already exists under a different name (see docs/API.md):
//
//   - POST /api/config/add_all_from_archive  (bulk-create monitored series)
//   - GET  /api/system/lt_mem_suggest        (libtorrent memory sizing)
//   - GET  /api/debug/torrent_match          (why a release does/doesn't match)
//   - GET  /api/ramdisk_check                (validate a RAM disk path)
//   - GET  /api/license                      (bundled EUPL text)
//
// The handlers follow the gextto conventions: AppState injection, `jsonStatus`
// / `jsonResponse` helpers and the same response keys used by the rest of the
// API.

import (
	_ "embed"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/buzzqw/gextto/internal/logging"
)

//go:embed LICENSE
var licenseText string

// archiveNoiseDirs are directory names that commonly live next to a media
// library but are not series. They are skipped by `add_all_from_archive` so a
// scan does not pollute the library with system folders.
var archiveNoiseDirs = map[string]bool{
	"lost+found":                true,
	"$recycle.bin":              true,
	"system volume information": true,
	"@eadir":                    true,
	"#recycle":                  true,
	".stfolder":                 true,
}

// AddAllFromArchiveInput is the body of POST /api/config/add_all_from_archive.
// `root` is optional when exactly one `archive_root` is configured.
type AddAllFromArchiveInput struct {
	Root string `json:"root"`
}

// AddAllFromArchive ports extto's `add_all_from_archive`: it scans the archive
// root and registers every folder that is not already a monitored series.
//
// The defaults match the legacy endpoint (seasons `*`, quality `1080p`,
// configured default language, enabled, `archive_path` set to the folder). A
// leading dot and well-known system folders are skipped; existing series are
// matched case-insensitively so no duplicate is created.
func AddAllFromArchive(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input AddAllFromArchiveInput
	_ = decodeJSON(r, &input) // body is optional

	cfg := latestConfig(s)
	root := strings.TrimSpace(input.Root)
	if root == "" && cfg.ArchiveRoot != nil {
		root = strings.TrimSpace(*cfg.ArchiveRoot)
	}
	if root == "" {
		jsonError(w, http.StatusBadRequest, "specifica la radice con il parametro root o configura archive_root")
		return
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("percorso archivio non valido: %s", root))
		return
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("errore scansione archivio: %v", err))
		return
	}

	existing := make(map[string]bool, len(cfg.Series))
	for index := range cfg.Series {
		existing[strings.ToLower(strings.TrimSpace(cfg.Series[index].Name))] = true
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || strings.HasPrefix(name, ".") || archiveNoiseDirs[strings.ToLower(name)] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	// Build a fresh slice: `latestConfig` may return a cached *Config shared
	// with other handlers, so it must not be mutated in place.
	series := append([]SeriesConfig(nil), cfg.Series...)
	language := strings.TrimSpace(cfg.DefaultLanguage())
	added := 0
	for _, name := range names {
		key := strings.ToLower(name)
		if existing[key] {
			continue
		}
		series = append(series, SeriesConfig{
			Name:        name,
			Seasons:     "*",
			Quality:     "1080p",
			Language:    language,
			ArchivePath: filepath.Join(root, name),
			Enabled:     true,
		})
		existing[key] = true
		added++
	}

	if added > 0 {
		if err := SaveLibraryConfig(s.cfg.DataDir, series, cfg.Movies); err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("salvataggio libreria fallito: %v", err))
			return
		}
		logging.Info("add_all_from_archive", "root", root, "added", added)
	}

	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":      true,
		"success": true,
		"root":    root,
		"added":   added,
		"message": fmt.Sprintf("Aggiunte %d serie dal percorso archivio", added),
	})
}

// LtMemSuggest ports extto's `lt_mem_suggest`: it reads the system RAM and
// returns the libtorrent session values sized for it (disk cache, queued disk
// bytes, send buffer and peer list).
func LtMemSuggest(w http.ResponseWriter, r *http.Request, s *AppState) {
	totalBytes, _ := memory_info()
	totalMB := int64(totalBytes / (1024 * 1024))

	var cacheSize, queueMB, sendKB, peerList int64
	switch {
	case totalMB > 0 && totalMB < 2048:
		cacheSize, queueMB, sendKB, peerList = 0, 8, 256, 100
	case totalMB < 4096:
		cacheSize, queueMB, sendKB, peerList = 1024, 32, 512, 200
	case totalMB < 8192:
		cacheSize, queueMB, sendKB, peerList = 8192, 64, 1024, 300
	case totalMB < 16384:
		cacheSize, queueMB, sendKB, peerList = 16384, 64, 1024, 500
	default:
		cacheSize, queueMB, sendKB, peerList = 32768, 128, 2048, 500
	}

	jsonResponse(w, map[string]any{
		"ok":         true,
		"total_mb":   totalMB,
		"cache_size": cacheSize,
		// cache_size is expressed in 16 KiB blocks, like libtorrent's
		// `cache_size` setting.
		"cache_mb":  cacheSize * 16 / 1024,
		"queue_mb":  queueMB,
		"send_kb":   sendKB,
		"peer_list": peerList,
	})
}

// DebugTorrentMatch ports extto's `debug_torrent_match`: given a torrent or
// file name (`?name=...`) it reports how gextto parses it and which configured
// series it matches, plus whether the target folders exist. It is a diagnostic
// aid, not part of the acquisition flow.
func DebugTorrentMatch(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := strings.TrimSpace(queryParam(r, "name"))
	cfg := latestConfig(s)
	steps := []string{}

	result := map[string]any{"ok": true, "name": name, "steps": steps}
	if name == "" {
		result["steps"] = append(steps, "NO name provided")
		jsonResponse(w, result)
		return
	}
	if len(name) > 1024 {
		jsonError(w, http.StatusBadRequest, "name troppo lungo (max 1024 caratteri)")
		return
	}

	const magnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	release := ParseRelease(name, magnet, "debug")
	if release == nil {
		result["steps"] = append(steps, "release non riconosciuta dal parser")
		jsonResponse(w, result)
		return
	}

	probe := release.Title
	if release.Series != nil {
		probe = *release.Series
	}
	matches := []map[string]any{}
	var matchedSeries *SeriesConfig
	for index := range cfg.Series {
		series := &cfg.Series[index]
		matched := release.Series != nil && SeriesNamesMatch(series.Name, *release.Series)
		via := ""
		if !matched {
			for _, alias := range series.Aliases {
				if release.Series != nil && SeriesNamesMatch(alias, *release.Series) {
					matched, via = true, alias
					break
				}
			}
		}
		// Keep a compact list only of the plausible candidates: an exact/
		// alias match, or a name match against the parsed title.
		if !matched && !SeriesNamesMatch(series.Name, probe) {
			continue
		}
		entry := map[string]any{
			"name":         series.Name,
			"normalized":   NormalizeSeriesName(series.Name),
			"match":        matched,
			"enabled":      series.Enabled,
			"archive_path": series.ArchivePath,
		}
		if via != "" {
			entry["via_alias"] = via
		}
		matches = append(matches, entry)
		if matched && matchedSeries == nil {
			matchedSeries = series
		}
	}

	// A configured series only counts as a match if it is monitored; report the
	// first enabled match, but keep the full candidate list for diagnosis.
	var archivePath string
	if matchedSeries != nil {
		archivePath = matchedSeries.ArchivePath
	}

	finalDir := strings.TrimSpace(cfg.LibtorrentDir)
	tempDir := ""
	if cfg.LibtorrentTempDir != nil {
		tempDir = strings.TrimSpace(*cfg.LibtorrentTempDir)
	}

	result["title"] = release.Title
	result["kind"] = release.Kind
	result["series"] = release.Series
	result["season"] = release.Season
	result["episode"] = release.Episode
	result["is_pack"] = release.IsPack
	result["episode_range"] = release.EpisodeRange
	result["year"] = release.Year
	result["normalized"] = NormalizeSeriesName(probe)
	result["matched_series"] = nil
	if matchedSeries != nil {
		result["matched_series"] = matchedSeries.Name
	}
	result["matches"] = matches
	result["archive_path"] = archivePath
	result["archive_path_exists"] = archivePath != "" && directoryExists(archivePath)
	result["final_dir"] = finalDir
	result["final_dir_exists"] = finalDir != "" && directoryExists(finalDir)
	result["temp_dir"] = tempDir
	result["temp_dir_exists"] = tempDir != "" && directoryExists(tempDir)
	result["quality"] = release.Quality
	result["score"] = cfg.ReleaseScore(release)
	result["allowed"] = cfg.ReleaseAllowed(release)
	result["steps"] = steps
	jsonResponse(w, result)
}

// RamdiskCheck ports extto's `ramdisk_check` (`?path=...`): it verifies that the
// path exists, is writable, is actually backed by tmpfs/ramfs and reports its
// capacity. It complements RamdiskView, which only lists the mounted RAM disks.
func RamdiskCheck(w http.ResponseWriter, r *http.Request, s *AppState) {
	path := strings.TrimSpace(queryParam(r, "path"))
	if path == "" {
		jsonResponse(w, map[string]any{"ok": false, "error": "Percorso non specificato"})
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		jsonResponse(w, map[string]any{"ok": false, "error": fmt.Sprintf("Directory non trovata: %s", path)})
		return
	}

	mountType, isRamdisk := ramdiskMountType(path)
	if !DirectoryWritable(path) {
		jsonResponse(w, map[string]any{
			"ok":         false,
			"writable":   false,
			"is_ramdisk": isRamdisk,
			"mount_type": mountType,
			"error":      fmt.Sprintf("Gextto non ha i permessi di scrittura su %s", path),
		})
		return
	}

	total, free, ok := FilesystemSpace(path)
	if !ok {
		jsonResponse(w, map[string]any{"ok": false, "writable": true, "error": "Errore lettura spazio"})
		return
	}
	const gib = 1024.0 * 1024.0 * 1024.0
	warning := ""
	if !isRamdisk {
		warning = fmt.Sprintf("Il filesystem rilevato è '%s', non tmpfs/ramfs. Gextto funzionerà, ma non si tratta di un vero RAM disk.", mountType)
	}
	round := func(bytes uint64) float64 { return math.Round(float64(bytes)/gib*100) / 100 }
	jsonResponse(w, map[string]any{
		"ok":          true,
		"writable":    true,
		"mount_type":  mountType,
		"is_ramdisk":  isRamdisk,
		"total_gb":    round(total),
		"used_gb":     round(total - free),
		"free_gb":     round(free),
		"total_bytes": total,
		"free_bytes":  free,
		"warning":     warning,
		"error":       "",
	})
}

// LicenseApi ports extto's `get_license`: it returns the bundled licence text.
func LicenseApi(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{
		"ok":      true,
		"content": licenseText,
		"license": "EUPL-1.2",
	})
}

// directoryExists reports whether path is an existing directory.
func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// tmpfsMagic / ramfsMagic are the statfs f_type values of the two RAM-backed
// filesystems. Detecting them from statfs is reliable even when /proc is not
// readable (container).
const (
	tmpfsMagic = 0x01021994
	ramfsMagic = 0x858458f6
)

// ramdiskMountType returns the filesystem type backing path and whether it is a
// RAM disk. The statfs magic is authoritative when available; otherwise the
// longest matching /proc/self/mountinfo entry is used for a readable label.
func ramdiskMountType(path string) (string, bool) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err == nil {
		switch stats.Type {
		case tmpfsMagic:
			return "tmpfs", true
		case ramfsMagic:
			return "ramfs", true
		}
	}
	best := ""
	bestType := "unknown"
	if contents, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		for _, line := range strings.Split(string(contents), "\n") {
			parts := strings.SplitN(line, " - ", 2)
			if len(parts) != 2 {
				continue
			}
			left := strings.Fields(parts[0])
			right := strings.Fields(parts[1])
			if len(left) < 5 || len(right) == 0 {
				continue
			}
			mountPoint := DecodeMountField(left[4])
			if mountPoint == path || strings.HasPrefix(path, strings.TrimSuffix(mountPoint, "/")+"/") {
				if len(mountPoint) > len(best) {
					best, bestType = mountPoint, right[0]
				}
			}
		}
	}
	isRamdisk := bestType == "tmpfs" || bestType == "ramfs"
	return bestType, isRamdisk
}
