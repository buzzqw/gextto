package gextto

// web_movies.go holds the HTTP handlers of movies: the detail view with its
// metadata, cast and archive matches, the library CRUD, the metadata search
// and apply, the seen lists and the re-download.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

func parseIntPtr(value string) *int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func parseIntOrNil(value string) any {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return nil
	}
	return parsed
}

func idFromAny(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func MovieDetail(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	id, _ := pathInt(r, "id")
	var movie *MovieConfig
	for i := range cfg.Movies {
		if cfg.Movies[i].ID == id {
			movie = &cfg.Movies[i]
			break
		}
	}
	if movie == nil {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	movieCopy := *movie

	var history []MovieHistory
	if all, err := s.db.DownloadedMovies(200); err == nil {
		for _, item := range all {
			if item.Name == movieCopy.Name {
				history = append(history, item)
			}
		}
	}
	if history == nil {
		history = []MovieHistory{}
	}

	query := movieCopy.Name
	if strings.TrimSpace(movieCopy.Year) != "" {
		query = fmt.Sprintf("%s %s", movieCopy.Name, movieCopy.Year)
	}
	matches := []any{}
	if entries, err := s.archive.Search(query); err == nil {
		for index, entry := range entries {
			if index >= 20 {
				break
			}
			matches = append(matches, map[string]any{"title": entry[0], "magnet": entry[1], "source": entry[2]})
		}
	}

	storedMetadata := map[string]any{
		"id":             parseIntOrNil(movieCopy.TmdbID),
		"title":          movieCopy.Name,
		"original_title": movieCopy.OriginalTitle,
		"overview":       movieCopy.Overview,
		"poster_path":    movieCopy.PosterPath,
		"release_date":   movieCopy.Year,
		"source":         metadataSource(movieCopy.TvdbID),
	}
	hasStoredMetadata := movieCopy.TmdbID != "" || movieCopy.TvdbID != "" || movieCopy.Overview != "" || movieCopy.PosterPath != ""

	var metadata any
	cast := []CastMember{}
	if cfg.TmdbAPIKey != nil {
		key := *cfg.TmdbAPIKey
		tmdb := NewTmdbClientWithLanguage(&key, cfg.TmdbLanguage())
		if tmdbID, err := strconv.ParseInt(strings.TrimSpace(movieCopy.TmdbID), 10, 64); err == nil {
			details, err := tmdb.MovieDetails(r.Context(), strconv.FormatInt(tmdbID, 10))
			if err == nil {
				metadata = map[string]any{
					"id":             details.ID,
					"title":          details.Title,
					"original_title": details.OriginalTitle,
					"overview":       details.Overview,
					"poster_path":    details.PosterPath,
					"release_date":   details.ReleaseDate,
					"source":         "tmdb",
				}
			} else {
				metadata = storedMetadata
			}
		} else if hasStoredMetadata {
			metadata = storedMetadata
		} else {
			item, err := tmdb.SearchMovie(r.Context(), movieCopy.Name, parseIntPtr(movieCopy.Year))
			if err == nil && item != nil {
				metadata = toMap(item)
			}
		}

		var creditID *int64
		if value, err := strconv.ParseInt(strings.TrimSpace(movieCopy.TmdbID), 10, 64); err == nil {
			creditID = &value
		} else if metadataMap, ok := metadata.(map[string]any); ok {
			if value, ok := idFromAny(metadataMap["id"]); ok {
				creditID = &value
			}
		}
		if creditID != nil {
			if credits, err := tmdb.MovieCredits(r.Context(), strconv.FormatInt(*creditID, 10)); err == nil {
				cast = credits
			}
		}
	} else if tvdb := tvdbClientFor(cfg); tvdb.Configured() && strings.TrimSpace(movieCopy.TvdbID) != "" {
		metadata = storedMetadata
		if item, err := tvdb.MovieDetails(r.Context(), movieCopy.TvdbID); err == nil {
			if details, ok := item.(map[string]any); ok {
				metadata = tvdbMovieMetadata(details, storedMetadata, cfg.TvdbLanguage())
			}
		}
	} else {
		metadata = storedMetadata
	}

	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":       true,
		"movie":    movieCopy,
		"history":  history,
		"metadata": metadata,
		"cast":     cast,
		"matches":  matches,
	})
}

// tvdbMovieMetadata maps a TVDB movie record onto the metadata of the movie
// detail, keeping the stored values TVDB does not have.
func tvdbMovieMetadata(details, stored map[string]any, language string) map[string]any {
	out := map[string]any{}
	for key, value := range stored {
		out[key] = value
	}
	out["source"] = "tvdb"
	originalName, _ := tvdbString(details["name"])
	if name := tvdbTranslation(details, "nameTranslations", "name", language, originalName); name != "" {
		out["title"] = name
	}
	if original, ok := tvdbString(details["originalName"]); ok && original != "" {
		out["original_title"] = original
	} else if originalName != "" {
		out["original_title"] = originalName
	}
	overview, _ := tvdbString(details["overview"])
	if text := tvdbTranslation(details, "overviewTranslations", "overview", language, overview); text != "" {
		out["overview"] = text
	}
	if image, ok := tvdbString(details["image"]); ok && image != "" {
		out["poster_path"] = image
	}
	if date, ok := tvdbString(tvdbGet(details["first_release"], "date")); ok && date != "" {
		out["release_date"] = date
	} else if year := v2AnyString(details["year"]); year != "" {
		out["release_date"] = year
	}
	return out
}

func metadataSource(tvdbID string) string {
	if tvdbID == "" {
		return "tmdb"
	}
	return "tvdb"
}

// purgeRemovedLibrary drops the stored state of the series and movies that
// are no longer in the library.
func purgeRemovedLibrary(db *Database, previousSeries []SeriesConfig, previousMovies []MovieConfig, series []SeriesConfig, movies []MovieConfig) {
	for _, old := range previousSeries {
		present := false
		for _, item := range series {
			if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) {
				present = true
				break
			}
		}
		if !present {
			if _, err := db.PurgeSeries(old.Name); err != nil {
				logging.Warn("cannot remove the state of the deleted series", "error", err.Error(), "series", old.Name)
			}
		}
	}
	for _, old := range previousMovies {
		present := false
		if old.ID > 0 {
			for _, item := range movies {
				if item.ID == old.ID {
					present = true
					break
				}
			}
		} else {
			for _, item := range movies {
				if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) &&
					strings.TrimSpace(item.Year) == strings.TrimSpace(old.Year) {
					present = true
					break
				}
			}
		}
		if !present {
			if _, err := db.PurgeMovie(old.Name); err != nil {
				logging.Warn("cannot remove the state of the deleted movie", "error", err.Error(), "movie", old.Name)
			}
		}
	}
}

func DeleteMovie(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cfg := *latestConfig(s)
	previousMovies := append([]MovieConfig(nil), cfg.Movies...)
	before := len(cfg.Movies)
	filtered := make([]MovieConfig, 0, len(cfg.Movies))
	for _, movie := range cfg.Movies {
		if movie.ID != id {
			filtered = append(filtered, movie)
		}
	}
	cfg.Movies = filtered
	if before == len(cfg.Movies) {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	purgeRemovedLibrary(s.db, cfg.Series, previousMovies, cfg.Series, cfg.Movies)
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

func MovieHistoryHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.DownloadedMovies(500)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func MovieList(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonResponse(w, map[string]any{"ok": true, "items": cfg.Movies})
}

func UpdateMovie(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid path parameter")
		return
	}
	var input MovieUpdateInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	current := latestConfig(s)
	movies := make([]MovieConfig, len(current.Movies))
	copy(movies, current.Movies)
	var movie *MovieConfig
	for index := range movies {
		if movies[index].ID == id {
			movie = &movies[index]
			break
		}
	}
	if movie == nil {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		jsonError(w, http.StatusBadRequest, "movie name is required")
		return
	}
	movie.Name = strings.TrimSpace(input.Name)
	movie.Year = input.Year
	movie.Quality = input.Quality
	if input.Audio != nil {
		movie.Audio = normalizeAudioRequirement(*input.Audio)
	}
	if input.AudioChannels != nil {
		movie.AudioChannels = normalizeAudioChannels(*input.AudioChannels)
	}
	movie.Language = input.Language
	movie.Subtitle = input.Subtitle
	movie.Exclude = input.Exclude
	movie.LanguageRequirements = SanitizeLanguageRequirements(input.LanguageRequirements)
	movie.SubtitleRequirements = SanitizeSubtitleRequirements(input.SubtitleRequirements)
	movie.TmdbID = strings.TrimSpace(input.TmdbId)
	movie.TvdbID = strings.TrimSpace(input.TvdbId)
	if input.Enabled != nil {
		movie.Enabled = *input.Enabled
	}
	if input.DisableUpgrades != nil {
		movie.DisableUpgrades = *input.DisableUpgrades
	}
	if input.QualityProfileID != nil && *input.QualityProfileID > 0 {
		profile, ok := qualityProfileExact(ensureQualityProfiles(s), *input.QualityProfileID)
		if !ok {
			jsonError(w, http.StatusBadRequest, "quality profile not found")
			return
		}
		movie.Quality, movie.Language, movie.Subtitle = profile.Quality, profile.Language, profile.Subtitle
		movie.Audio, movie.AudioChannels = profile.Audio, ""
		movie.LanguageRequirements = ""
		movie.DisableUpgrades = profile.DisableUpgrades
		movie.Exclude = mergeExclusions(profile.Exclude, movie.Exclude)
	}
	if input.SubtitleRequired != nil {
		movie.SubtitleRequirements = ""
		if *input.SubtitleRequired {
			movie.SubtitleRequirements = SanitizeSubtitleRequirements(movie.Subtitle)
		}
	}
	if err := SaveLibrary(s.cfg.DataDir, current.Series, movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// ExternalSearchTimeout bounds the metadata lookup of the search action.
const ExternalSearchTimeout = 12 * time.Second

func MovieMetadataSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	found := false
	for i := range cfg.Movies {
		if cfg.Movies[i].ID == id {
			found = true
			break
		}
	}
	if !found {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	var input MovieMetadataSearchInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, "query must contain 1-256 characters")
		return
	}
	source := strings.ToLower(strings.TrimSpace(input.Source))
	ctx, cancel := context.WithTimeout(r.Context(), ExternalSearchTimeout)
	defer cancel()
	items := []any{}
	var lookupErr error
	switch source {
	case "tvdb":
		tvdb := tvdbClientFor(cfg)
		if !tvdb.Configured() {
			jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "TVDB API key is not configured"})
			return
		}
		items, lookupErr = tvdb.SearchMovies(ctx, query)
	case "tmdb":
		key := cfg.TmdbAPIKey
		if key == nil {
			jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "TMDB API key is not configured"})
			return
		}
		tmdb := NewTmdbClientWithLanguage(key, cfg.TmdbLanguage())
		tmdbItems, err := tmdb.SearchMovies(ctx, query)
		lookupErr = err
		for i := range tmdbItems {
			items = append(items, tmdbItems[i])
		}
	default:
		jsonError(w, http.StatusBadRequest, "unsupported metadata source")
		return
	}
	if ctx.Err() == context.DeadlineExceeded {
		jsonError(w, http.StatusGatewayTimeout, "metadata provider timed out")
		return
	}
	if lookupErr != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": lookupErr.Error()})
		return
	}
	if items == nil {
		items = []any{}
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "source": source, "items": items})
}

func RedownloadMovie(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	var name string
	var movieYear int64
	found := false
	for i := range cfg.Movies {
		if cfg.Movies[i].ID == id {
			name = cfg.Movies[i].Name
			movieYear, _ = strconv.ParseInt(strings.TrimSpace(cfg.Movies[i].Year), 10, 64)
			found = true
			break
		}
	}
	if !found {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	avoidPrevious := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("avoid_previous")), "1")
	blockedTitle, reset, err := redownloadMovie(s, cfg, id, name, movieYear, avoidPrevious)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	message := "film rimesso in coda al prossimo ciclo"
	if avoidPrevious {
		// The damaged copy is gone and blocklisted: search now instead of
		// waiting for the next scheduled cycle.
		clone := r.Clone(r.Context())
		query := clone.URL.Query()
		query.Set("domain", "movies")
		clone.URL.RawQuery = query.Encode()
		RunNow(&v2DiscardWriter{}, clone, s)
		message = "film rimesso in coda: ricerca avviata"
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":            true,
		"reset":         reset,
		"blocked_title": blockedTitle,
		"message":       message,
	})
}

func MovieSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cfg := latestConfig(s)
	var movie *MovieConfig
	for index := range cfg.Movies {
		if cfg.Movies[index].ID == id {
			movie = &cfg.Movies[index]
			break
		}
	}
	if movie == nil {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	// Fill the alternative titles on first search, so the archive is queried
	// and the releases are matched under the title they were published with.
	if movie.Aliases == nil && cfg.TmdbAPIKey != nil {
		if client := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage()); client.Configured() {
			if err := refreshMovieAlias(r.Context(), cfg, movie, client); err != nil {
				logging.Debug("TMDB alternative titles unavailable", "movie", movie.Name, "error", err)
			}
		}
	}
	query := strings.TrimSpace(movie.Name + " " + movie.Year)
	results := s.engine.SearchMovie(r.Context(), cfg, movie, true, true)
	archiveQueries := movieSearchTitles(movie, 0)
	seenEntries := map[string]struct{}{}
	for _, base := range archiveQueries {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		if strings.TrimSpace(movie.Year) != "" {
			base += " " + strings.TrimSpace(movie.Year)
		}
		entries, _ := s.archive.Search(base)
		for _, entry := range entries {
			key := entry[0] + "\x00" + entry[1]
			if _, exists := seenEntries[key]; exists {
				continue
			}
			seenEntries[key] = struct{}{}
			release := ParseRelease(entry[0], entry[1], "archive:"+entry[2])
			if release == nil && IsTorrentURL(entry[1]) {
				link := strings.TrimSpace(entry[1])
				release = ParseReleaseSource(entry[0], "", &link, "archive:"+entry[2], time.Now().UTC())
			}
			if release != nil {
				results = append(results, *release)
			}
		}
	}
	seen := map[string]struct{}{}
	kept := results[:0]
	for index := range results {
		release := &results[index]
		if release.Kind != "movie" {
			continue
		}
		matched := cfg.FindMovieMatchForRelease(release, true)
		if matched == nil || matched.ID != movie.ID || !cfg.MovieReleaseAllowedForTitle(movie, &release.Quality, release.Title) {
			continue
		}
		hash, ok := releaseDedupKey(release)
		if !ok {
			continue
		}
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		kept = append(kept, *release)
	}
	results = kept
	sort.SliceStable(results, func(i, j int) bool {
		return cfg.ReleaseScoreForMovie(&results[i], movie) > cfg.ReleaseScoreForMovie(&results[j], movie)
	})
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "query": query, "results": results})
}

func MoviesSeenGroupedView(w http.ResponseWriter, r *http.Request, s *AppState) {
	seenGrouped(w, r, s, "movie")
}

func seenGrouped(w http.ResponseWriter, r *http.Request, s *AppState, kind string) {
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
	var (
		groups []FeedSeenGroup
		total  int64
		err    error
	)
	if kind == "series" {
		groups, total, err = s.db.SeriesSeenGrouped(offset, limit, queryText)
	} else {
		groups, total, err = s.db.MoviesSeenGrouped(offset, limit, queryText)
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pages := int64(1)
	if limit > 0 {
		pages = (total + int64(limit) - 1) / int64(limit)
	}
	if pages < 1 {
		pages = 1
	}
	jsonResponse(w, map[string]any{
		"ok":     true,
		"groups": groups,
		"total":  total,
		"page":   page,
		"pages":  pages,
	})
}

// gh6ExternalSearchTimeout bounds the metadata lookup of the apply action.
const gh6ExternalSearchTimeout = 12 * time.Second

func gh6JsonString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case *string:
		if typed != nil {
			return *typed
		}
	}
	return ""
}

func gh6SeenEntries(w http.ResponseWriter, r *http.Request, s *AppState, kind string) {
	group := queryParam(r, "key")
	if group == "" {
		group = queryParam(r, "group")
	}
	if strings.TrimSpace(group) == "" {
		jsonError(w, http.StatusBadRequest, "key required")
		return
	}
	limit := queryIntVal(r, "limit", 500)
	items, err := s.db.SeenByGroup(kind, strings.TrimSpace(group), limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

func ApplyMovieMetadata(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	var input MovieMetadataApplyInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	var current *MovieConfig
	for index := range cfg.Movies {
		if cfg.Movies[index].ID == id {
			current = &cfg.Movies[index]
			break
		}
	}
	if current == nil {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	source := strings.ToLower(strings.TrimSpace(input.Source))
	externalID := strings.TrimSpace(input.Id)
	if len(externalID) > 64 {
		jsonError(w, http.StatusBadRequest, "invalid metadata id")
		return
	}
	var details map[string]any
	// The alternative titles are read together with the details when the
	// source is TMDB: the page then shows them right after the refresh,
	// instead of waiting for the next automatic cycle.
	var aliases []string
	aliasesRead := false
	if source == "tmdb" {
		if cfg.TmdbAPIKey == nil {
			jsonError(w, http.StatusConflict, "TMDB API key is not configured")
			return
		}
		tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
		ctx, cancel := context.WithTimeout(r.Context(), gh6ExternalSearchTimeout)
		defer cancel()
		// A movie added by hand has no TMDB id yet: «Aggiorna TMDB» resolves it
		// by name and year, as the series refresh does, instead of refusing with
		// "invalid metadata id" and leaving the film without the alternative
		// titles the match needs.
		if externalID == "" {
			found, err := tmdb.SearchMovie(ctx, current.Name, parseIntPtr(current.Year))
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					jsonError(w, http.StatusGatewayTimeout, "TMDB timed out")
					return
				}
				jsonError(w, http.StatusBadGateway, err.Error())
				return
			}
			if found == nil {
				jsonError(w, http.StatusNotFound, "movie not found on TMDB")
				return
			}
			externalID = strconv.FormatInt(found.ID, 10)
		}
		item, err := tmdb.MovieDetails(ctx, externalID)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				jsonError(w, http.StatusGatewayTimeout, "TMDB timed out")
				return
			}
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		details = map[string]any{
			"title":          item.Title,
			"original_title": item.OriginalTitle,
			"overview":       item.Overview,
			"poster_path":    item.PosterPath,
			"release_date":   item.ReleaseDate,
		}
		aliasMovie := MovieConfig{
			Name:          strings.TrimSpace(gh6JsonString(details["title"])),
			OriginalTitle: gh6JsonString(details["original_title"]),
			TmdbID:        externalID,
		}
		if aliasMovie.Name == "" {
			aliasMovie.Name = current.Name
		}
		if found, aliasErr := movieAliasTitles(ctx, cfg, &aliasMovie, tmdb); aliasErr != nil {
			// The refresh is an extra: the metadata is saved all the same and
			// the next cycle retries the titles.
			logging.Debug("TMDB alternative titles unavailable", "movie", current.Name, "error", aliasErr)
		} else {
			aliases, aliasesRead = found, true
		}
	} else if source == "tvdb" {
		tvdb := tvdbClientFor(cfg)
		if !tvdb.Configured() {
			jsonError(w, http.StatusConflict, "TVDB API key is not configured")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), gh6ExternalSearchTimeout)
		defer cancel()
		item, err := tvdb.MovieDetails(ctx, externalID)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				jsonError(w, http.StatusGatewayTimeout, "TVDB timed out")
				return
			}
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		itemMap, _ := item.(map[string]any)
		poster := any(nil)
		if value, present := itemMap["image"]; present {
			poster = value
		} else if value, present := itemMap["image_url"]; present {
			poster = value
		}
		// Title and overview in the TVDB language when translated.
		translated := tvdbMovieMetadata(itemMap, map[string]any{}, cfg.TvdbLanguage())
		details = map[string]any{
			"title":          translated["title"],
			"original_title": itemMap["originalName"],
			"overview":       translated["overview"],
			"poster_path":    poster,
			"release_date":   itemMap["year"],
		}
	} else {
		jsonError(w, http.StatusBadRequest, "unsupported metadata source")
		return
	}
	title := strings.TrimSpace(gh6JsonString(details["title"]))
	if title == "" {
		title = current.Name
	}
	date := gh6JsonString(details["release_date"])
	metadataYear := ""
	if len(date) >= 4 {
		candidate := date[:4]
		digits := true
		for _, character := range candidate {
			if character < '0' || character > '9' {
				digits = false
				break
			}
		}
		if digits {
			metadataYear = candidate
		}
	}
	year := current.Year
	if metadataYear != "" {
		year = metadataYear
	}
	updated := *latestConfig(s)
	// Deep-copy the slice: `LatestConfig` may return a cached Config whose
	// backing array must not be mutated in place.
	updated.Movies = append([]MovieConfig(nil), updated.Movies...)
	index := -1
	for position := range updated.Movies {
		if updated.Movies[position].ID == id {
			index = position
			break
		}
	}
	if index < 0 {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	movie := &updated.Movies[index]
	movie.Name = title
	movie.Year = year
	movie.OriginalTitle = gh6JsonString(details["original_title"])
	movie.Overview = gh6JsonString(details["overview"])
	movie.PosterPath = gh6JsonString(details["poster_path"])
	if source == "tmdb" {
		movie.TmdbID = externalID
		movie.TvdbID = ""
		// An empty non-nil list means "TMDB checked, none": the page says so
		// instead of offering the refresh again.
		if aliasesRead {
			movie.Aliases = aliases
		}
	} else {
		movie.TvdbID = externalID
		movie.TmdbID = ""
	}
	if err := SaveLibrary(s.cfg.DataDir, updated.Series, updated.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.db.RenameMovieIdentity(current.Name, current.Year, title, year); err != nil {
		logging.Error("movie metadata saved but download identity update failed", "error", err, "movie_id", id)
		jsonError(w, http.StatusInternalServerError, "metadata saved, but could not preserve download history")
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "movie": updated.Movies[index]})
}

func MoviesSeenView(w http.ResponseWriter, r *http.Request, s *AppState) {
	gh6SeenEntries(w, r, s, "movie")
}
