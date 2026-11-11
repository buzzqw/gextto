package gextto

// tvmaze_metadata.go is the series metadata provider of last resort: when
// neither a TMDB nor a TVDB key is set, TVmaze (public, no key) answers the
// same calls as the other two with the same TMDB shapes. It covers seasons,
// episodes and air dates, the next episode, posters and the status; titles
// and plots come in English, and it has no movies. Ids handed out are TVmaze
// show ids; a series with a stored TVDB id is looked up by it ("tvdb:<id>"),
// so two series with the same title are never confused.

import (
	"context"
	"errors"
	"html"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/tvmaze"
)

type tvmazeSeriesMetadata struct{}

func (tvmazeSeriesMetadata) Source() string   { return "tvmaze" }
func (tvmazeSeriesMetadata) Configured() bool { return true }

// StoredID is the stored TVDB id in the form the provider resolves
// ("tvdb:<id>"): TVmaze ids are not kept with the series.
func (tvmazeSeriesMetadata) StoredID(series *SeriesConfig) string {
	if series == nil {
		return ""
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(series.TvdbID), 10, 64); err == nil && id > 0 {
		return "tvdb:" + strconv.FormatInt(id, 10)
	}
	return ""
}

// tvmazeCacheTTL keeps what TVmaze said for a few hours: the daily refresh
// and a page full of posters must not ask the same thing twice.
const tvmazeCacheTTL = 6 * time.Hour

type tvmazeCacheEntry struct {
	at    time.Time
	value any
}

var tvmazeCache = struct {
	sync.Mutex
	entries map[string]tvmazeCacheEntry
}{entries: map[string]tvmazeCacheEntry{}}

func tvmazeCached[T any](ctx context.Context, key string, fetch func(context.Context, *tvmaze.Client) (T, error)) (T, error) {
	tvmazeCache.Lock()
	if entry, ok := tvmazeCache.entries[key]; ok && time.Since(entry.at) < tvmazeCacheTTL {
		tvmazeCache.Unlock()
		return entry.value.(T), nil
	}
	tvmazeCache.Unlock()
	var zero T
	if ctx == nil {
		ctx = context.Background()
	}
	if err := tvmazeWait(ctx); err != nil {
		return zero, err
	}
	value, err := fetch(ctx, newSeriesTvmazeClient())
	if err != nil {
		return zero, err
	}
	tvmazeCache.Lock()
	tvmazeCache.entries[key] = tvmazeCacheEntry{at: time.Now(), value: value}
	tvmazeCache.Unlock()
	return value, nil
}

// showID turns a provider id into a TVmaze show id: a number is one already,
// "tvdb:<id>" is looked up.
func (tvmazeSeriesMetadata) showID(ctx context.Context, id string) (int, error) {
	id = strings.TrimSpace(id)
	if rest, ok := strings.CutPrefix(id, "tvdb:"); ok {
		tvdbID, err := strconv.ParseInt(rest, 10, 64)
		if err != nil || tvdbID <= 0 {
			return 0, errors.New("tvmaze: invalid TVDB id")
		}
		show, err := tvmazeCached(ctx, "lookup:"+rest, func(ctx context.Context, c *tvmaze.Client) (*tvmaze.Show, error) {
			return c.Lookup(ctx, tvdbID, "")
		})
		if err != nil {
			return 0, err
		}
		return show.ID, nil
	}
	value, err := strconv.Atoi(id)
	if err != nil || value <= 0 {
		return 0, errors.New("tvmaze: invalid show id")
	}
	return value, nil
}

func (m tvmazeSeriesMetadata) ResolveSeriesID(ctx context.Context, name string) (*string, error) {
	query := strings.TrimSpace(name)
	if query == "" {
		return nil, nil
	}
	shows, err := tvmazeCached(ctx, "search:"+strings.ToLower(query), func(ctx context.Context, c *tvmaze.Client) ([]tvmaze.Show, error) {
		return c.Search(ctx, query)
	})
	if err != nil || len(shows) == 0 {
		return nil, err
	}
	id := strconv.Itoa(shows[0].ID)
	return &id, nil
}

// episodes returns every regular episode of a series in the TMDB shape.
func (m tvmazeSeriesMetadata) episodes(ctx context.Context, id string) ([]TmdbEpisode, error) {
	showID, err := m.showID(ctx, id)
	if err != nil {
		return nil, err
	}
	raw, err := tvmazeCached(ctx, "episodes:"+strconv.Itoa(showID), func(ctx context.Context, c *tvmaze.Client) ([]tvmaze.Episode, error) {
		return c.Episodes(ctx, showID)
	})
	if err != nil {
		return nil, err
	}
	out := make([]TmdbEpisode, 0, len(raw))
	for _, episode := range raw {
		if episode.Season < 1 || episode.Number < 1 {
			continue
		}
		episodeID, season, number := int64(episode.ID), int64(episode.Season), int64(episode.Number)
		item := TmdbEpisode{ID: &episodeID, SeasonNumber: &season, EpisodeNumber: &number}
		if name := strings.TrimSpace(episode.Name); name != "" {
			item.Name = &name
		}
		if date := strings.TrimSpace(episode.AirDate); date != "" {
			item.AirDate = &date
		}
		out = append(out, item)
	}
	return out, nil
}

func (m tvmazeSeriesMetadata) SeasonCounts(ctx context.Context, id string) (map[int64]int64, error) {
	episodes, err := m.episodes(ctx, id)
	if err != nil {
		return nil, err
	}
	counts := map[int64]int64{}
	for _, episode := range episodes {
		counts[*episode.SeasonNumber]++
	}
	return counts, nil
}

func (m tvmazeSeriesMetadata) SeasonEpisodes(ctx context.Context, id string, season int64) ([]TmdbEpisode, error) {
	episodes, err := m.episodes(ctx, id)
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

func (m tvmazeSeriesMetadata) NextEpisode(ctx context.Context, id string) (*TmdbEpisode, error) {
	episodes, err := m.episodes(ctx, id)
	if err != nil {
		return nil, err
	}
	return tvdbNextEpisode(episodes, time.Now()), nil
}

func (m tvmazeSeriesMetadata) EpisodeTitle(ctx context.Context, id string, season, episode int64) (*string, error) {
	episodes, err := m.episodes(ctx, id)
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

func (m tvmazeSeriesMetadata) show(ctx context.Context, name string, id *string) (*tvmaze.Show, error) {
	value := ""
	if id != nil {
		value = strings.TrimSpace(*id)
	}
	if value == "" {
		resolved, err := m.ResolveSeriesID(ctx, name)
		if err != nil || resolved == nil {
			return nil, err
		}
		value = *resolved
	}
	showID, err := m.showID(ctx, value)
	if err != nil {
		return nil, err
	}
	return tvmazeCached(ctx, "show:"+strconv.Itoa(showID), func(ctx context.Context, c *tvmaze.Client) (*tvmaze.Show, error) {
		return c.ShowByID(ctx, showID)
	})
}

// tvmazeProviderStatus maps the TVmaze status onto the TMDB words the rest of
// Gextto knows. The raw value travels too ("tvmaze_status"), so "To Be
// Determined" still reaches the verdict.
func tvmazeProviderStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "to be determined":
		return "Returning Series"
	case "ended":
		return "Ended"
	case "in development":
		return "In Production"
	}
	return strings.TrimSpace(status)
}

var tvmazeTagPattern = regexp.MustCompile(`<[^>]*>`)

// tvmazePlainText turns the HTML summary of TVmaze into plain text.
func tvmazePlainText(value string) string {
	return strings.TrimSpace(html.UnescapeString(tvmazeTagPattern.ReplaceAllString(value, "")))
}

// SeriesInfo returns the details in the shape of the TMDB /tv record, plus
// "tvmaze_status" (the raw status) and "external_ids". There is no "id": it
// would be read as a TMDB id.
func (m tvmazeSeriesMetadata) SeriesInfo(ctx context.Context, name string, id *string) (map[string]any, error) {
	show, err := m.show(ctx, name, id)
	if err != nil || show == nil {
		return nil, err
	}
	info := map[string]any{
		"name":          show.Name,
		"overview":      tvmazePlainText(show.Summary),
		"status":        tvmazeProviderStatus(show.Status),
		"tvmaze_status": strings.TrimSpace(show.Status),
	}
	if show.Image != nil {
		if poster := tvmazeFirst(show.Image.Original, show.Image.Medium); poster != "" {
			info["poster_path"] = poster
		}
	}
	if show.Premiered != "" {
		info["first_air_date"] = show.Premiered
	}
	for _, channel := range []*tvmaze.Channel{show.Network, show.WebChannel} {
		if channel == nil || strings.TrimSpace(channel.Name) == "" {
			continue
		}
		info["networks"] = []any{map[string]any{"name": channel.Name}}
		if channel.Country != nil && channel.Country.Code != "" {
			info["origin_country"] = []any{channel.Country.Code}
		}
		break
	}
	genres := []any{}
	for _, genre := range show.Genres {
		genres = append(genres, map[string]any{"name": genre})
	}
	info["genres"] = genres
	external := map[string]any{}
	if show.Externals.TheTVDB != nil && *show.Externals.TheTVDB > 0 {
		external["tvdb_id"] = float64(*show.Externals.TheTVDB)
	}
	if show.Externals.IMDb != nil && *show.Externals.IMDb != "" {
		external["imdb_id"] = *show.Externals.IMDb
	}
	info["external_ids"] = external
	if episodes, err := m.episodes(ctx, strconv.Itoa(show.ID)); err == nil {
		seasons := map[int64]bool{}
		for _, episode := range episodes {
			seasons[*episode.SeasonNumber] = true
		}
		info["number_of_seasons"] = len(seasons)
		info["number_of_episodes"] = len(episodes)
		now := time.Now()
		if last := tvdbLastEpisode(episodes, now); last != nil {
			info["last_episode_to_air"] = toMap(last)
			if last.AirDate != nil {
				info["last_air_date"] = *last.AirDate
			}
		}
		if next := tvdbNextEpisode(episodes, now); next != nil {
			info["next_episode_to_air"] = toMap(next)
		}
	}
	return info, nil
}

func (m tvmazeSeriesMetadata) PosterForSeries(ctx context.Context, name string) (*string, error) {
	show, err := m.show(ctx, name, nil)
	if err != nil || show == nil || show.Image == nil {
		return nil, err
	}
	poster := tvmazeFirst(show.Image.Medium, show.Image.Original)
	if poster == "" {
		return nil, nil
	}
	return &poster, nil
}

// tvmazeFirst returns the first value that is not empty.
func tvmazeFirst(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
