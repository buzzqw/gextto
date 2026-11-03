package gextto

// Metadata lookup and library additions: the TVDB series detail, TMDB search,
// discovery and add, the TVDB search fallback, and the helpers that map a TVDB
// result to the shape of the TMDB ones.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

func TvdbSeries(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := tvdbClientFor(cfg)
	if !client.Configured() {
		jsonError(w, http.StatusPreconditionRequired, "TVDB API key non configurata")
		return
	}
	id, _ := pathInt(r, "id")
	series, err := client.SeriesExtended(r.Context(), id)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "series": series})
}

// TmdbAdd adds the movie or series chosen on the discovery page to the library
// and saves it; an explicit quality profile is applied to the new entry.
func TmdbAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TmdbAddInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Name) == "" || (strings.TrimSpace(input.TmdbId) == "" && strings.TrimSpace(input.TvdbId) == "") {
		jsonError(w, http.StatusBadRequest, "TMDB item is incomplete")
		return
	}
	if input.QualityProfileID > 0 {
		// An explicit id must exist: a stale profile (deleted after the page
		// was opened) must not silently become another policy.
		profile, ok := qualityProfileExact(ensureQualityProfiles(s), input.QualityProfileID)
		if !ok {
			jsonError(w, http.StatusBadRequest, "quality profile not found")
			return
		}
		applyQualityProfileToAdd(&input, profile)
	}
	cfg, err := LoadConfig(s.config_path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.EqualFold(input.Kind, "movie") {
		for index := range cfg.Movies {
			if strings.EqualFold(cfg.Movies[index].Name, strings.TrimSpace(input.Name)) && cfg.Movies[index].Year == input.Year {
				jsonError(w, http.StatusConflict, "film già presente")
				return
			}
		}
		language := strings.TrimSpace(input.Language)
		if language == "" {
			language = "ita"
		}
		cfg.Movies = append(cfg.Movies, MovieConfig{
			ID:                   0,
			Name:                 strings.TrimSpace(input.Name),
			Year:                 strings.TrimSpace(input.Year),
			TmdbID:               strings.TrimSpace(input.TmdbId),
			TvdbID:               strings.TrimSpace(input.TvdbId),
			OriginalTitle:        "",
			Overview:             "",
			PosterPath:           "",
			Quality:              strings.TrimSpace(input.Quality),
			Audio:                normalizeAudioRequirement(input.Audio),
			AudioChannels:        normalizeAudioChannels(input.AudioChannels),
			Language:             language,
			Enabled:              true,
			Subtitle:             strings.TrimSpace(input.Subtitle),
			Exclude:              strings.TrimSpace(input.Exclude),
			LanguageRequirements: "",
			SubtitleRequirements: "",
			DisableUpgrades:      input.DisableUpgrades,
		})
	} else {
		for index := range cfg.Series {
			if strings.EqualFold(cfg.Series[index].Name, strings.TrimSpace(input.Name)) {
				jsonError(w, http.StatusConflict, "serie già presente")
				return
			}
		}
		seasons := strings.TrimSpace(input.Seasons)
		if seasons == "" {
			seasons = "1+"
		}
		language := strings.TrimSpace(input.Language)
		if language == "" {
			language = "ita"
		}
		// "Crea cartella": make the series' archive folder. When the path is
		// empty fall back to <archive_root>/<name>, mirroring the default the
		// add modal proposes.
		archivePath := strings.TrimSpace(input.ArchivePath)
		if input.CreateFolder {
			if archivePath == "" {
				archivePath = proposedArchivePath(&cfg, input.Name)
			}
			if archivePath != "" {
				if err := os.MkdirAll(archivePath, 0o755); err != nil {
					jsonError(w, http.StatusInternalServerError, "impossibile creare la cartella: "+err.Error())
					return
				}
			}
		}
		cfg.Series = append(cfg.Series, SeriesConfig{
			Name:             strings.TrimSpace(input.Name),
			Seasons:          seasons,
			Quality:          strings.TrimSpace(input.Quality),
			Audio:            normalizeAudioRequirement(input.Audio),
			AudioChannels:    normalizeAudioChannels(input.AudioChannels),
			Language:         language,
			ArchivePath:      archivePath,
			Timeframe:        0,
			Aliases:          splitAliases(input.Aliases),
			TmdbID:           strings.TrimSpace(input.TmdbId),
			TvdbID:           strings.TrimSpace(input.TvdbId),
			Subtitle:         strings.TrimSpace(input.Subtitle),
			Exclude:          strings.TrimSpace(input.Exclude),
			Enabled:          true,
			IgnoredSeasons:   nil,
			SeasonSubfolders: input.SeasonSubfolders,
			DisableUpgrades:  input.DisableUpgrades,
		})
	}
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":               true,
		"name":             input.Name,
		"restart_required": true,
	})
}

// splitAliases parses a comma separated alias list into a clean slice.
func splitAliases(raw string) []string {
	aliases := []string{}
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			aliases = append(aliases, trimmed)
		}
	}
	if len(aliases) == 0 {
		return nil
	}
	return aliases
}

// TmdbDiscover returns the trending, popular or category list of TMDB for the
// requested kind, with the in-library flag already computed.
func TmdbDiscover(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if cfg.TmdbAPIKey == nil {
		jsonError(w, http.StatusConflict, "TMDB API key is not configured: Discover needs TMDB (TVDB has no trending or popular lists)")
		return
	}
	var input DiscoverInput
	if err := gh6DecodeOptionalJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	kind := DefaultTmdbKind()
	if input.Kind != nil {
		kind = *input.Kind
	}
	window := "week"
	if input.Window != nil {
		window = *input.Window
	}
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	ctx := r.Context()
	mode := ""
	if input.Mode != nil {
		mode = *input.Mode
	}
	var (
		items []TmdbItem
		err   error
	)
	switch {
	case mode == "popular":
		items, err = tmdb.Popular(ctx, kind)
	case mode == "top_rated" || mode == "now_playing" || mode == "upcoming":
		items, err = tmdb.Category(ctx, kind, mode)
	case mode == "trending" || input.Mode == nil:
		items, err = tmdb.Trending(ctx, kind, window)
	default:
		err = fmt.Errorf("unsupported TMDB discovery mode: %s", mode)
	}
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	for index := range items {
		items[index].InLibrary = ghTmdbItemInLibrary(cfg, kind, items[index])
	}
	jsonResponse(w, map[string]any{"ok": true, "kind": kind, "items": items})
}

const externalSearchTimeout = 12 * time.Second

// TmdbSearch handles POST /api/tmdb/search.
func TmdbSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TmdbQuery
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	// TMDB is the primary source; without its key TVDB (when configured) is
	// used on its own, for series and movies alike.
	tmdbConfigured := cfg.TmdbAPIKey != nil && strings.TrimSpace(*cfg.TmdbAPIKey) != ""
	tvdb := tvdbClientFor(cfg)
	if !tmdbConfigured && !tvdb.Configured() {
		jsonError(w, http.StatusConflict, "TMDB API key is not configured (nor a TVDB one)")
		return
	}
	if input.Kind == "" {
		input.Kind = "series"
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, "query must contain 1-256 characters")
		return
	}
	isMovie := strings.EqualFold(input.Kind, "movie")

	if !tmdbConfigured {
		ctx, cancel := context.WithTimeout(r.Context(), externalSearchTimeout)
		defer cancel()
		items, err := tvdbSearchItems(ctx, cfg, tvdb, isMovie, query)
		if ctx.Err() == context.DeadlineExceeded {
			jsonError(w, http.StatusGatewayTimeout,
				fmt.Sprintf("TVDB non ha risposto entro %ds", int(externalSearchTimeout.Seconds())))
			return
		}
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		jsonResponse(w, map[string]any{"ok": true, "kind": input.Kind, "items": items, "source": "tvdb"})
		return
	}

	tmdb := NewTmdbClient(cfg.TmdbAPIKey)
	ctx, cancel := context.WithTimeout(r.Context(), externalSearchTimeout)
	defer cancel()
	var items []TmdbItem
	var err error
	if isMovie {
		items, err = tmdb.SearchMovies(ctx, query)
	} else {
		items, err = tmdb.SearchSeries(ctx, query)
	}
	tmdbTimedOut := ctx.Err() == context.DeadlineExceeded
	tmdbFailed := tmdbTimedOut || err != nil
	if tmdbFailed && isMovie {
		if tmdbTimedOut {
			jsonError(w, http.StatusGatewayTimeout,
				fmt.Sprintf("TMDB non ha risposto entro %ds", int(externalSearchTimeout.Seconds())))
			return
		}
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !tmdbFailed && len(items) > 0 {
		values := make([]any, 0, len(items))
		for _, item := range items {
			item.InLibrary = ghTmdbItemInLibrary(cfg, input.Kind, item)
			encoded, _ := json.Marshal(item)
			var mapped map[string]any
			_ = json.Unmarshal(encoded, &mapped)
			mapped["external"] = "tmdb"
			values = append(values, mapped)
		}
		jsonResponse(w, map[string]any{"ok": true, "kind": input.Kind, "items": values, "source": "tmdb"})
		return
	}
	if !tmdbFailed && isMovie {
		jsonResponse(w, map[string]any{"ok": true, "kind": input.Kind, "items": []any{}, "source": "tmdb"})
		return
	}

	// Fallback TVDB (series only): when TMDB returns no results OR fails
	// (error/timeout). If TVDB also yields nothing and TMDB had failed, the
	// original TMDB error is reported.
	tctx, tcancel := context.WithTimeout(r.Context(), externalSearchTimeout)
	defer tcancel()
	tvdbItems, tvdbErr := tvdbSearchItems(tctx, cfg, tvdb, false, query)
	if tctx.Err() == context.DeadlineExceeded {
		logging.Debug("TVDB fallback search timed out")
	} else if tvdbErr != nil {
		logging.Debug("TVDB fallback search failed", "error", tvdbErr.Error())
	}
	if len(tvdbItems) == 0 && tmdbFailed {
		if tmdbTimedOut {
			jsonError(w, http.StatusGatewayTimeout,
				fmt.Sprintf("TMDB non ha risposto entro %ds e nessun risultato da TVDB", int(externalSearchTimeout.Seconds())))
			return
		}
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	source := "tmdb"
	if len(tvdbItems) > 0 {
		source = "tvdb"
		logging.Info("TMDB returned no results or is unreachable: using the TVDB fallback", "query", query)
	}
	jsonResponse(w, map[string]any{"ok": true, "kind": "series", "items": tvdbItems, "source": source})
}

// tvdbSearchItems searches TVDB series or movies and maps the results to
// the item shape of TmdbSearch ("id" 0, the TVDB id in "tvdb_id").
func tvdbSearchItems(ctx context.Context, cfg *Config, tvdb *TvdbClient, isMovie bool, query string) ([]any, error) {
	items := []any{}
	if isMovie {
		raw, err := tvdb.SearchMovies(ctx, query)
		if err != nil {
			return items, err
		}
		for _, item := range raw {
			mapped, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := v2AnyString(mapped["id"])
			year := v2AnyString(mapped["release_date"])
			items = append(items, map[string]any{
				"id":           0,
				"title":        mapped["title"],
				"overview":     mapped["overview"],
				"poster":       mapped["poster_path"],
				"release_date": mapped["release_date"],
				"external":     "tvdb",
				"tvdb_id":      mapped["id"],
				"in_library":   tvdbItemInLibrary(cfg, true, id, v2AnyString(mapped["title"]), year),
			})
		}
		return items, nil
	}
	raw, err := tvdb.SearchSeries(ctx, query)
	if err != nil {
		return items, err
	}
	for _, item := range raw {
		mapped, ok := item.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, map[string]any{
			"id":             0,
			"name":           mapped["name"],
			"overview":       mapped["overview"],
			"poster":         mapped["image"],
			"first_air_date": mapped["year"],
			"external":       "tvdb",
			"tvdb_id":        mapped["tvdb_id"],
			"in_library":     tvdbItemInLibrary(cfg, false, v2AnyString(mapped["tvdb_id"]), v2AnyString(mapped["name"]), ""),
		})
	}
	return items, nil
}

// tvdbItemInLibrary reports whether a TVDB search result is already in the
// library: same TVDB id, or same name (and year, for movies when both are
// known).
func tvdbItemInLibrary(cfg *Config, isMovie bool, id, name, year string) bool {
	if cfg == nil {
		return false
	}
	id, name, year = strings.TrimSpace(id), strings.TrimSpace(name), strings.TrimSpace(year)
	if isMovie {
		for _, movie := range cfg.Movies {
			if id != "" && strings.TrimSpace(movie.TvdbID) == id {
				return true
			}
			if name != "" && strings.EqualFold(strings.TrimSpace(movie.Name), name) &&
				(year == "" || movie.Year == "" || strings.HasPrefix(movie.Year, year)) {
				return true
			}
		}
		return false
	}
	for _, series := range cfg.Series {
		if id != "" && strings.TrimSpace(series.TvdbID) == id {
			return true
		}
		if name != "" && strings.EqualFold(strings.TrimSpace(series.Name), name) {
			return true
		}
	}
	return false
}

// TvdbSearch handles POST /api/tvdb/search.
func TvdbSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	// Decode the body into an anonymous struct: only the query field is needed
	// here.
	var input struct {
		Query string `json:"query"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	client := tvdbClientFor(cfg)
	if !client.Configured() {
		jsonError(w, http.StatusPreconditionRequired, "TVDB API key non configurata")
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, "query must contain 1-256 characters")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), externalSearchTimeout)
	defer cancel()
	results, err := client.SearchSeries(ctx, query)
	if ctx.Err() == context.DeadlineExceeded {
		jsonError(w, http.StatusGatewayTimeout,
			fmt.Sprintf("TVDB non ha risposto entro %ds", int(externalSearchTimeout.Seconds())))
		return
	}
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "results": results})
}
