package gextto

// series_metadata.go chooses where the series metadata comes from: TMDB, the
// recommended source, when its key is set, otherwise TVDB. Both clients
// answer the same calls with the same shapes (tvdb_metadata.go), so season
// sizes, missing episodes, anime numbering, status, calendar, episode titles
// and the series details work with either key.

import (
	"context"
	"strings"
)

// seriesMetadata is one metadata provider for series.
type seriesMetadata interface {
	// Source is "tmdb" or "tvdb".
	Source() string
	Configured() bool
	// StoredID is the id of this provider saved with the series ("" if none).
	StoredID(series *SeriesConfig) string
	ResolveSeriesID(ctx context.Context, name string) (*string, error)
	SeasonCounts(ctx context.Context, id string) (map[int64]int64, error)
	SeasonEpisodes(ctx context.Context, id string, season int64) ([]TmdbEpisode, error)
	NextEpisode(ctx context.Context, id string) (*TmdbEpisode, error)
	EpisodeTitle(ctx context.Context, id string, season, episode int64) (*string, error)
	// SeriesInfo returns the details in the shape of the TMDB /tv record.
	SeriesInfo(ctx context.Context, name string, id *string) (map[string]any, error)
	// PosterForSeries returns a TMDB path or a full URL: see metadataImageURL.
	PosterForSeries(ctx context.Context, name string) (*string, error)
}

type tmdbSeriesMetadata struct{ *TmdbClient }

func (tmdbSeriesMetadata) Source() string { return "tmdb" }

func (m tmdbSeriesMetadata) Configured() bool { return m.TmdbClient.Configured() }

func (tmdbSeriesMetadata) StoredID(series *SeriesConfig) string {
	if series == nil {
		return ""
	}
	return strings.TrimSpace(series.TmdbID)
}

type tvdbSeriesMetadata struct{ *TvdbClient }

func (tvdbSeriesMetadata) Source() string { return "tvdb" }

func (tvdbSeriesMetadata) StoredID(series *SeriesConfig) string {
	if series == nil {
		return ""
	}
	return strings.TrimSpace(series.TvdbID)
}

// seriesMetadataFor returns the series metadata provider of cfg: TMDB when
// its key is set, else TVDB when its key is set, else a TMDB provider that is
// not configured (every call answers "nothing known").
func seriesMetadataFor(cfg *Config) seriesMetadata {
	if cfg == nil {
		return tmdbSeriesMetadata{NewTmdbClient(nil)}
	}
	return seriesMetadataWith(cfg, NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage()))
}

// seriesMetadataWith is seriesMetadataFor reusing an existing TMDB client
// (and its cache) when TMDB is the provider.
func seriesMetadataWith(cfg *Config, tmdb *TmdbClient) seriesMetadata {
	if tmdb != nil && tmdb.Configured() {
		return tmdbSeriesMetadata{tmdb}
	}
	if tvdb := tvdbClientFor(cfg); tvdb.Configured() {
		return tvdbSeriesMetadata{tvdb}
	}
	if tmdb == nil {
		tmdb = NewTmdbClient(nil)
	}
	return tmdbSeriesMetadata{tmdb}
}

// metadataSeriesID returns the provider id of a series: the stored one, else
// the one resolved by name (nil when the series is not found).
func metadataSeriesID(ctx context.Context, meta seriesMetadata, series *SeriesConfig) (*string, error) {
	if id := meta.StoredID(series); id != "" {
		return &id, nil
	}
	return meta.ResolveSeriesID(ctx, series.Name)
}

// metadataConfigured reports whether a series metadata provider (TMDB or
// TVDB) is configured.
func metadataConfigured(cfg *Config) bool {
	return seriesMetadataFor(cfg).Configured()
}

// metadataImageURL turns a poster into a URL: TVDB already returns full URLs,
// TMDB returns a path to prefix with the image base and size ("w154", ...).
func metadataImageURL(path, size string) string {
	path = strings.TrimSpace(path)
	if path == "" || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return TmdbImageBaseURL + "/" + size + path
}
