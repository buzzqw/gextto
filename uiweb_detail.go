package gextto

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

// uiweb_detail.go builds the series and movie detail pages server-side, reusing
// the same data as the JSON API (SeriesDetail / MovieDetail) so nothing differs.

type uiEpisodeRow struct {
	Series       string
	Season       int64
	Episode      int64
	Title        string
	Status       string
	AirDate      string
	Ignored      bool
	QualityScore int64
	SizeBytes    int64
	IgnorePath   string
	IgnoreBody   string
	ForcePath    string
	Redownload   string
	SearchPath   string
	DeletePath   string
}

type uiSeriesDetail struct {
	Name        string
	PathName    string
	Seasons     string
	Quality     string
	Language    string
	ArchivePath string
	Exclude     string
	Enabled     bool
	Episodes    []uiEpisodeRow
	Gaps        int
}

type uiMovieMatch struct {
	Title  string
	Magnet template.URL
	Source string
}

type uiMovieDetail struct {
	ID                   int64
	Name                 string
	Year                 string
	Quality              string
	Language             string
	TmdbID               string
	TvdbID               string
	Overview             string
	Subtitle             string
	Exclude              string
	LanguageRequirements string
	SubtitleRequirements string
	Enabled              bool
	History              []MovieHistory
	Matches              []uiMovieMatch
}

// uiMagnetURL only trusts links with a safe scheme so the archive match links
// keep working (html/template would otherwise rewrite unknown schemes to
// "#ZgotmplZ") without allowing javascript: injection.
func uiMagnetURL(value string) template.URL {
	lowered := strings.ToLower(strings.TrimSpace(value))
	for _, scheme := range []string{"magnet:", "http://", "https://"} {
		if strings.HasPrefix(lowered, scheme) {
			return template.URL(value)
		}
	}
	return ""
}

// uiSeriesDetailFrom returns the series detail when the `series` query selects a
// known series.
func uiSeriesDetailFrom(s *AppState, r *http.Request) (uiSeriesDetail, bool) {
	cfg := latestConfig(s)
	name := r.URL.Query().Get("series")
	series := gh3FindSeries(cfg, name)
	if series == nil {
		return uiSeriesDetail{}, false
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
		logging.Warn("new UI: cannot list episodes", "series", series.Name, "error", err.Error())
	}
	escaped := url.PathEscape(series.Name)
	rows := make([]uiEpisodeRow, 0, len(episodes))
	for _, episode := range episodes {
		title := episode.RenamedTitle
		if title == "" {
			title = episode.Title
		}
		ignoreValue := "true"
		if !episode.Ignored {
			ignoreValue = "false"
		}
		base := "/api/episodes/" + escaped + "/" + strconv.FormatInt(episode.Season, 10) + "/" + strconv.FormatInt(episode.Episode, 10)
		rows = append(rows, uiEpisodeRow{
			Series:       series.Name,
			Season:       episode.Season,
			Episode:      episode.Episode,
			Title:        title,
			Status:       episode.Status,
			AirDate:      episode.AirDate,
			Ignored:      episode.Ignored,
			QualityScore: episode.QualityScore,
			SizeBytes:    episode.SizeBytes,
			IgnorePath:   base + "/ignore",
			IgnoreBody:   `{"ignored":` + ignoreValue + `}`,
			ForcePath:    base + "/force",
			Redownload:   base + "/redownload",
			SearchPath:   base + "/search",
			DeletePath:   base,
		})
	}
	gaps := 0
	if archiveGaps, err := s.db.ArchiveGapsForSeries(series.Name); err == nil {
		for _, gap := range archiveGaps {
			season := gap[0]
			if containsInt64(ignoredSeasons, season) || !SeasonAllowedForScan(series.Seasons, season) {
				continue
			}
			gaps++
		}
	}
	return uiSeriesDetail{
		Name:        series.Name,
		PathName:    escaped,
		Seasons:     series.Seasons,
		Quality:     series.Quality,
		Language:    series.Language,
		ArchivePath: series.ArchivePath,
		Exclude:     series.Exclude,
		Enabled:     series.Enabled,
		Episodes:    rows,
		Gaps:        gaps,
	}, true
}

// uiMovieDetailFrom returns the movie detail when the `movie` query selects a
// known movie.
func uiMovieDetailFrom(s *AppState, r *http.Request) (uiMovieDetail, bool) {
	cfg := latestConfig(s)
	id, err := strconv.ParseInt(r.URL.Query().Get("movie"), 10, 64)
	if err != nil {
		return uiMovieDetail{}, false
	}
	var movie *MovieConfig
	for index := range cfg.Movies {
		if cfg.Movies[index].ID == id {
			movie = &cfg.Movies[index]
			break
		}
	}
	if movie == nil {
		return uiMovieDetail{}, false
	}
	detail := uiMovieDetail{
		ID:                   movie.ID,
		Name:                 movie.Name,
		Year:                 movie.Year,
		Quality:              movie.Quality,
		Language:             movie.Language,
		TmdbID:               movie.TmdbID,
		TvdbID:               movie.TvdbID,
		Overview:             movie.Overview,
		Subtitle:             movie.Subtitle,
		Exclude:              movie.Exclude,
		LanguageRequirements: movie.LanguageRequirements,
		SubtitleRequirements: movie.SubtitleRequirements,
		Enabled:              movie.Enabled,
	}
	if all, err := s.db.DownloadedMovies(200); err == nil {
		for _, item := range all {
			if item.Name == movie.Name {
				detail.History = append(detail.History, item)
			}
		}
	}
	query := movie.Name
	if len(movie.Year) > 0 {
		query = movie.Name + " " + movie.Year
	}
	if entries, err := s.archive.Search(query); err == nil {
		for index, entry := range entries {
			if index >= 20 {
				break
			}
			detail.Matches = append(detail.Matches, uiMovieMatch{Title: entry[0], Magnet: uiMagnetURL(entry[1]), Source: entry[2]})
		}
	}
	return detail, true
}
