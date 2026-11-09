package gextto

// web_helpers.go holds the small helpers that several HTTP handler files share:
// JSON body decoding, the common torrent-action answers, release and series
// matching, the speed-policy sources and the episode origin labels. A helper
// lives here once, so every handler file uses the same implementation and name.

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/rules"
)

// findSeries returns the configured series whose name or alias matches name.
func findSeries(cfg *Config, name string) *SeriesConfig {
	for index := range cfg.Series {
		series := &cfg.Series[index]
		if series.Name == name {
			return series
		}
		for _, alias := range series.Aliases {
			if alias == name {
				return series
			}
		}
	}
	return nil
}

// derefString returns the string behind value, or "" when it is nil.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// mediaTestError is the message of a failed metadata-provider probe.
func mediaTestError(provider string, status int) string {
	switch status {
	case 401, 403:
		return fmt.Sprintf("%s: credenziali non valide", provider)
	case 404:
		return fmt.Sprintf("%s: endpoint not found, check the URL", provider)
	default:
		return fmt.Sprintf("%s: HTTP %d", provider, status)
	}
}

// newUUID returns a random RFC 4122 version 4 UUID string. It falls back to the
// clock when the random source is unavailable, so a caller always gets a unique
// name instead of an error path.
func newUUID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

// decodeOptionalJSON decodes a JSON body into target, treating an empty body as
// "nothing to change". It is for the endpoints whose body is optional.
func decodeOptionalJSON(r *http.Request, target any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, target)
}

// isHTTPURL reports whether value is an http(s) URL.
func isHTTPURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// storedDownloadTags returns the download tags saved in the settings.
func storedDownloadTags(cfg *Config) []string {
	raw, ok := cfg.Settings["download_tags"]
	if !ok {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil || tags == nil {
		return []string{}
	}
	return tags
}

// torrentAction writes the shared JSON answer of a torrent action: 400 on
// error, 200 on success, 404 when the torrent is unavailable (dry-run).
func torrentAction(w http.ResponseWriter, result bool, err error) {
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if result {
		jsonResponse(w, map[string]any{"ok": true})
		return
	}
	jsonError(w, http.StatusNotFound, "torrent unavailable in dry-run")
}

// normalizeSearch reduces a release title to the words a title search compares:
// lowercase letters and digits, punctuation as a separator.
func normalizeSearch(value string) string {
	head := value
	if index := strings.Index(value, "["); index >= 0 {
		head = value[:index]
	}
	cleaned := make([]rune, 0, len(head))
	for _, character := range strings.ToLower(head) {
		if (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'z') {
			cleaned = append(cleaned, character)
		} else {
			cleaned = append(cleaned, ' ')
		}
	}
	return strings.Join(strings.Fields(string(cleaned)), " ")
}

// torrentActionResult is the status and body of a torrent action, for the
// handlers that build the answer themselves instead of writing it.
func torrentActionResult(ok bool, err error) (int, any) {
	if err != nil {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
	}
	if !ok {
		return http.StatusNotFound, map[string]any{"ok": false, "error": "torrent unavailable in dry-run"}
	}
	return http.StatusOK, map[string]any{"ok": true}
}

// archiveReleasesForQuery turns the archive rows matching query into releases,
// leaving out the ones the global filters refuse. A failed search answers an
// empty list: this is a best-effort extra source, not the only one.
func archiveReleasesForQuery(s *AppState, cfg *Config, query string) []models.Release {
	results := []models.Release{}
	rows, err := s.archive.Search(query)
	if err != nil {
		return results
	}
	for _, row := range rows {
		release := ParseRelease(row[0], row[1], "archive:"+row[2])
		if release == nil {
			continue
		}
		if reason := cfg.AllReleaseDeniedReason(release); reason != "" {
			if cfg.ReleaseIsMonitored(release) {
				rules.LogRejection(release, reason)
			}
			continue
		}
		results = append(results, *release)
	}
	return results
}

func gh0ContainsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// toMap converts a JSON-marshalable value into a map, returning an empty map
// when the conversion fails.
func toMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// decodeOptional decodes a request body that may be absent: it reports whether
// there was a body to decode, and leaves the target untouched when there was not.
func decodeOptional(r *http.Request, target any) (bool, error) {
	if r.Body == nil {
		return false, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false, err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return false, err
	}
	return true, nil
}

func releaseMatchesSeriesEpisode(release models.Release, series *SeriesConfig, season, episode int64) bool {
	if release.Kind != "series" {
		return false
	}
	if release.Season == nil || *release.Season != season {
		return false
	}
	if len(release.EpisodeRange) == 0 {
		if release.Episode == nil || *release.Episode != episode {
			return false
		}
	} else {
		found := false
		for _, value := range release.EpisodeRange {
			if value == episode || value == 0 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if release.Series == nil {
		return false
	}
	name := *release.Series
	if SeriesNamesMatch(series.Name, name) {
		return true
	}
	for _, alias := range series.Aliases {
		if SeriesNamesMatch(alias, name) {
			return true
		}
	}
	return false
}

func runHousekeeping(s *AppState) (*HousekeepingReport, bool) {
	cfg := latestConfig(s)
	params := HousekeepingParams{}.FromSettings(cfg.Settings)
	report, err := s.db.Housekeeping(params)
	if err != nil {
		logging.Warn("housekeeping failed", "error", err)
		return nil, false
	}
	if _, err := runDbAction(s, "compact"); err != nil {
		logging.Warn("housekeeping failed", "error", err)
		return nil, false
	}
	if cfg != nil {
		report.StagedTorrentsRemoved = sweepStagedFeedTorrents(cfg.StateDir, time.Now())
	}
	return &report, true
}

var (
	errSetupIncomplete = errors.New("complete the initial setup first")
	errDaemonInactive  = errors.New("daemon inactive")
)

// historyDisplayName keeps provider suffixes out of the history title when
// the same provider is already exposed in the dedicated source column.
func historyDisplayName(name, source string) string {
	name = strings.TrimSpace(name)
	source = strings.TrimSpace(source)
	if name == "" || source == "" {
		return name
	}
	labels := []string{source}
	if parts := strings.Split(source, ":"); len(parts) > 1 {
		labels = append(labels, parts[len(parts)-1])
	}
	labels = append(labels, strings.Split(source, " - ")...)
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || strings.EqualFold(label, "archive") || strings.EqualFold(label, "unknown") {
			continue
		}
		bracketed := "[" + label + "]"
		if len(name) > len(bracketed) && strings.EqualFold(name[len(name)-len(bracketed):], bracketed) {
			trimmed := strings.TrimRight(strings.TrimSpace(name[:len(name)-len(bracketed)]), " -_.")
			if trimmed != "" {
				return trimmed
			}
		}
		if len(name) <= len(label) || !strings.EqualFold(name[len(name)-len(label):], label) {
			continue
		}
		start := len(name) - len(label)
		if !strings.ContainsRune("-_.", rune(name[start-1])) {
			continue
		}
		trimmed := strings.TrimRight(name[:start], " -_.")
		if trimmed != "" {
			return trimmed
		}
	}
	return name
}

// readOptionalBody returns the raw request body, tolerating an absent one.
func readOptionalBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	return io.ReadAll(r.Body)
}

func gh6PurgeRemovedLibrary(
	db *Database,
	previousSeries []SeriesConfig,
	previousMovies []MovieConfig,
	series []SeriesConfig,
	movies []MovieConfig,
) {
	for _, old := range previousSeries {
		present := false
		for _, item := range series {
			if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) {
				present = true
				break
			}
		}
		if !present {
			if _, err := db.PurgeSeries(old.Name); err != nil {
				logging.Warn("cannot remove the state of the deleted series", "error", err, "series", old.Name)
			}
		}
	}
	for _, old := range previousMovies {
		stillPresent := false
		if old.ID > 0 {
			for _, item := range movies {
				if item.ID == old.ID {
					stillPresent = true
					break
				}
			}
		} else {
			for _, item := range movies {
				if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) &&
					strings.TrimSpace(item.Year) == strings.TrimSpace(old.Year) {
					stillPresent = true
					break
				}
			}
		}
		if !stillPresent {
			if _, err := db.PurgeMovie(old.Name); err != nil {
				logging.Warn("cannot remove the state of the deleted movie", "error", err, "movie", old.Name)
			}
		}
	}
}

// Speed policy sources. The exception set in Downloads ("temp*") comes first;
// then the rule chosen in Configuration (see speed_mode.go): a schedule window,
// Auto, or the fixed limits.
const (
	speedSourceTemp         = "temp"          // exception: a limit with an expiry
	speedSourceTempKeep     = "temp_keep"     // exception: a limit kept until removed
	speedSourceTempAuto     = "temp_auto"     // exception: Auto, the whole line to the tuning
	speedSourceSchedule     = "schedule"      // rule: schedule window with limits
	speedSourceScheduleAuto = "schedule_auto" // rule: schedule window on Auto
	speedSourceAuto         = "auto"          // rule: Auto (or Auto outside the windows)
	speedSourceBase         = "base"          // rule: the fixed limits
)

// Episode source origins. They are shown in the UI and returned by the API, and
// the code also compares them, so every use goes through these constants.
const (
	episodeOriginFeed    = "Feed RSS"
	episodeOriginArchive = "Archivio"
	episodeOriginIndexer = "Indexer / web"
	episodeOriginED2K    = "eD2k"
)

// gh7_find_series returns a copy of the configured series whose name or alias
// matches name, and false when there is none.
func gh7FindSeries(cfg *Config, name string) (SeriesConfig, bool) {
	for _, series := range cfg.Series {
		if series.Name == name {
			return series, true
		}
		for _, alias := range series.Aliases {
			if alias == name {
				return series, true
			}
		}
	}
	return SeriesConfig{}, false
}
