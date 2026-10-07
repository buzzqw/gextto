package tui

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// SeriesConfig mirrors the editable fields of one monitored series.
type SeriesConfig struct {
	Name             string   `json:"name"`
	Seasons          string   `json:"seasons"`
	Quality          string   `json:"quality"`
	Language         string   `json:"language"`
	ArchivePath      string   `json:"archive_path"`
	Aliases          []string `json:"aliases"`
	TmdbID           string   `json:"tmdb_id"`
	TvdbID           string   `json:"tvdb_id"`
	Subtitle         string   `json:"subtitle"`
	Exclude          string   `json:"exclude"`
	Enabled          bool     `json:"enabled"`
	IgnoredSeasons   []int64  `json:"ignored_seasons"`
	SeasonSubfolders bool     `json:"season_subfolders"`
	DisableUpgrades  bool     `json:"disable_upgrades"`
	Anime            bool     `json:"anime"`
}

// Episode is one row of the series detail: downloaded, in progress or
// expected from the TMDB season counts.
type Episode struct {
	Season       int64   `json:"season"`
	Episode      int64   `json:"episode"`
	Title        string  `json:"title"`
	AirDate      string  `json:"air_date"`
	RenamedTitle string  `json:"renamed_title"`
	QualityScore int64   `json:"quality_score"`
	DownloadedAt *string `json:"downloaded_at"`
	ArchivePath  *string `json:"archive_path"`
	SizeBytes    int64   `json:"size_bytes"`
	MagnetLink   *string `json:"magnet_link"`
	Status       string  `json:"status"`
	Error        string  `json:"error"`
	Ignored      bool    `json:"ignored"`
}

// SeasonCount is the number of episodes TMDB expects in a season.
type SeasonCount struct {
	Season int64 `json:"season"`
	Count  int64 `json:"count"`
}

// SeriesDetail is the response of GET /api/series/{name}.
type SeriesDetail struct {
	Series   SeriesConfig  `json:"series"`
	Episodes []Episode     `json:"episodes"`
	Metadata []SeasonCount `json:"metadata"`
}

// MovieConfig mirrors one monitored movie (GET /api/config/library).
type MovieConfig struct {
	ID                   int64  `json:"id"`
	Name                 string `json:"name"`
	Year                 string `json:"year"`
	TmdbID               string `json:"tmdb_id"`
	TvdbID               string `json:"tvdb_id"`
	OriginalTitle        string `json:"original_title"`
	Overview             string `json:"overview"`
	Quality              string `json:"quality"`
	Language             string `json:"language"`
	Enabled              bool   `json:"enabled"`
	Subtitle             string `json:"subtitle"`
	Exclude              string `json:"exclude"`
	LanguageRequirements string `json:"language_requirements"`
	SubtitleRequirements string `json:"subtitle_requirements"`
}

// MovieHistoryItem is one past download of a movie.
type MovieHistoryItem struct {
	Title        string  `json:"title"`
	QualityScore int64   `json:"quality_score"`
	DownloadedAt *string `json:"downloaded_at"`
	SizeBytes    int64   `json:"size_bytes"`
}

// ArchiveMatch is one archived release that matches a movie.
type ArchiveMatch struct {
	Title  string `json:"title"`
	Magnet string `json:"magnet"`
	Source string `json:"source"`
}

// MovieDetail is the response of GET /api/movies/{id}.
type MovieDetail struct {
	Movie    MovieConfig        `json:"movie"`
	History  []MovieHistoryItem `json:"history"`
	Metadata map[string]any     `json:"metadata"`
	Matches  []ArchiveMatch     `json:"matches"`
}

// Library fetches the monitored series (with episode counts) and movies.
func (c *Client) Library(ctx context.Context) ([]SeriesLibraryItem, []MovieConfig, error) {
	var response struct {
		Series []SeriesLibraryItem `json:"series"`
		Movies []MovieConfig       `json:"movies"`
	}
	err := c.get(ctx, "/api/config/library", &response)
	if response.Series == nil {
		response.Series = []SeriesLibraryItem{}
	}
	if response.Movies == nil {
		response.Movies = []MovieConfig{}
	}
	return response.Series, response.Movies, err
}

func seriesPath(name string) string { return "/api/series/" + url.PathEscape(name) }

func episodePath(series string, season, episode int64) string {
	return fmt.Sprintf("/api/episodes/%s/%d/%d", url.PathEscape(series), season, episode)
}

func moviePath(id int64) string { return "/api/movies/" + strconv.FormatInt(id, 10) }

// SeriesDetail fetches a series with all its episodes.
func (c *Client) SeriesDetail(ctx context.Context, name string) (SeriesDetail, error) {
	var detail SeriesDetail
	err := c.get(ctx, seriesPath(name), &detail)
	return detail, err
}

// UpdateSeries changes only the given fields of a series.
func (c *Client) UpdateSeries(ctx context.Context, name string, fields map[string]any) error {
	return c.postJSON(ctx, seriesPath(name), fields, nil)
}

// DeleteSeries removes a series from the library.
func (c *Client) DeleteSeries(ctx context.Context, name string) error {
	return c.send(ctx, http.MethodDelete, seriesPath(name), nil)
}

// ToggleSeason turns the monitoring of one season on or off.
func (c *Client) ToggleSeason(ctx context.Context, name string, season int64, enabled bool) error {
	return c.postJSON(ctx, seriesPath(name)+"/toggle-season", map[string]any{"season": season, "enabled": enabled}, nil)
}

// SeriesSearchMissing searches the indexers for the series' missing episodes.
func (c *Client) SeriesSearchMissing(ctx context.Context, name string) ([]map[string]any, int, error) {
	var response struct {
		Searched int              `json:"searched"`
		Results  []map[string]any `json:"results"`
	}
	err := c.postJSON(ctx, seriesPath(name)+"/search-missing", map[string]any{}, &response)
	return flattenReleases(response.Results), response.Searched, err
}

// SeriesMetadata refreshes season counts and air dates from TMDB.
func (c *Client) SeriesMetadata(ctx context.Context, name string) error {
	return c.postJSON(ctx, seriesPath(name)+"/metadata", map[string]any{}, nil)
}

// SeriesRename previews (execute=false) or applies the episode renames and
// returns how many files are (or would be) renamed.
func (c *Client) SeriesRename(ctx context.Context, name string, execute bool) (int, error) {
	path := seriesPath(name) + "/rename-preview"
	if execute {
		path = seriesPath(name) + "/rename-execute"
	}
	var response struct {
		Skipped bool             `json:"skipped"`
		Reason  string           `json:"reason"`
		Items   []map[string]any `json:"items"`
	}
	if err := c.postJSON(ctx, path, map[string]any{}, &response); err != nil {
		return 0, err
	}
	if response.Skipped {
		return 0, fmt.Errorf("%s", response.Reason)
	}
	count := 0
	for _, item := range response.Items {
		if stringValue(item["to"]) != "" {
			count++
		}
	}
	return count, nil
}

// EpisodeSources lists the releases already collected for an episode.
func (c *Client) EpisodeSources(ctx context.Context, series string, season, episode int64) ([]map[string]any, error) {
	var response struct {
		Results []map[string]any `json:"results"`
	}
	err := c.get(ctx, episodePath(series, season, episode)+"/sources", &response)
	return flattenReleases(response.Results), err
}

// SearchEpisode searches the indexers for one episode.
func (c *Client) SearchEpisode(ctx context.Context, series string, season, episode int64) ([]map[string]any, error) {
	var response struct {
		Results []map[string]any `json:"results"`
	}
	err := c.postJSON(ctx, episodePath(series, season, episode)+"/search", map[string]any{}, &response)
	return flattenReleases(response.Results), err
}

// IgnoreEpisode marks an episode as ignored (or not) by the missing search.
func (c *Client) IgnoreEpisode(ctx context.Context, series string, season, episode int64, ignored bool) error {
	return c.postJSON(ctx, episodePath(series, season, episode)+"/ignore", map[string]any{"ignored": ignored, "reason": "tui"}, nil)
}

// RedownloadEpisode makes an episode downloadable again at the next cycle.
func (c *Client) RedownloadEpisode(ctx context.Context, series string, season, episode int64) error {
	return c.postJSON(ctx, episodePath(series, season, episode)+"/redownload", map[string]any{}, nil)
}

// MovieDetail fetches a movie with its history and archive matches.
func (c *Client) MovieDetail(ctx context.Context, id int64) (MovieDetail, error) {
	var detail MovieDetail
	err := c.get(ctx, moviePath(id), &detail)
	return detail, err
}

// UpdateMovie saves every editable field of a movie.
func (c *Client) UpdateMovie(ctx context.Context, movie MovieConfig) error {
	return c.postJSON(ctx, moviePath(movie.ID), map[string]any{
		"name":                  movie.Name,
		"year":                  movie.Year,
		"quality":               movie.Quality,
		"language":              movie.Language,
		"subtitle":              movie.Subtitle,
		"exclude":               movie.Exclude,
		"language_requirements": movie.LanguageRequirements,
		"subtitle_requirements": movie.SubtitleRequirements,
		"enabled":               movie.Enabled,
		"tmdb_id":               movie.TmdbID,
		"tvdb_id":               movie.TvdbID,
	}, nil)
}

// DeleteMovie removes a movie from the library.
func (c *Client) DeleteMovie(ctx context.Context, id int64) error {
	return c.send(ctx, http.MethodDelete, moviePath(id), nil)
}

// SearchMovie searches the indexers and the archive for a movie.
func (c *Client) SearchMovie(ctx context.Context, id int64) ([]map[string]any, error) {
	var response struct {
		Results []map[string]any `json:"results"`
	}
	err := c.postJSON(ctx, moviePath(id)+"/search", map[string]any{}, &response)
	return response.Results, err
}

// RedownloadMovie queues a movie again for the next cycle.
func (c *Client) RedownloadMovie(ctx context.Context, id int64) error {
	return c.postJSON(ctx, moviePath(id)+"/redownload", map[string]any{}, nil)
}

// TmdbSearch looks up series ("series") or movies ("movie") on TMDB.
func (c *Client) TmdbSearch(ctx context.Context, kind, query string) ([]map[string]any, error) {
	var response struct {
		Items []map[string]any `json:"items"`
	}
	err := c.postJSON(ctx, "/api/tmdb/search", map[string]any{"kind": kind, "query": query}, &response)
	if response.Items == nil {
		response.Items = []map[string]any{}
	}
	return response.Items, err
}

// TmdbAdd adds a series or movie to the library.
func (c *Client) TmdbAdd(ctx context.Context, fields map[string]any) error {
	return c.postJSON(ctx, "/api/tmdb/add", fields, nil)
}

// send issues a request without a body and checks the status.
func (c *Client) send(ctx context.Context, method, path string, out any) error {
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if err != nil {
		return err
	}
	response, err := c.do(request)
	if err != nil {
		return err
	}
	return c.decode(response, out)
}

// flattenReleases turns {release, origin, score} search results into release
// maps carrying origin and score, the shape the search overlay renders and
// /api/search/add accepts.
func flattenReleases(results []map[string]any) []map[string]any {
	flat := make([]map[string]any, 0, len(results))
	for _, result := range results {
		release, ok := result["release"].(map[string]any)
		if !ok {
			flat = append(flat, result)
			continue
		}
		item := make(map[string]any, len(release)+2)
		for key, value := range release {
			item[key] = value
		}
		for _, key := range []string{"origin", "score", "season", "episode"} {
			if value, ok := result[key]; ok {
				if _, exists := item[key]; !exists || key == "origin" || key == "score" {
					item[key] = value
				}
			}
		}
		flat = append(flat, item)
	}
	return flat
}
