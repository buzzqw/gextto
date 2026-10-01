package gextto

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
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
	SourcesPath  string
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
	Poster          string
	Overview        string
	Year            string
	Network         string
	Country         string
	Vote            string
	LastAirDate     string
	NextEpisode     string
	Genres          []string
	Cast            []uiDetailPerson
	TmdbURL         string
	TvdbURL         string
}

type uiDetailPerson struct {
	Name      string
	Character string
	URL       string
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
	Title       string
	Magnet      template.URL
	Source      string
	Body        string
	ExplainBody string
}

// uiSourceLabel turns long feed URLs into a compact provider label while the
// raw value remains available in the title/JSON action payload.
func uiSourceLabel(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	candidate := value
	parseValue := value
	if !strings.Contains(parseValue, "://") && strings.Contains(parseValue, ".") {
		parseValue = "https://" + parseValue
	}
	if parsed, err := url.Parse(parseValue); err == nil && parsed.Hostname() != "" {
		labels := strings.Split(strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www."), ".")
		if len(labels) >= 2 {
			index := len(labels) - 2
			if len(labels) >= 3 {
				suffix := labels[len(labels)-2] + "." + labels[len(labels)-1]
				if suffix == "co.uk" || suffix == "com.au" || suffix == "co.nz" || suffix == "com.br" || suffix == "co.jp" || suffix == "co.za" {
					index = len(labels) - 3
				}
			}
			if index >= 0 {
				candidate = labels[index]
			}
		}
	}
	return strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(candidate))
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
	Poster               string
	ReleaseDate          string
	Cast                 []uiDetailPerson
	TmdbURL              string
	TvdbURL              string
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
			SourcesPath:  base + "/sources",
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
	detail := uiSeriesDetail{
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
		TmdbURL:         tmdbURL(series.TmdbID, "tv"),
		TvdbURL:         tvdbURL(series.TvdbID, "series"),
	}
	// The v2 interface is served both at / and at /v2. Load the metadata for
	// both paths so the official root UI also renders poster, genres and cast.
	uiSeriesMetadataFrom(s, series.Name, &detail)
	return detail, true
}

func tmdbURL(id, kind string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	return "https://www.themoviedb.org/" + kind + "/" + url.PathEscape(strings.TrimSpace(id))
}

func tvdbURL(id, kind string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	return "https://thetvdb.com/dereferrer/" + kind + "/" + url.PathEscape(strings.TrimSpace(id))
}

func uiSeriesMetadataFrom(s *AppState, name string, detail *uiSeriesDetail) {
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/series/"+url.PathEscape(name)+"/info", nil, nil)
	if status >= 400 {
		return
	}
	var payload struct {
		Info map[string]any `json:"info"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Info == nil {
		return
	}
	info := payload.Info
	detail.Poster = v2AnyString(info["poster"])
	detail.Overview = v2AnyString(info["overview"])
	detail.Year = v2AnyString(info["year"])
	detail.Network = v2AnyString(info["network"])
	detail.Country = v2AnyString(info["country"])
	if vote := v2Float(info["vote"]); vote > 0 {
		detail.Vote = fmt.Sprintf("%.1f", vote)
	}
	detail.LastAirDate = v2AnyString(info["last_air_date"])
	if next, ok := info["next_episode"].(map[string]any); ok && next["name"] != nil {
		detail.NextEpisode = "S" + v2AnyString(next["season_number"]) + "E" + v2AnyString(next["episode_number"]) + " · " + v2AnyString(next["air_date"])
	}
	if detail.TmdbURL == "" {
		detail.TmdbURL = tmdbURL(v2AnyString(info["tmdb_id"]), "tv")
	}
	if detail.TvdbURL == "" {
		detail.TvdbURL = v2AnyString(info["tvdb_url"])
	}
	if genres, ok := info["genres"].([]any); ok {
		for _, genre := range genres {
			if text := v2AnyString(genre); text != "" {
				detail.Genres = append(detail.Genres, text)
			}
		}
	}
	if cast, ok := info["cast"].([]any); ok {
		for _, value := range cast {
			if item, ok := value.(map[string]any); ok {
				personURL := v2AnyString(item["url"])
				if personURL == "" {
					if personID := int64(v2Float(item["id"])); personID > 0 {
						personURL = fmt.Sprintf("https://www.themoviedb.org/person/%d", personID)
					}
				}
				detail.Cast = append(detail.Cast, uiDetailPerson{Name: v2AnyString(item["name"]), Character: v2AnyString(item["character"]), URL: personURL})
			}
		}
	}
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
		ID:       movie.ID,
		Name:     movie.Name,
		Year:     movie.Year,
		Quality:  movie.Quality,
		Language: movie.Language,
		TmdbID:   movie.TmdbID,
		TvdbID:   movie.TvdbID,
		Overview: movie.Overview,
		Subtitle: movie.Subtitle,
		Exclude:  movie.Exclude,
		// The persisted form is canonical JSON for the API/database. The edit
		// form accepts the human-readable comma-separated representation.
		LanguageRequirements: strings.Join(parseLanguageRequirements(movie.LanguageRequirements), ","),
		SubtitleRequirements: movie.SubtitleRequirements,
		Enabled:              movie.Enabled,
		Poster:               movie.PosterPath,
		TmdbURL:              tmdbURL(movie.TmdbID, "movie"),
		TvdbURL:              tvdbURL(movie.TvdbID, "movie"),
	}
	if raw, status := v2InternalJSON(s, http.MethodGet, "/api/movies/"+strconv.FormatInt(movie.ID, 10), nil, nil); status < 400 {
		var payload struct {
			Metadata map[string]any   `json:"metadata"`
			Cast     []map[string]any `json:"cast"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			detail.Overview = v2AnyString(payload.Metadata["overview"])
			detail.Poster = v2AnyString(payload.Metadata["poster_path"])
			if detail.Poster != "" && !strings.HasPrefix(detail.Poster, "http") {
				detail.Poster = "https://image.tmdb.org/t/p/w300" + detail.Poster
			}
			detail.ReleaseDate = v2AnyString(payload.Metadata["release_date"])
			for _, person := range payload.Cast {
				personURL := v2AnyString(person["url"])
				if personURL == "" {
					if personID := int64(v2Float(person["id"])); personID > 0 {
						personURL = fmt.Sprintf("https://www.themoviedb.org/person/%d", personID)
					}
				}
				detail.Cast = append(detail.Cast, uiDetailPerson{Name: v2AnyString(person["name"]), Character: v2AnyString(person["character"]), URL: personURL})
			}
		}
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
			release := ParseRelease(entry[0], entry[1], "archive:"+entry[2])
			if release == nil {
				release = &models.Release{Title: entry[0], Magnet: entry[1], Source: entry[2]}
			}
			explainBody, _ := json.Marshal(release)
			detail.Matches = append(detail.Matches, uiMovieMatch{Title: entry[0], Magnet: uiMagnetURL(entry[1]), Source: entry[2], Body: string(body), ExplainBody: string(explainBody)})
		}
	}
	return detail, true
}
