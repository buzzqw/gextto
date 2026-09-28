package gextto

import (
	"encoding/json"
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
	Archived     bool
	QualityScore int64
	SizeBytes    int64
	IgnorePath   string
	IgnoreBody   string
	ForcePath    string
	Redownload   string
	SearchPath   string
	DeletePath   string
}

// uiSeasonGroup groups the episode rows of one season for the collapsible
// per-season blocks of the series detail.
type uiSeasonGroup struct {
	Season int64
	Rows   []uiEpisodeRow
	Owned  int
	Total  int
}

type uiSeriesDetail struct {
	Name            string
	PathName        string
	Seasons         string
	Quality         string
	Language        string
	ArchivePath     string
	Exclude         string
	Subtitle        string
	TvdbID          string
	Aliases         string
	Enabled         bool
	Episodes        []uiEpisodeRow
	SeasonGroups    []uiSeasonGroup
	Gaps            int
	EpisodeCount    int
	DownloadedCount int
	IgnoredSeasons  []int64
	SeasonButtons   []uiSeasonButton
	StatusLabel     string
}

// uiSeasonButton is one season toggle in the series header.
type uiSeasonButton struct {
	Season  int64
	Ignored bool
}

// uiSeriesStatusLabel mirrors rextto's series status wording.
func uiSeriesStatusLabel(total, downloaded int, ignoredSeasons []int64) string {
	if total == 0 {
		return "Nessun episodio"
	}
	if downloaded >= total {
		return "Completa"
	}
	if downloaded == 0 {
		return "Da scaricare"
	}
	return "In corso"
}

type uiMovieMatch struct {
	Title  string
	Magnet template.URL
	Source string
	Body   string
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
	downloadedCount := 0
	for _, episode := range episodes {
		if episode.ArchivePath != nil && strings.TrimSpace(*episode.ArchivePath) != "" {
			downloadedCount++
		}
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
			Archived:     episode.ArchivePath != nil && strings.TrimSpace(*episode.ArchivePath) != "",
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
	seasonGroups := make([]uiSeasonGroup, 0)
	groupIndex := map[int64]int{}
	maxSeason := int64(0)
	for _, row := range rows {
		position, ok := groupIndex[row.Season]
		if !ok {
			position = len(seasonGroups)
			groupIndex[row.Season] = position
			seasonGroups = append(seasonGroups, uiSeasonGroup{Season: row.Season})
		}
		seasonGroups[position].Rows = append(seasonGroups[position].Rows, row)
		seasonGroups[position].Total++
		if row.Archived {
			seasonGroups[position].Owned++
		}
		if row.Season > maxSeason {
			maxSeason = row.Season
		}
	}
	for _, season := range ignoredSeasons {
		if season > maxSeason {
			maxSeason = season
		}
	}
	seasonButtons := make([]uiSeasonButton, 0, maxSeason)
	for season := int64(1); season <= maxSeason; season++ {
		seasonButtons = append(seasonButtons, uiSeasonButton{Season: season, Ignored: containsInt64(ignoredSeasons, season)})
	}
	return uiSeriesDetail{
		Name:            series.Name,
		PathName:        escaped,
		Seasons:         series.Seasons,
		Quality:         series.Quality,
		Language:        series.Language,
		ArchivePath:     series.ArchivePath,
		Exclude:         series.Exclude,
		Subtitle:        series.Subtitle,
		TvdbID:          series.TvdbID,
		Aliases:         strings.Join(series.Aliases, ", "),
		Enabled:         series.Enabled,
		Episodes:        rows,
		SeasonGroups:    seasonGroups,
		Gaps:            gaps,
		EpisodeCount:    len(rows),
		DownloadedCount: downloadedCount,
		IgnoredSeasons:  ignoredSeasons,
		SeasonButtons:   seasonButtons,
		StatusLabel:     uiSeriesStatusLabel(len(rows), downloadedCount, ignoredSeasons),
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
			body, _ := json.Marshal(map[string]any{
				"items": []map[string]string{{
					"title":  entry[0],
					"magnet": entry[1],
					"source": entry[2],
				}},
			})
			detail.Matches = append(detail.Matches, uiMovieMatch{Title: entry[0], Magnet: uiMagnetURL(entry[1]), Source: entry[2], Body: string(body)})
		}
	}
	return detail, true
}
