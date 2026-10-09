package gextto

// anime.go maps anime-style absolute episode numbers ("[Group] Title - 1071")
// onto the seasons TMDB uses, for the series marked as anime. Without TMDB
// data (no key, unknown series) the absolute number is used as an episode of
// season 1, which is how many long-running anime are listed anyway.

import (
	"context"
	"fmt"
	"sort"

	"github.com/buzzqw/gextto/internal/models"
)

// animeNumbering converts between absolute numbers and season/episode.
type animeNumbering struct {
	seasons []int64         // regular seasons in order (season 0 excluded)
	counts  map[int64]int64 // episodes per season
}

func newAnimeNumbering(counts map[int64]int64) animeNumbering {
	numbering := animeNumbering{counts: map[int64]int64{}}
	for season, count := range counts {
		if season >= 1 && count > 0 {
			numbering.seasons = append(numbering.seasons, season)
			numbering.counts[season] = count
		}
	}
	sort.Slice(numbering.seasons, func(a, b int) bool { return numbering.seasons[a] < numbering.seasons[b] })
	return numbering
}

func (n animeNumbering) known() bool { return len(n.seasons) > 0 }

// seasonEpisode returns the season and episode of an absolute number. Past
// the last known episode the number continues the last season (TMDB often
// lags behind a running anime).
func (n animeNumbering) seasonEpisode(absolute int64) (int64, int64, bool) {
	if absolute <= 0 {
		return 0, 0, false
	}
	if !n.known() {
		return 1, absolute, true
	}
	remaining := absolute
	for index, season := range n.seasons {
		count := n.counts[season]
		if remaining <= count || index == len(n.seasons)-1 {
			return season, remaining, true
		}
		remaining -= count
	}
	return 0, 0, false
}

// absolute returns the absolute number of season/episode.
func (n animeNumbering) absolute(season, episode int64) (int64, bool) {
	if season < 1 || episode < 1 {
		return 0, false
	}
	if !n.known() {
		if season == 1 {
			return episode, true
		}
		return 0, false
	}
	total := int64(0)
	for _, current := range n.seasons {
		if current == season {
			return total + episode, true
		}
		total += n.counts[current]
	}
	return 0, false
}

// animeNumberingFor reads the season sizes of an anime series from TMDB, or
// TVDB without a TMDB key (both clients cache them).
func animeNumberingFor(ctx context.Context, cfg *Config, series *SeriesConfig) animeNumbering {
	if cfg == nil || series == nil {
		return newAnimeNumbering(nil)
	}
	meta := seriesMetadataFor(cfg)
	if !meta.Configured() {
		return newAnimeNumbering(nil)
	}
	seriesID, err := metadataSeriesID(ctx, meta, series)
	if err != nil || seriesID == nil {
		return newAnimeNumbering(nil)
	}
	counts, err := meta.SeasonCounts(ctx, *seriesID)
	if err != nil {
		return newAnimeNumbering(nil)
	}
	return newAnimeNumbering(counts)
}

func hasAnimeSeries(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	for _, series := range cfg.Series {
		if series.Anime {
			return true
		}
	}
	return false
}

// resolveAnimeReleases turns the releases of the series marked as anime into
// regular season/episode releases:
//   - a title numbered by absolute episode ("Title - 1071") gets the season
//     and episode the number falls in;
//   - "Title S01E1071", which some indexers produce for long anime, is read as
//     absolute when season 1 has fewer episodes than that.
//
// Releases of other series are returned unchanged.
func resolveAnimeReleases(ctx context.Context, cfg *Config, releases []models.Release) []models.Release {
	if !hasAnimeSeries(cfg) {
		return releases
	}
	numberings := map[string]animeNumbering{}
	numberingFor := func(series *SeriesConfig) animeNumbering {
		if numbering, ok := numberings[series.Name]; ok {
			return numbering
		}
		numbering := animeNumberingFor(ctx, cfg, series)
		numberings[series.Name] = numbering
		return numbering
	}
	for index := range releases {
		release := &releases[index]
		switch {
		case release.Kind == "movie" && release.AbsoluteEpisode != nil && release.AbsoluteSeries != nil:
			series := cfg.FindSeriesMatch(*release.AbsoluteSeries, nil)
			if series == nil || !series.Anime {
				continue
			}
			season, episode, ok := numberingFor(series).seasonEpisode(*release.AbsoluteEpisode)
			if !ok {
				continue
			}
			name := series.Name
			release.Kind = "series"
			release.Series = &name
			release.Season = &season
			release.Episode = &episode
			release.EpisodeRange = []int64{episode}
			release.IsPack = false
			release.Year = nil
		case release.Kind == "series" && release.Series != nil && !release.IsPack &&
			release.Season != nil && *release.Season == 1 && release.Episode != nil:
			series := cfg.FindSeriesMatch(*release.Series, release.Season)
			if series == nil || !series.Anime {
				continue
			}
			numbering := numberingFor(series)
			if !numbering.known() || *release.Episode <= numbering.counts[1] {
				continue
			}
			absolute := *release.Episode
			season, episode, ok := numbering.seasonEpisode(absolute)
			if !ok {
				continue
			}
			release.AbsoluteEpisode = &absolute
			release.Season = &season
			release.Episode = &episode
			release.EpisodeRange = []int64{episode}
		}
	}
	return releases
}

// searchArchiveForEpisode runs the archive query for one episode and, for an
// anime series, also the absolute-number query, so releases named
// "Title - 1071" are found in the local archive too.
func searchArchiveForEpisode(ctx context.Context, cfg *Config, archive *Archive, series *SeriesConfig, query string, season, episode int64) ([][3]string, error) {
	items, err := archive.Search(query)
	if err != nil || series == nil || !series.Anime {
		return items, err
	}
	if absolute, ok := animeNumberingFor(ctx, cfg, series).absolute(season, episode); ok {
		if extra, extraErr := archive.Search(fmt.Sprintf("%s %02d", series.Name, absolute)); extraErr == nil {
			items = append(items, extra...)
		}
	}
	return items, nil
}

// parseArchiveRelease parses an archive row, resolving anime numbering for an
// anime series.
func parseArchiveRelease(ctx context.Context, cfg *Config, series *SeriesConfig, item [3]string) *models.Release {
	release := ParseRelease(item[0], item[1], "archive:"+item[2])
	if release == nil || series == nil || !series.Anime {
		return release
	}
	resolved := resolveAnimeReleases(ctx, cfg, []models.Release{*release})
	return &resolved[0]
}
