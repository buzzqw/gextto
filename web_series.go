package gextto

// web_series.go holds the HTTP handlers of series: the detail and season
// views, the rename, the re-download and forced/ignored episodes, the gap and
// episode search, the archive scan and the metadata refresh.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// renameInput is the optional body of the rename action: both flags stay
// false when the request carries no body.
type renameInput struct {
	Force      bool `json:"force"`
	SourceOnly bool `json:"source_only"`
}

func ToggleSeason(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	var input SeasonToggleInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	seriesList := make([]SeriesConfig, len(cfg.Series))
	copy(seriesList, cfg.Series)
	index := -1
	for i := range seriesList {
		if seriesList[i].Name == name || gh0ContainsString(seriesList[i].Aliases, name) {
			index = i
			break
		}
	}
	if index < 0 {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	if input.Enabled {
		kept := []int64{}
		for _, season := range seriesList[index].IgnoredSeasons {
			if season != input.Season {
				kept = append(kept, season)
			}
		}
		seriesList[index].IgnoredSeasons = kept
	} else if !containsInt64(seriesList[index].IgnoredSeasons, input.Season) {
		seriesList[index].IgnoredSeasons = append(seriesList[index].IgnoredSeasons, input.Season)
		sort.Slice(seriesList[index].IgnoredSeasons, func(a, b int) bool {
			return seriesList[index].IgnoredSeasons[a] < seriesList[index].IgnoredSeasons[b]
		})
	}
	ignoredSeasons := seriesList[index].IgnoredSeasons
	if err := SaveLibrary(s.cfg.DataDir, seriesList, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "ignored_seasons": ignoredSeasons})
}

func SeriesRenameExecute(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	var input renameInput
	hasInput, err := decodeOptional(r, &input)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	force := false
	sourceOnly := false
	if hasInput {
		force = input.Force
		sourceOnly = input.SourceOnly
	}
	status, body := seriesRenameApply(s, name, true, force, sourceOnly)
	jsonStatus(w, status, body)
}

func DeleteEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := pathParam(r, "series")
	season, _ := pathInt(r, "season")
	episode, _ := pathInt(r, "episode")
	if err := s.db.ResetEpisode(series, season, episode, true); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "deleted": true})
}

func Calendar(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if !metadataConfigured(cfg) {
		jsonError(w, http.StatusConflict, "TMDB API key is not configured (nor a TVDB one)")
		return
	}
	key := calendarCacheKey(cfg)
	if cached, ok := cacheGet(key, calendarCacheTTL); ok {
		jsonResponse(w, cached)
		return
	}
	response := buildCalendar(r.Context(), cfg)
	cachePut(key, response)
	jsonStatus(w, http.StatusOK, response)
}

func SeriesRenamePreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	var input RenameInput
	if err := decodeOptionalJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, value := seriesRenameApply(s, name, false, input.Force, input.SourceOnly)
	jsonStatus(w, status, value)
}

// episodeSource is one found episode release together with its origin.
type episodeSource struct {
	Release models.Release `json:"release"`
	Origin  string         `json:"origin"`
}

// gh2_episodeResult is the search result surfaced by the missing-episode search.
type gh2EpisodeResult struct {
	Release models.Release `json:"release"`
	Origin  string         `json:"origin"`
	Season  int64          `json:"season"`
	Episode int64          `json:"episode"`
}

func gh2ReleaseMatchesSeriesEpisode(release *models.Release, series *SeriesConfig, season, episode int64) bool {
	if release.Kind != "series" {
		return false
	}
	if release.Season == nil || *release.Season != season {
		return false
	}
	if len(release.EpisodeRange) == 0 {
		if release.Episode == nil || *release.Episode != episode {
			return false
		}
	} else if !containsInt64(release.EpisodeRange, episode) && !containsInt64(release.EpisodeRange, 0) {
		return false
	}
	if release.Series == nil {
		return false
	}
	name := *release.Series
	if SeriesNamesMatch(series.Name, name) {
		return true
	}
	for _, alias := range series.Aliases {
		if SeriesNamesMatch(alias, name) {
			return true
		}
	}
	return false
}

func gh2StoredSeriesEpisodeSources(s *AppState, series *SeriesConfig, season, episode int64) []episodeSource {
	query := fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode)
	results := []episodeSource{}
	if rows, err := s.db.SeriesFeedForEpisode(season, episode, 200); err == nil {
		for _, row := range rows {
			release := ParseRelease(row[0], row[1], row[2])
			if release == nil {
				continue
			}
			if gh2ReleaseMatchesSeriesEpisode(release, series, season, episode) {
				results = append(results, episodeSource{Release: *release, Origin: episodeOriginFeed})
			}
		}
	}
	if rows, err := s.archive.Search(query); err == nil {
		for _, row := range rows {
			release := ParseRelease(row[0], row[1], row[2])
			if release == nil {
				continue
			}
			if gh2ReleaseMatchesSeriesEpisode(release, series, season, episode) {
				results = append(results, episodeSource{Release: *release, Origin: episodeOriginArchive})
			}
		}
	}
	return results
}

func finalizeEpisodeSearchResults(results []episodeSource, cfg *Config, series *SeriesConfig) []episodeSource {
	filtered := make([]episodeSource, 0, len(results))
	for _, result := range results {
		if cfg.ReleaseAllowed(&result.Release) && cfg.SeriesReleaseAllowed(series, &result.Release.Quality, result.Release.Title) {
			filtered = append(filtered, result)
		}
	}
	seen := map[string]struct{}{}
	deduped := make([]episodeSource, 0, len(filtered))
	for _, result := range filtered {
		key, ok := releaseDedupKey(&result.Release)
		if !ok {
			continue
		}
		key += ":" + result.Origin
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, result)
	}
	sort.SliceStable(deduped, func(i, j int) bool {
		return cfg.ReleaseScore(&deduped[i].Release) > cfg.ReleaseScore(&deduped[j].Release)
	})
	return deduped
}

// lastResortEpisodeSources lists the copies of a gap that miss only the
// resolution of the profile, marked as the last resort. The caller uses them
// only when the gap has nothing within the profile, after every source
// (indexers, archive, eD2k) has answered: the same rule the automatic cycle
// applies, so the panel shows what the cycle would actually take.
func lastResortEpisodeSources(results []episodeSource, cfg *Config, series *SeriesConfig) []episodeSource {
	kept := make([]episodeSource, 0, len(results))
	for _, result := range results {
		if !cfg.ReleaseAllowed(&result.Release) {
			continue
		}
		if !cfg.SeriesLastResortAllowed(series, &result.Release.Quality, result.Release.Title) {
			continue
		}
		result.Release.LastResort = true
		kept = append(kept, result)
	}
	return kept
}

// RedownloadEpisode is the "Riscarica" action: it moves the archived file to
// the trash (so the disk check does not see it), deletes the episode row and
// records an explicit forced gap, then starts a series search. The forced gap is what makes the episode eligible
// again even when a season pack covers the season or a later episode is
// already archived.
func RedownloadEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := pathParam(r, "series")
	season, seasonOK := pathInt(r, "season")
	episode, episodeOK := pathInt(r, "episode")
	if !seasonOK || !episodeOK {
		jsonError(w, http.StatusBadRequest, "invalid path parameter")
		return
	}
	cfg := latestConfig(s)
	keepFiles := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("keep_files")), "1")
	avoidPrevious := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("avoid_previous")), "1")
	blockedTitle, moved, err := redownloadEpisode(s, cfg, series, season, episode, keepFiles, avoidPrevious)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if avoidPrevious {
		// Kick off a series search right away so the episode is re-queued without
		// waiting for the next scheduled cycle.
		clone := r.Clone(r.Context())
		query := clone.URL.Query()
		query.Set("domain", "series")
		clone.URL.RawQuery = query.Encode()
		RunNow(&v2DiscardWriter{}, clone, s)
	}
	logging.Info(fmt.Sprintf("🔁 «%s» S%02dE%02d marked for re-download%s", series, season, episode, func() string {
		if moved == "" {
			return ""
		}
		return " (file moved to the trash)"
	}()))
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":            true,
		"redownload":    true,
		"moved_to":      moved,
		"blocked_title": blockedTitle,
		"message":       "episodio reso nuovamente scaricabile: ricerca serie avviata",
	})
}

// missingSearchProgress reports how many episodes the live search has
// finished and which phase is running ("episodes" or "ed2k").
type missingSearchProgress func(done, total int, phase string)

// missingSearchSyncBudget is how long the synchronous API call waits before
// answering with the episodes it has already searched. The UI runs the same
// search as a background job: no deadline, live progress and a cancel button.
const missingSearchSyncBudget = 90 * time.Second

func SeriesSearchMissing(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	cfg := latestConfig(s)
	series := findSeries(cfg, name)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), missingSearchSyncBudget)
	defer cancel()
	outcome := searchMissingEpisodes(ctx, s, cfg, series, nil)
	episodes := make([]map[string]any, 0, len(outcome.Gaps))
	for _, gap := range outcome.Gaps {
		episodes = append(episodes, map[string]any{"season": gap[0], "episode": gap[1]})
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"series":             series.Name,
		"searched":           outcome.Searched,
		"total":              outcome.TotalMissing,
		"complete":           outcome.Complete,
		"episodes":           episodes,
		"results":            outcome.Results,
		"metadata_available": outcome.MetadataReady,
	})
}

func ForceEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := pathParam(r, "series")
	season, okSeason := pathInt(r, "season")
	episode, okEpisode := pathInt(r, "episode")
	if !okSeason || !okEpisode {
		jsonError(w, http.StatusBadRequest, "invalid episode path")
		return
	}
	if err := s.db.ResetEpisode(series, season, episode, false); err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "forced": true})
}

func SeriesDetail(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	name := pathParam(r, "name")
	series := findSeries(cfg, name)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	ignoredSeasons := append([]int64(nil), series.IgnoredSeasons...)
	seasonCounts, err := s.db.SeriesSeasonCounts(series.Name)
	if err != nil {
		seasonCounts = nil
	}
	for _, counts := range seasonCounts {
		season := counts[0]
		if !SeasonAllowedForScan(series.Seasons, season) && !containsInt64(ignoredSeasons, season) {
			ignoredSeasons = append(ignoredSeasons, season)
		}
	}
	episodes, err := s.db.EpisodesForSeries(series.Name, ignoredSeasons)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	gaps := []any{}
	archiveGaps, err := s.db.ArchiveGapsForSeries(series.Name)
	if err != nil {
		archiveGaps = nil
	}
	for _, gap := range archiveGaps {
		season := gap[0]
		if containsInt64(ignoredSeasons, season) || !SeasonAllowedForScan(series.Seasons, season) {
			continue
		}
		gaps = append(gaps, map[string]any{"season": gap[0], "episode": gap[1]})
	}
	metadata := []any{}
	for _, counts := range seasonCounts {
		metadata = append(metadata, map[string]any{"season": counts[0], "count": counts[1]})
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":       true,
		"series":   *series,
		"episodes": episodes,
		"gaps":     gaps,
		"metadata": metadata,
	})
}

func SeriesSeenGroupedView(w http.ResponseWriter, r *http.Request, s *AppState) {
	status, value := SeenGrouped(s, "series", r)
	jsonStatus(w, status, value)
}

func SetSeriesPath(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	var input SeriesPathInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := *latestConfig(s)
	cfg.Series = append([]SeriesConfig(nil), cfg.Series...)
	index := -1
	for i := range cfg.Series {
		if cfg.Series[i].Name == name {
			index = i
			break
		}
		for _, alias := range cfg.Series[i].Aliases {
			if alias == name {
				index = i
				break
			}
		}
		if index >= 0 {
			break
		}
	}
	if index < 0 {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	series := &cfg.Series[index]
	if input.ArchivePath != nil {
		series.ArchivePath = strings.TrimSpace(*input.ArchivePath)
	}
	if input.Timeframe != nil {
		series.Timeframe = *input.Timeframe
		if series.Timeframe < 0 {
			series.Timeframe = 0
		}
	}
	archivePath := series.ArchivePath
	timeframe := series.Timeframe
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":           true,
		"archive_path": archivePath,
		"timeframe":    timeframe,
	})
}

func SeenGrouped(state *AppState, kind string, r *http.Request) (int, any) {
	limit := queryIntVal(r, "limit", 50)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	page := queryIntVal(r, "page", 1)
	if page < 1 {
		page = 1
	}
	// Keep (page-1)*limit from overflowing on an absurd page number.
	if page > math.MaxInt32/limit {
		page = math.MaxInt32 / limit
	}
	offset := (page - 1) * limit
	queryText := queryParam(r, "q")
	var groups any
	var total int64
	var err error
	if kind == "series" {
		grouped, aggregated, callErr := state.db.SeriesSeenGrouped(offset, limit, queryText)
		groups, total, err = grouped, aggregated, callErr
	} else {
		grouped, aggregated, callErr := state.db.MoviesSeenGrouped(offset, limit, queryText)
		groups, total, err = grouped, aggregated, callErr
	}
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
	}
	pages := total / int64(limit)
	if total%int64(limit) != 0 {
		pages++
	}
	if pages < 1 {
		pages = 1
	}
	return http.StatusOK, map[string]any{
		"ok":     true,
		"groups": groups,
		"total":  total,
		"page":   page,
		"pages":  pages,
	}
}

func seenEntries(s *AppState, kind string, r *http.Request) (int, any) {
	group := queryParam(r, "key")
	if group == "" {
		group = queryParam(r, "group")
	}
	if strings.TrimSpace(group) == "" {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": "key required"}
	}
	limit := queryIntVal(r, "limit", 500)
	items, err := s.db.SeenByGroup(kind, strings.TrimSpace(group), limit)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
	}
	return http.StatusOK, map[string]any{"ok": true, "items": items}
}

func Gaps(w http.ResponseWriter, r *http.Request, s *AppState) {
	gaps, err := libraryGaps(s)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := []map[string]any{}
	for _, gap := range gaps {
		result = append(result, map[string]any{
			"series":   gap.Series,
			"season":   gap.Season,
			"episode":  gap.Episode,
			"air_date": gap.AirDate,
			"aliases":  gap.Aliases,
		})
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "count": len(result), "items": result})
}

func SeriesEpisodes(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	cfg := latestConfig(s)
	ignored := []int64{}
	if series := findSeries(cfg, name); series != nil {
		ignored = series.IgnoredSeasons
	}
	items, err := s.db.EpisodesForSeries(name, ignored)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func SeriesSeenView(w http.ResponseWriter, r *http.Request, s *AppState) {
	status, value := seenEntries(s, "series", r)
	jsonStatus(w, status, value)
}

// asInt64 converts a JSON number to int64 and rejects a fractional value.
func asInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case int:
		return int64(number), true
	case float64:
		if number != math.Trunc(number) {
			return 0, false
		}
		return int64(number), true
	case json.Number:
		parsed, err := number.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// parseSeasonEpisodes parses the season and every episode a file name
// carries: one episode, or the contiguous range of a multi-episode file
// ("S01E01-E02"), so an archive scan registers the file under each of them.
func parseSeasonEpisodes(name string) (int64, []int64, bool) {
	if season, first, last, ok := multiEpisodeRange(name); ok {
		episodes := make([]int64, 0, last-first+1)
		for episode := first; episode <= last; episode++ {
			episodes = append(episodes, episode)
		}
		return season, episodes, true
	}
	season, episode, ok := parseSeasonEpisode(name)
	if !ok {
		return 0, nil, false
	}
	return season, []int64{episode}, true
}

// scanArchivePath registers the videos of an archive folder as archived
// episodes.
func scanArchivePath(db *Database, series *SeriesConfig, path string, cfg *Config) (int, int, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return 0, 0, fmt.Errorf("archive path is not a directory: %s", path)
	}
	files, err := VideoFiles(path)
	if err != nil {
		return 0, 0, err
	}
	found := 0
	records := make([]ArchiveFileRecord, 0, len(files))
	for _, file := range files {
		name := filepath.Base(file)
		season, episodes, ok := parseSeasonEpisodes(name)
		if !ok || season <= 0 || len(episodes) == 0 || episodes[0] <= 0 {
			continue
		}
		// Never register a broken file as the archived copy: a zero-filled or
		// unrecognised video must not make the episode look present.
		if invalidErr := validateCompletedFile(file); invalidErr != nil {
			logging.Warn("⚠️ Archive scan skipped an invalid video file",
				"path", file, "error", invalidErr.Error())
			continue
		}
		found++
		title := archiveFileTitle(name)
		size := archiveFileSize(file)
		qualityScore := cfg.FileScore(file, "series", series.Name)
		for _, episode := range episodes {
			records = append(records, ArchiveFileRecord{
				Season:       season,
				Episode:      episode,
				Title:        title,
				Path:         file,
				SizeBytes:    size,
				QualityScore: qualityScore,
			})
		}
	}
	// One transaction for the whole series instead of one commit per file. The
	// returned count stays "files updated", as the page has always reported it:
	// a range file is one file even when it registers several episodes.
	if _, batchErr := db.SyncArchiveFilesScored(series.Name, records); batchErr != nil {
		return found, 0, batchErr
	}
	return found, found, nil
}

func IgnoreEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := pathParam(r, "series")
	season, okSeason := pathInt(r, "season")
	episode, okEpisode := pathInt(r, "episode")
	if !okSeason || !okEpisode {
		jsonError(w, http.StatusBadRequest, "invalid episode path")
		return
	}
	var input IgnoreEpisodeInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.SetEpisodeIgnored(series, season, episode, input.Ignored, input.Reason); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "ignored": input.Ignored})
}

func ScanSeriesArchive(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	name := pathParam(r, "name")
	series := findSeries(cfg, name)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	if ArchiveImportBusyContains(series.Name) {
		jsonResponse(w, map[string]any{
			"ok":      true,
			"skipped": true,
			"reason":  "archive import in progress",
		})
		return
	}
	var requestedPath *string
	body, bodyErr := readOptionalBody(r)
	if bodyErr != nil {
		jsonError(w, http.StatusBadRequest, bodyErr.Error())
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		var input ArchiveScanInput
		if err := json.Unmarshal(body, &input); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		requestedPath = input.Path
	}
	path := ""
	if resolved := cfg.ResolveArchivePath(series); resolved != nil {
		path = *resolved
	}
	if requestedPath != nil && strings.TrimSpace(*requestedPath) != "" {
		path = *requestedPath
	}
	found, updated, err := scanArchivePath(s.db, series, path, cfg)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":      true,
		"found":   found,
		"updated": updated,
		"series":  series.Name,
		"path":    path,
	})
}

func SeriesInfo(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	name := pathParam(r, "name")
	series := findSeries(cfg, name)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	ctx := r.Context()
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	// TMDB when its key is set, else TVDB: both return the TMDB /tv shape.
	meta := seriesMetadataWith(cfg, tmdb)
	var storedID *string
	if id := meta.StoredID(series); id != "" {
		storedID = &id
	}
	value, err := meta.SeriesInfo(ctx, series.Name, storedID)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if value == nil {
		jsonResponse(w, map[string]any{"ok": true, "info": nil})
		return
	}

	var poster *string
	if path, ok := value["poster_path"].(string); ok && path != "" {
		address := metadataImageURL(path, "w300")
		poster = &address
	}
	var network *string
	if items, ok := value["networks"].([]any); ok && len(items) > 0 {
		if item, ok := items[0].(map[string]any); ok {
			if label, ok := item["name"].(string); ok {
				network = &label
			}
		}
	}
	var year *string
	if text, ok := value["first_air_date"].(string); ok {
		runes := []rune(text)
		if len(runes) > 4 {
			runes = runes[:4]
		}
		trimmed := string(runes)
		year = &trimmed
	}
	var country *string
	if items, ok := value["origin_country"].([]any); ok && len(items) > 0 {
		if text, ok := items[0].(string); ok {
			country = &text
		}
	}
	if country == nil {
		if items, ok := value["production_countries"].([]any); ok && len(items) > 0 {
			if item, ok := items[0].(map[string]any); ok {
				if text, ok := item["iso_3166_1"].(string); ok {
					country = &text
				}
			}
		}
	}
	var lastAirDate *string
	if text, ok := value["last_air_date"].(string); ok {
		lastAirDate = &text
	}

	tvdb := tvdbClientFor(cfg)
	var tvdbID *int64
	if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(series.TvdbID), 10, 64); parseErr == nil {
		tvdbID = &parsed
	} else if parsed, ok := tvdbSeriesID(v2AnyString(value["tvdb_id"])); ok {
		tvdbID = &parsed
	}
	if tvdbID == nil {
		if results, searchErr := tvdb.SearchSeries(ctx, series.Name); searchErr == nil && len(results) > 0 {
			if item, ok := results[0].(map[string]any); ok {
				if parsed, ok := asInt64(item["tvdb_id"]); ok {
					tvdbID = &parsed
				} else if text, ok := item["tvdb_id"].(string); ok {
					if parsed, parseErr := strconv.ParseInt(text, 10, 64); parseErr == nil {
						tvdbID = &parsed
					}
				}
			}
		}
	}

	cast := []any{}
	if tvdbID != nil {
		if characters, charErr := tvdb.SeriesCharacters(ctx, *tvdbID); charErr == nil {
			for _, character := range characters {
				item, _ := character.(map[string]any)
				personName, hasName := item["name"].(string)
				person, hasPerson := asInt64(item["tvdb_id"])
				if hasName && hasPerson {
					cast = append(cast, map[string]any{
						"name": personName,
						"url":  fmt.Sprintf("https://thetvdb.com/dereferrer/people/%d", person),
					})
				}
			}
		}
	}
	if len(cast) == 0 {
		if id, ok := asInt64(value["id"]); ok {
			if personCast, castErr := tmdb.SeriesCast(ctx, strconv.FormatInt(id, 10)); castErr == nil {
				for _, entry := range personCast {
					cast = append(cast, map[string]any{
						"name": entry.Name,
						"url":  fmt.Sprintf("https://www.themoviedb.org/person/%d", entry.ID),
					})
				}
			}
		}
	}

	genres := []string{}
	if items, ok := value["genres"].([]any); ok {
		for _, item := range items {
			if entry, ok := item.(map[string]any); ok {
				if label, ok := entry["name"].(string); ok {
					genres = append(genres, label)
				}
			}
		}
	}
	tvdbURL := ""
	if tvdbID != nil {
		tvdbURL = fmt.Sprintf("https://thetvdb.com/dereferrer/series/%d", *tvdbID)
	} else {
		tvdbURL = "https://thetvdb.com/search?query=" + url.QueryEscape(series.Name)
	}

	jsonResponse(w, map[string]any{
		"ok": true,
		"info": map[string]any{
			"name":           value["name"],
			"overview":       value["overview"],
			"poster":         poster,
			"network":        network,
			"year":           year,
			"seasons":        value["number_of_seasons"],
			"episodes_total": value["number_of_episodes"],
			"vote":           value["vote_average"],
			"status":         value["status"],
			"country":        country,
			"last_air_date":  lastAirDate,
			"last_episode":   value["last_episode_to_air"],
			"next_episode":   value["next_episode_to_air"],
			"tmdb_id":        value["id"],
			"tvdb_id":        seriesTvdbID(series.TvdbID, tvdbID),
			"tvdb_url":       tvdbURL,
			"cast":           cast,
			"genres":         genres,
		},
	})
}

// seriesTvdbID is the stored TVDB id of a series, else the one found.
func seriesTvdbID(stored string, found *int64) string {
	if strings.TrimSpace(stored) != "" || found == nil {
		return stored
	}
	return strconv.FormatInt(*found, 10)
}

func SeriesList(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	items := []map[string]any{}
	for index, series := range cfg.Series {
		items = append(items, map[string]any{
			"id":           index + 1,
			"name":         series.Name,
			"seasons":      series.Seasons,
			"quality":      series.Quality,
			"language":     series.Language,
			"archive_path": series.ArchivePath,
			"enabled":      series.Enabled,
			"aliases":      series.Aliases,
			"tmdb_id":      series.TmdbID,
			"timeframe":    series.Timeframe,
		})
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

// gh7_episode_result is one episode search result with its origin and score.
type gh7EpisodeResult struct {
	Release models.Release `json:"release"`
	Origin  string         `json:"origin"`
	Score   int64          `json:"score"`
}

// gh7_release_matches_series_episode tells whether a release is the episode
// asked for: a targeted search must also surface packs that contain the
// episode (S01E01-08) and whole-season packs (S01).
func gh7ReleaseMatchesSeriesEpisode(release *models.Release, series SeriesConfig, season, episode int64) bool {
	if release.Kind != "series" || release.Season == nil || *release.Season != season {
		return false
	}
	if len(release.EpisodeRange) == 0 {
		if release.Episode == nil || *release.Episode != episode {
			return false
		}
	} else if !containsInt64(release.EpisodeRange, episode) && !containsInt64(release.EpisodeRange, 0) {
		return false
	}
	if release.Series == nil {
		return false
	}
	if SeriesNamesMatch(series.Name, *release.Series) {
		return true
	}
	for _, alias := range series.Aliases {
		if SeriesNamesMatch(alias, *release.Series) {
			return true
		}
	}
	return false
}

// storedSeriesEpisodeSources collects the results already stored locally
// (feed rows and archive) without any network traffic.
func storedSeriesEpisodeSources(s *AppState, series SeriesConfig, season, episode int64) []gh7EpisodeResult {
	query := fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode)
	results := []gh7EpisodeResult{}

	if s.db != nil {
		if rows, err := s.db.SeriesFeedForEpisode(season, episode, 200); err == nil {
			for _, row := range rows {
				if release := ParseRelease(row[0], row[1], row[2]); release != nil {
					if gh7ReleaseMatchesSeriesEpisode(release, series, season, episode) {
						results = append(results, gh7EpisodeResult{Release: *release, Origin: episodeOriginFeed})
					}
				}
			}
		}
	}

	if s.archive != nil {
		if rows, err := s.archive.Search(query); err == nil {
			for _, row := range rows {
				if release := ParseRelease(row[0], row[1], row[2]); release != nil {
					if gh7ReleaseMatchesSeriesEpisode(release, series, season, episode) {
						results = append(results, gh7EpisodeResult{Release: *release, Origin: episodeOriginArchive})
					}
				}
			}
		}
	}
	return results
}

// gh7_finalize_episode_search_results applies the same filters as the "Accoda"
// click, deduplicates by magnet hash + origin and sorts by release score.
func gh7FinalizeEpisodeSearchResults(results []gh7EpisodeResult, cfg *Config, series SeriesConfig) []gh7EpisodeResult {
	allowed := make([]gh7EpisodeResult, 0, len(results))
	for _, result := range results {
		if cfg.ReleaseAllowed(&result.Release) && cfg.SeriesReleaseAllowed(&series, &result.Release.Quality, result.Release.Title) {
			result.Score = cfg.ReleaseScore(&result.Release)
			allowed = append(allowed, result)
		}
	}
	seen := map[string]struct{}{}
	deduped := make([]gh7EpisodeResult, 0, len(allowed))
	for _, result := range allowed {
		key, ok := releaseDedupKey(&result.Release)
		if !ok {
			continue
		}
		key += ":" + result.Origin
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, result)
	}
	sort.SliceStable(deduped, func(i, j int) bool {
		return deduped[i].Score > deduped[j].Score
	})
	return deduped
}

// lastResortEpisodeResults lists the copies that miss only the
// resolution of the profile, marked as the last resort: the caller uses them
// only when the episode has nothing within the profile, exactly as the
// automatic gap pass does.
func lastResortEpisodeResults(results []gh7EpisodeResult, cfg *Config, series SeriesConfig) []gh7EpisodeResult {
	kept := make([]gh7EpisodeResult, 0, len(results))
	for _, result := range results {
		if !cfg.ReleaseAllowed(&result.Release) {
			continue
		}
		if !cfg.SeriesLastResortAllowed(&series, &result.Release.Quality, result.Release.Title) {
			continue
		}
		result.Release.LastResort = true
		result.Score = cfg.ReleaseScore(&result.Release)
		kept = append(kept, result)
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Score > kept[j].Score })
	return kept
}

// searchStageContextKey carries the observer of a search's stages.
type searchStageContextKey struct{}

func reportSearchStage(ctx context.Context, name string) {
	if report, ok := ctx.Value(searchStageContextKey{}).(func(string)); ok {
		report(name)
	}
}

func searchSeriesEpisodeSources(ctx context.Context, s *AppState, cfg *Config, series SeriesConfig, season, episode int64) []gh7EpisodeResult {
	results := storedSeriesEpisodeSources(s, series, season, episode)
	reportSearchStage(ctx, "sources")
	if s.engine != nil {
		for _, release := range s.engine.SearchSeriesEpisode(ctx, cfg, &series, season, episode, true) {
			if gh7ReleaseMatchesSeriesEpisode(&release, series, season, episode) {
				results = append(results, gh7EpisodeResult{Release: release, Origin: episodeOriginIndexer})
			}
		}
	}
	// Same fallback as the automatic gap pass, but on demand: when the torrent
	// plane leaves nothing usable for this episode, ask eD2k/Kad too. The check
	// runs after the usual filters, so a stored release rejected by quality or
	// language does not hide the episode from the fallback. The switch is the
	// explicit ed2k_fallback, so a manual search never surprises the operator.
	final := gh7FinalizeEpisodeSearchResults(results, cfg, series)
	if len(final) == 0 {
		reportSearchStage(ctx, "ed2k")
		for _, release := range ed2kManualEpisodeFallback(ctx, s.engine, cfg, &series, season, episode) {
			results = append(results, gh7EpisodeResult{Release: release, Origin: episodeOriginED2K})
		}
		final = gh7FinalizeEpisodeSearchResults(results, cfg, series)
	}
	if len(final) == 0 {
		// The cycle takes such a copy as a last resort; the manual search must
		// show it, marked, instead of pretending the episode has no source.
		final = lastResortEpisodeResults(results, cfg, series)
	}
	return final
}

func gh7JsonString(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	raw, ok := value[key]
	if !ok {
		return ""
	}
	if text, ok := raw.(string); ok {
		return text
	}
	return ""
}

// SearchEpisode handles POST /api/episodes/{series}/{season}/{episode}/search.
func SearchEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	seriesName := pathParam(r, "series")
	season, okSeason := pathInt(r, "season")
	episode, okEpisode := pathInt(r, "episode")
	if !okSeason || !okEpisode {
		jsonError(w, http.StatusBadRequest, "invalid episode")
		return
	}
	cfg := latestConfig(s)
	series, ok := gh7FindSeries(cfg, seriesName)
	if !ok {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	if containsInt64(series.IgnoredSeasons, season) ||
		!SeasonAllowedForScan(series.Seasons, season) {
		jsonError(w, http.StatusUnprocessableEntity, "season is not monitored")
		return
	}
	query := fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode)
	results := searchSeriesEpisodeSources(r.Context(), s, cfg, series, season, episode)
	// Persist what the online search found so the archive can answer the next
	// request locally (the feed cycle and the archive web search already do).
	if len(results) > 0 {
		releases := make([]models.Release, 0, len(results))
		for _, result := range results {
			releases = append(releases, result.Release)
		}
		if s.archive != nil {
			if err := s.archive.SaveBatch(releases, cfg); err != nil {
				logging.Warn("failed to save episode search results to archive",
					"series", seriesName, "season", season, "episode", episode, "error", err)
			}
		}
		if s.db != nil {
			if err := s.db.RecordSeenBatch(releases, cfg); err != nil {
				logging.Warn("episode search seen recording failed", "error", err)
			}
		}
	}
	feedMatches := 0
	for _, result := range results {
		if result.Origin == episodeOriginFeed {
			feedMatches++
		}
	}
	logging.Info(fmt.Sprintf("🔎 Search for %s S%02dE%02d: %s, %d from the feeds",
		seriesName, season, episode, countLabel(len(results), "result", "results"), feedMatches))
	_ = s.db.MarkGapSearched(series.Name, season, episode)
	jsonResponse(w, map[string]any{
		"ok":           true,
		"query":        query,
		"results":      results,
		"feed_matches": feedMatches,
	})
}

// EpisodeSources returns the sources already collected for one episode. It is
// deliberately read-only: opening the list must not start another search.
func EpisodeSources(w http.ResponseWriter, r *http.Request, s *AppState) {
	seriesName := pathParam(r, "series")
	season, okSeason := pathInt(r, "season")
	episode, okEpisode := pathInt(r, "episode")
	if !okSeason || !okEpisode {
		jsonError(w, http.StatusBadRequest, "invalid episode")
		return
	}
	cfg := latestConfig(s)
	series, ok := gh7FindSeries(cfg, seriesName)
	if !ok {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	results := storedSeriesEpisodeSources(s, series, season, episode)
	for i := range results {
		results[i].Score = cfg.ReleaseScore(&results[i].Release)
	}
	jsonResponse(w, map[string]any{
		"ok":      true,
		"series":  series.Name,
		"season":  season,
		"episode": episode,
		"results": results,
	})
}

// refreshSeriesMetadataForSearch loads the season episode counts needed by the
// manual missing search (from TMDB, or TVDB without a TMDB key). It is
// deliberately limited to counts: the explicit metadata action below
// additionally refreshes status and air dates.
func refreshSeriesMetadataForSearch(ctx context.Context, cfg *Config, db *Database, series *SeriesConfig) error {
	meta := seriesMetadataFor(cfg)
	if !meta.Configured() {
		return fmt.Errorf("TMDB API key is not configured (nor a TVDB one)")
	}
	resolved, err := metadataSeriesID(ctx, meta, series)
	if err != nil {
		return err
	}
	if resolved == nil {
		return fmt.Errorf("series not found on %s", strings.ToUpper(meta.Source()))
	}
	counts, err := meta.SeasonCounts(ctx, *resolved)
	if err != nil {
		return err
	}
	values := make([][2]int64, 0, len(counts))
	for season, count := range counts {
		values = append(values, [2]int64{season, count})
	}
	sort.Slice(values, func(i, j int) bool { return values[i][0] < values[j][0] })
	if len(values) == 0 {
		return fmt.Errorf("%s returned no season metadata", strings.ToUpper(meta.Source()))
	}
	return db.SaveSeriesMetadata(series.Name, values)
}

// SeriesMetadataRefresh handles POST /api/series/{name}/metadata.
func SeriesMetadataRefresh(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	live := latestConfig(s)
	cfg := *live
	cfg.Series = append([]SeriesConfig(nil), live.Series...)
	cfg.Movies = append([]MovieConfig(nil), live.Movies...)

	series, ok := gh7FindSeries(&cfg, name)
	if !ok {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	meta := seriesMetadataFor(&cfg)
	if !meta.Configured() {
		jsonError(w, http.StatusConflict, "TMDB API key is not configured (nor a TVDB one)")
		return
	}
	ctx := r.Context()
	source := strings.ToUpper(meta.Source())
	resolved := meta.StoredID(&series)
	if resolved == "" {
		id, err := meta.ResolveSeriesID(ctx, series.Name)
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		if id == nil {
			jsonError(w, http.StatusNotFound, "series not found on "+source)
			return
		}
		resolved = *id
	}
	counts, err := meta.SeasonCounts(ctx, resolved)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	values := make([][2]int64, 0, len(counts))
	for season, count := range counts {
		values = append(values, [2]int64{season, count})
	}
	sort.Slice(values, func(i, j int) bool { return values[i][0] < values[j][0] })

	airDates := []EpisodeAirDate{}
	airDateErrors := 0
	for _, value := range values {
		season := value[0]
		if season <= 0 {
			continue
		}
		episodes, err := meta.SeasonEpisodes(ctx, resolved, season)
		if err != nil {
			airDateErrors++
			logging.Warn("season air-date refresh failed",
				"series", series.Name, "season", season, "source", meta.Source(), "error", err.Error())
			continue
		}
		for _, episode := range episodes {
			episodeSeason := season
			if episode.SeasonNumber != nil {
				episodeSeason = *episode.SeasonNumber
			}
			if episode.EpisodeNumber == nil || episodeSeason != season {
				continue
			}
			airDate := ""
			if episode.AirDate != nil {
				airDate = *episode.AirDate
			}
			airDates = append(airDates, EpisodeAirDate{Season: season, Episode: *episode.EpisodeNumber, AirDate: airDate})
		}
	}
	if err := s.db.SaveSeriesMetadata(series.Name, values); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if info, err := meta.SeriesInfo(ctx, series.Name, &resolved); err == nil && info != nil {
		previous := SeriesEndRecord{}
		if records, err := s.db.SeriesEndRecords(); err == nil {
			previous = records[series.Name]
		}
		if _, err := refreshSeriesEndRecord(ctx, &cfg, s.db, &series, info, previous); err != nil {
			logging.Warn("The status of a series could not be saved", "series", series.Name, "error", err.Error())
		}
	}
	stored := false
	// A TVmaze id is not kept with the series: it is not a TMDB/TVDB id.
	if meta.StoredID(&series) == "" && meta.Source() != "tvmaze" {
		for i := range cfg.Series {
			item := &cfg.Series[i]
			aliasMatch := false
			for _, alias := range item.Aliases {
				if alias == name {
					aliasMatch = true
					break
				}
			}
			if item.Name == series.Name || aliasMatch {
				if meta.Source() == "tvdb" {
					item.TvdbID = resolved
				} else {
					item.TmdbID = resolved
				}
				stored = true
			}
		}
		if stored {
			_ = SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies)
		}
	}
	if err := s.db.SaveEpisodeAirDates(series.Name, airDates); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	seasons := make([]map[string]any, 0, len(values))
	for _, value := range values {
		seasons = append(seasons, map[string]any{"season": value[0], "episodes": value[1]})
	}
	tmdbID, tvdbID := resolved, ""
	switch meta.Source() {
	case "tvdb":
		tmdbID, tvdbID = "", resolved
	case "tvmaze":
		tmdbID, tvdbID = strings.TrimSpace(series.TmdbID), strings.TrimSpace(series.TvdbID)
	}
	jsonResponse(w, map[string]any{
		"ok":                true,
		"series":            series.Name,
		"tmdb_id":           tmdbID,
		"tvdb_id":           tvdbID,
		"tmdb_id_stored":    stored,
		"source":            meta.Source(),
		"air_dates_updated": len(airDates),
		"air_date_errors":   airDateErrors,
		"seasons":           seasons,
	})
}

// libraryGap is one missing episode of a monitored series.
type libraryGap struct {
	Series  string
	Season  int64
	Episode int64
	AirDate string
	Aliases []string
}

// libraryGaps lists the missing episodes of the monitored series: the API
// (GET /api/gaps) and the Missing tab of the Series page read the same list.
func libraryGaps(s *AppState) ([]libraryGap, error) {
	items, err := s.db.ArchiveGaps()
	if err != nil {
		return nil, err
	}
	cfg := latestConfig(s)
	airDates, _ := s.db.EpisodeAirDates()
	result := []libraryGap{}
	// The gaps of a season share one match: FindSeriesMatch tries every
	// configured name and alias, so it runs once per series and season.
	type seriesSeason struct {
		series string
		season int64
	}
	matches := map[seriesSeason]*SeriesConfig{}
	for _, gap := range items {
		key := seriesSeason{series: gap.Series, season: gap.Season}
		series, known := matches[key]
		if !known {
			season := gap.Season
			series = cfg.FindSeriesMatch(gap.Series, &season)
			matches[key] = series
		}
		if series == nil {
			continue
		}
		result = append(result, libraryGap{
			Series: gap.Series, Season: gap.Season, Episode: gap.Episode,
			AirDate: airDates[airDateKey(gap)],
			Aliases: series.Aliases,
		})
	}
	return result, nil
}
