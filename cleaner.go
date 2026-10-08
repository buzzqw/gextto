package gextto

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// ErrInferiorDuplicate is returned when an incoming release is discarded
// because an existing file is equal or superior in quality.
var ErrInferiorDuplicate = errors.New("release inferior to existing file")

// IndexArchive implements `index_archive`: scans an archive folder
// recursively, groups video files by `(season, episode)` and keeps the file
// with the highest score. It implements the legacy
// `_best_quality_in_path`/`episode_archive_presence` helpers.
func IndexArchive(seriesName, archivePath string, settings map[string]string) models.ArchiveQualityIndex {
	index := models.ArchiveQualityIndex{Best: map[[2]int64]models.ArchiveQuality{}}
	if archivePath == "" {
		return index
	}
	info, err := os.Stat(archivePath)
	if err != nil || !info.IsDir() {
		return index
	}
	pattern, err := utils.CachedRegex(
		`(?i)^(?P<name>.+?)[ ._-]+(?:s(?P<s>\d{1,2})e|(?P<ns>\d{1,2})x)(?P<e>\d{1,4})`,
	)
	if err != nil {
		return index
	}
	normalized := NormalizeSeriesName(seriesName)
	files, err := VideoFiles(archivePath)
	if err != nil {
		return index
	}
	for _, file := range files {
		name, ok := cleanerName(file)
		if !ok {
			continue
		}
		season := int64(0)
		episode := int64(0)
		matched := false
		captures := pattern.FindStringSubmatch(name)
		if captures != nil {
			fileSeries := cleanerCapture(pattern, captures, "name")
			if fileSeries != "" && SeriesNamesMatch(normalized, fileSeries) {
				seasonRaw := cleanerCapture(pattern, captures, "s", "ns")
				episodeRaw := cleanerCapture(pattern, captures, "e")
				if seasonRaw != "" && episodeRaw != "" {
					s, errS := strconv.ParseInt(seasonRaw, 10, 64)
					e, errE := strconv.ParseInt(episodeRaw, 10, 64)
					if errS == nil && errE == nil {
						season = s
						episode = e
						matched = true
					}
				}
			}
		}
		if !matched {
			idSeries, idSeason, idEpisode, ok := extractEpisodeIdentityFromPath(file, name)
			if ok && SeriesNamesMatch(normalized, idSeries) {
				season = idSeason
				episode = idEpisode
				matched = true
			}
		}
		if !matched {
			continue
		}
		quality := ParseQuality(name)
		score := ScoreQuality(&quality, settings)
		key := [2]int64{season, episode}
		existing, found := index.Best[key]
		if !found || score > existing.Score {
			index.Best[key] = models.ArchiveQuality{Quality: quality, Score: score}
		}
	}
	return index
}

// qualityUpgradeAllowed reports whether the semantic upgrade rules allow `new`
// to replace `old`.
func qualityUpgradeAllowed(newQuality, oldQuality *models.Quality, newScore, oldScore, minDiff int64) bool {
	return newQuality.UpgradeReason(oldQuality, newScore, oldScore, minDiff) != ""
}

// languageMatch implements the `LanguageMatch` enum.
type languageMatch int

const (
	languagePreferred languageMatch = iota
	languageOther
	languageUnknown
)

// languageMatchFor classifies the language declared by a file name. Unknown
// when no language tag is present (no preference is applied then).
func languageMatchFor(name, preferred string) languageMatch {
	preferred = strings.ToLower(strings.TrimSpace(preferred))
	if preferred == "" {
		return languageUnknown
	}
	quality := ParseQuality(name)
	detected := []string{}
	if quality.IsIta {
		detected = append(detected, "ita")
	}
	if strings.TrimSpace(quality.Language) != "" {
		detected = append(detected, strings.ToLower(quality.Language))
	}
	for _, code := range quality.Languages {
		detected = append(detected, strings.ToLower(code))
	}
	kept := detected[:0]
	for _, code := range detected {
		if code != "" {
			kept = append(kept, code)
		}
	}
	detected = kept
	if len(detected) == 0 {
		return languageUnknown
	}
	matches := func(code string) bool {
		return code == preferred ||
			(preferred == "ita" && code == "it") ||
			(preferred == "eng" && code == "en") ||
			(preferred == "spa" && code == "es") ||
			(preferred == "deu" && code == "de") ||
			(preferred == "fra" && code == "fr")
	}
	for _, code := range detected {
		if matches(code) {
			return languagePreferred
		}
	}
	return languageOther
}

// removeEmptyParents removes emptied directories after a move, stopping before
// the series/movie root (parity with EXTTO).
func removeEmptyParents(path, stopAt string) {
	directory := filepath.Dir(path)
	for {
		if directory == stopAt || !cleanerPathWithin(directory, stopAt) {
			break
		}
		entries, err := os.ReadDir(directory)
		empty := err == nil && len(entries) == 0
		if !empty || os.Remove(directory) != nil {
			break
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
}

// cleanerPathWithin reports whether child is lexically inside ancestor.
func cleanerPathWithin(child, ancestor string) bool {
	rel, err := filepath.Rel(ancestor, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// localPath reports whether a path is a local filesystem path (not a URL).
func localPath(path string) bool {
	value := strings.ToLower(path)
	for _, prefix := range []string{"http://", "https://", "ftp://", "smb://", "nfs://"} {
		if strings.HasPrefix(value, prefix) {
			return false
		}
	}
	return true
}

// duplicateTarget builds a unique destination inside the trash folder.
func duplicateTarget(trash, file string) string {
	name := ""
	if base, ok := cleanerName(file); ok {
		name = base
	}
	candidate := filepath.Join(trash, name)
	if !cleanerExists(candidate) {
		return candidate
	}
	stem := cleanerFileStem(file)
	if stem == "" {
		stem = "duplicate"
	}
	extension := cleanerFileExtension(file)
	for index := 1; index <= 10000; index++ {
		candidate := filepath.Join(trash, fmt.Sprintf("%s__duplicate_%d%s", stem, index, extension))
		if !cleanerExists(candidate) {
			return candidate
		}
	}
	// Pathological: thousands of duplicates already present. Fall back to a
	// random suffix so the loop always terminates instead of spinning forever.
	return filepath.Join(trash, fmt.Sprintf("%s__duplicate_%s%s", stem, randomToken(), extension))
}

// handleDuplicate removes a duplicate file according to the cleanup action.
func handleDuplicate(file string, cfg *Config) error {
	file = filepath.Clean(file)
	if strings.Contains(file, "..") {
		return fmt.Errorf("invalid path: %s", file)
	}
	if cfg.CleanupAction == "delete" {
		return os.Remove(file)
	}
	// ResolveTrashPath always yields a usable directory (configured trash_path
	// or <data>/trash); MoveToTrash creates it when missing.
	_, err := MoveToTrash(file, cfg.ResolveTrashPath())
	return err
}

// logInferiorFileReplaced records that an existing lower-quality file was
// removed because a better release replaced it, so the reason is visible in the
// log instead of only a bare count.
func logInferiorFileReplaced(cfg *Config, file, replacement string) {
	action := "is in the trash"
	if cfg.CleanupAction == "delete" {
		action = "was deleted"
	}
	logging.Info(fmt.Sprintf("🗑️ Replaced with a better version: «%s»; the old file «%s» %s",
		filepath.Base(replacement), filepath.Base(file), action))
	logging.Debug("inferior file replaced", "file", file, "replacement", replacement)
}

// logRemovedFiles logs one "replaced" line per removed file. Used by the
// callers that do not emit a combined upgrade line of their own (background
// cleanup and post-processing); the completion handler logs a single line.
func logRemovedFiles(cfg *Config, files []string, replacement string) {
	for _, file := range files {
		logInferiorFileReplaced(cfg, file, replacement)
	}
}

// MoveToTrash moves a file or directory to the trash with a unique name; it
// falls back to a recursive copy + remove when the trash is on another
// filesystem (a plain rename fails with EXDEV, typical with a NAS).
func MoveToTrash(source, trash string) (string, error) {
	source = filepath.Clean(source)
	trash = filepath.Clean(trash)
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return "", err
	}
	// A trash folder equal to or inside the source makes the cross-device copy
	// fallback recurse into itself until the disk is full. Refuse it up front.
	if SamePath(source, trash) || pathWithin(trash, source) {
		return "", fmt.Errorf("trash path %q is inside the source %q", trash, source)
	}
	if strings.Contains(source, "..") {
		return "", fmt.Errorf("invalid source path %q", source)
	}
	target := duplicateTarget(trash, source)
	if strings.Contains(target, "..") {
		return "", fmt.Errorf("invalid target path %q", target)
	}
	renameErr := os.Rename(source, target)
	if renameErr == nil {
		touchTrashEntry(target)
		return target, nil
	}
	// Only a cross-device rename needs the copy fallback. Any other failure
	// (permissions, invalid argument, target missing) must be reported, not
	// turned into a copy that can misbehave.
	if !errors.Is(renameErr, syscall.EXDEV) {
		return "", renameErr
	}
	// Copy into a hidden sibling first. The final rename makes the complete
	// trash entry visible atomically, so an interrupted cross-filesystem copy
	// cannot look like a valid archived duplicate.
	base := "item"
	if name, ok := cleanerName(target); ok && name != "" {
		base = name
	}
	partial := filepath.Join(trash, fmt.Sprintf(".%s.gextto-trash-%s", base, backupUUID()))
	if err := copyRecursive(source, partial); err != nil {
		_ = removeAfterCopy(partial)
		return "", err
	}
	if err := os.Rename(partial, target); err != nil {
		_ = removeAfterCopy(partial)
		return "", err
	}
	if err := removeAfterCopy(source); err != nil {
		return "", err
	}
	touchTrashEntry(target)
	return target, nil
}

// touchTrashEntry refreshes the modification time of a trash entry and its
// contents, so retention counts from the moment the entry entered the trash
// instead of the original file's mtime.
func touchTrashEntry(target string) {
	now := time.Now()
	_ = filepath.Walk(target, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		_ = os.Chtimes(path, now, now)
		return nil
	})
}

// removeAfterCopy retries the specific ENOTEMPTY race that can happen when a
// torrent client finishes a filesystem operation while its handle is being
// removed.
func removeAfterCopy(source string) error {
	const retries = 10
	const retryDelay = 100 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		var err error
		if info, statErr := os.Stat(source); statErr == nil && info.IsDir() {
			err = os.RemoveAll(source)
		} else {
			err = os.Remove(source)
		}
		if err == nil {
			return nil
		}
		if _, statErr := os.Stat(source); statErr != nil {
			return nil
		}
		if cleanerIsNotEmpty(err) && attempt < retries {
			lastErr = err
			time.Sleep(retryDelay)
			continue
		}
		return err
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("remove_after_copy exhausted its retry loop")
}

// cleanerIsNotEmpty reports whether err is a DirectoryNotEmpty failure.
func cleanerIsNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST)
}

// copyRecursive copies a file or directory tree.
func copyRecursive(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyRecursive(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if parent := filepath.Dir(target); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	return cleanerCopyFile(source, target, info.Mode())
}

// cleanerCopyFile copies the contents of source into target, preserving the
// source mode ( `fs::copy`). Runs of zeros are skipped with a seek instead of
// written, so a sparse file stays sparse: a download abandoned at 20% is
// mostly holes, and copying it to a trash on another share (NFS 4.1 has no
// SEEK_HOLE) used to write tens of gigabytes of zeros to the NAS.
func cleanerCopyFile(source, target string, mode os.FileMode) error {
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	if strings.Contains(source, "..") || strings.Contains(target, "..") {
		return fmt.Errorf("invalid path in copy: %s -> %s", source, target)
	}
	defer beginFileOperation(target)()
	reader, err := os.Open(source)
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if err := copySparse(writer, reader); err != nil {
		writer.Close()
		return err
	}
	return writer.Close()
}

// sparseBlock is the granularity of hole detection: big enough to be cheap,
// small enough to catch the unfinished pieces of a torrent.
const sparseBlock = 1 << 20

// copySparse copies reader into writer, seeking over blocks made only of
// zeros. The final Truncate gives the target its full size even when the
// file ends with a hole.
func copySparse(writer *os.File, reader io.Reader) error {
	buffer := make([]byte, sparseBlock)
	zeros := make([]byte, sparseBlock)
	var size int64
	for {
		count, readErr := io.ReadFull(reader, buffer)
		if count > 0 {
			chunk := buffer[:count]
			if bytes.Equal(chunk, zeros[:count]) {
				if _, err := writer.Seek(int64(count), io.SeekCurrent); err != nil {
					return err
				}
			} else if _, err := writer.Write(chunk); err != nil {
				return err
			}
			size += int64(count)
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return writer.Truncate(size)
}

// ResolveExistingTarget decides whether an incoming file must be discarded
// because an equal/better target already exists. It returns true when the
// incoming source was handled as a duplicate.
func ResolveExistingTarget(source, target string, newScore int64, cfg *Config, kind, title string) (bool, error) {
	if !cleanerExists(target) {
		return false, nil
	}
	oldScore := cfg.FileScore(target, kind, title)
	oldQuality := cleanerQualityFromPath(target)
	newQuality := cleanerQualityFromPath(source)
	if !qualityUpgradeAllowed(&newQuality, &oldQuality, newScore, oldScore, cfg.UpgradeMinScoreDiff) {
		if err := handleDuplicate(source, cfg); err != nil {
			return true, err
		}
		return true, nil
	}
	if !cfg.CleanupUpgrades {
		return false, fmt.Errorf(
			"refusing to overwrite existing file without cleanup_upgrades: %s",
			target,
		)
	}
	if err := handleDuplicate(target, cfg); err != nil {
		return false, err
	}
	return false, nil
}

// CleanupOldEpisode removes archived files of the same series/season/episode
// that are clearly inferior to the new file.
func CleanupOldEpisode(cfg *Config, series string, season, episode, newScore int64, newFile, archivePath string) (int, error) {
	removed, err := cleanupOldEpisode(cfg, series, season, episode, newScore, newFile, archivePath, nil)
	logRemovedFiles(cfg, removed, newFile)
	return len(removed), err
}

// CleanupOldEpisodeWithQuality is used when the caller still has the approved
// release metadata. The final archive filename intentionally omits some
// release flags (notably REPACK), so technical tags parsed from the filename
// are merged with the release quality before comparing older files.
func CleanupOldEpisodeWithQuality(cfg *Config, series string, season, episode, newScore int64, newFile, archivePath string, candidateQuality models.Quality) (int, error) {
	removed, err := cleanupOldEpisode(cfg, series, season, episode, newScore, newFile, archivePath, &candidateQuality)
	logRemovedFiles(cfg, removed, newFile)
	return len(removed), err
}

// cleanupOldEpisode removes the archived files that the new release supersedes
// and returns their paths, so the caller can log a single line about the
// upgrade instead of one line per removed file.
func cleanupOldEpisode(cfg *Config, series string, season, episode, newScore int64, newFile, archivePath string, candidateQuality *models.Quality) ([]string, error) {
	if !cfg.CleanupUpgrades || !localPath(archivePath) {
		return nil, nil
	}
	info, err := os.Stat(archivePath)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	pattern, err := utils.CachedRegex(
		`(?i)^(?P<name>.+?)[ ._-]+(?:s(?P<season>\d{1,2})e|(?P<nseason>\d{1,2})x)(?P<episode>\d{1,4})(?:[ ._-]|$)`,
	)
	if err != nil {
		return nil, err
	}
	normalized := NormalizeSeriesName(series)
	preferred := cfg.DefaultLanguage()
	var removed []string
	files, err := VideoFiles(archivePath)
	if err != nil {
		return removed, err
	}
	newName, _ := cleanerName(newFile)
	for _, file := range files {
		name, ok := cleanerName(file)
		if !ok {
			continue
		}
		if file == newFile || name == newName {
			continue
		}
		captures := pattern.FindStringSubmatch(name)
		epMatched := false
		if captures != nil {
			if cleanerEpisodeMatches(pattern, captures, season, episode) {
				fileSeries := cleanerCapture(pattern, captures, "name")
				if SeriesNamesMatch(normalized, fileSeries) {
					epMatched = true
				}
			}
		}
		if !epMatched {
			idSeries, idSeason, idEpisode, ok := extractEpisodeIdentityFromPath(file, name)
			if ok && idSeason == season && idEpisode == episode && SeriesNamesMatch(normalized, idSeries) {
				epMatched = true
			}
		}
		if !epMatched {
			continue
		}
		oldQuality := ParseQuality(name)
		oldScore := cfg.FileScore(file, "series", "")
		newQuality := cleanerQualityFromPath(newFile)
		if candidateQuality != nil {
			newQuality = MergeQuality(newQuality, *candidateQuality)
		}
		if oldQuality.ResolutionRank() > 0 && oldQuality.ResolutionRank() == newQuality.ResolutionRank() {
			switch languageMatchFor(name, preferred) {
			case languagePreferred:
				if languageMatchFor(newName, preferred) == languageOther {
					continue
				}
			case languageOther:
				if languageMatchFor(newName, preferred) == languagePreferred {
					if err := handleDuplicate(file, cfg); err != nil {
						return removed, err
					}
					removeEmptyParents(file, archivePath)
					removed = append(removed, file)
					continue
				}
			}
		}
		if !qualityUpgradeAllowed(&newQuality, &oldQuality, newScore, oldScore, cfg.CleanupMinScoreDiff) {
			continue
		}
		if err := handleDuplicate(file, cfg); err != nil {
			return removed, err
		}
		removeEmptyParents(file, archivePath)
		removed = append(removed, file)
	}
	return removed, nil
}

// DuplicateCandidate is a clearly inferior duplicate found in the library.
type DuplicateCandidate struct {
	Series         string `json:"series"`
	Season         int64  `json:"season"`
	Episode        int64  `json:"episode"`
	Path           string `json:"path"`
	ResolutionRank int    `json:"resolution_rank"`
	BestRank       int    `json:"best_rank"`
}

// inferiorGroupKey groups files by (directory, season, episode).
type inferiorGroupKey struct {
	directory string
	season    int64
	episode   int64
}

// inferiorFile pairs a resolution rank with a file path inside a group.
type inferiorFile struct {
	rank int
	path string
}

// FindInferiorDuplicatesInDir looks for strictly lower-resolution duplicates
// per episode inside a series folder. It is deliberately conservative: files at
// the same resolution (e.g. different languages) and files with an unrecognised
// resolution are never reported.
func FindInferiorDuplicatesInDir(series, archivePath string, protected map[string]struct{}, preferredLanguage string) ([]DuplicateCandidate, error) {
	if !localPath(archivePath) {
		return nil, nil
	}
	info, err := os.Stat(archivePath)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	pattern, err := utils.CachedRegex(
		`(?i)^(?P<name>.+?)[ ._-]+(?:s(?P<s>\d{1,2})e|(?P<ns>\d{1,2})x)(?P<e>\d{1,4})`,
	)
	if err != nil {
		return nil, err
	}
	normalized := NormalizeSeriesName(series)
	// Group by (folder, season, episode): the comparison only happens between
	// files in the same folder, so pack/source folders still seeding are left
	// alone.
	groups := map[inferiorGroupKey][]inferiorFile{}
	files, err := VideoFiles(archivePath)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if _, isProtected := protected[file]; isProtected {
			continue
		}
		name, ok := cleanerName(file)
		if !ok {
			continue
		}
		season := int64(0)
		episode := int64(0)
		matched := false
		captures := pattern.FindStringSubmatch(name)
		if captures != nil {
			fileSeries := cleanerCapture(pattern, captures, "name")
			if fileSeries != "" && SeriesNamesMatch(normalized, fileSeries) {
				seasonRaw := cleanerCapture(pattern, captures, "s", "ns")
				episodeRaw := cleanerCapture(pattern, captures, "e")
				if seasonRaw != "" && episodeRaw != "" {
					s, errS := strconv.ParseInt(seasonRaw, 10, 64)
					e, errE := strconv.ParseInt(episodeRaw, 10, 64)
					if errS == nil && errE == nil {
						season = s
						episode = e
						matched = true
					}
				}
			}
		}
		if !matched {
			idSeries, idSeason, idEpisode, ok := extractEpisodeIdentityFromPath(file, name)
			if ok && SeriesNamesMatch(normalized, idSeries) {
				season = idSeason
				episode = idEpisode
				matched = true
			}
		}
		if !matched {
			continue
		}
		quality := ParseQuality(name)
		rank := quality.ResolutionRank()
		directory := filepath.Dir(file)
		if directory == "" {
			directory = archivePath
		}
		key := inferiorGroupKey{directory: directory, season: season, episode: episode}
		groups[key] = append(groups[key], inferiorFile{rank: rank, path: file})
	}
	candidates := []DuplicateCandidate{}
	for key, grouped := range groups {
		if len(grouped) < 2 {
			continue
		}
		best := 0
		for _, item := range grouped {
			if item.rank > best {
				best = item.rank
			}
		}
		if best <= 0 {
			continue
		}
		bestHasPreferred := false
		for _, item := range grouped {
			if item.rank == best {
				if name, ok := cleanerName(item.path); ok && languageMatchFor(name, preferredLanguage) == languagePreferred {
					bestHasPreferred = true
					break
				}
			}
		}
		for _, item := range grouped {
			rank := item.rank
			file := item.path
			name, _ := cleanerName(file)
			lowerResolution := rank > 0 && rank < best
			wrongLanguage := bestHasPreferred && rank == best && languageMatchFor(name, preferredLanguage) == languageOther
			if lowerResolution || wrongLanguage {
				candidates = append(candidates, DuplicateCandidate{
					Series:         series,
					Season:         key.season,
					Episode:        key.episode,
					Path:           file,
					ResolutionRank: rank,
					BestRank:       best,
				})
			}
		}
	}
	return candidates, nil
}

// CleanupInferiorDuplicatesInDir moves the inferior duplicates found by
// FindInferiorDuplicatesInDir to the trash.
func CleanupInferiorDuplicatesInDir(cfg *Config, series, archivePath string, protected map[string]struct{}) (int, error) {
	if !cfg.CleanupUpgrades {
		return 0, nil
	}
	preferred := cfg.DefaultLanguage()
	candidates, err := FindInferiorDuplicatesInDir(series, archivePath, protected, preferred)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, candidate := range candidates {
		if err := handleDuplicate(candidate.Path, cfg); err != nil {
			return removed, err
		}
		removeEmptyParents(candidate.Path, archivePath)
		removed++
		action := "moved to the trash"
		if cfg.CleanupAction == "delete" {
			action = "deleted"
		}
		logging.Info(fmt.Sprintf("🗑️ %s S%02dE%02d: «%s» %s — a higher-resolution copy of the same episode is in the library",
			candidate.Series, candidate.Season, candidate.Episode, filepath.Base(candidate.Path), action))
		logging.Debug(
			"inferior duplicate removed",
			"series", candidate.Series,
			"season", candidate.Season,
			"episode", candidate.Episode,
			"file", candidate.Path,
			"rank", candidate.ResolutionRank,
			"best", candidate.BestRank,
		)
	}
	return removed, nil
}

// DiscardIfInferior reports whether the incoming file must be discarded because
// an existing file of the same series/season/episode is better. When it returns
// true the incoming file has been handled as a duplicate.
func DiscardIfInferior(cfg *Config, series string, season, episode, newScore int64, newFile, archivePath string) (bool, error) {
	return discardIfInferiorWithQuality(cfg, series, season, episode, newScore, newFile, archivePath, nil)
}

// DiscardIfInferiorWithQuality is DiscardIfInferior with the release quality
// preserved across a display-name rename. In particular, normalized archive
// filenames intentionally omit markers such as REPACK, but that marker remains
// meaningful to the replacement policy.
func DiscardIfInferiorWithQuality(cfg *Config, series string, season, episode, newScore int64, newFile, archivePath string, incoming *models.Quality) (bool, error) {
	return discardIfInferiorWithQuality(cfg, series, season, episode, newScore, newFile, archivePath, incoming)
}

func discardIfInferiorWithQuality(cfg *Config, series string, season, episode, newScore int64, newFile, archivePath string, incoming *models.Quality) (bool, error) {
	if !cfg.CleanupUpgrades || !localPath(archivePath) {
		return false, nil
	}
	info, err := os.Stat(archivePath)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	pattern, err := utils.CachedRegex(
		`(?i)^(?P<name>.+?)[ ._-]+(?:s(?P<season>\d{1,2})e|(?P<nseason>\d{1,2})x)(?P<episode>\d{1,4})(?:[ ._-]|$)`,
	)
	if err != nil {
		return false, err
	}
	normalized := NormalizeSeriesName(series)
	preferred := cfg.DefaultLanguage()
	files, err := VideoFiles(archivePath)
	if err != nil {
		return false, err
	}
	newName, _ := cleanerName(newFile)
	for _, file := range files {
		name, ok := cleanerName(file)
		if !ok {
			continue
		}
		if file == newFile || name == newName {
			continue
		}
		captures := pattern.FindStringSubmatch(name)
		epMatched := false
		if captures != nil {
			if cleanerEpisodeMatches(pattern, captures, season, episode) {
				fileSeries := cleanerCapture(pattern, captures, "name")
				if SeriesNamesMatch(normalized, fileSeries) {
					epMatched = true
				}
			}
		}
		if !epMatched {
			idSeries, idSeason, idEpisode, ok := extractEpisodeIdentityFromPath(file, name)
			if ok && idSeason == season && idEpisode == episode && SeriesNamesMatch(normalized, idSeries) {
				epMatched = true
			}
		}
		if !epMatched {
			continue
		}
		oldQuality := ParseQuality(name)
		oldScore := cfg.FileScore(file, "series", "")
		newQuality := ParseQuality(newName)
		if incoming != nil {
			// A normalized archive name intentionally omits release markers and
			// can also abbreviate source/audio information. Preserve all metadata
			// learned before the rename, using the final filename where it is more
			// specific and the original release as a fallback.
			newQuality = MergeQuality(newQuality, *incoming)
		}
		if oldQuality.ResolutionRank() > 0 && oldQuality.ResolutionRank() == newQuality.ResolutionRank() {
			switch languageMatchFor(name, preferred) {
			case languagePreferred:
				if languageMatchFor(newName, preferred) == languageOther {
					if err := handleDuplicate(newFile, cfg); err != nil {
						return true, err
					}
					removeEmptyParents(newFile, archivePath)
					return true, nil
				}
			case languageOther:
				if languageMatchFor(newName, preferred) == languagePreferred {
					return false, nil
				}
			}
		}
		if !qualityUpgradeAllowed(&newQuality, &oldQuality, newScore, oldScore, cfg.CleanupMinScoreDiff) &&
			oldScore >= saturatingAddInt64(newScore, cfg.CleanupMinScoreDiff) {
			if err := handleDuplicate(newFile, cfg); err != nil {
				return true, err
			}
			return true, nil
		}
	}
	return false, nil
}

// cleanupOldMovie removes archived files of the same movie that are clearly
// inferior to the new file and returns their paths, so the caller can log a
// single line about the upgrade.
func cleanupOldMovie(cfg *Config, movie string, year *int64, newScore int64, newFile, archivePath string) ([]string, error) {
	if !cfg.CleanupUpgrades || !localPath(archivePath) {
		return nil, nil
	}
	info, err := os.Stat(archivePath)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	words := []string{}
	for _, word := range strings.Fields(NormalizeSeriesName(movie)) {
		if len(word) > 1 {
			words = append(words, word)
		}
	}
	preferred := cfg.DefaultLanguage()
	var removed []string
	yearPattern, err := utils.CachedRegex(`\b(19\d{2}|20\d{2})\b`)
	if err != nil {
		return nil, err
	}
	files, err := VideoFiles(archivePath)
	if err != nil {
		return removed, err
	}
	newName, _ := cleanerName(newFile)
	for _, file := range files {
		name, ok := cleanerName(file)
		if !ok {
			continue
		}
		if file == newFile || name == newName {
			continue
		}
		normalizedName := NormalizeSeriesName(name)
		if !cleanerContainsAllWords(normalizedName, words) {
			continue
		}
		if year != nil {
			capture := yearPattern.FindStringSubmatch(name)
			if capture == nil || capture[1] == "" {
				continue
			}
			oldYear, err := strconv.ParseInt(capture[1], 10, 64)
			if err != nil || absInt64(oldYear-*year) > 1 {
				continue
			}
		}
		oldQuality := ParseQuality(name)
		oldScore := cfg.FileScore(file, "movie", movie)
		newQuality := ParseQuality(newName)
		if oldQuality.ResolutionRank() > 0 && oldQuality.ResolutionRank() == newQuality.ResolutionRank() {
			switch languageMatchFor(name, preferred) {
			case languagePreferred:
				if languageMatchFor(newName, preferred) == languageOther {
					continue
				}
			case languageOther:
				if languageMatchFor(newName, preferred) == languagePreferred {
					if err := handleDuplicate(file, cfg); err != nil {
						return removed, err
					}
					removeEmptyParents(file, archivePath)
					removed = append(removed, file)
					continue
				}
			}
		}
		if !qualityUpgradeAllowed(&newQuality, &oldQuality, newScore, oldScore, cfg.CleanupMinScoreDiff) {
			continue
		}
		if err := handleDuplicate(file, cfg); err != nil {
			return removed, err
		}
		removeEmptyParents(file, archivePath)
		removed = append(removed, file)
	}
	return removed, nil
}

// DiscardIfInferiorMovie reports whether the incoming movie file must be
// discarded because an existing file of the same movie is better.
func DiscardIfInferiorMovie(cfg *Config, movie string, year *int64, newScore int64, newFile, archivePath string) (bool, error) {
	if !cfg.CleanupUpgrades || !localPath(archivePath) {
		return false, nil
	}
	info, err := os.Stat(archivePath)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	words := []string{}
	for _, word := range strings.Fields(NormalizeSeriesName(movie)) {
		if len(word) > 1 {
			words = append(words, word)
		}
	}
	yearPattern, err := utils.CachedRegex(`\b(19\d{2}|20\d{2})\b`)
	if err != nil {
		return false, err
	}
	preferred := cfg.DefaultLanguage()
	files, err := VideoFiles(archivePath)
	if err != nil {
		return false, err
	}
	newName, _ := cleanerName(newFile)
	for _, file := range files {
		name, ok := cleanerName(file)
		if !ok {
			continue
		}
		if file == newFile || name == newName {
			continue
		}
		normalizedName := NormalizeSeriesName(name)
		if !cleanerContainsAllWords(normalizedName, words) {
			continue
		}
		if year != nil {
			capture := yearPattern.FindStringSubmatch(name)
			if capture == nil || capture[1] == "" {
				continue
			}
			oldYear, err := strconv.ParseInt(capture[1], 10, 64)
			if err != nil || absInt64(oldYear-*year) > 1 {
				continue
			}
		}
		oldQuality := ParseQuality(name)
		oldScore := cfg.FileScore(file, "movie", movie)
		newQuality := ParseQuality(newName)
		if oldQuality.ResolutionRank() > 0 && oldQuality.ResolutionRank() == newQuality.ResolutionRank() {
			switch languageMatchFor(name, preferred) {
			case languagePreferred:
				if languageMatchFor(newName, preferred) == languageOther {
					if err := handleDuplicate(newFile, cfg); err != nil {
						return true, err
					}
					removeEmptyParents(newFile, archivePath)
					return true, nil
				}
			case languageOther:
				if languageMatchFor(newName, preferred) == languagePreferred {
					return false, nil
				}
			}
		}
		if !qualityUpgradeAllowed(&newQuality, &oldQuality, newScore, oldScore, cfg.CleanupMinScoreDiff) &&
			oldScore >= saturatingAddInt64(newScore, cfg.CleanupMinScoreDiff) {
			if err := handleDuplicate(newFile, cfg); err != nil {
				return true, err
			}
			return true, nil
		}
	}
	return false, nil
}

// cleanerName implements `Path::file_name`, returning false when the path
// has no final component (“, `.`, `..`, `/`).
func cleanerName(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	base := filepath.Base(path)
	if base == "." || base == ".." || base == string(os.PathSeparator) {
		return "", false
	}
	return base, true
}

// cleanerFileStem implements `Path::file_stem`.
func cleanerFileStem(path string) string {
	base, ok := cleanerName(path)
	if !ok {
		return ""
	}
	if index := strings.LastIndex(base, "."); index > 0 {
		return base[:index]
	}
	return base
}

// cleanerFileExtension implements `Path::extension`, dotted and empty when
// there is none.
func cleanerFileExtension(path string) string {
	base, ok := cleanerName(path)
	if !ok {
		return ""
	}
	if index := strings.LastIndex(base, "."); index > 0 {
		return base[index:]
	}
	return ""
}

// cleanerQualityFromPath parses the quality of a path's file name, falling back
// to the zero Quality when there is none ( `unwrap_or_default`).
func cleanerQualityFromPath(path string) models.Quality {
	if name, ok := cleanerName(path); ok {
		return ParseQuality(name)
	}
	return models.Quality{}
}

// cleanerCapture returns the first non-empty named capture among names.
func cleanerCapture(pattern *regexp.Regexp, captures []string, names ...string) string {
	for _, name := range names {
		if index := pattern.SubexpIndex(name); index >= 0 && index < len(captures) && captures[index] != "" {
			return captures[index]
		}
	}
	return ""
}

// cleanerEpisodeMatches reports whether the captured season/episode match.
func cleanerEpisodeMatches(pattern *regexp.Regexp, captures []string, season, episode int64) bool {
	seasonRaw := cleanerCapture(pattern, captures, "season", "nseason")
	if seasonRaw == "" {
		return false
	}
	seasonValue, err := strconv.ParseInt(seasonRaw, 10, 64)
	if err != nil || seasonValue != season {
		return false
	}
	episodeRaw := cleanerCapture(pattern, captures, "episode")
	if episodeRaw == "" {
		return false
	}
	episodeValue, err := strconv.ParseInt(episodeRaw, 10, 64)
	if err != nil || episodeValue != episode {
		return false
	}
	return true
}

// extractEpisodeIdentityFromPath extracts the series, season and episode of an
// archived file. When the file name itself lacks the series title (common in
// season subfolders like "Stagione 01/S01E02.mkv" or "Stagione 01/02 - Titolo.mkv"),
// it falls back to inspecting the ancestor directories.
func extractEpisodeIdentityFromPath(filePath, fileName string) (series string, season int64, episode int64, ok bool) {
	primaryPattern, err := utils.CachedRegex(`(?i)^(?P<name>.+?)[ ._-]+(?:s(?P<s>\d{1,2})e|(?P<ns>\d{1,2})x)(?P<e>\d{1,4})(?:[ ._-]|$)`)
	if err == nil {
		if captures := primaryPattern.FindStringSubmatch(fileName); captures != nil {
			sName := cleanerCapture(primaryPattern, captures, "name")
			sRaw := cleanerCapture(primaryPattern, captures, "s", "ns")
			eRaw := cleanerCapture(primaryPattern, captures, "e")
			if sName != "" && sRaw != "" && eRaw != "" {
				sVal, errS := strconv.ParseInt(sRaw, 10, 64)
				eVal, errE := strconv.ParseInt(eRaw, 10, 64)
				if errS == nil && errE == nil {
					return sName, sVal, eVal, true
				}
			}
		}
	}
	sePattern, err := utils.CachedRegex(`(?i)(?:s(?P<s>\d{1,2})e|(?P<ns>\d{1,2})x)(?P<e>\d{1,4})`)
	if err == nil {
		if captures := sePattern.FindStringSubmatch(fileName); captures != nil {
			sRaw := cleanerCapture(sePattern, captures, "s", "ns")
			eRaw := cleanerCapture(sePattern, captures, "e")
			if sRaw != "" && eRaw != "" {
				sVal, errS := strconv.ParseInt(sRaw, 10, 64)
				eVal, errE := strconv.ParseInt(eRaw, 10, 64)
				if errS == nil && errE == nil {
					parentDir := filepath.Dir(filePath)
					parentName := filepath.Base(parentDir)
					seasonFolderPattern, _ := utils.CachedRegex(`(?i)^(?:season|stagione|s)\s*0*(\d{1,2})$`)
					if seasonFolderPattern != nil && seasonFolderPattern.MatchString(parentName) {
						grandParent := filepath.Dir(parentDir)
						return filepath.Base(grandParent), sVal, eVal, true
					}
					return parentName, sVal, eVal, true
				}
			}
		}
	}
	seasonFolderPattern, err := utils.CachedRegex(`(?i)^(?:season|stagione|s)\s*0*(\d{1,2})$`)
	if err == nil {
		parentDir := filepath.Dir(filePath)
		parentName := filepath.Base(parentDir)
		matchSeason := seasonFolderPattern.FindStringSubmatch(parentName)
		if matchSeason != nil {
			if sVal, errS := strconv.ParseInt(matchSeason[1], 10, 64); errS == nil {
				bareEpPattern, _ := utils.CachedRegex(`(?i)^(?:e(?:pisode)?[ ._-]?)?0*(\d{1,4})(?:[ ._-]|$)`)
				if bareEpPattern != nil {
					if matchEp := bareEpPattern.FindStringSubmatch(fileName); matchEp != nil {
						if eVal, errE := strconv.ParseInt(matchEp[1], 10, 64); errE == nil {
							grandParent := filepath.Dir(parentDir)
							return filepath.Base(grandParent), sVal, eVal, true
						}
					}
				}
			}
		}
	}
	return "", 0, 0, false
}

// cleanerContainsAllWords reports whether value contains every word.
func cleanerContainsAllWords(value string, words []string) bool {
	for _, word := range words {
		if !strings.Contains(value, word) {
			return false
		}
	}
	return true
}

// cleanerExists reports whether a path exists ( `Path::exists`).
func cleanerExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// saturatingAddInt64 reproduces the `i64::saturating_add`.
func saturatingAddInt64(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	if b < 0 && a < math.MinInt64-b {
		return math.MinInt64
	}
	return a + b
}

// SweepStaleTempFiles cleans up abandoned `.gextto-copy-*` and `*.gextto-part` files
// left behind by aborted atomic copies or daemon crashes. Only files older than
// minAge (2 hours) are removed so in-progress transfers are never disturbed.
func SweepStaleTempFiles(cfg *Config) int {
	if cfg == nil {
		return 0
	}
	roots := []string{}
	addRoot := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			for _, r := range roots {
				if SamePath(r, p) {
					return
				}
			}
			roots = append(roots, p)
		}
	}
	addRoot(cfg.LibtorrentDir)
	if cfg.LibtorrentTempDir != nil {
		addRoot(*cfg.LibtorrentTempDir)
	}
	if cfg.RamdiskDir() != nil {
		addRoot(*cfg.RamdiskDir())
	}
	if cfg.ArchiveRoot != nil {
		addRoot(*cfg.ArchiveRoot)
	}
	for _, s := range cfg.Series {
		addRoot(s.ArchivePath)
	}

	cleaned := 0
	now := time.Now()
	const minAge = 2 * time.Hour

	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				rel, relErr := filepath.Rel(root, path)
				if relErr == nil && strings.Count(rel, string(os.PathSeparator)) > 3 {
					return filepath.SkipDir
				}
				return nil
			}
			name := info.Name()
			isStaleTemp := (strings.HasPrefix(name, ".") && strings.Contains(name, ".gextto-copy-")) ||
				(strings.HasPrefix(name, ".") && strings.Contains(name, ".gextto-trash-")) ||
				strings.HasSuffix(name, ".gextto-part")
			if isStaleTemp && now.Sub(info.ModTime()) > minAge {
				if remErr := os.Remove(path); remErr == nil {
					cleaned++
					logging.Info("🧹 removed stale temporary copy file",
						"file", path, "size", logging.HumanBytesI64(info.Size()), "age", now.Sub(info.ModTime()).Round(time.Minute).String())
				}
			}
			return nil
		})
	}
	return cleaned
}
