package gextto

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// Orphans in the download temp folder (libtorrent_temp_dir): data no download
// owns any more, typically left by an unclean stop or a move that went wrong.
// They are moved to the trash, never deleted, so a mistake stays recoverable
// until the trash is emptied.

const (
	tempOrphanEnabledSetting = "temp_orphan_cleanup_enabled"
	tempOrphanMinAgeSetting  = "temp_orphan_min_age_days"
	tempOrphanDefaultDays    = 7
)

// tempOrphanMinAge reads how long an entry must be untouched before it counts
// as an orphan; 0 or a missing/invalid value falls back to the default.
func tempOrphanMinAge(cfg *Config) time.Duration {
	days := tempOrphanDefaultDays
	if value, err := strconv.Atoi(strings.TrimSpace(cfg.Settings[tempOrphanMinAgeSetting])); err == nil && value > 0 {
		days = value
	}
	return time.Duration(days) * 24 * time.Hour
}

// diskUsage is the space path really occupies (allocated blocks), which for
// an abandoned download full of holes is far less than its apparent size.
func diskUsage(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(entry string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			total += stat.Blocks * 512
		} else {
			total += info.Size()
		}
		return nil
	})
	return total
}

// newestModTime returns the most recent modification time inside path, so a
// folder whose download wrote a file yesterday is not mistaken for idle.
func newestModTime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	newest := info.ModTime()
	if !info.IsDir() {
		return newest, nil
	}
	err = filepath.Walk(path, func(_ string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.ModTime().After(newest) {
			newest = entry.ModTime()
		}
		return nil
	})
	return newest, err
}

// trashOrphanedTempData moves to the trash every entry of the temp folder
// that no download owns and that has not changed for temp_orphan_min_age_days
// (default 7). An entry is owned when a torrent in the session has its name or
// keeps its files inside it, or when the database still expects a download of
// that name (lost by an unclean stop, about to be added back). It returns how
// many entries were moved.
func trashOrphanedTempData(cfg *Config, torrents TorrentSession, db *Database, now time.Time) int {
	if cfg == nil || cfg.LibtorrentTempDir == nil || torrents == nil {
		return 0
	}
	if value, ok := cfg.Settings[tempOrphanEnabledSetting]; ok && !settingTruthy(value) {
		return 0
	}
	temp := strings.TrimSpace(*cfg.LibtorrentTempDir)
	if temp == "" {
		return 0
	}
	if ramdisk := cfg.RamdiskDir(); ramdisk != nil && SamePath(*ramdisk, temp) {
		return 0 // the RAM disk has its own orphan sweep
	}
	list := torrents.List()
	// With no torrent at all the session may simply not be loaded: every
	// folder would look orphaned. Do nothing rather than guess.
	if len(list) == 0 {
		logging.Debug("temp orphan sweep skipped: the torrent list is empty")
		return 0
	}
	owned := map[string]struct{}{}
	savePaths := make([]string, 0, len(list))
	live := make(map[string]struct{}, len(list))
	for _, torrent := range list {
		// A torrent still fetching metadata has no name yet: its folder would
		// look orphaned. Wait until every torrent is known.
		if !torrent.HasMetadata {
			logging.Debug("temp orphan sweep skipped: a torrent is still fetching metadata")
			return 0
		}
		owned[torrent.Name] = struct{}{}
		savePaths = append(savePaths, torrent.SavePath)
		live[strings.ToLower(torrent.Hash)] = struct{}{}
	}
	if db != nil {
		missing, err := db.missingActiveTorrents(live)
		if err != nil {
			logging.Debug("temp orphan sweep skipped: cannot read the active downloads", "error", err)
			return 0
		}
		for _, item := range missing {
			if name := magnetDisplayName(item.Release.Magnet); name != "" {
				owned[name] = struct{}{}
			}
			if title := strings.TrimSpace(item.Release.Title); title != "" {
				owned[title] = struct{}{}
			}
		}
	}
	entries, err := os.ReadDir(temp)
	if err != nil {
		return 0
	}
	minAge := tempOrphanMinAge(cfg)
	trash := cfg.ResolveTrashPath()
	moved := 0
	for _, entry := range entries {
		name := entry.Name()
		// Hidden entries are Gextto's own temporary copies, swept separately.
		if strings.HasPrefix(name, ".") {
			continue
		}
		if _, ok := owned[name]; ok {
			continue
		}
		path := filepath.Join(temp, name)
		inUse := false
		for _, savePath := range savePaths {
			if pathStartsWith(savePath, path) {
				inUse = true
				break
			}
		}
		if inUse || pathWithin(trash, path) || SamePath(trash, path) {
			continue
		}
		newest, err := newestModTime(path)
		if err != nil || now.Sub(newest) < minAge {
			continue
		}
		size := diskUsage(path)
		finish := beginFileOperation(path)
		target, err := MoveToTrash(path, trash)
		finish()
		if err != nil {
			logging.Warn(fmt.Sprintf("⚠️ Could not move «%s» from the download temp folder to the trash", name),
				"path", path, "error", err.Error())
			continue
		}
		moved++
		logging.Info(fmt.Sprintf("🧹 «%s» in the download temp folder belongs to no download and has not changed for %d days: moved to the trash (%s on disk)",
			name, int(now.Sub(newest).Hours()/24), logging.HumanBytesI64(size)), "trash", target)
	}
	return moved
}
