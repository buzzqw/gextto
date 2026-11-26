package gextto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/models"
	subtitleprovider "github.com/buzzqw/gextto/internal/subtitles"
)

const (
	subtitleSettingEnabled     = "subtitles_enabled"
	subtitleSettingProviderURL = "subtitles_provider_url"
	subtitleSettingAPIKey      = "subtitles_provider_api_key"
	subtitleSettingUsername    = "subtitles_provider_username"
	subtitleSettingPassword    = "subtitles_provider_password"
	subtitleSettingForced      = "subtitles_forced"
	subtitleSettingSDH         = "subtitles_sdh"
	subtitleSettingCollision   = "subtitles_collision_policy"
)

var errSubtitleCollision = errors.New("subtitle already exists")

// subtitleWriteMu makes the existence check and final rename one operation for
// every subtitle writer in this daemon. The instance lock prevents a second
// daemon from sharing the data directory.
var subtitleWriteMu sync.Mutex

type subtitlePreferences struct {
	Enabled                                 bool
	ProviderURL, APIKey, Username, Password string
	Forced, SDH, Collision                  string
}

func subtitlePreferencesFrom(cfg *Config) subtitlePreferences {
	if cfg == nil {
		return subtitlePreferences{ProviderURL: subtitleprovider.DefaultBaseURL, Forced: "any", SDH: "any", Collision: "keep"}
	}
	prefs := subtitlePreferences{
		Enabled:     settingTruthy(cfg.Settings[subtitleSettingEnabled]),
		ProviderURL: settingsOr(cfg, subtitleSettingProviderURL, subtitleprovider.DefaultBaseURL),
		APIKey:      strings.TrimSpace(cfg.Settings[subtitleSettingAPIKey]),
		Username:    strings.TrimSpace(cfg.Settings[subtitleSettingUsername]),
		Password:    cfg.Settings[subtitleSettingPassword],
		Forced:      strings.ToLower(settingsOr(cfg, subtitleSettingForced, "any")),
		SDH:         strings.ToLower(settingsOr(cfg, subtitleSettingSDH, "any")),
		Collision:   strings.ToLower(settingsOr(cfg, subtitleSettingCollision, "keep")),
	}
	if prefs.Forced != "yes" && prefs.Forced != "no" {
		prefs.Forced = "any"
	}
	if prefs.SDH != "yes" && prefs.SDH != "no" {
		prefs.SDH = "any"
	}
	if prefs.Collision != "replace" {
		prefs.Collision = "keep"
	}
	return prefs
}

// subtitleLanguagesFor returns the provider codes requested by the title. The
// choice belongs to the series/movie configuration: Integrations only sets up
// the provider and never overrides the languages.
func subtitleLanguagesFor(cfg *Config, target subtitleTarget) []string {
	preferred := strings.TrimSpace(target.SubtitlePreference)
	if preferred == "" || strings.EqualFold(preferred, "any") {
		preferred = cfg.DefaultLanguage()
	}
	languages := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(preferred, ",") {
		code := subtitleProviderLanguage(raw)
		if code != "" && !seen[code] {
			seen[code] = true
			languages = append(languages, code)
		}
	}
	if len(languages) == 0 {
		languages = []string{"en"}
	}
	return languages
}

func subtitleProviderClient(cfg *Config) (*subtitleprovider.Client, subtitlePreferences, error) {
	prefs := subtitlePreferencesFrom(cfg)
	if !prefs.Enabled {
		return nil, prefs, errors.New("subtitles are disabled")
	}
	client, err := subtitleprovider.New(subtitleprovider.Config{
		BaseURL: prefs.ProviderURL, APIKey: prefs.APIKey, Username: prefs.Username, Password: prefs.Password,
		UserAgent: "gexttod/" + constants.AppVersion(),
	})
	if err != nil {
		return nil, prefs, err
	}
	return client, prefs, nil
}

var subtitleLanguageToProvider = map[string]string{
	"ita": "it", "eng": "en", "deu": "de", "ger": "de", "fra": "fr", "fre": "fr",
	"spa": "es", "pol": "pl", "por": "pt", "rus": "ru", "ara": "ar", "jpn": "ja",
	"zho": "zh", "chi": "zh", "kor": "ko", "nld": "nl", "dut": "nl", "swe": "sv",
	"nor": "no", "dan": "da", "fin": "fi", "tur": "tr", "heb": "he", "hin": "hi",
}

var subtitleProviderToLanguage = map[string]string{
	"it": "ita", "en": "eng", "de": "deu", "fr": "fra", "es": "spa", "pl": "pol",
	"pt": "por", "ru": "rus", "ar": "ara", "ja": "jpn", "zh": "zho", "ko": "kor",
	"nl": "nld", "sv": "swe", "no": "nor", "da": "dan", "fi": "fin", "tr": "tur",
	"he": "heb", "hi": "hin",
}

func subtitleProviderLanguage(raw string) string {
	code := strings.ToLower(strings.TrimSpace(raw))
	if code == "" || code == "any" {
		return ""
	}
	if mapped := subtitleLanguageToProvider[code]; mapped != "" {
		return mapped
	}
	if len(code) == 2 {
		return code
	}
	return ""
}

func subtitleLanguageSuffix(raw string) string {
	code := strings.ToLower(strings.TrimSpace(raw))
	if mapped := subtitleProviderToLanguage[code]; mapped != "" {
		return mapped
	}
	if len(code) == 2 {
		return code
	}
	return "und"
}

type subtitleTarget struct {
	Kind               string `json:"kind"`
	Label              string `json:"label"`
	VideoPath          string `json:"video_path"`
	OSHash             string `json:"os_hash"`
	SizeBytes          int64  `json:"size_bytes"`
	RuntimeSeconds     int64  `json:"runtime_seconds"`
	Series             string `json:"series,omitempty"`
	Season             int64  `json:"season,omitempty"`
	Episode            int64  `json:"episode,omitempty"`
	MovieID            int64  `json:"movie_id,omitempty"`
	Title              string `json:"title,omitempty"`
	Year               int    `json:"year,omitempty"`
	TMDBID             string `json:"tmdb_id,omitempty"`
	SubtitlePreference string `json:"subtitle_preference,omitempty"`
}

func resolveSubtitleTarget(ctx context.Context, cfg *Config, db *Database, seriesName string, season, episode, movieID int64) (subtitleTarget, error) {
	if cfg == nil || db == nil {
		return subtitleTarget{}, errors.New("configuration or database unavailable")
	}
	var target subtitleTarget
	path := ""
	if movieID > 0 {
		var movie *MovieConfig
		for i := range cfg.Movies {
			if cfg.Movies[i].ID == movieID {
				movie = &cfg.Movies[i]
				break
			}
		}
		if movie == nil {
			return target, errors.New("movie not found")
		}
		copies, err := db.DownloadedMovieCopies()
		if err != nil {
			return target, err
		}
		copy, ok := movieDownloadedCopyFor(*movie, copies)
		if !ok || strings.TrimSpace(copy.Path) == "" {
			return target, errors.New("the downloaded movie path is not recorded")
		}
		path = copy.Path
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			path, err = resolveMovieCompletedFile(path)
			if err != nil {
				return target, err
			}
		}
		target = subtitleTarget{Kind: "movie", Label: movie.Name, MovieID: movie.ID, Title: movie.Name, TMDBID: movie.TmdbID, SubtitlePreference: movie.Subtitle}
		if year, err := parsePositiveYear(movie.Year); err == nil {
			target.Year = year
		}
	} else {
		if seriesName == "" || season <= 0 || episode < 0 {
			return target, errors.New("series, season and episode are required")
		}
		series, ok := gh7FindSeries(cfg, seriesName)
		if !ok {
			return target, errors.New("series not found")
		}
		path, _ = db.ArchivedEpisodePath(series.Name, season, episode)
		if strings.TrimSpace(path) == "" {
			release := &models.Release{Kind: "series", Series: &series.Name, Season: &season}
			if destination, ok := ConfiguredDestinationForSeason(release, season, cfg); ok {
				if found, ok := FindEpisodeFile(destination, season, episode); ok {
					path = found
				}
			}
		} else if info, err := os.Stat(path); err == nil && info.IsDir() {
			if found, ok := FindEpisodeFile(path, season, episode); ok {
				path = found
			}
		}
		if strings.TrimSpace(path) == "" {
			return target, errors.New("the archived episode path is not recorded or cannot be resolved")
		}
		target = subtitleTarget{Kind: "episode", Label: fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode), Series: series.Name, Season: season, Episode: episode, Title: series.Name, TMDBID: series.TmdbID, SubtitlePreference: series.Subtitle}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return target, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !subtitleVideoExtension(path) {
		return target, errors.New("the subtitle target is not a regular video file")
	}
	hash, size, err := subtitleprovider.HashFile(path)
	if err != nil {
		return target, err
	}
	target.VideoPath, target.OSHash, target.SizeBytes = path, hash, size
	if probe, err := probeFileWithinContext(ctx, path, 15*time.Second); err == nil {
		target.RuntimeSeconds = probe.RuntimeSeconds
	}
	return target, nil
}

func parsePositiveYear(raw string) (int, error) {
	var year int
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &year); err != nil || year < 1000 || year > 9999 {
		return 0, errors.New("invalid year")
	}
	return year, nil
}

func subtitleVideoExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mp4", ".avi", ".m4v", ".mov", ".ts":
		return true
	default:
		return false
	}
}

type subtitleCandidate struct {
	FileID        int64    `json:"file_id"`
	Language      string   `json:"language"`
	Release       string   `json:"release"`
	HashMatch     bool     `json:"hash_match"`
	SizeMatch     bool     `json:"size_match"`
	Forced        *bool    `json:"forced,omitempty"`
	SDH           *bool    `json:"sdh,omitempty"`
	DownloadCount int      `json:"download_count"`
	ProposedName  string   `json:"proposed_name"`
	Reasons       []string `json:"reasons"`
}

func subtitleBool(value *bool) bool { return value != nil && *value }

func (c subtitleCandidate) IsForced() bool { return subtitleBool(c.Forced) }
func (c subtitleCandidate) IsSDH() bool    { return subtitleBool(c.SDH) }

func searchSubtitleCandidates(ctx context.Context, cfg *Config, db *Database, target subtitleTarget) ([]subtitleCandidate, error) {
	client, prefs, err := subtitleProviderClient(cfg)
	if err != nil {
		return nil, err
	}
	languages := subtitleLanguagesFor(cfg, target)
	kind := "episode"
	if target.Kind == "movie" {
		kind = "movie"
	}
	results, err := client.Search(ctx, subtitleprovider.SearchQuery{
		Language: strings.Join(languages, ","), Kind: kind, Title: target.Title, Year: target.Year,
		TMDBID: target.TMDBID, Season: int(target.Season), Episode: int(target.Episode),
		MovieHash: target.OSHash, MovieSize: target.SizeBytes,
	})
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, language := range languages {
		allowed[language] = true
	}
	candidates := []subtitleCandidate{}
	for _, result := range results {
		language := strings.ToLower(result.Language)
		if !allowed[language] {
			continue
		}
		forced := result.ForeignPartsOnly
		sdh := result.HearingImpaired
		if prefs.Forced == "yes" && !subtitleBool(forced) {
			continue
		}
		if prefs.Forced == "no" && subtitleBool(forced) {
			continue
		}
		if prefs.SDH == "yes" && !subtitleBool(sdh) {
			continue
		}
		if prefs.SDH == "no" && subtitleBool(sdh) {
			continue
		}
		file, ok := result.PrimaryFile()
		if !ok {
			continue
		}
		reasons := []string{}
		if result.HashMatch {
			reasons = append(reasons, "exact file hash")
		}
		if result.SizeMatch {
			reasons = append(reasons, "exact size")
		}
		if target.TMDBID != "" && result.IMDbMatch {
			reasons = append(reasons, "external id")
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "title and episode/movie identity")
		}
		name := subtitleSidecarName(target.VideoPath, language, subtitleBool(forced), subtitleBool(sdh))
		candidates = append(candidates, subtitleCandidate{
			FileID: int64(file.ID), Language: language, Release: result.Release(), HashMatch: result.HashMatch,
			SizeMatch: result.SizeMatch, Forced: forced, SDH: sdh, DownloadCount: result.DownloadCount,
			ProposedName: name, Reasons: reasons,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.HashMatch != right.HashMatch {
			return left.HashMatch
		}
		if left.SizeMatch != right.SizeMatch {
			return left.SizeMatch
		}
		if left.DownloadCount != right.DownloadCount {
			return left.DownloadCount > right.DownloadCount
		}
		return left.Release < right.Release
	})
	if len(candidates) > 50 {
		candidates = candidates[:50]
	}
	return candidates, nil
}

func subtitleSidecarName(videoPath, language string, forced, sdh bool) string {
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	suffix := subtitleLanguageSuffix(language)
	if forced {
		suffix += ".forced"
	}
	if sdh {
		suffix += ".sdh"
	}
	return stem + "." + suffix + ".srt"
}

func applySubtitle(ctx context.Context, cfg *Config, db *Database, target subtitleTarget, fileID int64, language string, forced, sdh, replace bool) (string, error) {
	client, _, err := subtitleProviderClient(cfg)
	if err != nil {
		return "", err
	}
	download, err := client.Download(ctx, fileID, "srt")
	if err != nil {
		return "", err
	}
	if err := validateSubtitleSRT(download.Data); err != nil {
		return "", err
	}
	name := subtitleSidecarName(target.VideoPath, language, forced, sdh)
	path := filepath.Join(filepath.Dir(target.VideoPath), name)
	if err := writeSubtitleAtomic(path, download.Data, replace, cfg); err != nil {
		return "", err
	}
	return path, nil
}

func validateSubtitleSRT(data []byte) error {
	if len(data) == 0 || len(data) > 4<<20 {
		return errors.New("subtitle file is empty or too large")
	}
	text := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	if !utf8.ValidString(text) {
		return errors.New("subtitle file is not valid UTF-8")
	}
	if !strings.Contains(text, "-->") {
		return errors.New("subtitle file has no SRT timing lines")
	}
	return nil
}

func writeSubtitleAtomic(path string, data []byte, replace bool, cfg *Config) error {
	subtitleWriteMu.Lock()
	defer subtitleWriteMu.Unlock()

	path = filepath.Clean(path)
	if hasParentComponent(path) {
		return errors.New("invalid subtitle path")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to replace a subtitle symlink")
		}
		if !replace {
			return errSubtitleCollision
		}
		if existing, err := os.ReadFile(path); err == nil && string(existing) == string(data) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	temporary := filepath.Join(dir, fmt.Sprintf(".%s.gextto-subtitle-%d", filepath.Base(path), time.Now().UnixNano()))
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	backup := ""
	if _, err := os.Lstat(path); err == nil {
		backup = temporary + ".old"
		if err := os.Rename(path, backup); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		if backup != "" {
			_ = os.Rename(backup, path)
		}
		return err
	}
	keep = true
	if backup != "" {
		if err := trashOrRemove(backup, cfg); err != nil {
			return fmt.Errorf("subtitle installed but the old file could not be trashed: %w", err)
		}
	}
	return nil
}
