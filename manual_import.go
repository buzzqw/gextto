package gextto

// manual_import.go implements the manual import of a file that already exists
// on disk: it parses the name, matches a configured series or movie, proposes
// the archive destination and, when confirmed, renames and moves the file,
// replacing an inferior copy only when the operator asks for it. It is the
// missing Sonarr/Radarr operation described in docs/evoluzione.md §8.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// ManualImportCandidate is one video file the page proposes to import.
type ManualImportCandidate struct {
	Path        string
	Name        string
	Kind        string
	ParsedTitle string
	Series      string
	Season      int64
	Episode     int64
	Episodes    []int64
	Year        int64
	Matched     bool
	Destination string
	Target      string
	Error       string
}

// manualImportParse parses a file name. The parser wants a transfer identity
// even when only the name matters, so a placeholder magnet is supplied.
func manualImportParse(path string) *models.Release {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return ParseRelease(name, "magnet:?xt=urn:btih:"+strings.Repeat("0", 40), "manual")
}

// ManualImportPreview lists the video files under path with what the import
// would do. It never touches the filesystem.
func ManualImportPreview(cfg *Config, path string) ([]ManualImportCandidate, error) {
	if cfg == nil {
		return nil, errors.New("configuration unavailable")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("choose a file or a folder")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return nil, errors.New("the path is not a file or a folder")
	}
	files, err := VideoFiles(abs)
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, errors.New("no video file found")
	}
	if len(files) > 200 {
		files = files[:200]
	}
	out := make([]ManualImportCandidate, 0, len(files))
	for _, file := range files {
		out = append(out, manualImportCandidate(cfg, file))
	}
	return out, nil
}

// manualImportCandidate classifies one file. It is pure: matching, target
// computation and destination resolution, no move.
func manualImportCandidate(cfg *Config, file string) ManualImportCandidate {
	c := ManualImportCandidate{Path: file, Name: filepath.Base(file)}
	release := manualImportParse(file)
	if release == nil {
		c.Error = "the file name cannot be parsed"
		return c
	}
	c.Kind, c.ParsedTitle = release.Kind, release.Title
	switch release.Kind {
	case "series":
		if release.Series == nil || release.Season == nil || release.Episode == nil {
			c.Error = "the file name has no season and episode"
			return c
		}
		c.Series, c.Season, c.Episode = *release.Series, *release.Season, *release.Episode
		c.Episodes = episodeFileRun(release)
		series := cfg.FindSeriesMatch(c.Series, &c.Season)
		if series == nil {
			series = cfg.FindSeriesByName(c.Series)
		}
		if series == nil {
			c.Error = "no configured series matches"
			return c
		}
		c.Matched = true
		c.Series = series.Name
		release.Series = &series.Name
		_, target, ok, err := episodeTarget(context.Background(), file, release, cfg, nil)
		if err != nil {
			c.Error = err.Error()
			return c
		}
		if !ok {
			target = file
		}
		c.Destination = manualImportDestination(cfg, release)
		if c.Destination == "" {
			c.Error = "the series has no archive folder and no archive root is configured"
			return c
		}
		c.Target = filepath.Join(c.Destination, filepath.Base(target))
	case "movie":
		year := int64(0)
		if release.Year != nil {
			year = *release.Year
		}
		c.Year = year
		movie := cfg.FindMovieMatchManual(release.Title, release.Year)
		if movie == nil {
			c.Error = "no configured movie matches"
			return c
		}
		c.Matched = true
		c.ParsedTitle = movie.Name
		if year == 0 {
			if parsed, err := strconv.ParseInt(movie.Year, 10, 64); err == nil {
				c.Year = parsed
			}
		}
		c.Destination = manualImportDestination(cfg, release)
		if c.Destination == "" {
			c.Error = "the movie has no archive folder and no archive root is configured"
			return c
		}
		// The exact name is produced by RenameMovie on import (it may consult
		// TMDB and the media tags); the preview shows the folder.
		c.Target = filepath.Join(c.Destination, c.Name)
	default:
		c.Error = "the file name is neither a series episode nor a movie"
	}
	return c
}

func manualImportDestination(cfg *Config, release *models.Release) string {
	if destination, ok := ConfiguredDestinationFor(release, cfg); ok {
		return destination
	}
	if cfg.ArchiveRoot != nil {
		return strings.TrimSpace(*cfg.ArchiveRoot)
	}
	return ""
}

// ManualImportRequest is what the page confirms for one file.
type ManualImportRequest struct {
	Path     string
	Kind     string
	Series   string
	Movie    string
	Year     int64
	Season   int64
	Episode  int64
	Replace  bool
	TMDBKey  string
	Language string
}

// ManualImport applies one confirmed candidate. It returns the final path.
func ManualImport(ctx context.Context, cfg *Config, db *Database, req ManualImportRequest) (string, error) {
	if cfg == nil || db == nil {
		return "", errors.New("configuration unavailable")
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return "", errors.New("choose a file or a folder")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("manual import works on a single video file")
	}
	release := manualImportParse(abs)
	if release == nil {
		return "", errors.New("the file name cannot be parsed")
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = release.Kind
	}
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	switch kind {
	case "series":
		seriesName := strings.TrimSpace(req.Series)
		if seriesName == "" && release.Series != nil {
			seriesName = *release.Series
		}
		series := cfg.FindSeriesByName(seriesName)
		if series == nil {
			return "", fmt.Errorf("series %q is not configured", seriesName)
		}
		season := req.Season
		if season == 0 && release.Season != nil {
			season = *release.Season
		}
		episode := req.Episode
		if episode == 0 && release.Episode != nil {
			episode = *release.Episode
		}
		if season <= 0 || episode < 0 {
			return "", errors.New("the season and the episode are required")
		}
		release.Kind = "series"
		release.Series = &series.Name
		release.Season = &season
		release.Episode = &episode
		if len(release.EpisodeRange) == 0 {
			release.EpisodeRange = []int64{episode}
		}
		source, target, ok, err := episodeTarget(ctx, abs, release, cfg, tmdb)
		if err != nil {
			return "", err
		}
		if !ok || target == "" || source == "" {
			source, target = abs, abs
		}
		destination := manualImportDestination(cfg, release)
		if destination == "" {
			return "", errors.New("the series has no archive folder and no archive root is configured")
		}
		final, err := manualImportPlace(source, filepath.Base(target), destination, req.Replace, cfg)
		if err != nil {
			return "", err
		}
		title := strings.TrimSuffix(filepath.Base(final), filepath.Ext(final))
		size := int64(0)
		if st, err := os.Stat(final); err == nil {
			size = st.Size()
		}
		score := cfg.FileScore(final, "series", series.Name)
		for _, ep := range episodeFileRun(release) {
			if err := db.SyncArchiveFileScored(series.Name, season, ep, title, final, size, score); err != nil {
				return final, err
			}
		}
		RefreshMediaLibraries(cfg, filepath.Dir(final))
		logging.Info("manual import: episode archived", "series", series.Name, "season", season, "episode", episode, "path", final)
		return final, nil
	case "movie":
		movieName := strings.TrimSpace(req.Movie)
		year := req.Year
		if year == 0 && release.Year != nil {
			year = *release.Year
		}
		movie := cfg.FindMovieMatchManual(movieName, &year)
		if movie == nil && movieName == "" {
			movie = cfg.FindMovieMatchManual(release.Title, release.Year)
		}
		if movie == nil {
			return "", fmt.Errorf("movie %q is not configured", movieName)
		}
		release.Kind = "movie"
		release.Title = movie.Name
		renamed, err := RenameMovie(ctx, abs, release, cfg, tmdb)
		if err != nil {
			return "", err
		}
		if renamed == "" {
			renamed = abs
		}
		destination := manualImportDestination(cfg, release)
		if destination == "" {
			return "", errors.New("the movie has no archive folder and no archive root is configured")
		}
		final, err := manualImportPlace(renamed, filepath.Base(renamed), destination, req.Replace, cfg)
		if err != nil {
			return "", err
		}
		if err := db.RecordManualMovie(movie.Name, year, final); err != nil {
			return final, err
		}
		RefreshMediaLibraries(cfg, filepath.Dir(final))
		logging.Info("manual import: movie archived", "movie", movie.Name, "path", final)
		return final, nil
	}
	return "", errors.New("choose a series or a movie")
}

// manualImportPlace moves source into destination under targetName, replacing
// an existing file only when asked; the old copy goes to the trash, never
// deleted outright.
func manualImportPlace(source, targetName, destination string, replace bool, cfg *Config) (string, error) {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(destination, targetName)
	if SamePath(source, final) {
		return final, nil
	}
	if _, err := os.Stat(final); err == nil {
		if !replace {
			return "", fmt.Errorf("%s already exists: confirm the replacement", final)
		}
		if err := trashOrRemove(final, cfg); err != nil {
			return "", fmt.Errorf("cannot move the existing file to the trash: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := moveAcrossDevices(source, final); err != nil {
		return "", err
	}
	MoveSidecarsToTarget(source, final, cfg)
	return final, nil
}
