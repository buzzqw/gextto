package gextto

// api_v3.go implements the small Sonarr/Radarr v3 subset Jellyseerr needs:
// system status, the generated quality/language profiles, the root folders and
// tags, series/movie lookup and add, and the queue. It is deliberately a
// subset: full *arr compatibility is not a goal (docs/evoluzione.md §8), but
// the operations a request manager uses are real, not stubs.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
)

type v3QualityProfile struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	UpgradeAllowed bool   `json:"upgradeAllowed"`
	Cutoff         int    `json:"cutoff"`
	Items          []any  `json:"items"`
	Quality        string `json:"-"`
	Language       string `json:"-"`
	Subtitle       string `json:"-"`
	Exclude        string `json:"-"`
}

type v3LanguageProfile struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	UpgradeAllowed bool   `json:"upgradeAllowed"`
	Cutoff         int    `json:"cutoff"`
	Languages      []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"languages"`
}

type v3RootFolder struct {
	ID         int    `json:"id"`
	Path       string `json:"path"`
	Accessible bool   `json:"accessible"`
	FreeSpace  int64  `json:"freeSpace"`
}

type v3Tag struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

type v3Series struct {
	ID                int        `json:"id"`
	Title             string     `json:"title"`
	TitleSlug         string     `json:"titleSlug"`
	TvdbID            int64      `json:"tvdbId,omitempty"`
	TmdbID            int64      `json:"tmdbId,omitempty"`
	Year              int        `json:"year,omitempty"`
	Overview          string     `json:"overview,omitempty"`
	RemotePoster      string     `json:"remotePoster,omitempty"`
	RootFolderPath    string     `json:"rootFolderPath"`
	QualityProfileID  int        `json:"qualityProfileId"`
	LanguageProfileID int        `json:"languageProfileId"`
	SeasonFolder      bool       `json:"seasonFolder"`
	Monitored         bool       `json:"monitored"`
	Seasons           []v3Season `json:"seasons,omitempty"`
	Tags              []int      `json:"tags"`
}

type v3Season struct {
	SeasonNumber int  `json:"seasonNumber"`
	Monitored    bool `json:"monitored"`
}

type v3Movie struct {
	ID               int    `json:"id"`
	Title            string `json:"title"`
	TitleSlug        string `json:"titleSlug"`
	TmdbID           int64  `json:"tmdbId,omitempty"`
	Year             int    `json:"year,omitempty"`
	Overview         string `json:"overview,omitempty"`
	RemotePoster     string `json:"remotePoster,omitempty"`
	RootFolderPath   string `json:"rootFolderPath"`
	QualityProfileID int    `json:"qualityProfileId"`
	Monitored        bool   `json:"monitored"`
	Tags             []int  `json:"tags"`
}

type v3QueueItem struct {
	ID                      string  `json:"id"`
	Title                   string  `json:"title"`
	Status                  string  `json:"status"`
	Size                    int64   `json:"size"`
	SizeLeft                int64   `json:"sizeleft"`
	TimeLeft                string  `json:"timeleft,omitempty"`
	EstimatedCompletionTime string  `json:"estimatedCompletionTime,omitempty"`
	Protocol                string  `json:"protocol"`
	DownloadClient          string  `json:"downloadClient,omitempty"`
	Indexer                 string  `json:"indexer,omitempty"`
	Progress                float64 `json:"-"`
}

// v3Profiles derives the named profiles from the per-title overrides: every
// distinct combination becomes one profile, so an existing library is migrated
// without a rewrite and the first profile is always "Default".
func v3Profiles(cfg *Config) []v3QualityProfile {
	defaultLanguage := ""
	if cfg != nil {
		defaultLanguage = cfg.DefaultLanguage()
	}
	seen := map[string]bool{}
	usedNames := map[string]bool{}
	out := []v3QualityProfile{}
	add := func(name, quality, language, subtitle, exclude string) {
		key := strings.Join([]string{quality, language, subtitle, exclude}, "\x00")
		if seen[key] {
			return
		}
		seen[key] = true
		parts := []string{}
		for _, part := range []string{quality, language, subtitle} {
			if strings.TrimSpace(part) != "" {
				parts = append(parts, strings.TrimSpace(part))
			}
		}
		if strings.TrimSpace(name) == "" {
			name = strings.Join(parts, " · ")
		}
		if name == "" {
			name = "Profilo " + strconv.Itoa(len(out)+1)
		}
		for usedNames[strings.ToLower(name)] {
			name += " 2"
		}
		usedNames[strings.ToLower(name)] = true
		out = append(out, v3QualityProfile{
			ID: len(out) + 1, Name: name, Quality: quality, Language: language,
			Subtitle: subtitle, Exclude: exclude, Items: []any{},
		})
	}
	// The named profiles come first, in their saved order; the first one is the
	// default policy. A saved "Default" wins over the generated one.
	stored := qualityProfilesFromConfig(cfg)
	hasDefault := false
	for _, profile := range stored {
		if strings.EqualFold(profile.Name, "Default") {
			hasDefault = true
		}
	}
	if !hasDefault {
		add("Default", "", defaultLanguage, "", "")
	}
	for _, profile := range stored {
		add(profile.Name, profile.Quality, profile.Language, profile.Subtitle, profile.Exclude)
	}
	// Every combination found in the library that has no name yet becomes a
	// profile too: this is the automatic migration from the per-title data.
	if cfg != nil {
		for _, series := range cfg.Series {
			add("", series.Quality, series.Language, series.Subtitle, series.Exclude)
		}
		for _, movie := range cfg.Movies {
			add("", movie.Quality, movie.Language, movie.Subtitle, movie.Exclude)
		}
	}
	return out
}

func v3ProfileByID(cfg *Config, id int) v3QualityProfile {
	profiles := v3Profiles(cfg)
	for _, profile := range profiles {
		if profile.ID == id {
			return profile
		}
	}
	return profiles[0]
}

func v3RootFolders(cfg *Config) []v3RootFolder {
	seen := map[string]bool{}
	var out []v3RootFolder
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		free := int64(0)
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err == nil {
			free = int64(st.Bavail) * int64(st.Bsize)
		}
		out = append(out, v3RootFolder{ID: len(out) + 1, Path: path, Accessible: true, FreeSpace: free})
	}
	if cfg != nil {
		if cfg.ArchiveRoot != nil {
			add(*cfg.ArchiveRoot)
		}
		for _, series := range cfg.Series {
			add(series.ArchivePath)
		}
	}
	return out
}

func v3Tags(cfg *Config) []v3Tag {
	if cfg == nil {
		return []v3Tag{}
	}
	raw := strings.TrimSpace(cfg.Settings["v3_tags"])
	if raw == "" {
		raw = strings.TrimSpace(cfg.Settings["qbittorrent_tag"])
	}
	seen := map[string]bool{}
	out := []v3Tag{}
	for _, label := range strings.Split(raw, ",") {
		label = strings.TrimSpace(label)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, v3Tag{ID: len(out) + 1, Label: label})
	}
	return out
}

func V3SystemStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	v3JSON(w, http.StatusOK, map[string]any{
		"appName":        "Gextto",
		"instanceName":   "Gextto",
		"version":        constants.AppVersion(),
		"buildTime":      constants.BuiltAt,
		"isDebug":        false,
		"isProduction":   true,
		"startupPath":    "",
		"appData":        s.cfg.DataDir,
		"osName":         "linux",
		"osVersion":      "",
		"isNetCore":      false,
		"isLinux":        true,
		"isOsx":          false,
		"isWindows":      false,
		"isDocker":       false,
		"mode":           "console",
		"branch":         "main",
		"authentication": "none",
		"urlBase":        "",
	})
}

func V3QualityProfiles(w http.ResponseWriter, r *http.Request, s *AppState) {
	profiles := ensureQualityProfiles(s)
	out := make([]v3QualityProfile, 0, len(profiles))
	for i, profile := range profiles {
		out = append(out, v3QualityProfile{ID: i + 1, Name: profile.Name, Quality: profile.Quality,
			Language: profile.Language, Subtitle: profile.Subtitle, Exclude: profile.Exclude, Items: []any{}})
	}
	v3JSON(w, http.StatusOK, out)
}

func V3LanguageProfiles(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	name := "Default"
	if cfg != nil && strings.TrimSpace(cfg.DefaultLanguage()) != "" {
		name = strings.Title(strings.TrimSpace(cfg.DefaultLanguage()))
	}
	profile := v3LanguageProfile{ID: 1, Name: name, Cutoff: 1}
	profile.Languages = append(profile.Languages, struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}{ID: 1, Name: name})
	v3JSON(w, http.StatusOK, []v3LanguageProfile{profile})
}

func V3RootFolders(w http.ResponseWriter, r *http.Request, s *AppState) {
	v3JSON(w, http.StatusOK, v3RootFolders(latestConfig(s)))
}

func V3Tags(w http.ResponseWriter, r *http.Request, s *AppState) {
	v3JSON(w, http.StatusOK, v3Tags(latestConfig(s)))
}

func V3SeriesLookup(w http.ResponseWriter, r *http.Request, s *AppState) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		v3JSON(w, http.StatusOK, []v3Series{})
		return
	}
	cfg := latestConfig(s)
	out := []v3Series{}
	// Sonarr addresses a series by TVDB id: use TVDB when it is configured,
	// otherwise fall back to TMDB (whose id is reported as tmdbId only).
	if tvdb := tvdbClientFor(cfg); tvdb.Configured() {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if items, err := tvdb.SearchSeries(ctx, term); err == nil {
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				title := v3Any(item["name"])
				if title == "" {
					continue
				}
				year := 0
				if n, err := strconv.Atoi(v3Any(item["year"])); err == nil {
					year = n
				}
				out = append(out, v3Series{ID: int(v3Int(item["tvdb_id"])), TvdbID: v3Int(item["tvdb_id"]),
					Title: title, TitleSlug: v3Slug(title), Year: year, Overview: v3Any(item["overview"]),
					RemotePoster: v3Any(item["image"]), QualityProfileID: 1, LanguageProfileID: 1, Monitored: true})
			}
		}
		if len(out) > 0 {
			v3JSON(w, http.StatusOK, out)
			return
		}
	}
	client := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	if client.Configured() {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		items, err := client.SearchSeries(ctx, term)
		if err == nil {
			for _, item := range items {
				title := v3ItemName(item)
				out = append(out, v3Series{
					ID: int(item.ID), Title: title, TitleSlug: v3Slug(title),
					TmdbID: item.ID, Year: v3ItemYear(item),
					Overview: v3ItemOverview(item), RemotePoster: v3ItemPoster(item),
					QualityProfileID: 1, LanguageProfileID: 1, Monitored: true,
				})
			}
		}
	}
	v3JSON(w, http.StatusOK, out)
}

func v3Any(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func v3Int(value any) int64 {
	n, _ := strconv.ParseInt(v3Any(value), 10, 64)
	return n
}

func V3SeriesAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	var body struct {
		Title             string     `json:"title"`
		TitleSlug         string     `json:"titleSlug"`
		TvdbID            int64      `json:"tvdbId"`
		TmdbID            int64      `json:"tmdbId"`
		Year              int        `json:"year"`
		RootFolderPath    string     `json:"rootFolderPath"`
		QualityProfileID  int        `json:"qualityProfileId"`
		LanguageProfileID int        `json:"languageProfileId"`
		SeasonFolder      bool       `json:"seasonFolder"`
		Monitored         bool       `json:"monitored"`
		Seasons           []v3Season `json:"seasons"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		v3Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		v3Error(w, http.StatusBadRequest, "title is required")
		return
	}
	cfg := latestConfig(s)
	for _, series := range cfg.Series {
		if strings.EqualFold(series.Name, title) {
			v3Error(w, http.StatusConflict, "series already exists")
			return
		}
	}
	profile := v3ProfileByID(cfg, body.QualityProfileID)
	seasons := "1+"
	if len(body.Seasons) > 0 {
		values := make([]string, 0, len(body.Seasons))
		for _, season := range body.Seasons {
			if season.SeasonNumber >= 0 {
				values = append(values, strconv.Itoa(season.SeasonNumber))
			}
		}
		if len(values) > 0 {
			seasons = strings.Join(values, ",")
		}
	}
	series := SeriesConfig{
		Name: title, Seasons: seasons, Quality: profile.Quality, Language: profile.Language,
		ArchivePath: strings.TrimSpace(body.RootFolderPath), TmdbID: v3ID(body.TmdbID), TvdbID: v3ID(body.TvdbID),
		Subtitle: profile.Subtitle, Exclude: profile.Exclude, Enabled: true,
		SeasonSubfolders: body.SeasonFolder,
	}
	cfg.Series = append(cfg.Series, series)
	if err := SaveLibrary(cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		v3Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	v3JSON(w, http.StatusCreated, v3Series{ID: len(cfg.Series), Title: title, TitleSlug: v3Slug(title),
		TvdbID: body.TvdbID, TmdbID: body.TmdbID, RootFolderPath: series.ArchivePath,
		QualityProfileID: profile.ID, LanguageProfileID: 1, SeasonFolder: body.SeasonFolder,
		Monitored: true, Seasons: body.Seasons})
}

func V3MovieLookup(w http.ResponseWriter, r *http.Request, s *AppState) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		v3JSON(w, http.StatusOK, []v3Movie{})
		return
	}
	cfg := latestConfig(s)
	out := []v3Movie{}
	client := NewTmdbClient(cfg.TmdbAPIKey)
	if client.Configured() {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		items, err := client.SearchMovies(ctx, term)
		if err == nil {
			for _, item := range items {
				title := v3ItemName(item)
				out = append(out, v3Movie{ID: int(item.ID), Title: title, TitleSlug: v3Slug(title),
					TmdbID: item.ID, Year: v3ItemYear(item), Overview: v3ItemOverview(item),
					RemotePoster: v3ItemPoster(item), QualityProfileID: 1, Monitored: true})
			}
		}
	}
	v3JSON(w, http.StatusOK, out)
}

func V3MovieAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	var body struct {
		Title            string `json:"title"`
		TitleSlug        string `json:"titleSlug"`
		TmdbID           int64  `json:"tmdbId"`
		Year             int    `json:"year"`
		RootFolderPath   string `json:"rootFolderPath"`
		QualityProfileID int    `json:"qualityProfileId"`
		Monitored        bool   `json:"monitored"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		v3Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		v3Error(w, http.StatusBadRequest, "title is required")
		return
	}
	cfg := latestConfig(s)
	for _, movie := range cfg.Movies {
		if strings.EqualFold(movie.Name, title) {
			v3Error(w, http.StatusConflict, "movie already exists")
			return
		}
	}
	profile := v3ProfileByID(cfg, body.QualityProfileID)
	year := ""
	if body.Year > 0 {
		year = strconv.Itoa(body.Year)
	}
	movie := MovieConfig{Name: title, Year: year, TmdbID: v3ID(body.TmdbID),
		Quality: profile.Quality, Language: profile.Language, Subtitle: profile.Subtitle,
		Exclude: profile.Exclude, Enabled: true}
	cfg.Movies = append(cfg.Movies, movie)
	if err := SaveLibrary(cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		v3Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	v3JSON(w, http.StatusCreated, v3Movie{ID: int(movie.ID), Title: title, TitleSlug: v3Slug(title),
		TmdbID: body.TmdbID, Year: body.Year, RootFolderPath: body.RootFolderPath,
		QualityProfileID: profile.ID, Monitored: true})
}

func V3Queue(w http.ResponseWriter, r *http.Request, s *AppState) {
	items := []v3QueueItem{}
	if engine := s.activeEngine(); engine != nil {
		for _, torrent := range engine.List() {
			size := torrent.TotalSize
			left := size - torrent.TotalDone
			status := "downloading"
			if torrent.Progress >= 100 {
				status = "completed"
			} else if strings.EqualFold(torrent.State, "paused") {
				status = "paused"
			}
			items = append(items, v3QueueItem{
				ID: torrent.Hash, Title: torrent.Name, Status: status, Size: size, SizeLeft: left,
				Protocol: "torrent", DownloadClient: "gx-torrent", Progress: torrent.Progress,
			})
		}
	}
	v3JSON(w, http.StatusOK, map[string]any{"page": 1, "pageSize": len(items), "totalRecords": len(items), "records": items})
}

func v3JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func v3Error(w http.ResponseWriter, status int, message string) {
	v3JSON(w, status, []map[string]string{{"errorMessage": message}})
}

func v3ItemName(item TmdbItem) string {
	if item.Name != nil && strings.TrimSpace(*item.Name) != "" {
		return strings.TrimSpace(*item.Name)
	}
	if item.Title != nil {
		return strings.TrimSpace(*item.Title)
	}
	return ""
}

func v3ItemYear(item TmdbItem) int {
	date := ""
	if item.FirstAirDate != nil {
		date = *item.FirstAirDate
	} else if item.ReleaseDate != nil {
		date = *item.ReleaseDate
	}
	if len(date) >= 4 {
		if year, err := strconv.Atoi(date[:4]); err == nil {
			return year
		}
	}
	return 0
}

func v3ItemOverview(item TmdbItem) string {
	if item.Overview != nil {
		return strings.TrimSpace(*item.Overview)
	}
	return ""
}

func v3ItemPoster(item TmdbItem) string {
	if item.PosterPath == nil || strings.TrimSpace(*item.PosterPath) == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w500" + *item.PosterPath
}

func v3Slug(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ".", "-", "'", "").Replace(slug)
	return strings.Trim(slug, "-")
}

func v3ID(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// v3FreeSpace is shared with tests: the free bytes of a folder, 0 when it does
// not exist.
func v3FreeSpace(path string) int64 {
	for _, folder := range v3RootFolders(&Config{ArchiveRoot: &path}) {
		return folder.FreeSpace
	}
	return 0
}
