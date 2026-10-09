package gextto

// tvdb_metadata.go gives the TVDB client the series metadata calls of the
// TMDB client (season sizes, episodes and air dates, next episode, episode
// titles, status and details), with the same signatures and result shapes, so
// a TVDB-only installation gets the same features. See series_metadata.go.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// tvdbMetadataTTL is how long episode lists and series details stay cached:
// they feed the cycle, the calendar and every rename, and change rarely.
const tvdbMetadataTTL = 6 * time.Hour

// tvdbMaxEpisodePages caps the paginated episode list (500 per page).
const tvdbMaxEpisodePages = 20

var tvdbMetadataCache = struct {
	sync.Mutex
	items map[string]tvdbCacheEntry
}{items: map[string]tvdbCacheEntry{}}

type tvdbCacheEntry struct {
	value  any
	expiry time.Time
}

// tvdbCacheGet and tvdbCacheSet key on the API base URL too, so tests with
// their own server never see each other's data.
func tvdbCacheGet(key string) (any, bool) {
	tvdbMetadataCache.Lock()
	defer tvdbMetadataCache.Unlock()
	entry, ok := tvdbMetadataCache.items[tvdbAPI+"\x00"+key]
	if !ok || time.Now().After(entry.expiry) {
		return nil, false
	}
	return entry.value, true
}

func tvdbCacheSet(key string, value any) {
	tvdbMetadataCache.Lock()
	defer tvdbMetadataCache.Unlock()
	tvdbMetadataCache.items[tvdbAPI+"\x00"+key] = tvdbCacheEntry{value: value, expiry: time.Now().Add(tvdbMetadataTTL)}
}

// getData performs an authenticated GET of path and returns the decoded body.
// A refused token is forgotten so the next call logs in again.
func (c *TvdbClient) getData(ctx context.Context, path string) (any, int, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, 0, err
	}
	headers := map[string]string{
		"Accept-Language": c.language,
		"Authorization":   "Bearer " + token,
	}
	body, status, err := HTTPGetBytes(ctx, tvdbAPI+path, headers)
	if err != nil {
		return nil, status, err
	}
	if status == 401 {
		c.forgetToken()
	}
	if !tvdbSuccess(status) {
		return nil, status, fmt.Errorf("TVDB HTTP %d", status)
	}
	value, err := tvdbJSON(body)
	return value, status, err
}

// tvdbSeriesID parses a stored or resolved TVDB series id.
func tvdbSeriesID(id string) (int64, bool) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	return parsed, err == nil && parsed > 0
}

// ResolveSeriesID returns the TVDB id of the first series found by name.
func (c *TvdbClient) ResolveSeriesID(ctx context.Context, name string) (*string, error) {
	if !c.Configured() {
		return nil, nil
	}
	cacheKey := "resolve:" + c.language + ":" + strings.ToLower(strings.TrimSpace(name))
	if cached, ok := tvdbCacheGet(cacheKey); ok {
		value, _ := cached.(*string)
		return value, nil
	}
	items, err := c.SearchSeries(ctx, name)
	if err != nil {
		return nil, err
	}
	var result *string
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if id := strings.TrimPrefix(v2AnyString(item["tvdb_id"]), "series-"); id != "" {
			result = &id
			break
		}
	}
	tvdbCacheSet(cacheKey, result)
	return result, nil
}

// SeriesEpisodes returns every episode of a series (aired order), with the
// titles in the configured language when TVDB has them, else the original.
func (c *TvdbClient) SeriesEpisodes(ctx context.Context, tvdbID string) ([]TmdbEpisode, error) {
	if !c.Configured() {
		return []TmdbEpisode{}, nil
	}
	id, ok := tvdbSeriesID(tvdbID)
	if !ok {
		return []TmdbEpisode{}, nil
	}
	cacheKey := fmt.Sprintf("episodes:%d:%s", id, c.language)
	if cached, ok := tvdbCacheGet(cacheKey); ok {
		return cached.([]TmdbEpisode), nil
	}
	episodes, err := c.episodePages(ctx, fmt.Sprintf("/series/%d/episodes/default", id))
	if err != nil {
		return nil, err
	}
	// Translated titles: a missing translation keeps the original title.
	if translated, err := c.episodePages(ctx, fmt.Sprintf("/series/%d/episodes/default/%s", id, c.language)); err == nil {
		names := map[[2]int64]string{}
		for _, episode := range translated {
			if episode.Name != nil && strings.TrimSpace(*episode.Name) != "" {
				names[[2]int64{*episode.SeasonNumber, *episode.EpisodeNumber}] = *episode.Name
			}
		}
		for index := range episodes {
			if name, ok := names[[2]int64{*episodes[index].SeasonNumber, *episodes[index].EpisodeNumber}]; ok {
				episodes[index].Name = &name
			}
		}
	}
	tvdbCacheSet(cacheKey, episodes)
	return episodes, nil
}

// episodePages reads a paginated episode list. Episodes without a season or
// number are skipped; the result is sorted by season and episode.
func (c *TvdbClient) episodePages(ctx context.Context, path string) ([]TmdbEpisode, error) {
	episodes := []TmdbEpisode{}
	for page := 0; page < tvdbMaxEpisodePages; page++ {
		value, _, err := c.getData(ctx, fmt.Sprintf("%s?page=%d", path, page))
		if err != nil {
			return nil, err
		}
		items := tvdbArray(tvdbGet(tvdbGet(value, "data"), "episodes"))
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			season, okSeason := tvdbInt64(item["seasonNumber"])
			number, okNumber := tvdbInt64(item["number"])
			if !okSeason || !okNumber {
				continue
			}
			episode := TmdbEpisode{SeasonNumber: &season, EpisodeNumber: &number}
			if episodeID, ok := tvdbInt64(item["id"]); ok {
				episode.ID = &episodeID
			}
			if name, ok := tvdbString(item["name"]); ok && strings.TrimSpace(name) != "" {
				episode.Name = &name
			}
			if aired, ok := tvdbString(item["aired"]); ok && strings.TrimSpace(aired) != "" {
				episode.AirDate = &aired
			}
			episodes = append(episodes, episode)
		}
		next, _ := tvdbString(tvdbGet(tvdbGet(value, "links"), "next"))
		if len(items) == 0 || strings.TrimSpace(next) == "" {
			break
		}
	}
	sort.SliceStable(episodes, func(i, j int) bool {
		if *episodes[i].SeasonNumber != *episodes[j].SeasonNumber {
			return *episodes[i].SeasonNumber < *episodes[j].SeasonNumber
		}
		return *episodes[i].EpisodeNumber < *episodes[j].EpisodeNumber
	})
	return episodes, nil
}

// SeasonCounts returns the number of episodes of each season (season 0, the
// specials, included like TMDB does).
func (c *TvdbClient) SeasonCounts(ctx context.Context, tvdbID string) (map[int64]int64, error) {
	episodes, err := c.SeriesEpisodes(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	counts := map[int64]int64{}
	for _, episode := range episodes {
		counts[*episode.SeasonNumber]++
	}
	return counts, nil
}

// SeasonEpisodes returns the episodes of one season with their air dates.
func (c *TvdbClient) SeasonEpisodes(ctx context.Context, tvdbID string, season int64) ([]TmdbEpisode, error) {
	if season < 1 {
		return []TmdbEpisode{}, nil
	}
	episodes, err := c.SeriesEpisodes(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	out := []TmdbEpisode{}
	for _, episode := range episodes {
		if *episode.SeasonNumber == season {
			out = append(out, episode)
		}
	}
	return out, nil
}

// NextEpisode returns the first regular episode airing today or later.
func (c *TvdbClient) NextEpisode(ctx context.Context, tvdbID string) (*TmdbEpisode, error) {
	episodes, err := c.SeriesEpisodes(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	return tvdbNextEpisode(episodes, time.Now()), nil
}

func tvdbNextEpisode(episodes []TmdbEpisode, now time.Time) *TmdbEpisode {
	today := now.Format("2006-01-02")
	var next *TmdbEpisode
	for index := range episodes {
		episode := episodes[index]
		if *episode.SeasonNumber < 1 || episode.AirDate == nil || *episode.AirDate < today {
			continue
		}
		if next == nil || *episode.AirDate < *next.AirDate {
			next = &episode
		}
	}
	return next
}

// tvdbLastEpisode returns the last regular episode aired before today.
func tvdbLastEpisode(episodes []TmdbEpisode, now time.Time) *TmdbEpisode {
	today := now.Format("2006-01-02")
	var last *TmdbEpisode
	for index := range episodes {
		episode := episodes[index]
		if *episode.SeasonNumber < 1 || episode.AirDate == nil || *episode.AirDate >= today {
			continue
		}
		if last == nil || *episode.AirDate >= *last.AirDate {
			last = &episode
		}
	}
	return last
}

// EpisodeTitle returns the title of one episode (nil when unknown).
func (c *TvdbClient) EpisodeTitle(ctx context.Context, tvdbID string, season, episode int64) (*string, error) {
	episodes, err := c.SeriesEpisodes(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	for _, item := range episodes {
		if *item.SeasonNumber == season && *item.EpisodeNumber == episode && item.Name != nil {
			name := *item.Name
			return &name, nil
		}
	}
	return nil, nil
}

// seriesDetails returns the cached extended record (short, with translations).
func (c *TvdbClient) seriesDetails(ctx context.Context, id int64) (map[string]any, error) {
	cacheKey := fmt.Sprintf("details:%d", id)
	if cached, ok := tvdbCacheGet(cacheKey); ok {
		return cached.(map[string]any), nil
	}
	value, _, err := c.getData(ctx, fmt.Sprintf("/series/%d/extended?meta=translations&short=true", id))
	if err != nil {
		return nil, err
	}
	data, _ := tvdbGet(value, "data").(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	tvdbCacheSet(cacheKey, data)
	return data, nil
}

// PosterForSeries returns the poster of the first series found by name, as a
// full URL (TMDB returns a path; metadataImageURL handles both).
func (c *TvdbClient) PosterForSeries(ctx context.Context, name string) (*string, error) {
	id, err := c.ResolveSeriesID(ctx, name)
	if err != nil || id == nil {
		return nil, err
	}
	parsed, _ := tvdbSeriesID(*id)
	details, err := c.seriesDetails(ctx, parsed)
	if err != nil {
		return nil, err
	}
	if image, ok := tvdbString(details["image"]); ok && strings.TrimSpace(image) != "" {
		return &image, nil
	}
	return nil, nil
}

// tvdbStatus maps the TVDB status names onto the TMDB ones the interface
// knows ("Ended", "Returning Series", ...).
func tvdbStatus(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "continuing":
		return "Returning Series"
	case "upcoming":
		return "Planned"
	case "ended":
		return "Ended"
	}
	return strings.TrimSpace(name)
}

// tvdbCountries maps the ISO 3166-1 alpha-3 codes TVDB uses onto the alpha-2
// codes of TMDB for the most common countries.
var tvdbCountries = map[string]string{
	"usa": "US", "gbr": "GB", "ita": "IT", "fra": "FR", "deu": "DE", "esp": "ES",
	"jpn": "JP", "kor": "KR", "can": "CA", "aus": "AU", "swe": "SE", "dnk": "DK",
	"nor": "NO", "nld": "NL", "bel": "BE", "irl": "IE", "nzl": "NZ", "bra": "BR",
	"mex": "MX", "arg": "AR", "chn": "CN", "ind": "IN", "tur": "TR", "pol": "PL",
	"isl": "IS", "fin": "FI", "aut": "AT", "che": "CH", "prt": "PT", "rus": "RU",
}

func tvdbCountry(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if mapped, ok := tvdbCountries[code]; ok {
		return mapped
	}
	return strings.ToUpper(code)
}

// tvdbTranslation picks the translation in language, then English, then the
// fallback.
func tvdbTranslation(details map[string]any, list, field, language, fallback string) string {
	translations, _ := tvdbGet(details, "translations").(map[string]any)
	byLanguage := map[string]string{}
	for _, raw := range tvdbArray(translations[list]) {
		item, _ := raw.(map[string]any)
		lang, _ := tvdbString(item["language"])
		text, _ := tvdbString(item[field])
		if strings.TrimSpace(text) != "" {
			byLanguage[lang] = text
		}
	}
	for _, lang := range []string{language, "eng"} {
		if text, ok := byLanguage[lang]; ok {
			return text
		}
	}
	return fallback
}

// SeriesInfo returns the series details in the shape of the TMDB /tv record
// (name, overview, poster_path, status, dates, networks, genres, counts, last
// and next episode), plus "tvdb_id". There is no "id": it would be read as a
// TMDB id.
func (c *TvdbClient) SeriesInfo(ctx context.Context, name string, tvdbID *string) (map[string]any, error) {
	if !c.Configured() {
		return nil, nil
	}
	id := ""
	if tvdbID != nil {
		if _, ok := tvdbSeriesID(*tvdbID); ok {
			id = strings.TrimSpace(*tvdbID)
		}
	}
	if id == "" {
		resolved, err := c.ResolveSeriesID(ctx, name)
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			return nil, nil
		}
		id = *resolved
	}
	parsed, ok := tvdbSeriesID(id)
	if !ok {
		return nil, nil
	}
	details, err := c.seriesDetails(ctx, parsed)
	if err != nil {
		return nil, err
	}
	originalName, _ := tvdbString(details["name"])
	overview, _ := tvdbString(details["overview"])
	info := map[string]any{
		"tvdb_id":  id,
		"name":     tvdbTranslation(details, "nameTranslations", "name", c.language, originalName),
		"overview": tvdbTranslation(details, "overviewTranslations", "overview", c.language, overview),
	}
	if image, ok := tvdbString(details["image"]); ok && image != "" {
		info["poster_path"] = image
	}
	if status, ok := tvdbString(tvdbGet(details["status"], "name")); ok {
		info["status"] = tvdbStatus(status)
	}
	if first, ok := tvdbString(details["firstAired"]); ok && first != "" {
		info["first_air_date"] = first
	}
	if last, ok := tvdbString(details["lastAired"]); ok && last != "" {
		info["last_air_date"] = last
	}
	for _, key := range []string{"originalNetwork", "latestNetwork"} {
		if network, ok := tvdbString(tvdbGet(details[key], "name")); ok && network != "" {
			info["networks"] = []any{map[string]any{"name": network}}
			break
		}
	}
	if country, ok := tvdbString(details["originalCountry"]); ok && country != "" {
		info["origin_country"] = []any{tvdbCountry(country)}
	}
	genres := []any{}
	for _, raw := range tvdbArray(details["genres"]) {
		item, _ := raw.(map[string]any)
		if genre, ok := tvdbString(item["name"]); ok && genre != "" {
			genres = append(genres, map[string]any{"name": genre})
		}
	}
	info["genres"] = genres
	// Counts and last/next episode come from the episode list; without it the
	// details above are still worth showing.
	if episodes, err := c.SeriesEpisodes(ctx, id); err == nil {
		seasons := map[int64]bool{}
		total := 0
		for _, episode := range episodes {
			if *episode.SeasonNumber > 0 {
				seasons[*episode.SeasonNumber] = true
				total++
			}
		}
		info["number_of_seasons"] = len(seasons)
		info["number_of_episodes"] = total
		now := time.Now()
		if last := tvdbLastEpisode(episodes, now); last != nil {
			info["last_episode_to_air"] = gh0_toMap(last)
		}
		if next := tvdbNextEpisode(episodes, now); next != nil {
			info["next_episode_to_air"] = gh0_toMap(next)
		}
	}
	return info, nil
}

// tvdbMoviePoster returns the poster URL of the first TVDB movie found by
// name (and year, when known), cached like the other metadata.
func tvdbMoviePoster(ctx context.Context, c *TvdbClient, name string, year *int64) *string {
	if !c.Configured() || strings.TrimSpace(name) == "" {
		return nil
	}
	query := strings.TrimSpace(name)
	cacheKey := "movie_poster:" + strings.ToLower(query)
	if year != nil {
		cacheKey += fmt.Sprintf(":%d", *year)
	}
	if cached, ok := tvdbCacheGet(cacheKey); ok {
		value, _ := cached.(*string)
		return value
	}
	items, err := c.SearchMovies(ctx, query)
	if err != nil {
		return nil
	}
	var poster *string
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if year != nil && v2AnyString(item["release_date"]) != "" && v2AnyString(item["release_date"]) != strconv.FormatInt(*year, 10) {
			continue
		}
		if image := v2AnyString(item["poster_path"]); image != "" {
			poster = &image
		}
		break
	}
	tvdbCacheSet(cacheKey, poster)
	return poster
}
