package gextto

// Folder rename review: inspect an arbitrary media folder, compare the names
// with TMDB/TVDB, and apply only the renames explicitly accepted by the user.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	folderRenameMaxFiles = 500
	folderRenameMaxItems = 500
)

var (
	folderRenameEpisodeRE  = regexp.MustCompile(`(?i)^(.+?)[ ._-]+s(\d{1,2})e(\d{1,4})(?:[^0-9]|$)`)
	folderRenameEpisodeXRE = regexp.MustCompile(`(?i)^(.+?)[ ._-]+(\d{1,2})x(\d{1,4})(?:[^0-9]|$)`)
	folderRenameYearRE     = regexp.MustCompile(`(?:^|[ ._\-(\[])((?:19|20)\d{2})(?:$|[ ._\-)\]])`)
)

type folderRenameScanInput struct {
	Path string `json:"path"`
}

type folderRenameApplyInput struct {
	Path  string                  `json:"path"`
	Items []folderRenameApplyItem `json:"items"`
}

type folderRenameApplyItem struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type folderRenameCandidate struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Year     string `json:"year,omitempty"`
	Score    int    `json:"score"`
	Target   string `json:"target"`
}

type folderRenameItem struct {
	Source     string                  `json:"source"`
	Relative   string                  `json:"relative"`
	Kind       string                  `json:"kind"`
	Detected   string                  `json:"detected"`
	Season     *int64                  `json:"season,omitempty"`
	Episode    *int64                  `json:"episode,omitempty"`
	Year       *int64                  `json:"year,omitempty"`
	Candidates []folderRenameCandidate `json:"candidates"`
	Target     string                  `json:"target"`
	Status     string                  `json:"status"`
	Reason     string                  `json:"reason,omitempty"`
	Conflict   bool                    `json:"conflict,omitempty"`
}

type folderRenameResult struct {
	Source string `json:"source"`
	Target string `json:"target"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

// MaintenanceRenameFolderScan implements the review phase of the arbitrary
// folder rename tool. It never modifies files.
func MaintenanceRenameFolderScan(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input folderRenameScanInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, err := folderRenameRoot(input.Path)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	if cfg.TmdbAPIKey == nil && cfg.TvdbAPIKey() == nil {
		jsonError(w, http.StatusConflict, "configura una chiave API TMDB o TVDB prima della scansione")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	items, err := folderRenameScan(ctx, root, cfg)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":    true,
		"path":  root,
		"items": items,
	})
}

// MaintenanceRenameFolderApply applies only the source/target pairs sent by
// the review UI. The root and same-directory checks prevent a modified client
// request from moving files outside the scanned folder.
func MaintenanceRenameFolderApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input folderRenameApplyInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Items) == 0 || len(input.Items) > folderRenameMaxItems {
		jsonError(w, http.StatusBadRequest, "nessun elemento da rinominare o elenco troppo grande")
		return
	}
	root, err := folderRenameRoot(input.Path)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.cfg != nil && s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run: la rinomina non modifica i file")
		return
	}
	cfg := latestConfig(s)
	results := make([]folderRenameResult, 0, len(input.Items))
	renamed := 0
	for _, item := range input.Items {
		result := folderRenameResult{Source: item.Source, Target: item.Target}
		source, target, validationErr := folderRenameValidatePair(root, item.Source, item.Target)
		if validationErr != nil {
			result.Error = validationErr.Error()
			results = append(results, result)
			continue
		}
		if SamePath(source, target) {
			result.OK = true
			results = append(results, result)
			continue
		}
		if _, statErr := os.Stat(target); statErr == nil {
			result.Error = "destinazione già esistente"
			results = append(results, result)
			continue
		} else if !os.IsNotExist(statErr) {
			result.Error = statErr.Error()
			results = append(results, result)
			continue
		}
		if _, err := RenameSidecars(source, target, cfg); err != nil {
			result.Error = fmt.Sprintf("sidecar: %v", err)
			results = append(results, result)
			continue
		}
		if err := moveAcrossDevices(source, target); err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		result.OK = true
		renamed++
		results = append(results, result)
	}
	jsonResponse(w, map[string]any{
		"ok":      true,
		"path":    root,
		"renamed": renamed,
		"items":   results,
	})
}

func folderRenameRoot(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("percorso cartella vuoto")
	}
	root, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("percorso non valido: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("cartella non accessibile: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("il percorso non è una cartella")
	}
	return filepath.Clean(root), nil
}

func folderRenameValidatePair(root, sourceValue, targetValue string) (string, string, error) {
	source, err := filepath.Abs(strings.TrimSpace(sourceValue))
	if err != nil {
		return "", "", fmt.Errorf("sorgente non valida")
	}
	target, err := filepath.Abs(strings.TrimSpace(targetValue))
	if err != nil {
		return "", "", fmt.Errorf("destinazione non valida")
	}
	if !folderRenameWithin(root, source) || !folderRenameWithin(root, target) {
		return "", "", fmt.Errorf("sorgente o destinazione fuori dalla cartella analizzata")
	}
	if filepath.Dir(source) != filepath.Dir(target) {
		return "", "", fmt.Errorf("la destinazione deve restare nella stessa sottocartella")
	}
	if filepath.Base(target) == "." || filepath.Base(target) == ".." || strings.ContainsRune(filepath.Base(target), 0) {
		return "", "", fmt.Errorf("nome destinazione non valido")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return "", "", fmt.Errorf("sorgente non accessibile: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("la sorgente è un collegamento simbolico")
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("la sorgente non è un file regolare")
	}
	return filepath.Clean(source), filepath.Clean(target), nil
}

func folderRenameWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func folderRenameScan(ctx context.Context, root string, cfg *Config) ([]folderRenameItem, error) {
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || !folderRenameVideoFile(path) {
			return nil
		}
		paths = append(paths, path)
		if len(paths) > folderRenameMaxFiles {
			return fmt.Errorf("la cartella contiene più di %d video: restringi il percorso", folderRenameMaxFiles)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	seriesCache := map[string][]folderRenameCandidate{}
	movieCache := map[string][]folderRenameCandidate{}
	items := make([]folderRenameItem, 0, len(paths))
	for _, path := range paths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		item, err := folderRenameInspect(ctx, root, path, cfg, seriesCache, movieCache)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func folderRenameVideoFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mp4", ".avi", ".m4v", ".mov", ".wmv", ".ts", ".webm", ".mpg", ".mpeg":
		return true
	default:
		return false
	}
}

func folderRenameInspect(ctx context.Context, root, path string, cfg *Config, seriesCache, movieCache map[string][]folderRenameCandidate) (folderRenameItem, error) {
	item := folderRenameItem{
		Source:   path,
		Relative: filepath.ToSlash(mustRelative(root, path)),
		Kind:     "unknown",
		Status:   "unmatched",
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if match := folderRenameEpisodeRE.FindStringSubmatch(base); match != nil {
		item.Kind = "series"
		item.Detected = folderRenameTitle(match[1])
		item.Season = folderRenameIntPtr(match[2])
		item.Episode = folderRenameIntPtr(match[3])
	} else if match := folderRenameEpisodeXRE.FindStringSubmatch(base); match != nil {
		item.Kind = "series"
		item.Detected = folderRenameTitle(match[1])
		item.Season = folderRenameIntPtr(match[2])
		item.Episode = folderRenameIntPtr(match[3])
	} else {
		item.Kind = "movie"
		item.Detected, item.Year = folderRenameMovieTitle(base)
	}
	if item.Detected == "" {
		item.Reason = "titolo non riconosciuto dal nome file"
		return item, nil
	}
	var candidates []folderRenameCandidate
	var err error
	if item.Kind == "series" {
		if cached, ok := seriesCache[folderRenameCacheKey(item.Detected)]; ok {
			candidates = cached
		} else {
			candidates, err = folderRenameSearchSeries(ctx, cfg, item.Detected)
			if err != nil {
				item.Reason = err.Error()
			} else {
				seriesCache[folderRenameCacheKey(item.Detected)] = candidates
			}
		}
	} else {
		if cached, ok := movieCache[folderRenameCacheKey(item.Detected)]; ok {
			candidates = cached
		} else {
			candidates, err = folderRenameSearchMovies(ctx, cfg, item.Detected, item.Year)
			if err != nil {
				item.Reason = err.Error()
			} else {
				movieCache[folderRenameCacheKey(item.Detected)] = candidates
			}
		}
	}
	for index := range candidates {
		candidates[index].Target = folderRenameTarget(path, item, candidates[index], "")
	}
	if len(candidates) == 0 {
		if item.Reason == "" {
			item.Reason = "nessuna corrispondenza TMDB/TVDB"
		}
		return item, nil
	}
	// The first candidate is the default proposal. Fetch the episode title only
	// for that proposal to keep a scan of a large folder bounded.
	if item.Kind == "series" && candidates[0].Provider == "tmdb" && item.Season != nil && item.Episode != nil {
		if title, titleErr := folderRenameEpisodeTitle(ctx, cfg, candidates[0], *item.Season, *item.Episode); titleErr == nil {
			candidates[0].Target = folderRenameTarget(path, item, candidates[0], title)
		}
	}
	item.Candidates = candidates
	item.Target = candidates[0].Target
	item.Status = "review"
	if candidates[0].Score >= 90 {
		item.Status = "proposta"
	}
	if _, err := os.Stat(item.Target); err == nil && !SamePath(item.Target, path) {
		item.Conflict = true
		item.Status = "conflict"
		item.Reason = "destinazione già esistente"
	}
	return item, nil
}

func folderRenameSearchSeries(ctx context.Context, cfg *Config, query string) ([]folderRenameCandidate, error) {
	candidates := []folderRenameCandidate{}
	var firstErr error
	if cfg.TmdbAPIKey != nil {
		items, err := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage()).SearchSeries(ctx, query)
		if err != nil {
			firstErr = err
		} else {
			for _, item := range items {
				if item.Name == nil || strings.TrimSpace(*item.Name) == "" {
					continue
				}
				year := ""
				if item.FirstAirDate != nil && len(*item.FirstAirDate) >= 4 {
					year = (*item.FirstAirDate)[:4]
				}
				candidates = append(candidates, folderRenameCandidate{Provider: "tmdb", ID: strconv.FormatInt(item.ID, 10), Title: *item.Name, Year: year, Score: folderRenameMatchScore(query, *item.Name, "")})
			}
		}
	}
	if cfg.TvdbAPIKey() != nil {
		items, err := tvdbClientFor(cfg).SearchSeries(ctx, query)
		if err != nil {
			if len(candidates) == 0 && firstErr == nil {
				firstErr = err
			}
		} else {
			for _, raw := range items {
				candidate := folderRenameTVDBCandidate(raw, query, false)
				if candidate.Title != "" {
					candidates = append(candidates, candidate)
				}
			}
		}
	}
	if len(candidates) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return folderRenameSortCandidates(candidates), nil
}

func folderRenameSearchMovies(ctx context.Context, cfg *Config, query string, year *int64) ([]folderRenameCandidate, error) {
	candidates := []folderRenameCandidate{}
	var firstErr error
	if cfg.TmdbAPIKey != nil {
		items, err := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage()).SearchMovies(ctx, query)
		if err != nil {
			firstErr = err
		} else {
			for _, item := range items {
				if item.Title == nil || strings.TrimSpace(*item.Title) == "" {
					continue
				}
				itemYear := ""
				if item.ReleaseDate != nil && len(*item.ReleaseDate) >= 4 {
					itemYear = (*item.ReleaseDate)[:4]
				}
				candidates = append(candidates, folderRenameCandidate{Provider: "tmdb", ID: strconv.FormatInt(item.ID, 10), Title: *item.Title, Year: itemYear, Score: folderRenameMatchScore(query, *item.Title, itemYear)})
			}
		}
	}
	if cfg.TvdbAPIKey() != nil {
		items, err := tvdbClientFor(cfg).SearchMovies(ctx, query)
		if err != nil {
			if len(candidates) == 0 && firstErr == nil {
				firstErr = err
			}
		} else {
			for _, raw := range items {
				candidate := folderRenameTVDBCandidate(raw, query, true)
				if candidate.Title != "" {
					candidates = append(candidates, candidate)
				}
			}
		}
	}
	if len(candidates) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return folderRenameSortCandidates(candidates), nil
}

func folderRenameTVDBCandidate(raw any, query string, movie bool) folderRenameCandidate {
	value, _ := raw.(map[string]any)
	candidate := folderRenameCandidate{Provider: "tvdb", ID: folderRenameAnyString(value["tvdb_id"]), Title: folderRenameAnyString(value["name"]), Year: folderRenameAnyString(value["year"])}
	if movie {
		candidate.ID = folderRenameAnyString(value["id"])
		candidate.Title = folderRenameAnyString(value["title"])
		candidate.Year = folderRenameAnyString(value["release_date"])
	}
	if len(candidate.Year) > 4 {
		candidate.Year = candidate.Year[:4]
	}
	candidate.Score = folderRenameMatchScore(query, candidate.Title, candidate.Year)
	return candidate
}

func folderRenameSortCandidates(candidates []folderRenameCandidate) []folderRenameCandidate {
	unique := make(map[string]folderRenameCandidate)
	for _, candidate := range candidates {
		key := candidate.Provider + ":" + candidate.ID + ":" + strings.ToLower(candidate.Title)
		if old, ok := unique[key]; !ok || candidate.Score > old.Score {
			unique[key] = candidate
		}
	}
	result := make([]folderRenameCandidate, 0, len(unique))
	for _, candidate := range unique {
		result = append(result, candidate)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		if result[i].Provider != result[j].Provider {
			return result[i].Provider == "tmdb"
		}
		return result[i].Title < result[j].Title
	})
	if len(result) > 6 {
		result = result[:6]
	}
	return result
}

func folderRenameEpisodeTitle(ctx context.Context, cfg *Config, candidate folderRenameCandidate, season, episode int64) (string, error) {
	if candidate.ID == "" {
		return "", fmt.Errorf("episodio non disponibile")
	}
	// The id belongs to the provider that found the candidate.
	var title *string
	var err error
	switch candidate.Provider {
	case "tvdb":
		tvdb := tvdbClientFor(cfg)
		if !tvdb.Configured() {
			return "", fmt.Errorf("episodio non disponibile")
		}
		title, err = tvdb.EpisodeTitle(ctx, candidate.ID, season, episode)
	default:
		if cfg.TmdbAPIKey == nil {
			return "", fmt.Errorf("episodio non disponibile")
		}
		title, err = NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage()).EpisodeTitle(ctx, candidate.ID, season, episode)
	}
	if err != nil || title == nil {
		return "", fmt.Errorf("episodio non disponibile")
	}
	return strings.TrimSpace(*title), nil
}

func folderRenameTarget(path string, item folderRenameItem, candidate folderRenameCandidate, episodeTitle string) string {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if item.Kind == "series" && item.Season != nil && item.Episode != nil {
		name := fmt.Sprintf("%s - S%02dE%02d", candidate.Title, *item.Season, *item.Episode)
		if episodeTitle != "" {
			name += " - " + episodeTitle
		}
		return filepath.Join(filepath.Dir(path), cleanupFilename(sanitizeInvalid(name))+"."+ext)
	}
	name := candidate.Title
	year := candidate.Year
	if year == "" && item.Year != nil {
		year = strconv.FormatInt(*item.Year, 10)
	}
	if year != "" {
		name += " (" + year + ")"
	}
	return filepath.Join(filepath.Dir(path), cleanupFilename(sanitizeInvalid(name))+"."+ext)
}

func folderRenameMovieTitle(base string) (string, *int64) {
	match := folderRenameYearRE.FindStringSubmatchIndex(base)
	if match == nil {
		return folderRenameTitle(base), nil
	}
	year, err := strconv.ParseInt(base[match[2]:match[3]], 10, 64)
	if err != nil {
		return folderRenameTitle(base), nil
	}
	title := strings.Trim(base[:match[0]], " ._-([")
	return folderRenameTitle(title), &year
}

func folderRenameTitle(value string) string {
	value = strings.NewReplacer(".", " ", "_", " ").Replace(value)
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func folderRenameIntPtr(value string) *int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func folderRenameAnyString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func folderRenameMatchScore(query, title, year string) int {
	if strings.EqualFold(strings.TrimSpace(query), strings.TrimSpace(title)) || SeriesNamesMatch(query, title) {
		if year != "" {
			return 100
		}
		return 95
	}
	queryTokens := strings.Fields(strings.ToLower(folderRenameTitle(query)))
	titleTokens := strings.Fields(strings.ToLower(folderRenameTitle(title)))
	if len(queryTokens) == 0 || len(titleTokens) == 0 {
		return 0
	}
	matched := 0
	for _, queryToken := range queryTokens {
		for _, titleToken := range titleTokens {
			if queryToken == titleToken {
				matched++
				break
			}
		}
	}
	return matched * 80 / len(queryTokens)
}

func folderRenameCacheKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func mustRelative(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return relative
}
