package gextto

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
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

// subtitleLanguagesFilter parses the browser's explicit language filter. Nil
// means every language; an empty or "all" value is the same choice.
func subtitleLanguagesFilter(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "all") {
		return nil, nil
	}
	languages := []string{}
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		code := subtitleProviderLanguage(item)
		if code == "" {
			return nil, fmt.Errorf("unsupported subtitle language %q", strings.TrimSpace(item))
		}
		if !seen[code] {
			seen[code] = true
			languages = append(languages, code)
		}
	}
	return languages, nil
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
	ReferenceName      string `json:"reference_name,omitempty"`
	TorrentHash        string `json:"torrent_hash,omitempty"`
	FileIndex          *int   `json:"file_index,omitempty"`
	FileComplete       bool   `json:"file_complete"`
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
	target.FileComplete = true
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
	FileID            int64    `json:"file_id"`
	Language          string   `json:"language"`
	Release           string   `json:"release"`
	HashMatch         bool     `json:"hash_match"`
	SizeMatch         bool     `json:"size_match"`
	ExternalIDMatch   bool     `json:"external_id_match"`
	Forced            *bool    `json:"forced,omitempty"`
	SDH               *bool    `json:"sdh,omitempty"`
	FromTrusted       *bool    `json:"from_trusted,omitempty"`
	AITranslated      *bool    `json:"ai_translated,omitempty"`
	MachineTranslated *bool    `json:"machine_translated,omitempty"`
	DownloadCount     int      `json:"download_count"`
	ReleaseSimilarity int      `json:"release_similarity"`
	Score             int      `json:"score"`
	Recommended       bool     `json:"recommended"`
	ProposedName      string   `json:"proposed_name"`
	Reasons           []string `json:"reasons"`
}

func subtitleBool(value *bool) bool { return value != nil && *value }

func (c subtitleCandidate) IsForced() bool            { return subtitleBool(c.Forced) }
func (c subtitleCandidate) IsSDH() bool               { return subtitleBool(c.SDH) }
func (c subtitleCandidate) IsTrusted() bool           { return subtitleBool(c.FromTrusted) }
func (c subtitleCandidate) IsAITranslated() bool      { return subtitleBool(c.AITranslated) }
func (c subtitleCandidate) IsMachineTranslated() bool { return subtitleBool(c.MachineTranslated) }

// subtitleCandidateScore combines how precisely the provider matched the video
// with the reputation signals it exposes. It deliberately does not claim to
// measure subtitle content quality: no candidate is downloaded for this score.
func subtitleCandidateScore(candidate subtitleCandidate) int {
	score := 10 // the provider returned it for the requested title/episode identity
	score += candidate.ReleaseSimilarity * 30 / 100
	if candidate.HashMatch {
		score += 45
	}
	if candidate.SizeMatch {
		score += 10
	}
	if candidate.ExternalIDMatch {
		score += 10
	}
	if candidate.IsTrusted() {
		score += 10
	}
	if candidate.AITranslated != nil && !*candidate.AITranslated && candidate.MachineTranslated != nil && !*candidate.MachineTranslated {
		score += 5
	}
	if candidate.DownloadCount > 0 {
		// Ten points at 100,000 downloads, with diminishing returns before that.
		score += int(math.Round(10 * math.Log1p(float64(candidate.DownloadCount)) / math.Log1p(100000)))
	}
	if candidate.IsAITranslated() {
		score -= 15
	}
	if candidate.IsMachineTranslated() {
		score -= 10
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// subtitleReleaseSimilarity compares the names an operator recognises, not
// provider IDs. Punctuation, separators and the media extension are ignored;
// the remaining edit similarity is bounded so a hostile provider cannot make
// the request spend unbounded CPU.
func subtitleReleaseSimilarity(names ...string) int {
	if len(names) == 0 {
		return 0
	}
	candidate := normalizeSubtitleReleaseName(names[len(names)-1])
	if candidate == "" {
		return 0
	}
	best := 0
	for _, name := range names[:len(names)-1] {
		reference := normalizeSubtitleReleaseName(name)
		if reference == "" {
			continue
		}
		if reference == candidate {
			return 100
		}
		longest := utf8.RuneCountInString(reference)
		if count := utf8.RuneCountInString(candidate); count > longest {
			longest = count
		}
		distance := subtitleEditDistance(reference, candidate)
		similarity := 100 - distance*100/longest
		if similarity > best {
			best = similarity
		}
	}
	return best
}

func normalizeSubtitleReleaseName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch filepath.Ext(name) {
	case ".mkv", ".mp4", ".avi", ".m4v", ".mov", ".ts", ".wmv", ".mpeg", ".mpg":
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, name)
	runes := []rune(normalized)
	if len(runes) > 512 {
		runes = runes[:512]
	}
	return string(runes)
}

func subtitleEditDistance(left, right string) int {
	a, b := []rune(left), []rune(right)
	if len(a) > len(b) {
		a, b = b, a
	}
	previous := make([]int, len(a)+1)
	current := make([]int, len(a)+1)
	for index := range previous {
		previous[index] = index
	}
	for row, rightRune := range b {
		current[0] = row + 1
		for column, leftRune := range a {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			value := previous[column+1] + 1
			if deletion := previous[column] + 1; deletion < value {
				value = deletion
			}
			if substitution := previous[column] + cost; substitution < value {
				value = substitution
			}
			current[column+1] = value
		}
		previous, current = current, previous
	}
	return previous[len(a)]
}

func searchSubtitleCandidates(ctx context.Context, cfg *Config, db *Database, target subtitleTarget) ([]subtitleCandidate, error) {
	return searchSubtitleCandidatesForLanguages(ctx, cfg, db, target, subtitleLanguagesFor(cfg, target))
}

func searchSubtitleCandidatesForLanguages(ctx context.Context, cfg *Config, db *Database, target subtitleTarget, languages []string) ([]subtitleCandidate, error) {
	client, prefs, err := subtitleProviderClient(cfg)
	if err != nil {
		return nil, err
	}
	return searchSubtitleCandidatesWithLanguages(ctx, client, prefs, cfg, db, target, languages)
}

// subtitleEpisodeNumber narrows a season or episode number for the provider
// query: a value outside a sane range is sent as "unknown" (0) rather than
// converted blindly.
func subtitleEpisodeNumber(value int64) int {
	if value < 0 || value > math.MaxInt32 {
		return 0
	}
	return int(value)
}

// searchSubtitleCandidatesWith is searchSubtitleCandidates with a client the
// caller already holds: the automatic pass shares one client, and so one
// provider login, across all its searches and downloads.
func searchSubtitleCandidatesWith(ctx context.Context, client *subtitleprovider.Client, prefs subtitlePreferences, cfg *Config, db *Database, target subtitleTarget) ([]subtitleCandidate, error) {
	return searchSubtitleCandidatesWithLanguages(ctx, client, prefs, cfg, db, target, subtitleLanguagesFor(cfg, target))
}

// searchSubtitleCandidatesWithLanguages accepts nil to browse every provider
// language. The automatic pass and the archived-title modal keep their title
// preference; the active-torrent browser uses nil unless a language is chosen.
func searchSubtitleCandidatesWithLanguages(ctx context.Context, client *subtitleprovider.Client, prefs subtitlePreferences, cfg *Config, db *Database, target subtitleTarget, languages []string) ([]subtitleCandidate, error) {
	kind := "episode"
	if target.Kind == "movie" {
		kind = "movie"
	}
	results, err := client.Search(ctx, subtitleprovider.SearchQuery{
		Language: strings.Join(languages, ","), Kind: kind, Title: target.Title, Year: target.Year,
		TMDBID: target.TMDBID, Season: subtitleEpisodeNumber(target.Season), Episode: subtitleEpisodeNumber(target.Episode),
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
		if language == "" {
			continue
		}
		if len(allowed) > 0 && !allowed[language] {
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
			reasons = append(reasons, "hash file esatto")
		}
		if result.SizeMatch {
			reasons = append(reasons, "dimensione esatta")
		}
		if result.IMDbMatch {
			reasons = append(reasons, "ID esterno")
		}
		if result.FromTrusted != nil && *result.FromTrusted {
			reasons = append(reasons, "autore attendibile")
		}
		if result.AITranslated != nil && *result.AITranslated {
			reasons = append(reasons, "tradotto con AI")
		}
		if result.MachineTranslated != nil && *result.MachineTranslated {
			reasons = append(reasons, "traduzione automatica")
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "titolo e identità episodio/film")
		}
		name := subtitleSidecarName(target.VideoPath, language, subtitleBool(forced), subtitleBool(sdh))
		similarity := subtitleReleaseSimilarity(filepath.Base(target.VideoPath), target.ReferenceName, result.Release())
		candidate := subtitleCandidate{
			FileID: int64(file.ID), Language: language, Release: result.Release(), HashMatch: result.HashMatch,
			SizeMatch: result.SizeMatch, ExternalIDMatch: result.IMDbMatch, Forced: forced, SDH: sdh,
			FromTrusted: result.FromTrusted, AITranslated: result.AITranslated, MachineTranslated: result.MachineTranslated,
			DownloadCount: result.DownloadCount, ReleaseSimilarity: similarity, ProposedName: name, Reasons: reasons,
		}
		candidate.Score = subtitleCandidateScore(candidate)
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		if left.HashMatch != right.HashMatch {
			return left.HashMatch
		}
		if left.SizeMatch != right.SizeMatch {
			return left.SizeMatch
		}
		if left.DownloadCount != right.DownloadCount {
			return left.DownloadCount > right.DownloadCount
		}
		if left.Language != right.Language {
			return left.Language < right.Language
		}
		return left.Release < right.Release
	})
	if len(candidates) > 50 {
		candidates = candidates[:50]
	}
	// One useful default per language, but never recommend a weak title-only
	// match. The operator still sees and chooses every other candidate.
	bestByLanguage := map[string]bool{}
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.Score < 40 || bestByLanguage[candidate.Language] {
			continue
		}
		candidate.Recommended = true
		bestByLanguage[candidate.Language] = true
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
	return applySubtitleWith(ctx, client, cfg, target, fileID, language, forced, sdh, replace)
}

// applySubtitleWith is applySubtitle with a client the caller already holds.
func applySubtitleWith(ctx context.Context, client *subtitleprovider.Client, cfg *Config, target subtitleTarget, fileID int64, language string, forced, sdh, replace bool) (string, error) {
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
