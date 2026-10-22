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
	"math"
	"net/http"
	"sort"
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
	Audio          string `json:"-"`
	Language       string `json:"-"`
	Subtitle       string `json:"-"`
	Exclude        string `json:"-"`
	// DisableUpgrades is «Permetti upgrade: No». UpgradeAllowed, which the v3
	// clients read, is its opposite.
	DisableUpgrades bool `json:"-"`
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
	ID                int    `json:"id"`
	Title             string `json:"title"`
	TitleSlug         string `json:"titleSlug"`
	TvdbID            int64  `json:"tvdbId,omitempty"`
	TmdbID            int64  `json:"tmdbId,omitempty"`
	Year              int    `json:"year,omitempty"`
	Overview          string `json:"overview,omitempty"`
	RemotePoster      string `json:"remotePoster,omitempty"`
	RootFolderPath    string `json:"rootFolderPath"`
	QualityProfileID  int    `json:"qualityProfileId"`
	LanguageProfileID int    `json:"languageProfileId"`
	SeasonFolder      bool   `json:"seasonFolder"`
	Monitored         bool   `json:"monitored"`
	// Seasons is always present: Jellyseerr maps over the lookup result's
	// seasons to build the add request.
	Seasons []v3Season `json:"seasons"`
	Tags    []int      `json:"tags"`
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
	HasFile          bool   `json:"hasFile"`
	IsAvailable      bool   `json:"isAvailable"`
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
// distinct combination of quality, minimum audio, language, subtitles and
// upgrade switch becomes one profile, so an existing library is migrated
// without a rewrite and the first profile is always "Default". Exclusions are
// the title's own and never make a profile. It seeds the saved list once
// (ensureQualityProfiles); afterwards the saved list, with its stable ids, is
// the only source.
func v3Profiles(cfg *Config) []v3QualityProfile {
	defaultLanguage := ""
	if cfg != nil {
		defaultLanguage = cfg.DefaultLanguage()
	}
	seen := map[string]bool{}
	usedNames := map[string]bool{}
	out := []v3QualityProfile{}
	add := func(name, quality, audio, language, subtitle, exclude string, disableUpgrades bool) {
		// "any" sets no resolution limit, exactly like an empty quality.
		if strings.EqualFold(strings.TrimSpace(quality), "any") {
			quality = ""
		}
		audio = normalizeAudioRequirement(audio)
		key := strings.Join([]string{quality, audio, language, subtitle, exclude,
			strconv.FormatBool(disableUpgrades)}, "\x00")
		if seen[key] {
			return
		}
		seen[key] = true
		parts := []string{}
		for _, part := range []string{quality, audio, language, subtitle} {
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
			ID: len(out) + 1, Name: name, Quality: quality, Audio: audio, Language: language,
			Subtitle: subtitle, Exclude: exclude, DisableUpgrades: disableUpgrades,
			Items: []any{},
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
		add("Default", "", "", defaultLanguage, "", "", false)
	}
	for _, profile := range stored {
		add(profile.Name, profile.Quality, profile.Audio, profile.Language, profile.Subtitle,
			profile.Exclude, profile.DisableUpgrades)
	}
	// Every combination found in the library that has no name yet becomes a
	// profile too: this is the automatic migration from the per-title data.
	// A title's exclusions are its own (another film with the same name, a
	// spin-off): they never make a profile of their own.
	if cfg != nil {
		for _, series := range cfg.Series {
			add("", series.Quality, series.Audio, qualityProfileLanguage(cfg, series.Language),
				series.Subtitle, "", series.DisableUpgrades)
		}
		for _, movie := range cfg.Movies {
			add("", movie.Quality, movie.Audio, qualityProfileLanguage(cfg, movie.Language),
				movie.Subtitle, "", movie.DisableUpgrades)
		}
	}
	return out
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
	for _, profile := range profiles {
		out = append(out, v3QualityProfile{ID: profile.ID, Name: profile.Name, Quality: profile.Quality,
			Audio: profile.Audio, Language: profile.Language, Subtitle: profile.Subtitle,
			Exclude: profile.Exclude, DisableUpgrades: profile.DisableUpgrades,
			UpgradeAllowed: !profile.DisableUpgrades, Items: []any{}})
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
	if id, ok := v3TermID(term, "tvdb"); ok {
		v3JSON(w, http.StatusOK, []v3Series{v3SeriesByTvdbID(r.Context(), cfg, id)})
		return
	}
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
				out = append(out, v3Series{ID: v3IntID(item["tvdb_id"]), TvdbID: v3Int(item["tvdb_id"]),
					Title: title, TitleSlug: v3Slug(title), Year: year, Overview: v3Any(item["overview"]),
					RemotePoster: v3Any(item["image"]), QualityProfileID: 1, LanguageProfileID: 1, Monitored: true,
					Seasons: []v3Season{}, Tags: []int{}})
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
					Seasons: []v3Season{}, Tags: []int{},
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

// v3IntID converts an external id to an int without truncating it: an id that
// does not fit is dropped rather than wrapped into a negative or wrong one.
func v3IntID(value any) int {
	n := v3Int(value)
	if n <= 0 || n > 1<<31-1 {
		return 0
	}
	return int(n)
}

// v3TermID reads the "tvdb:123"/"tmdb:123" form of a lookup term, which is
// how Jellyseerr looks up the title it is about to add.
func v3TermID(term, source string) (int64, bool) {
	prefix, value, found := strings.Cut(term, ":")
	if !found || !strings.EqualFold(strings.TrimSpace(prefix), source) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return id, err == nil && id > 0
}

// v3SeriesByTvdbID answers a "tvdb:" lookup. A configured series is returned
// with its id, so Jellyseerr updates it instead of adding a duplicate; an
// unknown one is described from TVDB, or TMDB, with its season list, which
// Jellyseerr needs to say which seasons are requested. Without metadata the
// record carries only the id and Jellyseerr's own title is used on add.
func v3SeriesByTvdbID(ctx context.Context, cfg *Config, tvdbID int64) v3Series {
	out := v3Series{TvdbID: tvdbID, QualityProfileID: 1, LanguageProfileID: 1, Seasons: []v3Season{}, Tags: []int{}}
	var seasons []int64
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if tvdb := tvdbClientFor(cfg); tvdb.Configured() {
		if data, err := tvdb.SeriesExtended(ctx, tvdbID); err == nil {
			out.Title = v3Any(v3Field(data, "name"))
			out.Overview = v3Any(v3Field(data, "overview"))
			out.RemotePoster = v3Any(v3Field(data, "image"))
			if year, err := strconv.Atoi(v3Any(v3Field(data, "year"))); err == nil {
				out.Year = year
			}
			seasons = v3TvdbSeasons(v3Field(data, "seasons"))
		}
	}
	if out.Title == "" {
		client := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
		if client.Configured() {
			if item, err := client.FindSeriesByTvdbID(ctx, tvdbID); err == nil && item != nil {
				out.Title, out.TmdbID, out.Year = v3ItemName(*item), item.ID, v3ItemYear(*item)
				out.Overview, out.RemotePoster = v3ItemOverview(*item), v3ItemPoster(*item)
				if counts, err := client.SeasonCounts(ctx, strconv.FormatInt(item.ID, 10)); err == nil {
					seasons = seasons[:0]
					for season := range counts {
						seasons = append(seasons, season)
					}
				}
			}
		}
	}
	index := v3SeriesIndex(cfg, tvdbID, out.Title)
	if index >= 0 {
		series := cfg.Series[index]
		out.ID, out.Title, out.Monitored = index+1, series.Name, series.Enabled
		out.RootFolderPath, out.SeasonFolder = series.ArchivePath, series.SeasonSubfolders
	}
	sort.Slice(seasons, func(i, j int) bool { return seasons[i] < seasons[j] })
	for _, season := range seasons {
		if season < 0 || season > math.MaxInt32 {
			continue
		}
		monitored := false
		if index >= 0 {
			monitored = seasonAllowed(cfg.Series[index].Seasons, season) &&
				!containsInt64(cfg.Series[index].IgnoredSeasons, season)
		}
		out.Seasons = append(out.Seasons, v3Season{SeasonNumber: int(season), Monitored: monitored})
	}
	out.TitleSlug = v3Slug(out.Title)
	return out
}

// v3Field reads one key of a decoded JSON object.
func v3Field(value any, key string) any {
	if object, ok := value.(map[string]any); ok {
		return object[key]
	}
	return nil
}

// v3TvdbSeasons lists the season numbers of the official (aired) order of a
// TVDB extended record.
func v3TvdbSeasons(value any) []int64 {
	items, _ := value.([]any)
	seen := map[int64]bool{}
	var out []int64
	for _, item := range items {
		kind := strings.ToLower(v3Any(v3Field(v3Field(item, "type"), "type")))
		if kind != "" && kind != "official" {
			continue
		}
		raw := v3Any(v3Field(item, "number"))
		number, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || seen[number] {
			continue
		}
		seen[number] = true
		out = append(out, number)
	}
	return out
}

// v3SeriesIndex finds a configured series by TVDB id, then by name.
func v3SeriesIndex(cfg *Config, tvdbID int64, title string) int {
	if cfg == nil {
		return -1
	}
	if tvdbID > 0 {
		id := strconv.FormatInt(tvdbID, 10)
		for i, series := range cfg.Series {
			if strings.TrimSpace(series.TvdbID) == id {
				return i
			}
		}
	}
	title = strings.TrimSpace(title)
	for i, series := range cfg.Series {
		if title != "" && strings.EqualFold(series.Name, title) {
			return i
		}
	}
	return -1
}

// v3SeasonSpec is the seasons field for the monitored seasons of a request.
// Jellyseerr sends every season and marks the requested ones as monitored;
// with no season marked the whole series is followed from season 1.
func v3SeasonSpec(seasons []v3Season) string {
	values := []string{}
	for _, season := range seasons {
		if season.Monitored && season.SeasonNumber >= 0 {
			values = append(values, strconv.Itoa(season.SeasonNumber))
		}
	}
	if len(values) == 0 {
		return "1+"
	}
	return strings.Join(values, ",")
}

type v3SeriesBody struct {
	ID                int        `json:"id"`
	Title             string     `json:"title"`
	TvdbID            int64      `json:"tvdbId"`
	TmdbID            int64      `json:"tmdbId"`
	RootFolderPath    string     `json:"rootFolderPath"`
	QualityProfileID  int        `json:"qualityProfileId"`
	LanguageProfileID int        `json:"languageProfileId"`
	SeasonFolder      bool       `json:"seasonFolder"`
	Monitored         bool       `json:"monitored"`
	Seasons           []v3Season `json:"seasons"`
	Tags              []int      `json:"tags"`
}

func v3DecodeSeries(w http.ResponseWriter, r *http.Request) (v3SeriesBody, bool) {
	var body v3SeriesBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		v3Error(w, http.StatusBadRequest, "invalid JSON body")
		return body, false
	}
	return body, true
}

func V3SeriesAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	body, ok := v3DecodeSeries(w, r)
	if !ok {
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		v3Error(w, http.StatusBadRequest, "title is required")
		return
	}
	cfg := latestConfig(s)
	if v3SeriesIndex(cfg, body.TvdbID, title) >= 0 {
		v3Error(w, http.StatusConflict, "series already exists")
		return
	}
	profile := qualityProfileByID(ensureQualityProfiles(s), body.QualityProfileID)
	added := SeriesConfig{
		Name: title, Seasons: v3SeasonSpec(body.Seasons), Quality: profile.Quality, Language: profile.Language,
		ArchivePath: strings.TrimSpace(body.RootFolderPath), TmdbID: v3ID(body.TmdbID), TvdbID: v3ID(body.TvdbID),
		Subtitle: profile.Subtitle, Exclude: profile.Exclude, Enabled: true,
		SeasonSubfolders: body.SeasonFolder, Audio: profile.Audio, DisableUpgrades: profile.DisableUpgrades,
	}
	// The cached configuration is shared: build a new slice, never append to it.
	series := append(append([]SeriesConfig{}, cfg.Series...), added)
	if err := SaveLibrary(cfg.DataDir, series, cfg.Movies); err != nil {
		v3Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	v3JSON(w, http.StatusCreated, v3Series{ID: len(series), Title: title, TitleSlug: v3Slug(title),
		TvdbID: body.TvdbID, TmdbID: body.TmdbID, RootFolderPath: added.ArchivePath,
		QualityProfileID: profile.ID, LanguageProfileID: 1, SeasonFolder: body.SeasonFolder,
		Monitored: true, Seasons: v3NonNilSeasons(body.Seasons), Tags: []int{}})
}

// V3SeriesUpdate is PUT /api/v3/series: Jellyseerr sends it when a request
// adds seasons to a series that is already in the library. The newly
// monitored seasons are added to the series' season list; nothing is removed.
func V3SeriesUpdate(w http.ResponseWriter, r *http.Request, s *AppState) {
	body, ok := v3DecodeSeries(w, r)
	if !ok {
		return
	}
	cfg := latestConfig(s)
	index := v3SeriesIndex(cfg, body.TvdbID, body.Title)
	if index < 0 && body.ID > 0 && body.ID <= len(cfg.Series) {
		index = body.ID - 1
	}
	if index < 0 {
		v3Error(w, http.StatusNotFound, "series not found")
		return
	}
	series := append([]SeriesConfig{}, cfg.Series...)
	updated := series[index]
	for _, season := range body.Seasons {
		number := int64(season.SeasonNumber)
		if !season.Monitored || number < 0 {
			continue
		}
		if !seasonAllowed(updated.Seasons, number) {
			if strings.TrimSpace(updated.Seasons) == "" {
				updated.Seasons = strconv.FormatInt(number, 10)
			} else {
				updated.Seasons += "," + strconv.FormatInt(number, 10)
			}
		}
		ignored := updated.IgnoredSeasons[:0:0]
		for _, value := range updated.IgnoredSeasons {
			if value != number {
				ignored = append(ignored, value)
			}
		}
		updated.IgnoredSeasons = ignored
	}
	if body.Monitored {
		updated.Enabled = true
	}
	series[index] = updated
	if err := SaveLibrary(cfg.DataDir, series, cfg.Movies); err != nil {
		v3Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	v3JSON(w, http.StatusAccepted, v3Series{ID: index + 1, Title: updated.Name, TitleSlug: v3Slug(updated.Name),
		TvdbID: body.TvdbID, TmdbID: body.TmdbID, RootFolderPath: updated.ArchivePath,
		QualityProfileID: body.QualityProfileID, LanguageProfileID: 1, SeasonFolder: updated.SeasonSubfolders,
		Monitored: updated.Enabled, Seasons: v3NonNilSeasons(body.Seasons), Tags: []int{}})
}

func v3NonNilSeasons(seasons []v3Season) []v3Season {
	if seasons == nil {
		return []v3Season{}
	}
	return seasons
}

func V3MovieLookup(w http.ResponseWriter, r *http.Request, s *AppState) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		v3JSON(w, http.StatusOK, []v3Movie{})
		return
	}
	cfg := latestConfig(s)
	if id, ok := v3TermID(term, "tmdb"); ok {
		v3JSON(w, http.StatusOK, []v3Movie{v3MovieByTmdbID(r.Context(), s, cfg, id)})
		return
	}
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
					RemotePoster: v3ItemPoster(item), QualityProfileID: 1, Monitored: true, Tags: []int{}})
			}
		}
	}
	v3JSON(w, http.StatusOK, out)
}

// v3MovieIndex finds a configured movie by TMDB id, then by title and year.
func v3MovieIndex(cfg *Config, tmdbID int64, title string, year int) int {
	if cfg == nil {
		return -1
	}
	if tmdbID > 0 {
		id := strconv.FormatInt(tmdbID, 10)
		for i, movie := range cfg.Movies {
			if strings.TrimSpace(movie.TmdbID) == id {
				return i
			}
		}
	}
	title = strings.TrimSpace(title)
	for i, movie := range cfg.Movies {
		if title == "" || !strings.EqualFold(movie.Name, title) {
			continue
		}
		if year > 0 && strings.TrimSpace(movie.Year) != "" && movie.Year != strconv.Itoa(year) {
			continue
		}
		return i
	}
	return -1
}

// v3MovieRecord is the v3 view of a configured movie: its library id, whether
// it is monitored and whether it is already archived.
func v3MovieRecord(s *AppState, movie MovieConfig) v3Movie {
	out := v3Movie{ID: int(movie.ID), Title: movie.Name, TitleSlug: v3Slug(movie.Name),
		TmdbID: v3Int(movie.TmdbID), Monitored: movie.Enabled, IsAvailable: true,
		QualityProfileID: 1, Tags: []int{}}
	var year *int64
	if parsed, err := strconv.ParseInt(strings.TrimSpace(movie.Year), 10, 32); err == nil {
		out.Year = int(parsed)
		year = &parsed
	}
	if s != nil && s.db != nil {
		if size, err := s.db.MovieArchivedSize(movie.Name, year); err == nil && size != nil {
			out.HasFile = true
		}
	}
	return out
}

// v3MovieByTmdbID answers a "tmdb:" lookup: the configured movie with its id,
// or the TMDB description of a new one (id 0, so Jellyseerr adds it).
func v3MovieByTmdbID(ctx context.Context, s *AppState, cfg *Config, tmdbID int64) v3Movie {
	if index := v3MovieIndex(cfg, tmdbID, "", 0); index >= 0 {
		return v3MovieRecord(s, cfg.Movies[index])
	}
	out := v3Movie{TmdbID: tmdbID, QualityProfileID: 1, Tags: []int{}}
	client := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	if !client.Configured() {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	details, err := client.MovieDetails(ctx, strconv.FormatInt(tmdbID, 10))
	if err != nil || details == nil {
		return out
	}
	item := TmdbItem{ID: details.ID, Title: details.Title, Overview: details.Overview,
		PosterPath: details.PosterPath, ReleaseDate: details.ReleaseDate}
	out.Title, out.Year = v3ItemName(item), v3ItemYear(item)
	out.Overview, out.RemotePoster = v3ItemOverview(item), v3ItemPoster(item)
	out.TitleSlug = v3Slug(out.Title)
	if index := v3MovieIndex(cfg, 0, out.Title, out.Year); index >= 0 {
		return v3MovieRecord(s, cfg.Movies[index])
	}
	return out
}

type v3MovieBody struct {
	ID               int    `json:"id"`
	Title            string `json:"title"`
	TmdbID           int64  `json:"tmdbId"`
	Year             int    `json:"year"`
	RootFolderPath   string `json:"rootFolderPath"`
	QualityProfileID int    `json:"qualityProfileId"`
	Monitored        bool   `json:"monitored"`
}

func v3DecodeMovie(w http.ResponseWriter, r *http.Request) (v3MovieBody, bool) {
	var body v3MovieBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		v3Error(w, http.StatusBadRequest, "invalid JSON body")
		return body, false
	}
	return body, true
}

func V3MovieAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	body, ok := v3DecodeMovie(w, r)
	if !ok {
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		v3Error(w, http.StatusBadRequest, "title is required")
		return
	}
	cfg := latestConfig(s)
	if v3MovieIndex(cfg, body.TmdbID, title, body.Year) >= 0 {
		v3Error(w, http.StatusConflict, "movie already exists")
		return
	}
	profile := qualityProfileByID(ensureQualityProfiles(s), body.QualityProfileID)
	year := ""
	if body.Year > 0 {
		year = strconv.Itoa(body.Year)
	}
	added := MovieConfig{Name: title, Year: year, TmdbID: v3ID(body.TmdbID),
		Quality: profile.Quality, Language: profile.Language, Subtitle: profile.Subtitle,
		Exclude: profile.Exclude, Enabled: true, Audio: profile.Audio, DisableUpgrades: profile.DisableUpgrades}
	// The cached configuration is shared: build a new slice, never append to it.
	movies := append(append([]MovieConfig{}, cfg.Movies...), added)
	if err := SaveLibrary(cfg.DataDir, cfg.Series, movies); err != nil {
		v3Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Jellyseerr treats a response without an id as a failed add: the id is
	// the one the library database assigned on save.
	saved := latestConfig(s)
	index := v3MovieIndex(saved, body.TmdbID, title, body.Year)
	if index < 0 || saved.Movies[index].ID <= 0 {
		v3Error(w, http.StatusInternalServerError, "the movie was saved but its id is unavailable")
		return
	}
	out := v3MovieRecord(s, saved.Movies[index])
	out.RootFolderPath, out.QualityProfileID = body.RootFolderPath, profile.ID
	v3JSON(w, http.StatusCreated, out)
}

// V3MovieUpdate is PUT /api/v3/movie: Jellyseerr sends it for a movie that is
// in the library but not monitored, to monitor it again.
func V3MovieUpdate(w http.ResponseWriter, r *http.Request, s *AppState) {
	body, ok := v3DecodeMovie(w, r)
	if !ok {
		return
	}
	cfg := latestConfig(s)
	index := -1
	for i, movie := range cfg.Movies {
		if body.ID > 0 && int(movie.ID) == body.ID {
			index = i
		}
	}
	if index < 0 {
		index = v3MovieIndex(cfg, body.TmdbID, body.Title, body.Year)
	}
	if index < 0 {
		v3Error(w, http.StatusNotFound, "movie not found")
		return
	}
	movies := append([]MovieConfig{}, cfg.Movies...)
	movies[index].Enabled = body.Monitored
	if err := SaveLibrary(cfg.DataDir, cfg.Series, movies); err != nil {
		v3Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	v3JSON(w, http.StatusAccepted, v3MovieRecord(s, movies[index]))
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
