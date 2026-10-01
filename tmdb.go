package gextto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// tmdbAPIBaseURL is the TMDB API v3 base URL used by every endpoint. It is a
// package-level var so tests can point it at an httptest server.
var tmdbAPIBaseURL = "https://api.themoviedb.org/3"

// TmdbImageBaseURL is the TMDB image CDN prefix. Callers append a size segment
// (for example "w154") and the poster path, exactly as the web module did with
// "https://image.tmdb.org/t/p/w154{path}".
const TmdbImageBaseURL = "https://image.tmdb.org/t/p"

// TmdbClient is the Go implementation of the `TmdbClient`. The cache mirrors the
// `Arc<Mutex<HashMap<String, Option<String>>>>`: a present key with a nil value
// is a cached `None`.
type TmdbClient struct {
	client   *http.Client
	key      *string
	language string
	cache    map[string]*string
	cacheMu  *sync.Mutex
}

// SearchResult mirrors the `SearchResult`.
type SearchResult struct {
	Results []TmdbItem `json:"results"`
}

// TmdbItem mirrors the `TmdbItem`.
type TmdbItem struct {
	ID           int64    `json:"id"`
	Name         *string  `json:"name"`
	Title        *string  `json:"title"`
	Overview     *string  `json:"overview"`
	PosterPath   *string  `json:"poster_path"`
	FirstAirDate *string  `json:"first_air_date"`
	ReleaseDate  *string  `json:"release_date"`
	VoteAverage  *float64 `json:"vote_average"`
	// InLibrary marks a discovery/search result already present in the local
	// library so the UI can show a "Già in lista" badge instead of offering a
	// duplicate insert.
	InLibrary bool `json:"in_library,omitempty"`
}

// gh_tmdbItemInLibrary reports whether a TMDB discovery/search result already
// exists in the monitored library. It matches first by external id, then by
// name (with the year for movies) so a card never offers a duplicate insert.
func gh_tmdbItemInLibrary(cfg *Config, kind string, item TmdbItem) bool {
	if cfg == nil {
		return false
	}
	name := ""
	if item.Name != nil {
		name = strings.TrimSpace(*item.Name)
	}
	if name == "" && item.Title != nil {
		name = strings.TrimSpace(*item.Title)
	}
	id := ""
	if item.ID > 0 {
		id = strconv.FormatInt(item.ID, 10)
	}
	if strings.EqualFold(strings.TrimSpace(kind), "movie") {
		year := ""
		if item.ReleaseDate != nil {
			year = strings.TrimSpace(*item.ReleaseDate)
			if len(year) > 4 {
				year = year[:4]
			}
		}
		for _, movie := range cfg.Movies {
			if id != "" && strings.TrimSpace(movie.TmdbID) == id {
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
		if id != "" && strings.TrimSpace(series.TmdbID) == id {
			return true
		}
		if name != "" && strings.EqualFold(strings.TrimSpace(series.Name), name) {
			return true
		}
	}
	return false
}

// TmdbMovieDetails mirrors the `TmdbMovieDetails`: editorial movie data
// that can be persisted, separate from download settings.
type TmdbMovieDetails struct {
	ID            int64   `json:"id"`
	Title         *string `json:"title"`
	OriginalTitle *string `json:"original_title"`
	Overview      *string `json:"overview"`
	PosterPath    *string `json:"poster_path"`
	ReleaseDate   *string `json:"release_date"`
}

// episodeResult mirrors the private `EpisodeResult`.
type episodeResult struct {
	Name *string `json:"name"`
}

// CastMember mirrors the `CastMember`.
type CastMember struct {
	ID        *int64  `json:"id"`
	Name      *string `json:"name"`
	Character *string `json:"character"`
	Order     *int64  `json:"order"`
}

// creditsResponse mirrors the private `CreditsResponse`.
type creditsResponse struct {
	Cast []CastMember `json:"cast"`
}

// seriesDetails mirrors the private `SeriesDetails`.
type seriesDetails struct {
	Seasons          []seasonSummary `json:"seasons"`
	NextEpisodeToAir *TmdbEpisode    `json:"next_episode_to_air"`
}

// seasonSummary mirrors the private `SeasonSummary`.
type seasonSummary struct {
	SeasonNumber *int64 `json:"season_number"`
	EpisodeCount *int64 `json:"episode_count"`
}

// seasonDetails mirrors the private `SeasonDetails`.
type seasonDetails struct {
	Episodes []TmdbEpisode `json:"episodes"`
}

// TmdbEpisode mirrors the `TmdbEpisode`.
type TmdbEpisode struct {
	ID            *int64  `json:"id"`
	Name          *string `json:"name"`
	SeasonNumber  *int64  `json:"season_number"`
	EpisodeNumber *int64  `json:"episode_number"`
	AirDate       *string `json:"air_date"`
}

// TmdbCastEntry is one `(person_id, name)` pair returned by SeriesCast,
// standing in for the tuple `Vec<(i64, String)>`.
type TmdbCastEntry struct {
	ID   int64
	Name string
}

// NewTmdbClient mirrors `TmdbClient::new` (Italian default language).
func NewTmdbClient(key *string) *TmdbClient {
	return NewTmdbClientWithLanguage(key, "it-IT")
}

// NewTmdbClientWithLanguage mirrors `TmdbClient::with_language`.
func NewTmdbClientWithLanguage(key *string, language string) *TmdbClient {
	// tested `language.trim().is_empty()` but stored the original string.
	stored := language
	if strings.TrimSpace(language) == "" {
		stored = "it-IT"
	}
	return &TmdbClient{
		client: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				Proxy:       http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			},
		},
		key:      key,
		language: stored,
		cache:    map[string]*string{},
		cacheMu:  &sync.Mutex{},
	}
}

// Clone mirrors the derived `Clone` impl: the cache (and its lock) is shared,
// just like the `Arc<Mutex<...>>`.
func (t *TmdbClient) Clone() *TmdbClient {
	if t == nil {
		return nil
	}
	copied := *t
	return &copied
}

// getJSON mirrors the `get_json`: it appends the API key, retries 429 and
// 5xx responses up to three times with a linear backoff, and decodes the body
// into target on success.
func (t *TmdbClient) getJSON(ctx context.Context, rawURL string, params [][2]string, target any) error {
	if t.key == nil {
		return errors.New("TMDB API key is not configured")
	}
	for attempt := 0; attempt < 3; attempt++ {
		query := url.Values{}
		for _, param := range params {
			query.Add(param[0], param[1])
		}
		query.Set("api_key", *t.key)
		requestURL := rawURL
		if encoded := query.Encode(); encoded != "" {
			requestURL = rawURL + "?" + encoded
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return err
		}
		response, err := t.client.Do(request)
		if err != nil {
			// A temporary DNS/TCP reset is as recoverable as a 5xx. Do not retry
			// caller cancellation: that is a deliberate lifecycle event, not a
			// TMDB outage.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt == 2 {
				return fmt.Errorf("TMDB request failed: %w", err)
			}
			if err := tmdbRetryWait(ctx, attempt); err != nil {
				return err
			}
			continue
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			decodeErr := json.NewDecoder(response.Body).Decode(target)
			response.Body.Close()
			return decodeErr
		}
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		response.Body.Close()
		if !retryable || attempt == 2 {
			return fmt.Errorf("TMDB request failed: HTTP %d", response.StatusCode)
		}
		if err := tmdbRetryWait(ctx, attempt); err != nil {
			return err
		}
	}
	return errors.New("unreachable")
}

func tmdbRetryWait(ctx context.Context, attempt int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(250*(attempt+1)) * time.Millisecond):
		return nil
	}
}

func (t *TmdbClient) cacheGet(key string) (*string, bool) {
	t.cacheMu.Lock()
	defer t.cacheMu.Unlock()
	value, ok := t.cache[key]
	return value, ok
}

func (t *TmdbClient) cacheSet(key string, value *string) {
	t.cacheMu.Lock()
	defer t.cacheMu.Unlock()
	t.cache[key] = value
}

// SearchSeries mirrors `search_series`.
func (t *TmdbClient) SearchSeries(ctx context.Context, name string) ([]TmdbItem, error) {
	if t.key == nil {
		return []TmdbItem{}, nil
	}
	var result SearchResult
	err := t.getJSON(ctx, tmdbAPIBaseURL+"/search/tv", [][2]string{
		{"query", name},
		{"language", t.language},
	}, &result)
	if err != nil {
		return nil, err
	}
	return result.Results, nil
}

// SearchMovies mirrors `search_movies`.
func (t *TmdbClient) SearchMovies(ctx context.Context, name string) ([]TmdbItem, error) {
	if t.key == nil {
		return []TmdbItem{}, nil
	}
	var result SearchResult
	err := t.getJSON(ctx, tmdbAPIBaseURL+"/search/movie", [][2]string{
		{"query", name},
		{"language", t.language},
	}, &result)
	if err != nil {
		return nil, err
	}
	return result.Results, nil
}

// Trending mirrors `trending`.
func (t *TmdbClient) Trending(ctx context.Context, kind, window string) ([]TmdbItem, error) {
	if t.key == nil {
		return []TmdbItem{}, nil
	}
	media := "tv"
	if kind == "movie" {
		media = "movie"
	}
	period := "week"
	if window == "day" {
		period = "day"
	}
	rawURL := fmt.Sprintf("%s/trending/%s/%s", tmdbAPIBaseURL, media, period)
	var result SearchResult
	err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &result)
	if err != nil {
		return nil, err
	}
	return result.Results, nil
}

// Popular mirrors `popular`.
func (t *TmdbClient) Popular(ctx context.Context, kind string) ([]TmdbItem, error) {
	if t.key == nil {
		return []TmdbItem{}, nil
	}
	path := "tv/popular"
	if kind == "movie" {
		path = "movie/popular"
	}
	rawURL := fmt.Sprintf("%s/%s", tmdbAPIBaseURL, path)
	var result SearchResult
	err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &result)
	if err != nil {
		return nil, err
	}
	return result.Results, nil
}

// Category mirrors `category`: curated TMDB categories used by the discovery
// view. TV has no exact counterpart for the two movie-only categories, so the
// closest official TV endpoints are used.
func (t *TmdbClient) Category(ctx context.Context, kind, category string) ([]TmdbItem, error) {
	if t.key == nil {
		return []TmdbItem{}, nil
	}
	path, ok := categoryPath(kind, category)
	if !ok {
		return nil, fmt.Errorf("unsupported TMDB category: %s", category)
	}
	rawURL := fmt.Sprintf("%s/%s", tmdbAPIBaseURL, path)
	var result SearchResult
	err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &result)
	if err != nil {
		return nil, err
	}
	return result.Results, nil
}

// SearchMovie mirrors `search_movie`.
func (t *TmdbClient) SearchMovie(ctx context.Context, name string, year *int64) (*TmdbItem, error) {
	if t.key == nil {
		return nil, nil
	}
	params := [][2]string{
		{"query", name},
		{"language", t.language},
	}
	if year != nil {
		params = append(params, [2]string{"year", strconv.FormatInt(*year, 10)})
	}
	var result SearchResult
	if err := t.getJSON(ctx, tmdbAPIBaseURL+"/search/movie", params, &result); err != nil {
		return nil, err
	}
	if len(result.Results) == 0 {
		return nil, nil
	}
	return &result.Results[0], nil
}

// MovieDetails mirrors `movie_details`.
func (t *TmdbClient) MovieDetails(ctx context.Context, tmdbID string) (*TmdbMovieDetails, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(tmdbID), 10, 64)
	if err != nil {
		return nil, errors.New("invalid TMDB movie id")
	}
	var details TmdbMovieDetails
	rawURL := fmt.Sprintf("%s/movie/%d", tmdbAPIBaseURL, id)
	if err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &details); err != nil {
		return nil, err
	}
	return &details, nil
}

// MovieCredits mirrors `movie_credits`: top 12 cast members ordered by billing.
func (t *TmdbClient) MovieCredits(ctx context.Context, tmdbID string) ([]CastMember, error) {
	if t.key == nil {
		return []CastMember{}, nil
	}
	id, err := strconv.ParseInt(strings.TrimSpace(tmdbID), 10, 64)
	if err != nil {
		return []CastMember{}, nil
	}
	var response creditsResponse
	rawURL := fmt.Sprintf("%s/movie/%d/credits", tmdbAPIBaseURL, id)
	if err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &response); err != nil {
		return nil, err
	}
	cast := response.Cast
	if cast == nil {
		cast = []CastMember{}
	}
	sort.SliceStable(cast, func(i, j int) bool {
		return tmdbOrderOrMax(cast[i].Order) < tmdbOrderOrMax(cast[j].Order)
	})
	if len(cast) > 12 {
		cast = cast[:12]
	}
	return cast, nil
}

// ResolveSeriesID mirrors `resolve_series_id`.
func (t *TmdbClient) ResolveSeriesID(ctx context.Context, name string) (*string, error) {
	cacheKey := "series:" + name
	if value, ok := t.cacheGet(cacheKey); ok {
		return value, nil
	}
	items, err := t.SearchSeries(ctx, name)
	if err != nil {
		return nil, err
	}
	var result *string
	if len(items) > 0 {
		id := strconv.FormatInt(items[0].ID, 10)
		result = &id
	}
	t.cacheSet(cacheKey, result)
	return result, nil
}

// PosterForSeries mirrors `poster_for_series`.
func (t *TmdbClient) PosterForSeries(ctx context.Context, name string) (*string, error) {
	if t.key == nil {
		return nil, nil
	}
	// Posters change rarely: cache to avoid a TMDB search on every listing.
	cacheKey := "poster:" + name
	if value, ok := t.cacheGet(cacheKey); ok {
		return value, nil
	}
	items, err := t.SearchSeries(ctx, name)
	if err != nil {
		return nil, err
	}
	var result *string
	if len(items) > 0 {
		result = items[0].PosterPath
	}
	t.cacheSet(cacheKey, result)
	return result, nil
}

// SeriesInfo mirrors `series_info`: extended TV metadata for the series detail
// header. It uses the stored numeric TMDB id when available, otherwise it
// resolves the series by name.
func (t *TmdbClient) SeriesInfo(ctx context.Context, name string, tmdbID *string) (map[string]any, error) {
	if t.key == nil {
		return nil, nil
	}
	var numeric *string
	if tmdbID != nil {
		trimmed := strings.TrimSpace(*tmdbID)
		if _, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			numeric = &trimmed
		}
	}
	var id string
	if numeric != nil {
		id = *numeric
	} else {
		resolved, err := t.ResolveSeriesID(ctx, name)
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			return nil, nil
		}
		id = *resolved
	}
	var value map[string]any
	rawURL := fmt.Sprintf("%s/tv/%s", tmdbAPIBaseURL, id)
	if err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// SeriesCast mirrors `series_cast`: main cast (up to 8 members) as
// `(tmdb_person_id, name)` pairs.
func (t *TmdbClient) SeriesCast(ctx context.Context, tmdbID string) ([]TmdbCastEntry, error) {
	if t.key == nil {
		return []TmdbCastEntry{}, nil
	}
	id, err := strconv.ParseInt(strings.TrimSpace(tmdbID), 10, 64)
	if err != nil {
		return []TmdbCastEntry{}, nil
	}
	var response struct {
		Cast []struct {
			ID    int64   `json:"id"`
			Name  *string `json:"name"`
			Order *int64  `json:"order"`
		} `json:"cast"`
	}
	rawURL := fmt.Sprintf("%s/tv/%d/aggregate_credits", tmdbAPIBaseURL, id)
	if err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &response); err != nil {
		return nil, err
	}
	cast := response.Cast
	sort.SliceStable(cast, func(i, j int) bool {
		return tmdbOrderOrMax(cast[i].Order) < tmdbOrderOrMax(cast[j].Order)
	})
	out := []TmdbCastEntry{}
	for _, member := range cast {
		if member.Name == nil {
			continue
		}
		out = append(out, TmdbCastEntry{ID: member.ID, Name: *member.Name})
		if len(out) == 8 {
			break
		}
	}
	return out, nil
}

// EpisodeTitle mirrors `episode_title`.
func (t *TmdbClient) EpisodeTitle(ctx context.Context, tmdbID string, season, episode int64) (*string, error) {
	if t.key == nil {
		return nil, nil
	}
	id, err := strconv.ParseInt(strings.TrimSpace(tmdbID), 10, 64)
	if err != nil {
		return nil, nil
	}
	cacheKey := fmt.Sprintf("episode:%d:%d:%d", id, season, episode)
	if value, ok := t.cacheGet(cacheKey); ok {
		return value, nil
	}
	var response episodeResult
	rawURL := fmt.Sprintf("%s/tv/%d/season/%d/episode/%d", tmdbAPIBaseURL, id, season, episode)
	if err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &response); err != nil {
		return nil, err
	}
	var result *string
	if response.Name != nil && strings.TrimSpace(*response.Name) != "" {
		result = response.Name
	}
	t.cacheSet(cacheKey, result)
	return result, nil
}

// SeasonCounts mirrors `season_counts`.
func (t *TmdbClient) SeasonCounts(ctx context.Context, tmdbID string) (map[int64]int64, error) {
	if t.key == nil {
		return map[int64]int64{}, nil
	}
	id, err := strconv.ParseInt(strings.TrimSpace(tmdbID), 10, 64)
	if err != nil {
		return map[int64]int64{}, nil
	}
	var details seriesDetails
	rawURL := fmt.Sprintf("%s/tv/%d", tmdbAPIBaseURL, id)
	if err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &details); err != nil {
		return nil, err
	}
	counts := map[int64]int64{}
	for _, season := range details.Seasons {
		if season.SeasonNumber != nil && season.EpisodeCount != nil {
			counts[*season.SeasonNumber] = *season.EpisodeCount
		}
	}
	return counts, nil
}

// SeasonEpisodes mirrors `season_episodes`: episodes and air dates for one
// season. Called only during an explicit metadata refresh; the caller persists
// them in the database.
func (t *TmdbClient) SeasonEpisodes(ctx context.Context, tmdbID string, season int64) ([]TmdbEpisode, error) {
	if t.key == nil || season < 1 {
		return []TmdbEpisode{}, nil
	}
	id, err := strconv.ParseInt(strings.TrimSpace(tmdbID), 10, 64)
	if err != nil {
		return []TmdbEpisode{}, nil
	}
	var details seasonDetails
	rawURL := fmt.Sprintf("%s/tv/%d/season/%d", tmdbAPIBaseURL, id, season)
	if err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &details); err != nil {
		return nil, err
	}
	if details.Episodes == nil {
		return []TmdbEpisode{}, nil
	}
	return details.Episodes, nil
}

// NextEpisode mirrors `next_episode`.
func (t *TmdbClient) NextEpisode(ctx context.Context, tmdbID string) (*TmdbEpisode, error) {
	if t.key == nil {
		return nil, nil
	}
	id, err := strconv.ParseInt(strings.TrimSpace(tmdbID), 10, 64)
	if err != nil {
		return nil, nil
	}
	var details seriesDetails
	rawURL := fmt.Sprintf("%s/tv/%d", tmdbAPIBaseURL, id)
	if err := t.getJSON(ctx, rawURL, [][2]string{{"language", t.language}}, &details); err != nil {
		return nil, err
	}
	return details.NextEpisodeToAir, nil
}

// categoryPath mirrors the free function `category_path`.
func categoryPath(kind, category string) (string, bool) {
	movie := strings.EqualFold(kind, "movie")
	switch {
	case movie && category == "top_rated":
		return "movie/top_rated", true
	case !movie && category == "top_rated":
		return "tv/top_rated", true
	case movie && category == "now_playing":
		return "movie/now_playing", true
	case !movie && category == "now_playing":
		return "tv/on_the_air", true
	case movie && category == "upcoming":
		return "movie/upcoming", true
	case !movie && category == "upcoming":
		return "tv/airing_today", true
	default:
		return "", false
	}
}

// tmdbOrderOrMax mirrors `order.unwrap_or(i64::MAX)`.
func tmdbOrderOrMax(order *int64) int64 {
	if order == nil {
		return 1<<63 - 1
	}
	return *order
}
