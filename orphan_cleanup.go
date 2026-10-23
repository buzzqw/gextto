package gextto

// orphan_cleanup.go: guided cleanup of the download folders. A torrent that
// finished and was removed from the session can leave its source behind when
// automatic removal is off. This scan proposes only entries that no live
// torrent owns and whose every video file has a library copy at equal or
// better quality; everything else is shown as skipped with the reason. The
// action moves the safe entries to the trash, never deletes them.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

// OrphanDownload is one leftover entry in a download folder.
type OrphanDownload struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Episodes  string `json:"episodes,omitempty"`
	Safe      bool   `json:"safe"`
	Reason    string `json:"reason,omitempty"`
}

// orphanVideoFile is one video inside a candidate.
type orphanVideoFile struct {
	path    string
	series  string
	season  int64
	episode int64
	score   int64
}

// FindOrphanedDownloads scans the configured download folders. It never looks
// inside a folder owned by a live torrent, and it ignores entries that do not
// contain at least one episode-named video.
func FindOrphanedDownloads(cfg *Config, db *Database, torrents TorrentEngine) []OrphanDownload {
	if cfg == nil || db == nil || torrents == nil {
		return nil
	}
	roots := orphanRoots(cfg)
	if len(roots) == 0 {
		return nil
	}
	owned := orphanOwnedPaths(torrents)
	out := make([]OrphanDownload, 0)
	seen := map[string]struct{}{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			full := filepath.Join(root, name)
			if _, skip := seen[full]; skip {
				continue
			}
			seen[full] = struct{}{}
			if orphanInsideRoot(full, roots) || orphanOwned(owned, full) {
				continue
			}
			info, err := os.Lstat(full)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			videos := orphanVideoFiles(full, cfg.Settings)
			if len(videos) == 0 {
				continue
			}
			candidate := OrphanDownload{Path: full, Name: name, Safe: true}
			if info.IsDir() {
				candidate.SizeBytes, _ = SizeOfPath(full)
			} else {
				candidate.SizeBytes = info.Size()
			}
			seasons := map[string]struct{}{}
			for _, video := range videos {
				seasons[fmt.Sprintf("S%02dE%02d", video.season, video.episode)] = struct{}{}
				series := cfg.FindSeriesMatch(video.series, &video.season)
				if series == nil {
					candidate.Safe = false
					candidate.Reason = "serie non monitorata: " + video.series
					continue
				}
				libraryScore, libraryPath, ok := orphanLibraryEpisode(db, series.Name, video.season, video.episode)
				if !ok {
					candidate.Safe = false
					candidate.Reason = fmt.Sprintf("episodio S%02dE%02d non presente in libreria", video.season, video.episode)
					continue
				}
				if libraryScore < video.score && !orphanSameSize(video.path, libraryPath) {
					candidate.Safe = false
					candidate.Reason = fmt.Sprintf("la copia in libreria di S%02dE%02d è inferiore", video.season, video.episode)
					continue
				}
			}
			candidate.Episodes = orphanEpisodeLabel(seasons)
			if candidate.Safe {
				candidate.Reason = ""
			}
			out = append(out, candidate)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Safe != out[j].Safe {
			return out[i].Safe
		}
		return out[i].SizeBytes > out[j].SizeBytes
	})
	return out
}

// CleanOrphanedDownloads re-scans and moves every safe entry to the trash. It
// returns how many entries and how many bytes were moved. Nothing is deleted
// when the trash path is not configured.
func CleanOrphanedDownloads(cfg *Config, db *Database, torrents TorrentEngine) (int, int64, error) {
	if cfg == nil || cfg.TrashPath == nil || strings.TrimSpace(*cfg.TrashPath) == "" {
		return 0, 0, fmt.Errorf("trash folder is not configured")
	}
	items := FindOrphanedDownloads(cfg, db, torrents)
	moved, movedBytes := 0, int64(0)
	for _, item := range items {
		if !item.Safe {
			continue
		}
		target, err := MoveToTrash(item.Path, *cfg.TrashPath)
		if err != nil {
			logging.Warn("could not move the orphaned download to the trash",
				"path", item.Path, "error", err.Error())
			continue
		}
		moved++
		movedBytes += item.SizeBytes
		logging.Info(fmt.Sprintf("🧹 Orphaned download moved to the trash — «%s» · %s → %s",
			item.Name, logging.HumanBytesI64(item.SizeBytes), target))
	}
	return moved, movedBytes, nil
}

// orphanRoots lists the distinct configured download folders.
func orphanRoots(cfg *Config) []string {
	candidates := []string{cfg.DownloadDir}
	if cfg.DownloadTempDir != nil {
		candidates = append(candidates, *cfg.DownloadTempDir)
	}
	roots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		cleaned := filepath.Clean(candidate)
		duplicate := false
		for _, existing := range roots {
			if existing == cleaned {
				duplicate = true
				break
			}
		}
		if !duplicate {
			roots = append(roots, cleaned)
		}
	}
	return roots
}

// orphanInsideRoot reports whether path is one of the roots themselves (the
// temp folder while scanning its parent), which is handled separately.
func orphanInsideRoot(path string, roots []string) bool {
	for _, root := range roots {
		if path == root {
			return true
		}
	}
	return false
}

// orphanOwnedPaths returns the top-level entries owned by a live torrent.
func orphanOwnedPaths(torrents TorrentEngine) map[string]struct{} {
	owned := map[string]struct{}{}
	for _, torrent := range torrents.List() {
		if strings.TrimSpace(torrent.Name) != "" && strings.TrimSpace(torrent.SavePath) != "" {
			owned[filepath.Clean(filepath.Join(torrent.SavePath, torrent.Name))] = struct{}{}
		}
		if files, ok, err := torrents.Files(torrent.Hash); err == nil && ok {
			for _, file := range files {
				abs := filepath.Clean(filepath.Join(torrent.SavePath, file.Path))
				// Mark the top-level component below the save path.
				rel, relErr := filepath.Rel(torrent.SavePath, abs)
				if relErr != nil || rel == "." || strings.HasPrefix(rel, "..") {
					continue
				}
				first := strings.Split(rel, string(filepath.Separator))[0]
				owned[filepath.Clean(filepath.Join(torrent.SavePath, first))] = struct{}{}
			}
		}
	}
	return owned
}

// orphanOwned reports whether path (or one of its parents) is owned.
func orphanOwned(owned map[string]struct{}, path string) bool {
	cleaned := filepath.Clean(path)
	if _, ok := owned[cleaned]; ok {
		return true
	}
	for parent := filepath.Dir(cleaned); parent != cleaned && parent != "." && parent != string(filepath.Separator); parent = filepath.Dir(parent) {
		if _, ok := owned[parent]; ok {
			return true
		}
		cleaned = parent
	}
	return false
}

// orphanVideoFiles collects the video files of a candidate (recursively for a
// folder, depth-limited) and resolves their episode identity.
func orphanVideoFiles(path string, settings map[string]string) []orphanVideoFile {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	files := []string{}
	if info.Mode().IsRegular() {
		if isVideoExtension(path) {
			files = append(files, path)
		}
	} else if info.IsDir() {
		root := path
		_ = filepath.WalkDir(root, func(entry string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				if entry != root {
					rel, err := filepath.Rel(root, entry)
					if err != nil || strings.Count(rel, string(filepath.Separator)) >= 2 {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if isVideoExtension(entry) {
				files = append(files, entry)
			}
			return nil
		})
	}
	out := make([]orphanVideoFile, 0, len(files))
	for _, file := range files {
		series, season, episode, ok := extractEpisodeIdentityFromPath(file, filepath.Base(file))
		if !ok {
			continue
		}
		quality := ParseQuality(filepath.Base(file))
		out = append(out, orphanVideoFile{
			path:    file,
			series:  series,
			season:  season,
			episode: episode,
			score:   ScoreQuality(&quality, settings),
		})
	}
	return out
}

// orphanLibraryEpisode returns the score and path of an episode in the library.
func orphanLibraryEpisode(db *Database, seriesName string, season, episode int64) (int64, string, bool) {
	var score int64
	var archivePath string
	err := db.db.QueryRow(`SELECT e.quality_score, COALESCE(e.archive_path,'')
		FROM episodes e JOIN series s ON s.id=e.series_id
		WHERE s.name=?1 AND e.season=?2 AND e.episode=?3`, seriesName, season, episode).Scan(&score, &archivePath)
	if err != nil || strings.TrimSpace(archivePath) == "" {
		return 0, "", false
	}
	if _, err := os.Stat(archivePath); err != nil {
		return 0, "", false
	}
	return score, archivePath, true
}

// orphanSameSize reports whether the source and the library file have the same
// non-zero size: the usual sign that they are the same release, even when a
// slightly different score came from the audio tag or a scoring change.
func orphanSameSize(source, library string) bool {
	sourceInfo, err := os.Stat(source)
	if err != nil || sourceInfo.IsDir() || sourceInfo.Size() <= 0 {
		return false
	}
	libraryInfo, err := os.Stat(library)
	if err != nil || libraryInfo.IsDir() {
		return false
	}
	return sourceInfo.Size() == libraryInfo.Size()
}

// orphanEpisodeLabel renders a compact list of episodes.
func orphanEpisodeLabel(episodes map[string]struct{}) string {
	if len(episodes) == 0 {
		return ""
	}
	list := make([]string, 0, len(episodes))
	for episode := range episodes {
		list = append(list, episode)
	}
	sort.Strings(list)
	if len(list) > 6 {
		return strings.Join(list[:6], ", ") + "…"
	}
	return strings.Join(list, ", ")
}
