package gextto

// web_helpers.go holds the small helpers the HTTP handler files share.
//
// The handlers live in eight files split by size (web_handlers_g0..g7.go), and
// before this file existed each of them carried its own prefixed copy of the
// same helper (`gh0_`, `gh1_`, …) so the copies could not collide. The copies
// are merged here: one implementation, one name, no prefix.

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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
