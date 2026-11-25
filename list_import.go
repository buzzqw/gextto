package gextto

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const maxListImportItems = 500

type ListImportItem struct {
	Key         string            `json:"key"`
	Source      string            `json:"source"`
	Kind        string            `json:"kind"`
	Title       string            `json:"title"`
	Year        string            `json:"year,omitempty"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Anime       bool              `json:"anime,omitempty"`
	Status      string            `json:"status,omitempty"`
	MatchedName string            `json:"matched_name,omitempty"`
	Reason      string            `json:"reason,omitempty"`
}

type ListImportOptions struct {
	QualityProfileID int    `json:"quality_profile_id"`
	SeriesRoot       string `json:"series_root"`
	SeasonSubfolders bool   `json:"season_subfolders"`
	DryRun           bool   `json:"dry_run"`
}

type ListImportReport struct {
	SeriesAdded   int              `json:"series_added"`
	MoviesAdded   int              `json:"movies_added"`
	AlreadyExists int              `json:"already_present"`
	IDsUpdated    int              `json:"ids_updated"`
	Conflicts     int              `json:"conflicts"`
	DryRun        bool             `json:"dry_run"`
	Items         []ListImportItem `json:"items"`
}

func listImportContainerKind(key string) (string, bool) {
	switch strings.ToLower(key) {
	case "show", "shows", "series", "tv":
		return "series", false
	case "anime":
		return "series", true
	case "movie", "movies":
		return "movie", false
	default:
		return "", false
	}
}

func listImportYear(value any) string {
	switch typed := value.(type) {
	case string:
		typed = strings.TrimSpace(typed)
		if len(typed) == 4 {
			if number, err := strconv.Atoi(typed); err == nil && number > 0 {
				return typed
			}
		}
	case float64:
		if typed >= 1000 && typed <= 9999 {
			return strconv.FormatInt(int64(typed), 10)
		}
	case int64:
		if typed >= 1000 && typed <= 9999 {
			return strconv.FormatInt(typed, 10)
		}
	case json.Number:
		value, err := strconv.Atoi(typed.String())
		if err == nil && value >= 1000 && value <= 9999 {
			return strconv.Itoa(value)
		}
	}
	return ""
}

func listImportExternalIDs(value any) map[string]string {
	ids := map[string]string{}
	object, ok := value.(map[string]any)
	if !ok {
		return ids
	}
	for provider, raw := range object {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		var text string
		switch typed := raw.(type) {
		case string:
			text = strings.TrimSpace(typed)
		case json.Number:
			text = typed.String()
		case float64:
			text = strconv.FormatInt(int64(typed), 10)
		case int64:
			text = strconv.FormatInt(typed, 10)
		}
		if text == "" || text == "0" || len(text) > 64 {
			continue
		}
		ids[provider] = text
	}
	return ids
}

func collectListImportItems(source string, value any) []ListImportItem {
	items := []ListImportItem{}
	var walk func(any, string, bool)
	walk = func(current any, kind string, anime bool) {
		switch typed := current.(type) {
		case []any:
			for _, child := range typed {
				walk(child, kind, anime)
			}
		case map[string]any:
			title := ""
			for _, key := range []string{"title", "name"} {
				if raw, ok := typed[key].(string); ok && strings.TrimSpace(raw) != "" {
					title = strings.TrimSpace(raw)
					break
				}
			}
			if title != "" && kind != "" {
				if len([]rune(title)) <= 300 {
					item := ListImportItem{Source: source, Kind: kind, Title: title, Year: listImportYear(typed["year"]), ExternalIDs: listImportExternalIDs(typed["ids"]), Anime: anime}
					if raw, ok := typed["anime"].(bool); ok {
						item.Anime = raw
					}
					hash := sha256.Sum256([]byte(source + "\x00" + kind + "\x00" + strings.ToLower(title) + "\x00" + item.Year + "\x00" + listImportIDsKey(item.ExternalIDs)))
					item.Key = hex.EncodeToString(hash[:8])
					items = append(items, item)
				}
				return
			}
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				childKind, childAnime := listImportContainerKind(key)
				if childKind == "" {
					childKind = kind
				}
				walk(typed[key], childKind, anime || childAnime)
			}
		}
	}
	walk(value, "", false)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		if items[i].Title != items[j].Title {
			return items[i].Title < items[j].Title
		}
		return items[i].Year < items[j].Year
	})
	if len(items) > maxListImportItems {
		items = items[:maxListImportItems]
	}
	return items
}

func listImportIDsKey(ids map[string]string) string {
	keys := make([]string, 0, len(ids))
	for key := range ids {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+ids[key])
	}
	return strings.Join(parts, "&")
}

func validListImportItem(item ListImportItem) bool {
	if item.Source != "simkl" || (item.Kind != "series" && item.Kind != "movie") || strings.TrimSpace(item.Title) == "" || len([]rune(item.Title)) > 300 {
		return false
	}
	if item.Year != "" {
		year, err := strconv.Atoi(item.Year)
		if err != nil || year < 1000 || year > 9999 {
			return false
		}
	}
	for provider, id := range item.ExternalIDs {
		if strings.TrimSpace(provider) == "" || strings.TrimSpace(id) == "" || len(id) > 64 {
			return false
		}
	}
	return true
}

func planListImport(cfg *Config, items []ListImportItem) []ListImportItem {
	planned := append([]ListImportItem(nil), items...)
	type identity struct {
		kind, provider, id string
	}
	ids := map[identity]string{}
	titles := map[string][]string{}
	titleYears := map[string][]string{}
	add := func(kind, name, year string, external map[string]string) {
		for provider, id := range external {
			key := identity{kind: kind, provider: strings.ToLower(provider), id: id}
			if _, exists := ids[key]; !exists {
				ids[key] = name
			}
		}
		if strings.TrimSpace(name) == "" {
			return
		}
		key := kind + "\x00" + matchingName(name)
		titles[key] = append(titles[key], name)
		if year != "" {
			titleYears[key+"\x00"+year] = append(titleYears[key+"\x00"+year], name)
		}
	}
	if cfg != nil {
		for _, series := range cfg.Series {
			external := map[string]string{}
			for key, value := range series.ExternalIDs {
				external[key] = value
			}
			if external["tmdb"] == "" && series.TmdbID != "" {
				external["tmdb"] = series.TmdbID
			}
			if external["tvdb"] == "" && series.TvdbID != "" {
				external["tvdb"] = series.TvdbID
			}
			add("series", series.Name, series.Year, external)
		}
		for _, movie := range cfg.Movies {
			external := map[string]string{}
			for key, value := range movie.ExternalIDs {
				external[key] = value
			}
			if external["tmdb"] == "" && movie.TmdbID != "" {
				external["tmdb"] = movie.TmdbID
			}
			if external["tvdb"] == "" && movie.TvdbID != "" {
				external["tvdb"] = movie.TvdbID
			}
			add("movie", movie.Name, movie.Year, external)
		}
	}
	for i := range planned {
		item := &planned[i]
		item.Status, item.MatchedName, item.Reason = "", "", ""
		if !validListImportItem(*item) {
			item.Status, item.Reason = "invalid", "invalid title, kind, year or external id"
			continue
		}
		matched := ""
		for provider, id := range item.ExternalIDs {
			if name := ids[identity{kind: item.Kind, provider: strings.ToLower(provider), id: id}]; name != "" {
				matched = name
				break
			}
		}
		if matched != "" {
			item.Status, item.MatchedName, item.Reason = "duplicate_id", matched, "an external id already exists in the library"
			continue
		}
		key := item.Kind + "\x00" + matchingName(item.Title)
		if item.Year != "" {
			matches := titleYears[key+"\x00"+item.Year]
			if len(matches) == 1 {
				item.Status, item.MatchedName, item.Reason = "duplicate_title_year", matches[0], "title and year already exist"
				continue
			}
			if len(matches) > 1 {
				item.Status, item.Reason = "ambiguous", "more than one library title matches"
				continue
			}
		} else {
			matches := titles[key]
			if len(matches) == 1 {
				item.Status, item.MatchedName, item.Reason = "duplicate_title", matches[0], "title already exists"
				continue
			}
			if len(matches) > 1 {
				item.Status, item.Reason = "ambiguous", "more than one library title matches"
				continue
			}
		}
		item.Status = "new"
	}
	return planned
}

func applyListImportProfile(cfg *Config, options ListImportOptions) (QualityProfile, error) {
	if options.QualityProfileID <= 0 {
		return QualityProfile{}, nil
	}
	profiles := qualityProfilesFromConfig(cfg)
	profile, ok := qualityProfileExact(profiles, options.QualityProfileID)
	if !ok {
		return QualityProfile{}, errors.New("quality profile not found")
	}
	return profile, nil
}

func importedSeries(cfg *Config, item ListImportItem, profile QualityProfile, options ListImportOptions) SeriesConfig {
	language := profile.Language
	if strings.TrimSpace(language) == "" {
		language = cfg.DefaultLanguage()
	}
	series := SeriesConfig{
		Name: item.Title, Year: item.Year, Seasons: "1+", Quality: profile.Quality,
		Qualities: append([]string(nil), profile.Qualities...), Audio: profile.Audio,
		Language: language, Subtitle: profile.Subtitle, Exclude: profile.Exclude,
		Enabled: true, Anime: item.Anime, DisableUpgrades: profile.DisableUpgrades,
		SeasonSubfolders: options.SeasonSubfolders, Priority: titlePriorityNormal,
		SeasonStrategy: seasonStrategyAuto, ExternalIDs: copyStringMap(item.ExternalIDs),
	}
	if root := strings.TrimSpace(options.SeriesRoot); root != "" {
		series.ArchivePath = filepath.Join(root, folderNameForTitle(item.Title))
	}
	series.TmdbID = series.ExternalIDs["tmdb"]
	series.TvdbID = series.ExternalIDs["tvdb"]
	return series
}

func importedMovie(cfg *Config, item ListImportItem, profile QualityProfile) MovieConfig {
	language := profile.Language
	if strings.TrimSpace(language) == "" {
		language = cfg.DefaultLanguage()
	}
	movie := MovieConfig{
		Name: item.Title, Year: item.Year, Quality: profile.Quality,
		Qualities: append([]string(nil), profile.Qualities...), Audio: profile.Audio,
		Language: language, Subtitle: profile.Subtitle, Exclude: profile.Exclude,
		Enabled: true, DisableUpgrades: profile.DisableUpgrades, Priority: titlePriorityNormal,
		ExternalIDs: copyStringMap(item.ExternalIDs),
	}
	movie.TmdbID = movie.ExternalIDs["tmdb"]
	movie.TvdbID = movie.ExternalIDs["tvdb"]
	return movie
}

func copyStringMap(source map[string]string) map[string]string {
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func readImportedSeries(tx *sql.Tx, fallback []SeriesConfig) ([]SeriesConfig, error) {
	var raw sql.NullString
	err := tx.QueryRow("SELECT value FROM settings WHERE key='_migrated_series'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return append([]SeriesConfig(nil), fallback...), nil
	}
	if err != nil {
		return nil, err
	}
	var series []SeriesConfig
	if raw.String != "" {
		if err := json.Unmarshal([]byte(raw.String), &series); err != nil {
			return nil, fmt.Errorf("read the current series list: %w", err)
		}
	}
	return series, nil
}

type importedMovieIdentity struct {
	ID          int64
	Name, Year  string
	ExternalIDs map[string]string
}

func readImportedMovieIdentities(tx *sql.Tx) ([]MovieConfig, error) {
	rows, err := tx.Query("SELECT id,name,COALESCE(year,''),COALESCE(tmdb_id,''),COALESCE(tvdb_id,''),COALESCE(external_ids,'{}') FROM movies_config")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	movies := []MovieConfig{}
	for rows.Next() {
		var item importedMovieIdentity
		var tmdbID, tvdbID, externalRaw string
		if err := rows.Scan(&item.ID, &item.Name, &item.Year, &tmdbID, &tvdbID, &externalRaw); err != nil {
			return nil, err
		}
		external := decodeTitleTranslations(externalRaw)
		if external == nil {
			external = map[string]string{}
		}
		if external["tmdb"] == "" && tmdbID != "" {
			external["tmdb"] = tmdbID
		}
		if external["tvdb"] == "" && tvdbID != "" {
			external["tvdb"] = tvdbID
		}
		movies = append(movies, MovieConfig{ID: item.ID, Name: item.Name, Year: item.Year, TmdbID: tmdbID, TvdbID: tvdbID, ExternalIDs: external})
	}
	return movies, rows.Err()
}

func insertImportedMovie(tx *sql.Tx, movie MovieConfig) (int64, error) {
	external := copyStringMap(movie.ExternalIDs)
	result, err := tx.Exec(`INSERT INTO movies_config(name,year,quality,language,enabled,subtitle,exclude,language_requirements,subtitle_requirements,tmdb_id,tvdb_id,original_title,overview,poster_path,external_ids,disable_upgrades,audio,audio_channels,aliases,title_translations,qualities,cutoff,priority)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15,?16,?17,?18,?19,?20,?21,?22,?23)`,
		movie.Name, movie.Year, movie.Quality, movie.Language, boolToInt(movie.Enabled), movie.Subtitle, movie.Exclude,
		movie.LanguageRequirements, movie.SubtitleRequirements, movie.TmdbID, movie.TvdbID, movie.OriginalTitle,
		movie.Overview, movie.PosterPath, encodeTitleTranslations(external), boolToInt(movie.DisableUpgrades), movie.Audio,
		movie.AudioChannels, encodeMovieAliases(movie.Aliases), encodeTitleTranslations(movie.TitleTranslations),
		encodeMovieQualities(movie.Qualities), movie.Cutoff, movie.Priority)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

const listImportMovieSchema = `CREATE TABLE IF NOT EXISTS movies_config (id INTEGER PRIMARY KEY, name TEXT NOT NULL, year TEXT DEFAULT '', quality TEXT DEFAULT '', language TEXT DEFAULT '', enabled INTEGER DEFAULT 1, subtitle TEXT DEFAULT '', exclude TEXT DEFAULT '', language_requirements TEXT DEFAULT '', subtitle_requirements TEXT DEFAULT '', tmdb_id TEXT DEFAULT '', tvdb_id TEXT DEFAULT '', original_title TEXT DEFAULT '', overview TEXT DEFAULT '', poster_path TEXT DEFAULT '', external_ids TEXT NOT NULL DEFAULT '{}', disable_upgrades INTEGER NOT NULL DEFAULT 0, audio TEXT NOT NULL DEFAULT '', audio_channels TEXT NOT NULL DEFAULT '', aliases TEXT NOT NULL DEFAULT '', title_translations TEXT NOT NULL DEFAULT '', qualities TEXT NOT NULL DEFAULT '[]', cutoff TEXT NOT NULL DEFAULT '', priority TEXT NOT NULL DEFAULT 'normal');`

func ensureListImportMovieSchema(conn *sql.DB) error {
	if _, err := conn.Exec(listImportMovieSchema); err != nil {
		return err
	}
	for _, column := range []string{
		"exclude TEXT NOT NULL DEFAULT ''",
		"language_requirements TEXT NOT NULL DEFAULT ''",
		"subtitle_requirements TEXT NOT NULL DEFAULT ''",
		"tmdb_id TEXT NOT NULL DEFAULT ''",
		"tvdb_id TEXT NOT NULL DEFAULT ''",
		"original_title TEXT NOT NULL DEFAULT ''",
		"overview TEXT NOT NULL DEFAULT ''",
		"poster_path TEXT NOT NULL DEFAULT ''",
		"external_ids TEXT NOT NULL DEFAULT '{}'",
		"disable_upgrades INTEGER NOT NULL DEFAULT 0",
		"audio TEXT NOT NULL DEFAULT ''",
		"audio_channels TEXT NOT NULL DEFAULT ''",
		"aliases TEXT NOT NULL DEFAULT ''",
		"title_translations TEXT NOT NULL DEFAULT ''",
		"qualities TEXT NOT NULL DEFAULT '[]'",
		"cutoff TEXT NOT NULL DEFAULT ''",
		"priority TEXT NOT NULL DEFAULT 'normal'",
	} {
		_, _ = conn.Exec("ALTER TABLE movies_config ADD COLUMN " + column)
	}
	return nil
}

// ApplyListImport re-reads the library under an immediate transaction, re-plans
// the selected items and only appends missing titles. Existing movie rows are
// never deleted or rewritten wholesale.
func ApplyListImport(cfg *Config, items []ListImportItem, options ListImportOptions) (ListImportReport, error) {
	report := ListImportReport{DryRun: options.DryRun, Items: []ListImportItem{}}
	if len(items) == 0 {
		return report, errors.New("select at least one title")
	}
	if len(items) > maxListImportItems {
		return report, errors.New("too many selected titles")
	}
	selected := []ListImportItem{}
	seen := map[string]bool{}
	for _, item := range items {
		if !validListImportItem(item) {
			report.Conflicts++
			report.Items = append(report.Items, item)
			continue
		}
		if seen[item.Key] {
			continue
		}
		seen[item.Key] = true
		selected = append(selected, item)
	}
	if len(selected) == 0 {
		return report, errors.New("no valid selected title")
	}
	profile, err := applyListImportProfile(cfg, options)
	if err != nil {
		return report, err
	}
	if options.DryRun {
		planned := planListImport(cfg, selected)
		report.Items = planned
		for _, item := range planned {
			switch item.Status {
			case "new":
				if item.Kind == "series" {
					report.SeriesAdded++
				} else {
					report.MoviesAdded++
				}
			case "duplicate_id", "duplicate_title", "duplicate_title_year":
				report.AlreadyExists++
			default:
				report.Conflicts++
			}
		}
		return report, nil
	}
	if cfg == nil {
		return report, errors.New("configuration unavailable")
	}
	conn, err := OpenConfigDBWritable(filepath.Join(cfg.DataDir, "gextto_config.db"))
	if err != nil {
		return report, err
	}
	defer conn.Close()
	if err := ensureListImportMovieSchema(conn); err != nil {
		return report, err
	}
	tx, err := conn.Begin()
	if err != nil {
		return report, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	series, err := readImportedSeries(tx, cfg.Series)
	if err != nil {
		return report, err
	}
	movies, err := readImportedMovieIdentities(tx)
	if err != nil {
		return report, err
	}
	current := &Config{Series: series, Movies: movies}
	planned := planListImport(current, selected)
	report.Items = planned
	seriesChanged := false
	for _, item := range planned {
		switch item.Status {
		case "new":
			if item.Kind == "series" {
				series = append(series, importedSeries(cfg, item, profile, options))
				report.SeriesAdded++
				seriesChanged = true
			} else {
				movie := importedMovie(cfg, item, profile)
				if _, err := insertImportedMovie(tx, movie); err != nil {
					return report, err
				}
				report.MoviesAdded++
			}
		case "duplicate_id", "duplicate_title", "duplicate_title_year":
			report.AlreadyExists++
			// Fill only missing external ids; never overwrite a conflicting id.
			for i := range series {
				if item.Kind == "series" && strings.EqualFold(series[i].Name, item.MatchedName) {
					before := listImportIDsKey(series[i].ExternalIDs)
					mergeMissingStringMap(&series[i].ExternalIDs, item.ExternalIDs)
					if before != listImportIDsKey(series[i].ExternalIDs) {
						report.IDsUpdated++
						seriesChanged = true
					}
				}
			}
			if item.Kind == "movie" {
				for provider, id := range item.ExternalIDs {
					query := "SELECT COALESCE(external_ids,'{}') FROM movies_config WHERE lower(name)=lower(?1)"
					args := []any{item.MatchedName}
					if item.Year != "" {
						query += " AND year=?2"
						args = append(args, item.Year)
					}
					query += " LIMIT 1"
					var raw string
					if err := tx.QueryRow(query, args...).Scan(&raw); err == nil {
						existing := decodeTitleTranslations(raw)
						if existing == nil {
							existing = map[string]string{}
						}
						before := listImportIDsKey(existing)
						mergeMissingStringMap(&existing, map[string]string{provider: id})
						if before != listImportIDsKey(existing) {
							update := "UPDATE movies_config SET external_ids=?1 WHERE lower(name)=lower(?2)"
							updateArgs := []any{encodeTitleTranslations(existing), item.MatchedName}
							if item.Year != "" {
								update += " AND year=?3"
								updateArgs = append(updateArgs, item.Year)
							}
							if _, err := tx.Exec(update, updateArgs...); err != nil {
								return report, err
							}
							report.IDsUpdated++
						}
					}
				}
			}
		default:
			report.Conflicts++
		}
	}
	if seriesChanged {
		encoded, err := json.Marshal(series)
		if err != nil {
			return report, err
		}
		if _, err := tx.Exec("INSERT INTO settings(key,value) VALUES ('_migrated_series',?1) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(encoded)); err != nil {
			return report, err
		}
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	committed = true
	TouchConfigGeneration()
	return report, nil
}

func mergeMissingStringMap(target *map[string]string, source map[string]string) {
	if *target == nil {
		*target = map[string]string{}
	}
	for key, value := range source {
		if (*target)[key] == "" {
			(*target)[key] = value
		}
	}
}
