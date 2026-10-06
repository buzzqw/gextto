package gextto

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// videoExtensions is the set of container extensions accepted as video files by
// `video_files` (implementation of the match arm).
var videoExtensions = map[string]bool{
	"mkv":  true,
	"mp4":  true,
	"avi":  true,
	"m4v":  true,
	"mov":  true,
	"ts":   true,
	"webm": true,
	"wmv":  true,
}

// isVideoExtension reports whether a file name ends in a recognised video
// extension.
func isVideoExtension(name string) bool {
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	return videoExtensions[extension]
}

// tagMatchesRelease implements `tag_matches_release`: a rule tag matches a
// release when it names its source/kind or one of the legacy aliases.
func tagMatchesRelease(tag string, release *models.Release) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return false
	}
	if tag == strings.ToLower(release.Source) || tag == strings.ToLower(release.Kind) {
		return true
	}
	var aliases []string
	switch release.Kind {
	case "movie":
		aliases = []string{"film", "movies", "movie"}
	case "series":
		aliases = []string{"serie", "series", "serie tv", "tv", "show", "tv show"}
	}
	for _, alias := range aliases {
		if alias == tag {
			return true
		}
	}
	return false
}

// ruleString returns the string value stored under key, or "" when the key is
// absent or not a JSON string (mirrors `Value::as_str().unwrap_or_default()`).
func ruleString(rule map[string]any, key string) string {
	if value, ok := rule[key]; ok {
		if text, ok := value.(string); ok {
			return text
		}
	}
	return ""
}

// parseTagDirRules parses the `tag_dir_rules` setting; invalid JSON yields none.
func parseTagDirRules(raw string) []map[string]any {
	var rules []map[string]any
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil
	}
	return rules
}

// ConfiguredDestinationFor resolves only an explicitly configured archive
// destination. It deliberately does not fall back to the download directory:
// folder-shaped downloads must stay there when no NAS/archive is configured.
func ConfiguredDestinationFor(release *models.Release, cfg *Config) (string, bool) {
	if raw, ok := cfg.Settings["tag_dir_rules"]; ok {
		if rules := parseTagDirRules(raw); rules != nil {
			for _, rule := range rules {
				if !tagMatchesRelease(ruleString(rule, "tag"), release) {
					continue
				}
				if finalDir := strings.TrimSpace(ruleString(rule, "final_dir")); finalDir != "" {
					return finalDir, true
				}
				break
			}
		}
	}
	if release.Kind == "series" && release.Series != nil {
		if series := cfg.FindSeriesMatch(*release.Series, release.Season); series != nil {
			if destination := cfg.ResolveArchivePath(series); destination != nil {
				if series.SeasonSubfolders && release.Season != nil {
					return filepath.Join(*destination, fmt.Sprintf("Stagione %02d", *release.Season)), true
				}
				return *destination, true
			}
		}
	}
	if cfg.ArchiveRoot != nil {
		if destination := strings.TrimSpace(*cfg.ArchiveRoot); destination != "" {
			return destination, true
		}
	}
	return "", false
}

// DestinationFor resolves the archive folder of a release (implementation of
// `destination_for`). Its historical download-directory fallback remains for
// direct single-file downloads; folder-shaped downloads use
// ConfiguredDestinationFor when they must distinguish a real archive from the
// download area.
func DestinationFor(release *models.Release, cfg *Config) (string, bool) {
	if destination, ok := ConfiguredDestinationFor(release, cfg); ok {
		return destination, true
	}
	return cfg.LibtorrentDir, true
}

// DownloadDirFor returns the per-tag temporary download directory for a
// release, if configured (implementation of `download_dir_for`).
func DownloadDirFor(release *models.Release, cfg *Config) (string, bool) {
	raw, ok := cfg.Settings["tag_dir_rules"]
	if !ok {
		return "", false
	}
	rules := parseTagDirRules(raw)
	if rules == nil {
		return "", false
	}
	for _, rule := range rules {
		if !tagMatchesRelease(ruleString(rule, "tag"), release) {
			continue
		}
		temp := strings.TrimSpace(ruleString(rule, "temp_dir"))
		if temp == "" {
			return "", false
		}
		if info, err := os.Stat(temp); err != nil || !info.IsDir() {
			return "", false
		}
		return temp, true
	}
	return "", false
}

// CategoryDirs returns the temporary and final directories of a category from
// the "Percorsi NAS per categoria (tag)" rules. `temp` is only reported when it
// exists as a directory (implementation of `category_dirs`).
func CategoryDirs(cfg *Config, category string) (temp string, tempOK bool, finalDir string, finalDirOK bool) {
	raw, ok := cfg.Settings["tag_dir_rules"]
	if !ok {
		return
	}
	rules := parseTagDirRules(raw)
	for _, rule := range rules {
		tag := ruleString(rule, "tag")
		if !strings.EqualFold(strings.TrimSpace(tag), category) {
			continue
		}
		tempValue := strings.TrimSpace(ruleString(rule, "temp_dir"))
		if tempValue != "" {
			if info, err := os.Stat(tempValue); err == nil && info.IsDir() {
				temp = tempValue
				tempOK = true
			}
		}
		finalValue := strings.TrimSpace(ruleString(rule, "final_dir"))
		if finalValue != "" {
			finalDir = finalValue
			finalDirOK = true
		}
		return
	}
	return
}

// CategoryDownloadDir prefers the final destination of a category, then the
// temporary one (implementation of `category_download_dir`).
func CategoryDownloadDir(cfg *Config, category string) (string, bool) {
	temp, tempOK, finalDir, finalOK := CategoryDirs(cfg, category)
	if finalOK {
		return finalDir, true
	}
	if tempOK {
		return temp, true
	}
	return "", false
}

// ComicDownloadDir resolves the comics download folder, accepting both `Comic`
// and `Fumetto` as the category name (implementation of `comic_download_dir`).
func ComicDownloadDir(cfg *Config) (string, bool) {
	if value, ok := CategoryDownloadDir(cfg, "Comic"); ok {
		return value, true
	}
	return CategoryDownloadDir(cfg, "Fumetto")
}

// canonicalizePath returns the absolute, symlink-resolved form of path,
// mirroring `fs::canonicalize`.
func canonicalizePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// pathStartsWith reports whether path lies at or below prefix, comparing whole
// path components like `Path::starts_with`.
func pathStartsWith(path, prefix string) bool {
	relative, err := filepath.Rel(prefix, path)
	if err != nil {
		return false
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// SamePath reports whether two paths refer to the same filesystem location
// (implementation of `same_path`).
func SamePath(left, right string) bool {
	if left == right || filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftPath, leftErr := canonicalizePath(left)
	rightPath, rightErr := canonicalizePath(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return leftPath == rightPath
}

// saturatingAdd adds two int64 values, clamping at the i64 bounds.
func saturatingAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	if right < 0 && left < math.MinInt64-right {
		return math.MinInt64
	}
	return left + right
}

// SizeOfPath returns the total size of a file or directory tree without
// following symlinks (implementation of `size_of_path`).
func SizeOfPath(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("completed torrent path does not exist: %s", path)
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	total := int64(0)
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := filepath.Join(path, entry.Name())
		switch {
		case entry.IsDir():
			size, err := SizeOfPath(child)
			if err != nil {
				return 0, err
			}
			total = saturatingAdd(total, size)
		case entry.Type().IsRegular():
			childInfo, err := entry.Info()
			if err != nil {
				return 0, err
			}
			total = saturatingAdd(total, childInfo.Size())
		}
	}
	return total, nil
}

// completionComponent mimics `Path::file_name`: it returns the final component
// unless the path is empty or terminates in `.`/`..`.
func completionComponent(name string) (string, bool) {
	trimmed := strings.TrimRight(name, "/")
	if trimmed == "" {
		return "", false
	}
	index := strings.LastIndexByte(trimmed, '/')
	component := trimmed[index+1:]
	if component == "" || component == "." || component == ".." {
		return "", false
	}
	return component, true
}

// CompletionPath is the path of the completed download inside the torrent's
// `save_path` (implementation of `completion_path`). `event.name` is untrusted: only its
// final component is kept, and a missing name never falls back to the shared
// root.
func CompletionPath(event *models.TorrentEvent) string {
	root := event.SavePath
	if name, ok := completionComponent(event.Name); ok {
		return filepath.Join(root, name)
	}
	return filepath.Join(root, fmt.Sprintf(".gextto-missing-%s", event.Hash))
}

// ValidateDestination creates the post-processing destination (implementation of
// `validate_destination`). It also verifies that the target directory is writable,
// catching read-only mounts, network timeouts or disconnected NAS shares.
func ValidateDestination(destination string) error {
	if destination == "" {
		return errors.New("empty post-processing destination")
	}
	if strings.ContainsRune(destination, '\x00') {
		return errors.New("invalid post-processing destination")
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	// Verify directory writability with a temporary probe to catch read-only mounts or hung NAS.
	probe := filepath.Join(destination, fmt.Sprintf(".gextto-probe-%s", randomToken()))
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("destination directory not writable: %w", err)
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return nil
}

// ValidateDestinationFrom creates the destination and refuses to write inside
// the source tree (implementation of `validate_destination_from`).
func ValidateDestinationFrom(source, destination string) error {
	if err := ValidateDestination(destination); err != nil {
		return err
	}
	sourceRoot, err := canonicalizePath(source)
	if err != nil {
		return err
	}
	destinationRoot, err := canonicalizePath(destination)
	if err != nil {
		return err
	}
	if destinationRoot == sourceRoot || pathStartsWith(destinationRoot, sourceRoot) {
		return fmt.Errorf(
			"refusing to write post-processing output inside its source tree: %s",
			destination,
		)
	}
	return nil
}

// copyFiles copies the given files flat into destination, skipping targets that
// already exist (implementation of `copy_files`).
func copyFiles(files []string, source, destination string) ([]string, error) {
	if err := ValidateDestinationFrom(source, destination); err != nil {
		return nil, err
	}
	copied := []string{}
	for _, file := range files {
		name := filepath.Base(file)
		if name == "" || name == "." || name == ".." {
			return nil, errors.New("season pack file has no name")
		}
		target := filepath.Join(destination, name)
		if _, err := os.Stat(target); err == nil {
			copied = append(copied, target)
			continue
		}
		if err := copyFileAtomically(file, target); err != nil {
			return nil, err
		}
		copied = append(copied, target)
	}
	if len(copied) == 0 {
		return nil, fmt.Errorf("season pack contains no video files: %s", source)
	}
	return copied, nil
}

// CopyPackFiles copies every video file of a season pack flat into destination
// without removing the source (implementation of `copy_pack_files`).
func CopyPackFiles(source, destination string) ([]string, error) {
	files, err := VideoFiles(source)
	if err != nil {
		return nil, err
	}
	return copyFiles(files, source, destination)
}

// PackSourceFile pairs a pack file path with its resolved season/episode.
type PackSourceFile struct {
	Path    string
	Season  int64
	Episode int64
}

// submatchNamed returns the named capture from a match, or "".
func submatchNamed(pattern *regexp.Regexp, match []string, name string) string {
	index := pattern.SubexpIndex(name)
	if index < 0 || index >= len(match) {
		return ""
	}
	return match[index]
}

// containsPath reports whether files already contains path.
func containsPath(files []PackSourceFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

// sameIntSet reports whether two int64 sets hold the same members.
func sameIntSet(left, right map[int64]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if !right[key] {
			return false
		}
	}
	return true
}

// fileStem returns a path's file name without its final extension.
func fileStem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// MatchingPackFiles resolves the episode identity of every file that can safely
// be imported from a pack.
func MatchingPackFiles(source string, release *models.Release) ([]PackSourceFile, error) {
	episodePattern, err := utils.CachedRegex(
		`(?i)(?:s(?P<season>\d{1,2})e|(?P<nseason>\d{1,2})x)(?P<episode>\d{1,4})`,
	)
	if err != nil {
		return nil, err
	}
	files, err := VideoFiles(source)
	if err != nil {
		return nil, err
	}
	matched := []PackSourceFile{}
	for _, file := range files {
		name := filepath.Base(file)
		match := episodePattern.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		seasonText := submatchNamed(episodePattern, match, "season")
		if seasonText == "" {
			seasonText = submatchNamed(episodePattern, match, "nseason")
		}
		if seasonText == "" {
			continue
		}
		season, err := strconv.ParseInt(seasonText, 10, 64)
		if err != nil {
			continue
		}
		episode, err := strconv.ParseInt(submatchNamed(episodePattern, match, "episode"), 10, 64)
		if err != nil {
			continue
		}
		if len(release.EpisodeRange) != 0 &&
			!containsInt64(release.EpisodeRange, 0) &&
			!containsInt64(release.EpisodeRange, episode) {
			continue
		}
		if release.Season == nil || season != *release.Season {
			continue
		}
		matched = append(matched, PackSourceFile{Path: file, Season: season, Episode: episode})
	}
	expected := map[int64]bool{}
	for _, episode := range release.EpisodeRange {
		if episode > 0 {
			expected[episode] = true
		}
	}
	// A complete pack has no trustworthy explicit episode list: import only
	// files carrying their own season/episode identity.
	if len(expected) == 0 || release.Season == nil {
		return matched, nil
	}
	found := map[int64]bool{}
	for _, file := range matched {
		found[file.Episode] = true
	}
	bareEpisode, err := utils.CachedRegex(`(?i)^(?:e(?:pisode)?[ ._-]?)?0*(\d{1,4})$`)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if containsPath(matched, file) {
			continue
		}
		stem := fileStem(file)
		match := bareEpisode.FindStringSubmatch(stem)
		if match == nil {
			continue
		}
		episode, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return []PackSourceFile{}, nil
		}
		if !expected[episode] || found[episode] {
			return []PackSourceFile{}, nil
		}
		found[episode] = true
		matched = append(matched, PackSourceFile{
			Path:    file,
			Season:  *release.Season,
			Episode: episode,
		})
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Episode < matched[j].Episode })
	deduped := make([]PackSourceFile, 0, len(matched))
	for index, file := range matched {
		if index > 0 && matched[index-1].Episode == file.Episode {
			continue
		}
		deduped = append(deduped, file)
	}
	matched = deduped
	if !sameIntSet(found, expected) {
		return []PackSourceFile{}, nil
	}
	return matched, nil
}

// CopyMatchingPackFiles copies only the pack files whose identity agrees with
// the release metadata (implementation of `copy_matching_pack_files`).
func CopyMatchingPackFiles(source, destination string, release *models.Release) ([]string, error) {
	files, err := MatchingPackFiles(source, release)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf(
			"season pack files do not match declared season: %s",
			source,
		)
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return copyFiles(paths, source, destination)
}

// PackFileResult is the outcome of processing one staged pack episode.
type PackFileResult struct {
	Episode      int64
	Path         string
	SizeBytes    int64
	QualityScore int64
	Discarded    bool
	Upgrade      bool
	TrashCount   int
}

// BestEpisodeFile returns the best-quality video file in dir matching the given
// season/episode (implementation of `best_episode_file`).
func BestEpisodeFile(dir string, season, episode int64) (string, bool) {
	files, err := VideoFiles(dir)
	if err != nil {
		return "", false
	}
	return BestEpisodeFileFromFiles(files, season, episode)
}

// BestEpisodeFileFromFiles is BestEpisodeFile over an already-enumerated file
// list, so callers that process many episodes can walk the directory once
// instead of once per episode.
func BestEpisodeFileFromFiles(files []string, season, episode int64) (string, bool) {
	best := ""
	bestScore := int64(0)
	haveBest := false
	for _, file := range files {
		name := filepath.Base(file)
		if !filenameMatchesEpisode(name, season, episode) {
			continue
		}
		quality := ParseQuality(name)
		score := quality.Score()
		if haveBest && bestScore >= score {
			continue
		}
		best = file
		bestScore = score
		haveBest = true
	}
	if !haveBest {
		return "", false
	}
	return best, true
}

// randomToken returns a random hex token used for temporary copy names.
func randomToken() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buffer)
}

// copyFileAtomically copies a file through a hidden sibling and publishes it
// with one rename (implementation of `copy_file_atomically`). Both paths are
// normalized first so a crafted name cannot introduce “..“ traversal.
func copyFileAtomically(source, target string) error {
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	defer beginFileOperation(target)()
	parent := filepath.Dir(target)
	if parent == "" {
		return fmt.Errorf("target has no parent: %s", target)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	name := filepath.Base(target)
	temporary := filepath.Join(
		parent,
		fmt.Sprintf(".%s.gextto-copy-%s", name, randomToken()),
	)
	result := func() error {
		// Open the source through an os.Root so its name cannot escape the
		// directory it lives in, even if it carries path separators.
		sourceRoot, err := os.OpenRoot(filepath.Dir(source))
		if err != nil {
			return err
		}
		defer sourceRoot.Close()
		sourceName := filepath.Base(source)
		input, err := sourceRoot.Open(sourceName)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		copied, err := io.Copy(output, input)
		if err != nil {
			output.Close()
			return err
		}
		if err := output.Sync(); err != nil {
			output.Close()
			return err
		}
		if err := output.Close(); err != nil {
			return err
		}
		sourceInfo, err := sourceRoot.Stat(sourceName)
		if err != nil {
			return err
		}
		targetInfo, err := os.Stat(temporary)
		if err != nil {
			return err
		}
		if copied != sourceInfo.Size() || targetInfo.Size() != sourceInfo.Size() {
			return fmt.Errorf(
				"atomic copy size mismatch for %s: source=%d copied=%d target=%d",
				source,
				sourceInfo.Size(),
				copied,
				targetInfo.Size(),
			)
		}
		return os.Rename(temporary, target)
	}()
	if result != nil {
		_ = os.Remove(temporary)
	}
	return result
}

// moveAcrossDevices renames source to target, falling back to a copy + remove
// on EXDEV (implementation of `move_across_devices`).
func moveAcrossDevices(source, target string) error {
	err := os.Rename(source, target)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EXDEV) {
		if err := copyFileAtomically(source, target); err != nil {
			return err
		}
		return os.Remove(source)
	}
	return err
}

// StagePackFile copies one season-pack episode into the destination through a
// temporary `.gextto-part` file (implementation of `stage_pack_file`).
// destinationFiles is the pre-enumerated content of the destination directory,
// so the caller can walk it once for the whole pack instead of once per episode.
func StagePackFile(
	file *PackSourceFile,
	source, destination string,
	destinationFiles []string,
	cfg *Config,
	releaseQualityScore int64,
) (string, bool, error) {
	name := filepath.Base(file.Path)
	if name == "" || name == "." || name == ".." {
		return "", false, nil
	}
	season := file.Season
	episode := file.Episode
	if existing, ok := BestEpisodeFileFromFiles(destinationFiles, season, episode); ok {
		existingScore := cfg.FileScore(existing, "series", "")
		incomingScore := cfg.FileScore(file.Path, "series", "")
		if existingScore >= maxInt64(incomingScore, releaseQualityScore) {
			return "", false, nil
		}
	}
	identityPattern, err := utils.CachedRegex(`(?i)(?:s\d{1,2}e|\d{1,2}x)\d{1,4}`)
	if err != nil {
		return "", false, err
	}
	targetName := name
	if !identityPattern.MatchString(name) {
		targetName = fmt.Sprintf(".gextto-pack-S%02dE%02d-%s", season, episode, name)
	}
	target := filepath.Join(destination, targetName)
	if _, err := os.Stat(target); err == nil {
		return target, true, nil
	}
	// A preallocated-but-never-written or truncated pack file has the right
	// size but is not a real video: never import it into the library.
	if err := validateCompletedFile(file.Path); err != nil {
		logging.Warn("season pack file failed integrity validation; skipping",
			"file", file.Path, "error", err.Error())
		return "", false, nil
	}
	if err := ValidateDestinationFrom(source, destination); err != nil {
		return "", false, err
	}
	temporary := filepath.Join(destination, name+".gextto-part")
	defer beginFileOperation(file.Path)()
	_ = os.Remove(temporary)
	input, err := os.Open(file.Path)
	if err != nil {
		return "", false, err
	}
	defer input.Close()
	output, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", false, err
	}
	copied, err := io.Copy(output, input)
	if err != nil {
		output.Close()
		_ = os.Remove(temporary)
		return "", false, err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		_ = os.Remove(temporary)
		return "", false, err
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(temporary)
		return "", false, err
	}
	sourceInfo, err := os.Stat(file.Path)
	if err != nil {
		_ = os.Remove(temporary)
		return "", false, err
	}
	temporaryInfo, err := os.Stat(temporary)
	if err != nil {
		_ = os.Remove(temporary)
		return "", false, err
	}
	if copied != sourceInfo.Size() || temporaryInfo.Size() != sourceInfo.Size() {
		_ = os.Remove(temporary)
		return "", false, fmt.Errorf(
			"season pack copy size mismatch for %s: source=%d copied=%d target=%d",
			file.Path,
			sourceInfo.Size(),
			copied,
			temporaryInfo.Size(),
		)
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return "", false, err
	}
	return target, true, nil
}

// PackInput pairs a staged file path with its pack identity, mirroring the
// `(PathBuf, PackSourceFile)` tuple.
type PackInput struct {
	Path   string
	Source PackSourceFile
}

// ProcessPackFiles renames, deduplicates and scores each staged pack episode
// (implementation of `process_pack_files`).
func ProcessPackFiles(
	ctx context.Context,
	files []PackInput,
	release *models.Release,
	cfg *Config,
	tmdb *TmdbClient,
) ([]PackFileResult, error) {
	results := []PackFileResult{}
	series := ""
	if release.Series != nil {
		series = *release.Series
	}
	for _, input := range files {
		packFile := input.Source
		season := packFile.Season
		episode := packFile.Episode
		episodeRelease := *release
		episodeRelease.IsPack = false
		episodeValue := episode
		episodeRelease.Episode = &episodeValue
		episodeRelease.EpisodeRange = []int64{episodeValue}
		actual := input.Path
		if _, err := os.Stat(actual); err != nil {
			directory := filepath.Dir(actual)
			if found, ok := BestEpisodeFile(directory, season, episode); ok {
				actual = found
			}
		}
		renamed, err := RenameEpisode(ctx, actual, &episodeRelease, cfg, tmdb)
		if errors.Is(err, ErrInferiorDuplicate) {
			results = append(results, PackFileResult{
				Episode:   episode,
				Path:      actual,
				Discarded: true,
			})
			continue
		} else if err != nil {
			return nil, err
		}
		finalPath := actual
		if renamed != "" {
			finalPath = renamed
		}
		archive := filepath.Dir(finalPath)
		score := cfg.ReleaseScore(&episodeRelease)
		trashCount := 0
		discarded, err := DiscardIfInferior(
			cfg,
			series,
			season,
			episode,
			score,
			finalPath,
			archive,
		)
		if err != nil {
			return nil, err
		}
		if !discarded {
			removed, err := CleanupOldEpisodeWithQuality(
				cfg,
				series,
				season,
				episode,
				score,
				finalPath,
				archive,
				episodeRelease.Quality,
			)
			if err != nil {
				return nil, err
			}
			trashCount = removed
		}
		qualityScore := cfg.FileScore(finalPath, "series", series)
		if score > qualityScore {
			qualityScore = score
		}
		size, _ := SizeOfPath(finalPath)
		results = append(results, PackFileResult{
			Episode:      episode,
			Path:         finalPath,
			SizeBytes:    size,
			QualityScore: qualityScore,
			Discarded:    discarded,
			Upgrade:      !discarded && trashCount > 0,
			TrashCount:   trashCount,
		})
	}
	return results, nil
}

// RenameEpisode renames an episode file according to the configured template
// and returns the new path, or "" when there is nothing to do (implementation of
// `rename_episode`).
func RenameEpisode(
	ctx context.Context,
	path string,
	release *models.Release,
	cfg *Config,
	tmdb *TmdbClient,
) (string, error) {
	source, target, ok, err := episodeTarget(ctx, path, release, cfg, tmdb)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	if SamePath(source, target) {
		ApplySidecars(source, target, cfg)
		return target, nil
	}
	series := ""
	if release.Series != nil {
		series = *release.Series
	}
	resolved, err := ResolveExistingTarget(
		source,
		target,
		cfg.ReleaseScore(release),
		cfg,
		"series",
		series,
	)
	if err != nil {
		return "", err
	}
	if resolved {
		return "", ErrInferiorDuplicate
	}
	if err := moveAcrossDevices(source, target); err != nil {
		return "", err
	}
	ApplySidecars(source, target, cfg)
	return target, nil
}

// PreviewEpisodeRename reports only the files that would actually change name
// (implementation of `preview_episode_rename`).
func PreviewEpisodeRename(
	ctx context.Context,
	path string,
	release *models.Release,
	cfg *Config,
	tmdb *TmdbClient,
) (string, error) {
	source, target, ok, err := episodeTarget(ctx, path, release, cfg, tmdb)
	if err != nil {
		return "", err
	}
	if ok && !SamePath(source, target) {
		return target, nil
	}
	return "", nil
}

// filenameMatchesEpisode reports whether a file name contains the given
// season/episode identity (S01E02, 1x02, …).
func filenameMatchesEpisode(name string, season, episode int64) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, fmt.Sprintf("s%02de%02d", season, episode)) ||
		strings.Contains(lower, fmt.Sprintf("s%de%d", season, episode)) ||
		strings.Contains(lower, fmt.Sprintf("%dx%02d", season, episode)) ||
		strings.Contains(lower, fmt.Sprintf("%dx%d", season, episode))
}

// episodeTarget computes the (source, target) rename pair for an episode
// (implementation of `episode_target`).
func episodeTarget(
	ctx context.Context,
	path string,
	release *models.Release,
	cfg *Config,
	tmdb *TmdbClient,
) (string, string, bool, error) {
	if !cfg.RenameEpisodes || release.IsPack || release.Kind != "series" {
		return "", "", false, nil
	}
	if release.Season == nil || release.Episode == nil || release.Series == nil {
		return "", "", false, nil
	}
	season := *release.Season
	episode := *release.Episode
	series := cfg.FindSeriesByName(*release.Series)
	if series == nil {
		return "", "", false, nil
	}
	var resolvedTitle *string
	// TMDB has no per-episode endpoint for E00 specials/recaps. Keep the
	// configured fallback title instead of failing the completed import on its
	// expected 404 response.
	if tmdb != nil && episode > 0 {
		tmdbID := series.TmdbID
		if strings.TrimSpace(tmdbID) == "" {
			resolved, err := tmdb.ResolveSeriesID(ctx, series.Name)
			if err != nil {
				return "", "", false, err
			}
			if resolved != nil {
				tmdbID = *resolved
			} else {
				tmdbID = ""
			}
		}
		if tmdbID != "" {
			t, err := tmdb.EpisodeTitle(ctx, tmdbID, season, episode)
			if err != nil {
				// Metadata only improves the display name. A missing/unknown
				// episode (or a temporary provider failure) must never prevent a
				// completed video from being archived and compared by quality.
				logging.Warn("episode metadata unavailable; using fallback title",
					"series", series.Name, "season", season, "episode", episode, "error", err.Error())
			} else {
				resolvedTitle = t
			}
		}
	}
	title := fmt.Sprintf("Episodio %d", episode)
	if episode == 0 {
		title = "Speciale"
	}
	if resolvedTitle != nil {
		title = *resolvedTitle
	}
	files, err := VideoFiles(path)
	if err != nil {
		return "", "", false, err
	}
	var source string
	if len(files) == 1 {
		source = files[0]
	} else {
		matches := []string{}
		for _, file := range files {
			if filenameMatchesEpisode(filepath.Base(file), season, episode) {
				matches = append(matches, file)
			}
		}
		if len(matches) != 1 {
			return "", "", false, nil
		}
		source = matches[0]
	}
	extension := strings.TrimPrefix(filepath.Ext(source), ".")
	if extension == "" {
		extension = "mkv"
	}
	title = safeComponent(title)
	media := readMediaTags(source)
	resolution := release.Quality.Resolution
	if media.Resolution != nil {
		resolution = *media.Resolution
	}
	codec := release.Quality.Codec
	if media.VideoCodec != nil {
		codec = *media.VideoCodec
	}
	audio := release.Quality.Audio
	if media.AudioCodec != nil {
		audio = *media.AudioCodec
	}
	hdr := release.Quality.HDR
	if media.HDR != nil {
		hdr = *media.HDR
	}
	defaultLanguage := cfg.DefaultLanguage()
	language := defaultLanguage
	if media.Languages != nil && *media.Languages != "" {
		language = *media.Languages
	} else if strings.TrimSpace(release.Quality.Language) != "" {
		language = release.Quality.Language
	}
	// legacy `{Audio}` (and `{AudioCodec}`) join codec and channels: "AC3 5.1".
	audioParts := []string{}
	if audio != "" {
		audioParts = append(audioParts, audio)
	}
	if media.Channels != nil && *media.Channels != "" {
		audioParts = append(audioParts, *media.Channels)
	}
	audioFull := strings.Join(audioParts, " ")
	seriesName := sanitizeInvalid(series.Name)
	sourceTag := sourceLabel(release.Quality.Source)
	var targetStem string
	switch cfg.RenameFormat {
	case "standard":
		targetStem = fmt.Sprintf(
			"%s - S%02dE%02d - %s [%s][%s]",
			seriesName, season, episode, title, resolution, codec,
		)
	case "full", "completo":
		targetStem = fmt.Sprintf(
			"%s - S%02dE%02d - %s [%s][%s][%s][%s][%s]",
			seriesName, season, episode, title, resolution, audio, hdr, codec, language,
		)
	case "custom":
		token := func(value string) string {
			value = strings.TrimSpace(value)
			if value == "" || strings.EqualFold(value, "unknown") {
				return ""
			}
			return value
		}
		languageToken := token(language)
		sourceToken := token(sourceTag)
		channels := ""
		if media.Channels != nil {
			channels = *media.Channels
		}
		targetStem = cfg.RenameTemplate
		replacements := []struct{ from, to string }{
			{"{Serie}", seriesName},
			{"{Stagione}", fmt.Sprintf("S%02d", season)},
			{"{Episodio}", fmt.Sprintf("E%02d", episode)},
			{"{Titolo}", title},
			{"{Source}", sourceToken},
			{"{Sorgente}", sourceToken},
			{"{Gruppo}", token(release.Quality.Group)},
			{"{Risoluzione}", token(resolution)},
			{"{VideoCodec}", token(codec)},
			{"{Audio}", token(audioFull)},
			{"{AudioCodec}", token(audioFull)},
			{"{Canali}", token(channels)},
			{"{HDR}", token(hdr)},
			{"{Lingue}", languageToken},
		}
		for _, replacement := range replacements {
			targetStem = strings.ReplaceAll(targetStem, replacement.from, replacement.to)
		}
	default:
		targetStem = fmt.Sprintf("%s - S%02dE%02d - %s", seriesName, season, episode, title)
	}
	targetStem = cleanupFilename(sanitizeInvalid(targetStem))
	targetName := fmt.Sprintf("%s.%s", targetStem, extension)
	target := filepath.Join(filepath.Dir(source), targetName)
	return source, target, true, nil
}

// RenameMovie renames a movie file according to the configured template and
// returns the new path, or "" when there is nothing to do (implementation of
// `rename_movie`).
func RenameMovie(
	ctx context.Context,
	path string,
	release *models.Release,
	cfg *Config,
	tmdb *TmdbClient,
) (string, error) {
	if !cfg.RenameEpisodes || release.Kind != "movie" {
		return "", nil
	}
	configured := release.Title
	configuredName := strings.ReplaceAll(configured, ".", " ")
	if movie := cfg.FindMovieMatchManual(release.Title, release.Year); movie != nil {
		configuredName = movie.Name
	}
	officialTitle := configuredName
	if tmdb != nil {
		tmdbItem, err := tmdb.SearchMovie(ctx, configuredName, release.Year)
		if err != nil {
			return "", err
		}
		if tmdbItem != nil && tmdbItem.Title != nil {
			officialTitle = *tmdbItem.Title
		}
	}
	officialYear := release.Year
	files, err := VideoFiles(path)
	if err != nil {
		return "", err
	}
	if len(files) != 1 {
		return "", nil
	}
	source := files[0]
	if strings.Contains(strings.ToLower(filepath.Base(source)), "sample") {
		return "", nil
	}
	media := readMediaTags(source)
	resolution := release.Quality.Resolution
	if media.Resolution != nil {
		resolution = *media.Resolution
	}
	codec := release.Quality.Codec
	if media.VideoCodec != nil {
		codec = *media.VideoCodec
	}
	audio := release.Quality.Audio
	if media.AudioCodec != nil {
		audio = *media.AudioCodec
	}
	hdr := release.Quality.HDR
	if media.HDR != nil {
		hdr = *media.HDR
	}
	audioParts := []string{}
	if audio != "" {
		audioParts = append(audioParts, audio)
	}
	if media.Channels != nil && *media.Channels != "" {
		audioParts = append(audioParts, *media.Channels)
	}
	audioFull := strings.Join(audioParts, " ")
	officialTitle = sanitizeInvalid(officialTitle)
	year := ""
	if officialYear != nil {
		year = strconv.FormatInt(*officialYear, 10)
	}
	yearTag := ""
	if year != "" {
		yearTag = fmt.Sprintf(" (%s)", year)
	}
	var stem string
	switch cfg.RenameFormat {
	case "standard":
		stem = fmt.Sprintf("%s%s [%s][%s]", officialTitle, yearTag, resolution, codec)
	case "full", "completo":
		stem = fmt.Sprintf(
			"%s%s [%s][%s][%s][%s]",
			officialTitle, yearTag, resolution, audioFull, hdr, codec,
		)
	case "custom":
		stem = fmt.Sprintf("%s%s [%s]", officialTitle, yearTag, resolution)
	default:
		stem = officialTitle + yearTag
	}
	stem = cleanupFilename(sanitizeInvalid(stem))
	extension := strings.TrimPrefix(filepath.Ext(source), ".")
	if extension == "" {
		extension = "mkv"
	}
	target := filepath.Join(filepath.Dir(source), fmt.Sprintf("%s.%s", stem, extension))
	if SamePath(source, target) {
		return target, nil
	}
	resolved, err := ResolveExistingTarget(
		source,
		target,
		cfg.ReleaseScore(release),
		cfg,
		"movie",
		release.Title,
	)
	if err != nil {
		return "", err
	}
	if resolved {
		return "", ErrInferiorDuplicate
	}
	if err := moveAcrossDevices(source, target); err != nil {
		return "", err
	}
	return target, nil
}

// movieArtworkKinds maps the generic artwork names bundled in release folders
// to the Jellyfin/Kodi sidecar suffix written next to the video.
var movieArtworkKinds = map[string]string{
	"poster":     "poster",
	"folder":     "poster",
	"cover":      "poster",
	"backdrop":   "backdrop",
	"fanart":     "backdrop",
	"background": "backdrop",
	"landscape":  "landscape",
	"thumb":      "thumb",
	"logo":       "logo",
	"clearart":   "clearart",
	"banner":     "banner",
	"disc":       "disc",
}

// movieSubtitleExtensions are the external subtitle containers kept next to the
// video when a movie folder is flattened.
var movieSubtitleExtensions = map[string]bool{
	".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".vtt": true, ".sup": true,
}

// movieFlatFilesEnabled reports whether single-video movie folders are moved up
// into the archive root as flat files. It is on by default; set
// `movies_flat_files` to no/false/0 to keep the original torrent folder.
func movieFlatFilesEnabled(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	value, ok := cfg.Settings["movies_flat_files"]
	if !ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "no", "false", "0", "off":
		return false
	default:
		return true
	}
}

// flattenMovieFolder moves a movie's single video — together with its external
// subtitles and bundled artwork — from its torrent folder up to the archive
// root, then removes the now-empty folder(s). It is a no-op when the feature is
// disabled, the archive root is unknown, the video already sits in the root, or
// the destination name is already taken (never clobber an existing movie).
func flattenMovieFolder(cfg *Config, release *models.Release, videoPath string) (string, error) {
	if !movieFlatFilesEnabled(cfg) || release == nil || strings.TrimSpace(videoPath) == "" {
		return videoPath, nil
	}
	root, ok := ConfiguredDestinationFor(release, cfg)
	if !ok || strings.TrimSpace(root) == "" {
		return videoPath, nil
	}
	root = filepath.Clean(root)
	videoPath = filepath.Clean(videoPath)
	dir := filepath.Clean(filepath.Dir(videoPath))
	if SamePath(dir, root) || !pathWithin(dir, root) {
		return videoPath, nil
	}
	target := filepath.Clean(filepath.Join(root, filepath.Base(videoPath)))
	if strings.Contains(target, "..") {
		return videoPath, fmt.Errorf("invalid movie destination path: %s", target)
	}
	if SamePath(videoPath, target) {
		return videoPath, nil
	}
	if _, err := os.Stat(target); err == nil {
		// An equally named movie is already archived: leave this one untouched
		// rather than overwrite it.
		return videoPath, nil
	}
	if err := moveAcrossDevices(videoPath, target); err != nil {
		return videoPath, err
	}
	moveMovieCompanions(dir, root, filepath.Base(target))
	removeEmptyDirsUpTo(dir, root)
	renameLeftoverMovieFolder(dir, root, strings.TrimSuffix(filepath.Base(target), filepath.Ext(target)))
	return target, nil
}

// renameLeftoverMovieFolder gives a clean name to a movie folder that could not
// be fully flattened (unknown companion files kept it non-empty), so it is
// still recognisable instead of keeping the raw torrent folder name.
func renameLeftoverMovieFolder(dir, root, stem string) {
	stem = safeComponent(strings.TrimSpace(stem))
	if stem == "" {
		return
	}
	dir = filepath.Clean(dir)
	root = filepath.Clean(root)
	if strings.Contains(dir, "..") || strings.Contains(root, "..") {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return
	}
	if !SamePath(filepath.Dir(dir), root) {
		return
	}
	target := filepath.Clean(filepath.Join(root, stem))
	if strings.Contains(target, "..") {
		return
	}
	if _, err := os.Stat(target); err == nil {
		return
	}
	_ = os.Rename(dir, target)
}

// moveMovieCompanions relocates the subtitles and artwork of a flattened movie
// next to the video, renaming them so media servers still associate them.
func moveMovieCompanions(oldDir, root, videoBase string) {
	oldDir = filepath.Clean(oldDir)
	root = filepath.Clean(root)
	if strings.Contains(oldDir, "..") || strings.Contains(root, "..") {
		return
	}
	stem := strings.TrimSuffix(filepath.Base(videoBase), filepath.Ext(videoBase))
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		source := filepath.Clean(filepath.Join(oldDir, name))
		ext := strings.ToLower(filepath.Ext(name))
		var targetName string
		switch {
		case movieSubtitleExtensions[ext]:
			base := strings.TrimSuffix(name, filepath.Ext(name))
			lang := ""
			if dot := strings.LastIndex(base, "."); dot >= 0 {
				lang = strings.TrimSpace(base[dot+1:])
			}
			if lang != "" && len(lang) <= 8 {
				targetName = stem + "." + lang + filepath.Ext(name)
			} else {
				targetName = stem + filepath.Ext(name)
			}
		case ext == ".nfo":
			targetName = stem + ".nfo"
		default:
			kind := movieArtworkKind(name)
			if kind == "" {
				// Unknown companion: keep it in place so nothing is lost or
				// silently clobbered in the shared root.
				continue
			}
			targetName = stem + "-" + kind + filepath.Ext(name)
		}
		target := filepath.Clean(filepath.Join(root, targetName))
		if strings.Contains(target, "..") {
			continue
		}
		if _, statErr := os.Stat(target); statErr == nil {
			continue
		}
		_ = moveAcrossDevices(source, target)
	}
}

// movieArtworkKind returns the Jellyfin sidecar suffix for a bundled artwork
// file, or "" when the name is not a recognised artwork kind.
func movieArtworkKind(name string) string {
	base := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name))))
	if kind, ok := movieArtworkKinds[base]; ok {
		return kind
	}
	return ""
}

// removeEmptyDirsUpTo deletes `from` and its empty parents up to (excluding)
// root, so a flattened folder leaves no empty shell behind.
func removeEmptyDirsUpTo(from, root string) {
	dir := filepath.Clean(from)
	root = filepath.Clean(root)
	if strings.Contains(dir, "..") || strings.Contains(root, "..") {
		return
	}
	for !SamePath(dir, root) && pathWithin(dir, root) {
		if strings.Contains(dir, "..") {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			return
		}
		parent := filepath.Dir(dir)
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = parent
	}
}

// RestoreSourceToken restores the source token in a name that lost it,
// inserting `[WEB-DL]`/`[HDTV]`… right before the first resolution tag (implementation of
// `restore_source_token`).
func RestoreSourceToken(name, source string) (string, bool) {
	label := strings.TrimSpace(sourceLabel(source))
	if label == "" || strings.EqualFold(label, "unknown") {
		return "", false
	}
	if strings.Contains(strings.ToLower(name), strings.ToLower(label)) {
		return "", false
	}
	marker, err := utils.CachedRegex(`(?i)\[(?:2160p|1080p|720p|576p|480p|360p)\]`)
	if err != nil {
		return "", false
	}
	location := marker.FindStringIndex(name)
	if location == nil {
		return "", false
	}
	var builder strings.Builder
	builder.Grow(len(name) + len(label) + 2)
	builder.WriteString(name[:location[0]])
	builder.WriteByte('[')
	builder.WriteString(label)
	builder.WriteByte(']')
	builder.WriteString(name[location[0]:])
	return builder.String(), true
}

// EpisodeNameConforms reports whether a file already follows the configured
// rename format, without TMDB or MediaInfo (implementation of `episode_name_conforms`).
func EpisodeNameConforms(path string, release *models.Release, cfg *Config) bool {
	if !cfg.RenameEpisodes || release.Kind != "series" {
		return false
	}
	if release.Season == nil || release.Episode == nil || release.Series == nil {
		return false
	}
	season := *release.Season
	episode := *release.Episode
	seriesName := *release.Series
	series := cfg.FindSeriesByName(seriesName)
	if series == nil {
		return false
	}
	stem := strings.ToLower(fileStem(path))
	seriesName = sanitizeInvalid(series.Name)
	var prefix string
	switch cfg.RenameFormat {
	case "custom":
		rendered := cfg.RenameTemplate
		rendered = strings.ReplaceAll(rendered, "{Serie}", seriesName)
		rendered = strings.ReplaceAll(rendered, "{Stagione}", fmt.Sprintf("S%02d", season))
		rendered = strings.ReplaceAll(rendered, "{Episodio}", fmt.Sprintf("E%02d", episode))
		if index := strings.Index(rendered, "{"); index >= 0 {
			prefix = rendered[:index]
		} else {
			prefix = rendered
		}
	default:
		prefix = fmt.Sprintf("%s - S%02dE%02d - ", seriesName, season, episode)
	}
	// Names with empty groups (`[]`/`()`) or literal placeholders are artifacts
	// of a partially applied template: they must be renamed to clean them up.
	emptyGroups, err := utils.CachedRegex(`\[\s*\]|\(\s*\)`)
	if (err == nil && emptyGroups.MatchString(stem)) ||
		strings.Contains(stem, "{") ||
		strings.Contains(stem, "}") {
		return false
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	return prefix != "" && strings.HasPrefix(stem, prefix)
}

// RenameSidecars renames the files associated with a video (thumbnail,
// subtitles, nfo, …) so they follow the new base name (implementation of
// `rename_sidecars`). The second element of each pair is empty when the sidecar
// was moved to the trash.
func RenameSidecars(source, target string, cfg *Config) ([][2]string, error) {
	parent := filepath.Dir(source)
	sourceStem := fileStem(source)
	targetStem := fileStem(target)
	if sourceStem == "" || targetStem == "" {
		return [][2]string{}, nil
	}
	targetStemLower := strings.ToLower(targetStem)
	targetSeason, targetEpisode, targetSEOK := parseSE(targetStem)
	videoExts := map[string]bool{
		"mkv": true, "mp4": true, "avi": true, "m4v": true, "mov": true, "ts": true,
	}
	sidecarExts := map[string]bool{
		"jpg": true, "jpeg": true, "png": true, "webp": true, "srt": true, "sub": true,
		"ass": true, "ssa": true, "vtt": true, "idx": true, "sup": true, "smi": true,
		"nfo": true, "txt": true,
	}
	existing := sidecarNames(parent)
	moved := [][2]string{}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(parent, entry.Name())
		if path == source {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		if videoExts[extension] || !sidecarExts[extension] {
			continue
		}
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, targetStemLower) {
			continue
		}
		isDuplicate := strings.Contains(lower, "(copia") || strings.Contains(lower, "(copy")
		if remainder, ok := strings.CutPrefix(name, sourceStem); ok {
			if isDuplicate {
				if err := trashOrRemove(path, cfg); err != nil {
					return nil, err
				}
				moved = append(moved, [2]string{path, ""})
				continue
			}
			newPath := filepath.Join(parent, targetStem+remainder)
			if newPath == path {
				continue
			}
			if _, err := os.Stat(newPath); err == nil {
				if err := trashOrRemove(path, cfg); err != nil {
					return nil, err
				}
			} else if err := moveAcrossDevices(path, newPath); err != nil {
				return nil, err
			}
			moved = append(moved, [2]string{path, newPath})
			continue
		}
		// 2) sidecar "spurio" di un nome precedente: stesso episodio
		// (SxxEyy/NxNN).
		if !targetSEOK {
			continue
		}
		season, episode, ok := parseSE(name)
		if !ok || season != targetSeason || episode != targetEpisode {
			continue
		}
		if isDuplicate {
			if err := trashOrRemove(path, cfg); err != nil {
				return nil, err
			}
			moved = append(moved, [2]string{path, ""})
			continue
		}
		hasCounterpart := false
		for _, candidate := range existing {
			candidateLower := strings.ToLower(candidate)
			if candidate != name &&
				strings.HasPrefix(candidateLower, targetStemLower) &&
				strings.HasSuffix(candidateLower, "."+extension) {
				hasCounterpart = true
				break
			}
		}
		if hasCounterpart {
			if err := trashOrRemove(path, cfg); err != nil {
				return nil, err
			}
			moved = append(moved, [2]string{path, ""})
			continue
		}
		expected := fmt.Sprintf("%s.%s", targetStem, extension)
		if strings.Contains(lower, "-thumb") {
			expected = fmt.Sprintf("%s-thumb.%s", targetStem, extension)
		}
		newPath := filepath.Join(parent, expected)
		if newPath != path {
			if _, err := os.Stat(newPath); err == nil {
				if err := trashOrRemove(path, cfg); err != nil {
					return nil, err
				}
			} else if err := moveAcrossDevices(path, newPath); err != nil {
				return nil, err
			}
			moved = append(moved, [2]string{path, newPath})
		}
	}
	return moved, nil
}

// sidecarNames lists the names of the entries directly inside dir.
func sidecarNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// parseSE parses a (season, episode) pair from a name, accepting `S01E02` and
// `1x02`.
func parseSE(name string) (int64, int64, bool) {
	pattern, err := utils.CachedRegex(
		`(?i)(?:s(?P<s>\d{1,2})e|(?P<ns>\d{1,2})x)(?P<e>\d{1,4})`,
	)
	if err != nil {
		return 0, 0, false
	}
	match := pattern.FindStringSubmatch(name)
	if match == nil {
		return 0, 0, false
	}
	seasonText := submatchNamed(pattern, match, "s")
	if seasonText == "" {
		seasonText = submatchNamed(pattern, match, "ns")
	}
	if seasonText == "" {
		return 0, 0, false
	}
	season, err := strconv.ParseInt(seasonText, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	episode, err := strconv.ParseInt(submatchNamed(pattern, match, "e"), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return season, episode, true
}

// ApplySidecars applies the sidecar rename and logs the outcome (implementation of
// `apply_sidecars`).
func ApplySidecars(source, target string, cfg *Config) {
	sidecars, err := RenameSidecars(source, target, cfg)
	if err != nil {
		logging.Warn("rename sidecar failed", "error", err)
		return
	}
	for _, pair := range sidecars {
		if pair[1] == "" {
			logging.Debug("rename sidecar: duplicate moved to trash", "from", pair[0])
		} else {
			logging.Debug("rename sidecar", "from", pair[0], "to", pair[1])
		}
	}
}

// ApplySidecarsTo moves the sidecars of source into the directory of target,
// renaming them after the target stem. Used when the video moves to a different
// directory (manual archive): RenameSidecars would only rename them in place in
// the source folder, leaving the archived video without subtitles/NFO.
func ApplySidecarsTo(source, target string, cfg *Config) {
	sidecars, err := MoveSidecarsToTarget(source, target, cfg)
	if err != nil {
		logging.Warn("move sidecar failed", "error", err)
		return
	}
	for _, pair := range sidecars {
		if pair[1] == "" {
			logging.Debug("move sidecar: duplicate moved to trash", "from", pair[0])
		} else {
			logging.Debug("move sidecar", "from", pair[0], "to", pair[1])
		}
	}
}

// MoveSidecarsToTarget moves the sidecar files sitting next to source into the
// directory of target, renamed after the target stem.
func MoveSidecarsToTarget(source, target string, cfg *Config) ([][2]string, error) {
	sourceDir := filepath.Dir(source)
	targetDir := filepath.Dir(target)
	sourceStem := fileStem(source)
	targetStem := fileStem(target)
	if sourceStem == "" || targetStem == "" {
		return [][2]string{}, nil
	}
	sidecarExts := map[string]bool{
		"jpg": true, "jpeg": true, "png": true, "webp": true, "srt": true, "sub": true,
		"ass": true, "ssa": true, "vtt": true, "idx": true, "sup": true, "smi": true,
		"nfo": true, "txt": true,
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return [][2]string{}, nil
	}
	moved := [][2]string{}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(sourceDir, entry.Name())
		if SamePath(path, source) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		if !sidecarExts[extension] {
			continue
		}
		remainder, ok := strings.CutPrefix(name, sourceStem)
		if !ok {
			// A sidecar named only by the episode marker (e.g. "Show.S01E01.ita.srt"):
			// keep its extension and let the target name lead.
			remainder = "." + extension
		}
		newPath := filepath.Join(targetDir, targetStem+remainder)
		if SamePath(path, newPath) {
			continue
		}
		if _, statErr := os.Stat(newPath); statErr == nil {
			if err := trashOrRemove(path, cfg); err != nil {
				return moved, err
			}
			moved = append(moved, [2]string{path, ""})
			continue
		}
		if err := moveAcrossDevices(path, newPath); err != nil {
			return moved, err
		}
		moved = append(moved, [2]string{path, newPath})
	}
	return moved, nil
}

// DiscardSidecars moves the sidecars of a discarded duplicate video to the
// trash (implementation of `discard_sidecars`).
func DiscardSidecars(source string, cfg *Config) (int, error) {
	stem := fileStem(source)
	if stem == "" {
		return 0, nil
	}
	parent := filepath.Dir(source)
	sidecarExts := map[string]bool{
		"jpg": true, "jpeg": true, "png": true, "webp": true, "srt": true, "sub": true,
		"ass": true, "ssa": true, "vtt": true, "idx": true, "sup": true, "smi": true,
		"nfo": true, "xml": true,
	}
	moved := 0
	entries, err := os.ReadDir(parent)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		path := filepath.Join(parent, entry.Name())
		if path == source {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		if !strings.HasPrefix(name, stem) || !sidecarExts[extension] {
			continue
		}
		if err := trashOrRemove(path, cfg); err != nil {
			return 0, err
		}
		moved++
	}
	return moved, nil
}

// trashOrRemove removes a sidecar according to the cleanup action (implementation of
// `trash_or_remove`). When the action is "move" the trash folder must be
// configured: refusing is safer than deleting a file the user asked to keep.
func trashOrRemove(path string, cfg *Config) error {
	path = filepath.Clean(path)
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path: %s", path)
	}
	if cfg.CleanupAction == "delete" {
		return os.Remove(path)
	}
	// ResolveTrashPath always yields a usable directory (configured trash_path
	// or <data>/trash); MoveToTrash creates it when missing.
	_, err := MoveToTrash(path, cfg.ResolveTrashPath())
	return err
}

// safeComponent replaces the characters that cannot appear in a file name.
func safeComponent(value string) string {
	var builder strings.Builder
	for _, character := range value {
		switch character {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			builder.WriteRune('_')
		default:
			builder.WriteRune(character)
		}
	}
	return strings.TrimSpace(builder.String())
}

// mediaTags is the implemented `MediaTags`.
type mediaTags struct {
	Resolution *string
	VideoCodec *string
	AudioCodec *string
	Channels   *string
	HDR        *string
	Languages  *string
}

// readMediaTags runs the `mediainfo` binary and parses its JSON output (implementation of
// `read_media_tags`), falling back to `ffprobe` when `mediainfo` is not available or incomplete.
func readMediaTags(path string) mediaTags {
	// Bounded timeout so a hung probe or a stuck file cannot pin a worker
	// indefinitely (mirrors the ffprobe probe in mediainfo.go).
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var tags mediaTags
	output, err := exec.CommandContext(ctx, "mediainfo", "--Output=JSON", path).Output()
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(output))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err == nil {
			if object, ok := value.(map[string]any); ok {
				tags = parseMediaTags(object)
			}
		}
	}
	// Fallback/enrichment using ffprobe via Probe (mediainfo.go)
	if tags.Resolution == nil || tags.VideoCodec == nil || tags.AudioCodec == nil || tags.HDR == nil {
		if probe := Probe(path); probe != nil {
			tags = enrichTagsWithProbe(tags, probe)
		}
	}
	return tags
}

// enrichTagsWithProbe merges ffprobe probe results into mediaTags when fields are missing.
func enrichTagsWithProbe(tags mediaTags, probe *MediaInfo) mediaTags {
	if tags.Resolution == nil {
		if res := probe.Resolution(); res != "" {
			tags.Resolution = &res
		}
	}
	if tags.VideoCodec == nil {
		if label, ok := CodecLabel(probe.VideoCodec); ok {
			tags.VideoCodec = &label
		}
	}
	if tags.AudioCodec == nil {
		if label, ok := AudioLabel(probe.AudioCodec); ok {
			tags.AudioCodec = &label
		}
	}
	if tags.Channels == nil && probe.AudioChannels > 0 {
		var ch string
		switch probe.AudioChannels {
		case 1:
			ch = "Mono"
		case 2:
			ch = "Stereo"
		case 6:
			ch = "5.1"
		case 8:
			ch = "7.1"
		default:
			ch = fmt.Sprintf("%dch", probe.AudioChannels)
		}
		tags.Channels = &ch
	}
	if tags.HDR == nil && probe.HDR != "" {
		hdr := probe.HDR
		tags.HDR = &hdr
	}
	if tags.Languages == nil && len(probe.AudioLanguages) > 0 {
		langs := make([]string, 0, len(probe.AudioLanguages))
		for _, l := range probe.AudioLanguages {
			if mapped := mediaLanguage(l); mapped != nil && !containsString(langs, *mapped) {
				langs = append(langs, *mapped)
			}
		}
		if len(langs) > 0 {
			joined := strings.Join(langs, "+")
			tags.Languages = &joined
		}
	}
	return tags
}

// mediaText returns the first non-empty string stored under keys in track.
func mediaText(track map[string]any, keys ...string) (string, bool) {
	if track == nil {
		return "", false
	}
	for _, key := range keys {
		if raw, ok := track[key]; ok {
			if text, ok := raw.(string); ok {
				text = strings.TrimSpace(text)
				if text != "" {
					return text, true
				}
			}
		}
	}
	return "", false
}

// trackType returns the `@type` of a MediaInfo track.
func trackType(track map[string]any) string {
	if value, ok := track["@type"].(string); ok {
		return value
	}
	return ""
}

// mediaResolution implements legacy `_resolution_tag`.
func mediaResolution(video map[string]any) *string {
	parse := func(keys ...string) *int64 {
		text, ok := mediaText(video, keys...)
		if !ok {
			return nil
		}
		fields := strings.Fields(text)
		if len(fields) == 0 {
			return nil
		}
		value, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return nil
		}
		return &value
	}
	height := parse("Height")
	width := parse("Width")
	if height == nil && width == nil {
		return nil
	}
	heightValue := int64(0)
	if height != nil {
		heightValue = *height
	}
	widthValue := int64(0)
	if width != nil {
		widthValue = *width
	}
	suffix := "p"
	if scan, ok := mediaText(video, "Scan_type"); ok && strings.EqualFold(scan, "interlaced") {
		suffix = "i"
	}
	var base string
	switch {
	case widthValue >= 3800 || heightValue >= 2000:
		base = "2160"
	case widthValue >= 1900 || heightValue >= 1000:
		base = "1080"
	case widthValue >= 1200 || heightValue >= 700:
		base = "720"
	case heightValue >= 540:
		base = "576"
	default:
		base = "480"
	}
	result := base + suffix
	return &result
}

// mediaHDR implements legacy `_hdr_tag`.
func mediaHDR(video map[string]any) *string {
	format, _ := mediaText(video, "HDR_Format")
	formatString, _ := mediaText(video, "HDR_Format_String")
	compatibility, _ := mediaText(video, "HDR_Format_Compatibility")
	transfer, _ := mediaText(video, "Transfer_characteristics")
	all := strings.TrimSpace(format + " " + formatString + " " + compatibility)
	if all == "" && transfer == "" {
		return nil
	}
	dv := strings.Contains(all, "Dolby Vision")
	hdr10Plus := strings.Contains(all, "HDR10+") || strings.Contains(all, "HDR10 Plus")
	hdr10 := strings.Contains(all, "HDR10")
	hlg := strings.Contains(transfer, "HLG") || strings.Contains(all, "HLG")
	pq := strings.Contains(transfer, "PQ")
	var result string
	switch {
	case dv && hdr10:
		result = "DV HDR10"
	case dv:
		result = "DV"
	case hdr10Plus:
		result = "HDR10Plus"
	case hdr10:
		result = "HDR10"
	case hlg:
		result = "HLG"
	case pq:
		result = "HDR"
	default:
		return nil
	}
	return &result
}

// mediaVideoCodec implements legacy `_video_codec_tag`.
func mediaVideoCodec(video map[string]any) *string {
	format, _ := mediaText(video, "Format")
	format = strings.ToLower(format)
	codec, _ := mediaText(video, "CodecID")
	codec = strings.ToLower(codec)
	var result string
	switch {
	case format == "hevc" || strings.Contains(codec, "hevc"):
		result = "h265"
	case format == "avc" || strings.Contains(codec, "avc"):
		result = "h264"
	case format == "av1" || strings.Contains(codec, "av01"):
		result = "AV1"
	case strings.Contains(format, "xvid") || strings.Contains(codec, "xvid"):
		result = "XviD"
	case strings.Contains(format, "divx"):
		result = "DivX"
	case strings.Contains(format, "vc-1") || strings.Contains(format, "vc1"):
		result = "VC-1"
	case format != "":
		result = strings.ToUpper(format)
	default:
		return nil
	}
	return &result
}

// mediaAudioCodec implements legacy `_audio_codec_tag`.
func mediaAudioCodec(audio map[string]any) *string {
	format, _ := mediaText(audio, "Format")
	format = strings.ToLower(format)
	commercial, _ := mediaText(audio, "Commercial_Name")
	commercial = strings.ToLower(commercial)
	profile, _ := mediaText(audio, "Format_Profile")
	profile = strings.ToLower(profile)
	codec, _ := mediaText(audio, "CodecID")
	codec = strings.ToLower(codec)
	atmos := strings.Contains(profile, "atmos") ||
		strings.Contains(commercial, "atmos") ||
		strings.Contains(format, "joc")
	var result string
	switch {
	case strings.Contains(commercial, "truehd") || strings.Contains(format, "truehd"):
		if atmos {
			result = "TrueHD Atmos"
		} else {
			result = "TrueHD"
		}
	case strings.Contains(format, "e-ac-3") ||
		strings.Contains(format, "eac-3") ||
		strings.Contains(codec, "eac3") ||
		strings.Contains(commercial, "dolby digital plus"):
		if atmos {
			result = "EAC3 Atmos"
		} else {
			result = "EAC3"
		}
	case strings.Contains(format, "ac-3") ||
		strings.Contains(codec, "ac3") ||
		(strings.Contains(commercial, "dolby digital") && !strings.Contains(commercial, "plus")):
		result = "AC3"
	case strings.Contains(format, "dts") || strings.Contains(codec, "dts"):
		switch {
		case strings.Contains(profile, "ma") || strings.Contains(profile, "master"):
			result = "DTS-MA"
		case strings.Contains(profile, "x") && !strings.Contains(profile, "x:"):
			result = "DTS-X"
		default:
			result = "DTS"
		}
	case strings.Contains(format, "aac") || strings.Contains(codec, "aac"):
		result = "AAC"
	case strings.Contains(format, "flac"):
		result = "FLAC"
	case strings.Contains(format, "opus"):
		result = "Opus"
	case strings.Contains(format, "mp3") || strings.Contains(format, "mpeg"):
		result = "MP3"
	case strings.Contains(format, "pcm"):
		result = "PCM"
	case format != "":
		upper := strings.ToUpper(format)
		return &upper
	default:
		return nil
	}
	return &result
}

// mediaChannels implements legacy `_channels_tag`.
func mediaChannels(audio map[string]any) *string {
	text, ok := mediaText(audio, "Channel_s")
	if !ok {
		return nil
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}
	channels, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil
	}
	var result string
	switch channels {
	case 1:
		result = "Mono"
	case 2:
		result = "Stereo"
	case 6:
		result = "5.1"
	case 8:
		result = "7.1"
	default:
		result = fmt.Sprintf("%dch", channels)
	}
	return &result
}

// mediaLanguage implements legacy `_LANG_MAP` (primary subtag only).
func mediaLanguage(language string) *string {
	primary := strings.ToLower(language)
	if index := strings.IndexAny(primary, "-_"); index >= 0 {
		primary = primary[:index]
	}
	primary = strings.TrimSpace(primary)
	var mapped string
	switch primary {
	case "italian", "italiano", "it", "ita":
		mapped = "IT"
	case "english", "inglese", "en", "eng":
		mapped = "EN"
	case "french", "francese", "fr", "fra":
		mapped = "FR"
	case "spanish", "spagnolo", "es", "spa":
		mapped = "ES"
	case "german", "tedesco", "de", "deu":
		mapped = "DE"
	case "portuguese", "pt", "por":
		mapped = "PT"
	case "russian", "ru", "rus":
		mapped = "RU"
	case "japanese", "ja", "jpn":
		mapped = "JA"
	case "chinese", "zh", "zho":
		mapped = "ZH"
	case "arabic", "ar", "ara":
		mapped = "AR"
	default:
		if primary == "" {
			return nil
		}
		upper := strings.ToUpper(primary)
		return &upper
	}
	return &mapped
}

// sourceLabel implements legacy `_SOURCE_LABEL`.
func sourceLabel(source string) string {
	switch strings.ToLower(source) {
	case "bluray":
		return "BluRay"
	case "webdl":
		return "WEB-DL"
	case "webrip":
		return "WEBRip"
	case "hdtv":
		return "HDTV"
	case "dvdrip":
		return "DVDRip"
	case "remux":
		return "REMUX"
	default:
		return strings.ToLower(source)
	}
}

// sanitizeInvalid removes path-invalid characters without replacing them (port
// of legacy `_sanitize`).
func sanitizeInvalid(name string) string {
	var builder strings.Builder
	for _, character := range name {
		switch character {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			continue
		default:
			builder.WriteRune(character)
		}
	}
	return strings.TrimSpace(builder.String())
}

// cleanupFilename implements the legacy post-template cleanup: drop empty
// brackets, collapse whitespace and trim trailing separators.
func cleanupFilename(stem string) string {
	emptyBrackets, err := utils.CachedRegex(`\[\s*\]|\(\s*\)`)
	withoutEmpty := stem
	if err == nil {
		withoutEmpty = emptyBrackets.ReplaceAllString(stem, "")
	}
	whitespace, err := utils.CachedRegex(`\s+`)
	collapsed := withoutEmpty
	if err == nil {
		collapsed = strings.TrimSpace(whitespace.ReplaceAllString(withoutEmpty, " "))
	}
	trailing, err := utils.CachedRegex(`[\s\-_]+$`)
	if err != nil {
		return collapsed
	}
	return trailing.ReplaceAllString(collapsed, "")
}

// parseMediaTags implements `parse_media_tags`.
func parseMediaTags(value map[string]any) mediaTags {
	tracks := []map[string]any{}
	if media, ok := value["media"].(map[string]any); ok {
		if rawTracks, ok := media["track"].([]any); ok {
			for _, item := range rawTracks {
				if track, ok := item.(map[string]any); ok {
					tracks = append(tracks, track)
				}
			}
		}
	}
	var video map[string]any
	audioTracks := []map[string]any{}
	for _, track := range tracks {
		switch trackType(track) {
		case "Video":
			if video == nil {
				video = track
			}
		case "Audio":
			audioTracks = append(audioTracks, track)
		}
	}
	// legacy prefers the audio track flagged Default=Yes.
	var audio map[string]any
	for _, track := range audioTracks {
		if text, ok := mediaText(track, "Default"); ok {
			switch strings.ToLower(text) {
			case "yes", "true", "1":
				audio = track
			}
		}
		if audio != nil {
			break
		}
	}
	if audio == nil && len(audioTracks) > 0 {
		audio = audioTracks[0]
	}
	languages := []string{}
	for _, track := range audioTracks {
		if language, ok := mediaText(track, "Language", "Language_String"); ok {
			if normalized := mediaLanguage(language); normalized != nil {
				if !containsString(languages, *normalized) {
					languages = append(languages, *normalized)
				}
			}
		}
	}
	var languagesPtr *string
	if len(languages) > 0 {
		joined := strings.Join(languages, "+")
		languagesPtr = &joined
	}
	return mediaTags{
		Resolution: mediaResolution(video),
		VideoCodec: mediaVideoCodec(video),
		AudioCodec: mediaAudioCodec(audio),
		Channels:   mediaChannels(audio),
		HDR:        mediaHDR(video),
		Languages:  languagesPtr,
	}
}

// FindEpisodeFile returns the single video file for a season/episode inside a
// directory, if exactly one matches (implementation of `find_episode_file`).
func FindEpisodeFile(dir string, season, episode int64) (string, bool) {
	files, err := VideoFiles(dir)
	if err != nil {
		return "", false
	}
	matches := []string{}
	for _, file := range files {
		if filenameMatchesEpisode(filepath.Base(file), season, episode) {
			matches = append(matches, file)
		}
	}
	if len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}

// VideoFiles returns every video file under path without following symlinks
// (implementation of `video_files`).
func VideoFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err == nil && info.Mode().IsRegular() {
		if !isVideoExtension(path) {
			return []string{}, nil
		}
		return []string{path}, nil
	}
	if err != nil || !info.IsDir() {
		return []string{}, nil
	}
	files := []string{}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := filepath.Join(path, entry.Name())
		if entry.IsDir() {
			children, err := VideoFiles(child)
			if err != nil {
				return nil, err
			}
			files = append(files, children...)
			continue
		}
		if isVideoExtension(entry.Name()) {
			files = append(files, child)
		}
	}
	return files, nil
}
