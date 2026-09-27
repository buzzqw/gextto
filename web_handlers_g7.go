package gextto

// Handler group 7.
//
// Shared helpers/types (AppState, jsonResponse, jsonError, decodeJSON,
// pathParam, pathInt, queryParam, queryInt, latestConfig, ...) live in web.go
// and the JSON input structs are declared there exactly once. Everything that
// is private to this file is prefixed `gh7_`.

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

const gh7_external_search_timeout = 12 * time.Second

const gh7_handler_magnet = `#!/bin/bash
# Gextto - handler protocollo magnet: invia il link a Gextto.
GEXTTO_URL="${GEXTTO_URL:-__GEXTTO_URL__}"
LOG="/tmp/gextto-magnet.log"
MAGNET="${*}"

echo "$(date '+%F %T') magnet=${MAGNET}" >> "$LOG"
if [[ -z "$MAGNET" ]]; then
  notify-send "Gextto" "Nessun link magnet ricevuto" --icon=dialog-error 2>/dev/null
  exit 1
fi
JSON=$(python3 -c "import json,sys; print(json.dumps({'magnet': sys.argv[1]}))" "$MAGNET")
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${GEXTTO_URL}/api/send-magnet" \
  -H 'Content-Type: application/json' --data-raw "$JSON" --max-time 10 2>/dev/null)
HTTP_CODE=$(echo "$RESPONSE" | tail -1)
if [[ "$HTTP_CODE" == "200" || "$HTTP_CODE" == "202" ]]; then
  notify-send "Gextto" "Torrent aggiunto" --icon=emblem-downloads 2>/dev/null
else
  notify-send "Gextto" "Gextto non raggiungibile (${GEXTTO_URL}) - HTTP ${HTTP_CODE}" --icon=dialog-error 2>/dev/null
  echo "$(date '+%F %T') HTTP ${HTTP_CODE} ${RESPONSE}" >> "$LOG"
fi
`

const gh7_handler_torrent = `#!/bin/bash
# Gextto - handler file .torrent: invia il file locale (o l'URL) a Gextto.
GEXTTO_URL="${GEXTTO_URL:-__GEXTTO_URL__}"
LOG="/tmp/gextto-torrent.log"
INPUT="$1"

if [[ -z "$INPUT" ]]; then
  notify-send "Gextto" "Nessun file .torrent ricevuto" --icon=dialog-error 2>/dev/null
  exit 1
fi

if [[ "$INPUT" == http://* || "$INPUT" == https://* ]]; then
  JSON=$(python3 -c "import json,sys; print(json.dumps({'magnet': sys.argv[1]}))" "$INPUT")
  RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${GEXTTO_URL}/api/send-magnet" \
    -H 'Content-Type: application/json' --data-raw "$JSON" --max-time 30 2>/dev/null)
else
  FILEPATH="${INPUT#file://}"
  if [[ ! -f "$FILEPATH" ]]; then
    notify-send "Gextto" "File non trovato: $FILEPATH" --icon=dialog-error 2>/dev/null
    exit 1
  fi
  RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${GEXTTO_URL}/api/upload-torrent" \
    --data-binary @"$FILEPATH" -H 'Content-Type: application/x-bittorrent' --max-time 30 2>/dev/null)
fi
HTTP_CODE=$(echo "$RESPONSE" | tail -1)
if [[ "$HTTP_CODE" == "200" || "$HTTP_CODE" == "202" ]]; then
  notify-send "Gextto" "Torrent aggiunto" --icon=emblem-downloads 2>/dev/null
else
  notify-send "Gextto" "Gextto non raggiungibile (${GEXTTO_URL}) - HTTP ${HTTP_CODE}" --icon=dialog-error 2>/dev/null
  echo "$(date '+%F %T') HTTP ${HTTP_CODE} ${RESPONSE}" >> "$LOG"
fi
`

const gh7_handler_magnet_desktop = `[Desktop Entry]
Name=Gextto Magnet Handler
Comment=Invia link magnet a Gextto
Exec=/usr/local/bin/gextto-magnet %U
Type=Application
MimeType=x-scheme-handler/magnet;
NoDisplay=true
StartupNotify=false
Terminal=false
`

const gh7_handler_torrent_desktop = `[Desktop Entry]
Name=Gextto Torrent Handler
Comment=Invia file .torrent a Gextto
Exec=/usr/local/bin/gextto-torrent %U
Type=Application
MimeType=application/x-bittorrent;x-scheme-handler/magnet;
NoDisplay=true
StartupNotify=false
Terminal=false
`

const gh7_handler_install = `#!/bin/bash
# Gextto - installazione handler magnet/.torrent (Linux, xdg-utils).
# Richiede: curl, python3, xdg-utils, libnotify (opzionale).
set -e

GEXTTO_URL="${GEXTTO_URL:-__GEXTTO_URL__}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "Gextto - installazione handler torrent/magnet"
echo "URL Gextto: $GEXTTO_URL"
echo ""

for cmd in curl python3 xdg-mime xdg-open; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "Mancante: $cmd (sudo apt install xdg-utils curl python3)"
    exit 1
  fi
done

echo "Installo gli script in /usr/local/bin ..."
for f in gextto-magnet gextto-torrent; do
  sudo cp "$SCRIPT_DIR/$f" "/usr/local/bin/$f"
  sudo chmod +x "/usr/local/bin/$f"
  echo "  $f"
done

DESKTOP_DIR="$HOME/.local/share/applications"
mkdir -p "$DESKTOP_DIR"
for f in gextto-magnet.desktop gextto-torrent.desktop; do
  cp "$SCRIPT_DIR/$f" "$DESKTOP_DIR/$f"
  echo "  $f"
done

echo "Registro i MIME handler ..."
xdg-mime default gextto-magnet.desktop x-scheme-handler/magnet
xdg-mime default gextto-torrent.desktop application/x-bittorrent
update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true

for PREFS in $(find "$HOME/.mozilla/firefox" -name prefs.js 2>/dev/null); do
  sed -i '/network.protocol-handler.expose.magnet/d' "$PREFS"
  sed -i '/network.protocol-handler.external.magnet/d' "$PREFS"
  echo 'user_pref("network.protocol-handler.expose.magnet", false);' >> "$PREFS"
  echo 'user_pref("network.protocol-handler.external.magnet", true);' >> "$PREFS"
  echo "  Firefox: $PREFS"
done

if curl -s --max-time 3 "$GEXTTO_URL/api/status" &>/dev/null; then
  echo "Gextto raggiungibile."
else
  echo "Attenzione: Gextto non raggiungibile su $GEXTTO_URL"
fi

echo ""
echo "Fatto. Riavvia i browser aperti."
echo "Verifica: xdg-mime query default x-scheme-handler/magnet"
`

// gh7_episode_result mirrors the `{"release": ..., "origin": ...}` values
// produced by the episode search helpers.
type gh7_episode_result struct {
	Release models.Release `json:"release"`
	Origin  string         `json:"origin"`
}

// gh7_torrent_action reproduces `torrent_action`: 200 on true, 404 when the
// torrent session is unavailable (dry-run), 400 on error.
func gh7_torrent_action(w http.ResponseWriter, result bool, err error) {
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

// gh7_find_series reproduces `find_series`, returning a copy of the matching
// configuration entry (name or alias match).
func gh7_find_series(cfg *Config, name string) (SeriesConfig, bool) {
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

func gh7_contains_int64(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func gh7_int64_ptr_equal(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// gh7_release_matches_series_episode implements
// `release_matches_series_episode`: a targeted search must also surface packs
// that contain the episode (S01E01-08) and whole-season packs (S01).
func gh7_release_matches_series_episode(release *models.Release, series SeriesConfig, season, episode int64) bool {
	if release.Kind != "series" || release.Season == nil || *release.Season != season {
		return false
	}
	if len(release.EpisodeRange) == 0 {
		if release.Episode == nil || *release.Episode != episode {
			return false
		}
	} else if !gh7_contains_int64(release.EpisodeRange, episode) && !gh7_contains_int64(release.EpisodeRange, 0) {
		return false
	}
	if release.Series == nil {
		return false
	}
	if SeriesNamesMatch(series.Name, *release.Series) {
		return true
	}
	for _, alias := range series.Aliases {
		if SeriesNamesMatch(alias, *release.Series) {
			return true
		}
	}
	return false
}

// gh7_stored_series_episode_sources collects the results already stored locally
// (feed rows and archive) without any network traffic.
func gh7_stored_series_episode_sources(s *AppState, series SeriesConfig, season, episode int64) []gh7_episode_result {
	query := fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode)
	results := []gh7_episode_result{}

	if s.db != nil {
		if rows, err := s.db.SeriesFeedForEpisode(season, episode, 200); err == nil {
			for _, row := range rows {
				if release := ParseRelease(row[0], row[1], row[2]); release != nil {
					if gh7_release_matches_series_episode(release, series, season, episode) {
						results = append(results, gh7_episode_result{Release: *release, Origin: "Feed RSS"})
					}
				}
			}
		}
	}

	if s.archive != nil {
		if rows, err := s.archive.Search(query); err == nil {
			for _, row := range rows {
				if release := ParseRelease(row[0], row[1], row[2]); release != nil {
					if gh7_release_matches_series_episode(release, series, season, episode) {
						results = append(results, gh7_episode_result{Release: *release, Origin: "Archivio"})
					}
				}
			}
		}
	}
	return results
}

// gh7_finalize_episode_search_results applies the same filters as the "Accoda"
// click, deduplicates by magnet hash + origin and sorts by release score.
func gh7_finalize_episode_search_results(results []gh7_episode_result, cfg *Config, series SeriesConfig) []gh7_episode_result {
	allowed := make([]gh7_episode_result, 0, len(results))
	for _, result := range results {
		if cfg.ReleaseAllowed(&result.Release) && cfg.SeriesReleaseAllowed(&series, &result.Release.Quality, result.Release.Title) {
			allowed = append(allowed, result)
		}
	}
	seen := map[string]struct{}{}
	deduped := make([]gh7_episode_result, 0, len(allowed))
	for _, result := range allowed {
		hash, ok := utils.MagnetHash(result.Release.Magnet)
		if !ok {
			continue
		}
		key := hash + ":" + result.Origin
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, result)
	}
	sort.SliceStable(deduped, func(i, j int) bool {
		return cfg.ReleaseScore(&deduped[i].Release) > cfg.ReleaseScore(&deduped[j].Release)
	})
	return deduped
}

// gh7_search_series_episode_sources searches stored + live sources for an
// episode, keeping the origin on every result.
func gh7_search_series_episode_sources(ctx context.Context, s *AppState, cfg *Config, series SeriesConfig, season, episode int64) []gh7_episode_result {
	results := gh7_stored_series_episode_sources(s, series, season, episode)
	query := fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode)
	if s.engine != nil {
		for _, release := range s.engine.SearchQueryManual(ctx, cfg, query) {
			if gh7_release_matches_series_episode(&release, series, season, episode) {
				results = append(results, gh7_episode_result{Release: release, Origin: "Indexer / web"})
			}
		}
	}
	return gh7_finalize_episode_search_results(results, cfg, series)
}

// gh7_torrent_display_name returns the real torrent name, falling back to
// "unnamed torrent".
func gh7_torrent_display_name(torrents TorrentSession, hash string) string {
	if torrents == nil {
		return "unnamed torrent"
	}
	for _, torrent := range torrents.List() {
		if strings.EqualFold(torrent.Hash, hash) {
			if strings.TrimSpace(torrent.Name) != "" {
				return torrent.Name
			}
			break
		}
	}
	return "unnamed torrent"
}

// gh7_torrent_files_are_disposable is true when a completed season pack may be
// deleted: the pack is already copied to the library (status `completed`).
func gh7_torrent_files_are_disposable(db *Database, hash string) bool {
	if db == nil {
		return false
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return false
	}
	if !meta.Release.IsPack {
		return false
	}
	status, err := db.TorrentStatus(hash)
	if err != nil || status == nil {
		return false
	}
	return *status == "completed"
}

// gh7_mismatched_pack_release returns the configured release when the torrent's
// real name identifies a different season.
func gh7_mismatched_pack_release(torrents TorrentSession, db *Database, hash string) *models.Release {
	if db == nil {
		return nil
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return nil
	}
	release := meta.Release
	if release.Kind != "series" || !release.IsPack {
		return nil
	}
	name := gh7_torrent_display_name(torrents, hash)
	corrected := ReconcilePackIdentity(&release, name)
	if corrected == nil {
		return nil
	}
	if !gh7_int64_ptr_equal(corrected.Season, release.Season) {
		return &release
	}
	return nil
}

// gh7_blocklist_mismatched_pack permanently blocklists a mismatched season
// pack so gap filling cannot pick it again.
func gh7_blocklist_mismatched_pack(torrents TorrentSession, db *Database, hash string) bool {
	release := gh7_mismatched_pack_release(torrents, db, hash)
	if release == nil {
		return false
	}
	if db == nil {
		return false
	}
	if blocked, err := db.IsBlocklisted(hash); err == nil && blocked {
		return true
	}
	if err := db.Blocklist(release, "season_pack_identity_mismatch"); err != nil {
		logging.Warn("could not blocklist mismatched season pack", "hash", hash, "error", err.Error())
		return false
	}
	logging.Warn("mismatched season pack permanently blocklisted", "hash", hash, "title", release.Title)
	return true
}

func gh7_collect_watchlist_entries(value any, out *[][2]string) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			gh7_collect_watchlist_entries(item, out)
		}
	case map[string]any:
		if raw, ok := typed["title"]; ok {
			if title, ok := raw.(string); ok {
				year := ""
				if y, ok := typed["year"].(float64); ok {
					year = fmt.Sprintf("%d", int64(y))
				} else if y, ok := typed["year"].(int64); ok {
					year = fmt.Sprintf("%d", y)
				}
				*out = append(*out, [2]string{title, year})
				return
			}
		}
		for _, child := range typed {
			gh7_collect_watchlist_entries(child, out)
		}
	}
}

func gh7_collect_watchlist(value any, out *[][3]string) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			gh7_collect_watchlist(item, out)
		}
	case map[string]any:
		typedFound := false
		for key, child := range typed {
			var kind string
			switch key {
			case "show", "shows", "anime", "series", "tv":
				kind = "series"
			case "movie", "movies":
				kind = "movie"
			default:
				continue
			}
			typedFound = true
			entries := [][2]string{}
			gh7_collect_watchlist_entries(child, &entries)
			for _, entry := range entries {
				*out = append(*out, [3]string{kind, entry[0], entry[1]})
			}
		}
		if !typedFound {
			for _, child := range typed {
				gh7_collect_watchlist(child, out)
			}
		}
	}
}

func gh7_apply_watchlist_import(cfg *Config, entries [][3]string) map[string]any {
	seriesAdded := 0
	moviesAdded := 0
	for _, entry := range entries {
		kind, title, year := entry[0], entry[1], entry[2]
		if strings.TrimSpace(title) == "" {
			continue
		}
		if kind == "series" {
			found := false
			for _, item := range cfg.Series {
				if strings.EqualFold(item.Name, title) {
					found = true
					break
				}
			}
			if found {
				continue
			}
			cfg.Series = append(cfg.Series, SeriesConfig{
				Name:     title,
				Seasons:  "1+",
				Language: "ita",
				Enabled:  true,
			})
			seriesAdded++
		} else {
			found := false
			for _, item := range cfg.Movies {
				if strings.EqualFold(item.Name, title) {
					found = true
					break
				}
			}
			if found {
				continue
			}
			cfg.Movies = append(cfg.Movies, MovieConfig{
				Name:     title,
				Year:     year,
				Language: "ita",
				Enabled:  true,
			})
			moviesAdded++
		}
	}
	return map[string]any{
		"series_added": seriesAdded,
		"movies_added": moviesAdded,
		"series_total": len(cfg.Series),
		"movies_total": len(cfg.Movies),
	}
}

func gh7_xml_escape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	value = strings.ReplaceAll(value, "'", "&apos;")
	return value
}

// gh7_build_magnet_feed builds the rolling RSS 2.0 magnet feed.
func gh7_build_magnet_feed(entries [][3]string) string {
	var xml strings.Builder
	xml.Grow(len(entries)*256 + 256)
	xml.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<rss version=\"2.0\"><channel>")
	xml.WriteString("<title>Gextto Magnet Feed</title>")
	xml.WriteString("<description>Feed automatico di magnet link da Gextto</description>")
	xml.WriteString(fmt.Sprintf("<lastBuildDate>%s</lastBuildDate>", time.Now().UTC().Format(time.RFC1123Z)))
	for _, entry := range entries {
		title, magnet, source := entry[0], entry[1], entry[2]
		xml.WriteString("<item><title>")
		xml.WriteString(gh7_xml_escape(title))
		xml.WriteString("</title><link>")
		xml.WriteString(gh7_xml_escape(magnet))
		xml.WriteString("</link><description>")
		xml.WriteString(gh7_xml_escape(title))
		if source != "" {
			xml.WriteString(" [")
			xml.WriteString(gh7_xml_escape(source))
			xml.WriteString("]")
		}
		xml.WriteString("</description></item>")
	}
	xml.WriteString("</channel></rss>")
	return xml.String()
}

// gh7_fetch_ipfilter downloads an IP filter list, transparently decoding gzip
// and zip payloads.
func gh7_fetch_ipfilter(ctx context.Context, rawURL string) ([]byte, string) {
	resp, err := HTTPGet(ctx, rawURL, map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
	})
	if err != nil {
		return nil, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	data, err := readLimitedBody(resp.Body, maxFeedResponseBytes)
	if err != nil {
		return nil, err.Error()
	}
	lower := strings.ToLower(strings.SplitN(rawURL, "?", 2)[0])
	if strings.HasSuffix(lower, ".gz") || (len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b) {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err.Error()
		}
		defer reader.Close()
		text, err := readLimitedBody(reader, maxDecompressedBytes)
		if err != nil {
			return nil, err.Error()
		}
		return text, ""
	}
	if strings.HasSuffix(lower, ".zip") || (len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04) {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err.Error()
		}
		for _, file := range reader.File {
			name := strings.ToLower(file.Name)
			if strings.HasSuffix(name, ".p2p") || strings.HasSuffix(name, ".dat") ||
				strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".list") {
				handle, err := file.Open()
				if err != nil {
					return nil, err.Error()
				}
				text, err := readLimitedBody(handle, maxDecompressedBytes)
				handle.Close()
				if err != nil {
					return nil, err.Error()
				}
				return text, ""
			}
		}
		return nil, "archivio zip senza lista riconosciuta"
	}
	preview := data
	if len(preview) > 300 {
		preview = preview[:300]
	}
	previewText := strings.ToLower(string(preview))
	if strings.Contains(previewText, "<html") || strings.Contains(previewText, "<body") {
		return nil, "l'URL ha restituito una pagina web invece della lista"
	}
	return data, ""
}

// gh7_form_urlencode mirrors `url::form_urlencoded::byte_serialize`.
func gh7_form_urlencode(value string) string {
	var builder strings.Builder
	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch {
		case ch == ' ':
			builder.WriteByte('+')
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') ||
			ch == '*' || ch == '-' || ch == '.' || ch == '_':
			builder.WriteByte(ch)
		default:
			fmt.Fprintf(&builder, "%%%02X", ch)
		}
	}
	return builder.String()
}

// gh7_path_starts_with mirrors `Path::starts_with` (component-aware).
func gh7_path_starts_with(path, root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if cleanPath == cleanRoot {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator))
}

func gh7_json_string(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	raw, ok := value[key]
	if !ok {
		return ""
	}
	if text, ok := raw.(string); ok {
		return text
	}
	return ""
}

func gh7_setting_key_allowed(key string) bool {
	for _, prefix := range []string{
		"libtorrent_", "delay_", "housekeeping_", "media_info_", "score_", "tvdb_",
		"trakt_", "simkl_", "backup_", "notify_", "jellyfin_", "plex_",
	} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	switch key {
	case "active", "refresh_interval", "url", "indexers", "websearch_engines", "blacklist",
		"content_filters", "max_release_age_days", "gap_fill_max_per_series", "gap_fill_max_per_cycle",
		"gap_filling", "gap_deep_interval_hours", "gap_deep_max_per_cycle", "flaresolverr_url",
		"tmdb_api_key", "tmdb_language", "default_language", "rename_episodes", "rename_format",
		"rename_template", "api_token", "archive_root", "trash_path", "libtorrent_dir",
		"libtorrent_temp_dir", "libtorrent_ramdisk_dir", "libtorrent_extra_settings",
		"cleanup_upgrades", "cleanup_min_score_diff", "upgrade_min_score_diff", "cleanup_action",
		"min_free_space_gb", "trash_retention_days", "archive_retention_days", "archive_cleanup_enabled",
		"archive_max_age_days", "archive_keep_min", "stop_on_old_page_threshold", "debug_enabled",
		"move_episodes", "rename_verify_interval", "auto_remove_completed", "telegram_bot_token",
		"telegram_chat_id", "email_smtp", "email_from", "email_to", "email_password",
		"torrent_backend", "qbittorrent_url", "qbittorrent_username", "qbittorrent_password",
		"qbittorrent_category", "qbittorrent_tag", "qbittorrent_request_timeout_secs",
		"qbittorrent_poll_interval_ms", "qbittorrent_path_mappings":
		return true
	}
	return false
}

// ArchiveEntries handles GET /api/archive.
func ArchiveEntries(w http.ResponseWriter, r *http.Request, s *AppState) {
	term := queryParam(r, "q")
	if term == "" {
		term = queryParam(r, "query")
	}
	page := int(queryInt(r, "page", 1))
	limit := int(queryInt(r, "limit", 200))
	result, err := s.archive.BrowsePage(term, page, limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":      true,
		"success": true,
		"items":   result.Items,
		"total":   result.Total,
		"page":    result.Page,
		"pages":   result.Pages,
	})
}

// BrowserHandlerDownload handles GET /api/browser-handlers/download.
func BrowserHandlerDownload(w http.ResponseWriter, r *http.Request, s *AppState) {
	host := r.Header.Get("Host")
	if host == "" {
		host = "127.0.0.1:5000"
	}
	base := "http://" + host
	requested := strings.TrimSpace(queryParam(r, "file"))

	var template, mime string
	switch requested {
	case "gextto-magnet":
		template, mime = gh7_handler_magnet, "text/x-shellscript"
	case "gextto-torrent":
		template, mime = gh7_handler_torrent, "text/x-shellscript"
	case "gextto-magnet.desktop":
		template, mime = gh7_handler_magnet_desktop, "text/plain"
	case "gextto-torrent.desktop":
		template, mime = gh7_handler_torrent_desktop, "text/plain"
	case "install.sh":
		template, mime = gh7_handler_install, "text/x-shellscript"
	default:
		jsonError(w, http.StatusBadRequest, "file non valido")
		return
	}
	body := strings.ReplaceAll(template, "__GEXTTO_URL__", base)
	w.Header().Set("Content-Type", mime+"; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", requested))
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// ComicCycle handles POST /api/comics/cycle.
func ComicCycle(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := NewGetComicsClient()
	defaultRoot := cfg.LibtorrentDir
	if s.cycle_lock != nil {
		s.cycle_lock.Lock()
		defer s.cycle_lock.Unlock()
	}
	count, err := RunComicsCycle(s.comics, client, s.notifier, defaultRoot, s.activeEngine(), s.db, cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "downloaded": count, "dry_run": cfg.DryRun})
}

// ComicLinks handles POST /api/comics/links.
func ComicLinksHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ComicLinksInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Url) == "" || len(input.Url) > 4096 {
		jsonError(w, http.StatusBadRequest, "post URL is required")
		return
	}
	links, err := NewGetComicsClient().Links(strings.TrimSpace(input.Url))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "links": links})
}

// Cycles handles GET /api/cycles (and the aliases routed to it).
func Cycles(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.RecentCycles(100)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "cycles": items})
}

// DeleteComicsHistory handles POST /api/comics/history/delete.
func DeleteComicsHistory(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ComicLinksInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Url) == "" {
		jsonError(w, http.StatusBadRequest, "post URL is required")
		return
	}
	deleted, err := s.comics.RemoveHistory(strings.TrimSpace(input.Url))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "deleted": deleted})
}

// ExportMagnet handles GET /api/torrents/{hash}/magnet.
func ExportMagnet(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	magnet := ""
	if s.db != nil {
		if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
			candidate := meta.Release.Magnet
			if strings.HasPrefix(candidate, "magnet:") {
				magnet = candidate
			}
		}
	}
	if magnet == "" {
		magnet = "magnet:?xt=urn:btih:" + hash
	}
	if s.torrents != nil {
		if trackers, ok, err := s.activeEngine().Trackers(hash); err == nil && ok {
			for _, tracker := range trackers {
				if !strings.Contains(magnet, tracker.URL) {
					magnet += "&tr=" + gh7_form_urlencode(tracker.URL)
				}
			}
		}
	}
	jsonResponse(w, map[string]any{"ok": true, "magnet": magnet})
}

// IpfilterUpdate handles POST /api/torrents/ipfilter_update.
func IpfilterUpdate(w http.ResponseWriter, r *http.Request, s *AppState) {
	if _, err := s.requireEmbedded("ip_filter"); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	cfg := latestConfig(s)
	target := strings.TrimSpace(cfg.Libtorrent.IpFilterPath)
	if target == "" {
		jsonError(w, http.StatusBadRequest, "Nessun IP filter configurato")
		return
	}
	localPath := target
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		data, fetchErr := gh7_fetch_ipfilter(r.Context(), target)
		if fetchErr != "" {
			jsonError(w, http.StatusBadGateway, fetchErr)
			return
		}
		path := filepath.Join(cfg.DataDir, "ipfilter.dat")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		localPath = path
	}
	rules, err := s.torrents.LoadIPFilter(localPath)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "rules": rules, "path": localPath})
}

// MagnetFeed handles GET /feed.xml (and /api/feed.xml).
func MagnetFeed(w http.ResponseWriter, r *http.Request, s *AppState) {
	entries, err := s.archive.RecentEntries(1000)
	if err != nil {
		entries = nil
	}
	xml := gh7_build_magnet_feed(entries)
	_ = os.WriteFile(filepath.Join(s.cfg.DataDir, "gextto_magnet_feed.xml"), []byte(xml), 0o644)
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml))
}

// MoveTorrentStorage handles POST /api/torrents/{hash}/storage.
func MoveTorrentStorage(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input StoragePath
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Path) == "" || len(input.Path) > 4096 {
		jsonError(w, http.StatusBadRequest, "storage path is required")
		return
	}
	cfg := latestConfig(s)
	destination := strings.TrimSpace(input.Path)
	allowed := gh7_path_starts_with(destination, cfg.LibtorrentDir) ||
		(cfg.LibtorrentTempDir != nil && gh7_path_starts_with(destination, *cfg.LibtorrentTempDir)) ||
		(cfg.ArchiveRoot != nil && gh7_path_starts_with(destination, *cfg.ArchiveRoot))
	if !allowed {
		jsonError(w, http.StatusForbidden, "storage path must be inside a configured Gextto directory")
		return
	}
	name := hash
	for _, torrent := range s.activeEngine().List() {
		if strings.EqualFold(torrent.Hash, hash) {
			name = torrent.Name
			break
		}
	}
	target := filepath.Join(destination, name)
	logging.Info("manual torrent storage move requested",
		"hash", hash, "name", name, "destination", destination)
	if _, statErr := os.Stat(target); statErr == nil {
		// The destination already holds the payload: associate it (change the
		// save path and re-check) instead of failing. This turns a common case
		// into an advantage — the torrent seeds from the files already on disk.
		logging.Info("destination already contains the torrent data; associating existing files",
			"hash", hash, "name", name, "destination", destination, "target", target)
		associated, assocErr := s.activeEngine().AssociateStorage(hash, destination)
		if assocErr != nil {
			logging.Error("associate existing torrent data failed", "hash", hash, "name", name, "error", assocErr.Error())
			jsonError(w, http.StatusBadRequest, assocErr.Error())
			return
		}
		if !associated && !cfg.DryRun {
			jsonError(w, http.StatusConflict, "torrent unavailable")
			return
		}
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":         true,
			"associated": true,
			"path":       destination,
			"message":    "data già presente: torrent associato e in ricontrollo",
		})
		return
	}
	result, err := s.activeEngine().MoveStorage(hash, destination)
	switch {
	case err != nil:
		logging.Error("manual torrent storage move failed to start",
			"hash", hash, "name", name, "destination", destination, "error", err.Error())
	case result:
		logging.Info("manual torrent storage move accepted by libtorrent; waiting for completion",
			"hash", hash, "name", name, "destination", destination)
	default:
		logging.Warn("manual torrent storage move not started: torrent session unavailable",
			"hash", hash, "name", name, "destination", destination)
	}
	gh7_torrent_action(w, result, err)
}

// NetworkInterfacesView handles GET /api/network/interfaces.
func NetworkInterfacesView(w http.ResponseWriter, r *http.Request, s *AppState) {
	interfaces := map[string]any{}
	for _, iface := range utils.NetworkInterfaces() {
		interfaces[iface.Name] = map[string]any{"ip": iface.IP, "type": iface.Kind}
	}
	jsonResponse(w, map[string]any{"ok": true, "interfaces": interfaces})
}

// ReannounceTorrent handles POST /api/torrents/{hash}/reannounce.
func ReannounceTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	result, err := s.activeEngine().Reannounce(hash)
	gh7_torrent_action(w, result, err)
}

// RemoveTorrent handles DELETE /api/torrents/{hash}.
func RemoveTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	gh7_blocklist_mismatched_pack(s.torrents, s.db, hash)
	deleteFiles := gh7_torrent_files_are_disposable(s.db, hash)
	removed, err := s.activeEngine().Remove(hash, deleteFiles)
	if err == nil && removed && s.db != nil {
		_ = s.db.MarkTorrentRemoved(hash)
		_ = s.db.ForgetRemovedTorrent(hash)
	}
	gh7_torrent_action(w, removed, err)
}

// ResumeTorrent handles POST /api/torrents/{hash}/resume.
func ResumeTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	result, err := s.activeEngine().Resume(hash)
	gh7_torrent_action(w, result, err)
}

// SaveSetting handles POST /api/config/settings.
func SaveSettingHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SettingInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !gh7_setting_key_allowed(input.Key) || len(input.Key) > 128 || len(input.Value) > 4096 {
		jsonError(w, http.StatusBadRequest, "setting is not writable through this endpoint")
		return
	}
	value := input.Value
	if input.Key == "indexers" {
		var indexers []IndexerConfig
		if err := json.Unmarshal([]byte(value), &indexers); err != nil {
			jsonError(w, http.StatusBadRequest, "indexers must be a JSON array")
			return
		}
		current := latestConfig(s)
		for i := range indexers {
			if indexers[i].APIKey != "" {
				continue
			}
			for _, old := range current.Indexers {
				if old.Name == indexers[i].Name && old.URL == indexers[i].URL {
					indexers[i].APIKey = old.APIKey
					break
				}
			}
		}
		encoded, err := json.Marshal(indexers)
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		value = string(encoded)
	}
	if len(value) > 4096 {
		jsonError(w, http.StatusBadRequest, "setting is too large")
		return
	}
	if err := saveConfigSetting(s.cfg.DataDir, input.Key, value); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "restart_required": true})
}

// SearchEpisode handles POST /api/episodes/{series}/{season}/{episode}/search.
func SearchEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	seriesName := pathParam(r, "series")
	season, okSeason := pathInt(r, "season")
	episode, okEpisode := pathInt(r, "episode")
	if !okSeason || !okEpisode {
		jsonError(w, http.StatusBadRequest, "invalid episode")
		return
	}
	cfg := latestConfig(s)
	series, ok := gh7_find_series(cfg, seriesName)
	if !ok {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	if gh7_contains_int64(series.IgnoredSeasons, season) ||
		!SeasonAllowedForScan(series.Seasons, season) {
		jsonError(w, http.StatusUnprocessableEntity, "season is not monitored")
		return
	}
	query := fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode)
	results := gh7_search_series_episode_sources(r.Context(), s, cfg, series, season, episode)
	feedMatches := 0
	for _, result := range results {
		if result.Origin == "Feed RSS" {
			feedMatches++
		}
	}
	_ = s.db.MarkGapSearched(series.Name, season, episode)
	jsonResponse(w, map[string]any{
		"ok":           true,
		"query":        query,
		"results":      results,
		"feed_matches": feedMatches,
	})
}

// SeriesMetadataRefresh handles POST /api/series/{name}/metadata.
func SeriesMetadataRefresh(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	live := latestConfig(s)
	cfg := *live
	cfg.Series = append([]SeriesConfig(nil), live.Series...)
	cfg.Movies = append([]MovieConfig(nil), live.Movies...)

	series, ok := gh7_find_series(&cfg, name)
	if !ok {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	if cfg.TmdbAPIKey == nil {
		jsonError(w, http.StatusConflict, "TMDB API key is not configured")
		return
	}
	ctx := r.Context()
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	resolved := strings.TrimSpace(series.TmdbID)
	if resolved == "" {
		id, err := tmdb.ResolveSeriesID(ctx, series.Name)
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		if id == nil {
			jsonError(w, http.StatusNotFound, "series not found on TMDB")
			return
		}
		resolved = *id
	}
	counts, err := tmdb.SeasonCounts(ctx, resolved)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	values := make([][2]int64, 0, len(counts))
	for season, count := range counts {
		values = append(values, [2]int64{season, count})
	}
	sort.Slice(values, func(i, j int) bool { return values[i][0] < values[j][0] })

	airDates := []EpisodeAirDate{}
	airDateErrors := 0
	for _, value := range values {
		season := value[0]
		if season <= 0 {
			continue
		}
		episodes, err := tmdb.SeasonEpisodes(ctx, resolved, season)
		if err != nil {
			airDateErrors++
			logging.Warn("TMDB season air-date refresh failed",
				"series", series.Name, "season", season, "error", err.Error())
			continue
		}
		for _, episode := range episodes {
			episodeSeason := season
			if episode.SeasonNumber != nil {
				episodeSeason = *episode.SeasonNumber
			}
			if episode.EpisodeNumber == nil || episodeSeason != season {
				continue
			}
			airDate := ""
			if episode.AirDate != nil {
				airDate = *episode.AirDate
			}
			airDates = append(airDates, EpisodeAirDate{Season: season, Episode: *episode.EpisodeNumber, AirDate: airDate})
		}
	}
	if err := s.db.SaveSeriesMetadata(series.Name, values); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if info, err := tmdb.SeriesInfo(ctx, series.Name, &resolved); err == nil && info != nil {
		if err := s.db.SaveSeriesStatus(series.Name, gh7_json_string(info, "status"), gh7_json_string(info, "last_air_date")); err != nil {
			logging.Warn("series status save failed", "series", series.Name, "error", err.Error())
		}
	}
	stored := false
	if strings.TrimSpace(series.TmdbID) == "" {
		for i := range cfg.Series {
			item := &cfg.Series[i]
			aliasMatch := false
			for _, alias := range item.Aliases {
				if alias == name {
					aliasMatch = true
					break
				}
			}
			if item.Name == series.Name || aliasMatch {
				item.TmdbID = resolved
				stored = true
			}
		}
		if stored {
			_ = SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies)
		}
	}
	if err := s.db.SaveEpisodeAirDates(series.Name, airDates); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	seasons := make([]map[string]any, 0, len(values))
	for _, value := range values {
		seasons = append(seasons, map[string]any{"season": value[0], "episodes": value[1]})
	}
	jsonResponse(w, map[string]any{
		"ok":                true,
		"series":            series.Name,
		"tmdb_id":           resolved,
		"tmdb_id_stored":    stored,
		"air_dates_updated": len(airDates),
		"air_date_errors":   airDateErrors,
		"seasons":           seasons,
	})
}

// SetComicEnabled handles POST /api/comics/{id}/enabled.
func SetComicEnabled(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var input ComicEnabled
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.comics.SetEnabled(id, input.Enabled)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !updated {
		jsonError(w, http.StatusNotFound, "comic not found")
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// SetTorrentLimits handles POST /api/torrents/{hash}/limits.
func SetTorrentLimits(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input TorrentLimits
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	seedRatio := -1.0
	if input.SeedRatio != nil {
		seedRatio = *input.SeedRatio
	}
	seedDays := int64(-1)
	if input.SeedDays != nil {
		seedDays = *input.SeedDays
	}
	result, err := s.activeEngine().SetLimits(hash, input.DownloadLimit, input.UploadLimit, seedRatio, seedDays)
	if err != nil {
		gh7_torrent_action(w, result, err)
		return
	}
	if input.MaxConnections != nil || input.MaxUploads != nil {
		maxConnections, maxUploads := int64(-1), int64(-1)
		if input.MaxConnections != nil {
			maxConnections = *input.MaxConnections
		}
		if input.MaxUploads != nil {
			maxUploads = *input.MaxUploads
		}
		if maxConnections >= 0 {
			if _, err := s.activeEngine().SetMaxConnections(hash, int(maxConnections)); err != nil {
				jsonError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if maxUploads >= 0 {
			if _, err := s.activeEngine().SetMaxUploads(hash, int(maxUploads)); err != nil {
				jsonError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if s.torrent_engine == nil {
			if err := s.torrents.SaveConnLimits(hash, maxConnections, maxUploads); err != nil {
				logging.Warn("cannot persist torrent connection limits", "hash", hash, "error", err)
			}
		}
	}
	gh7_torrent_action(w, result, err)
}

// SimklAuthPoll handles POST /api/simkl/auth/poll.
func SimklAuthPoll(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input AuthCode
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not save integration tokens")
		return
	}
	cfg := latestConfig(s)
	value, err := (&SimklClient{}).FromSettings(cfg.Settings).PinPoll(r.Context(), input.Code)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	access, err := TokenString(value, "access_token")
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{
			"ok":    false,
			"error": "Simkl response did not contain an access token",
			"data":  value,
		})
		return
	}
	if err := saveConfigSetting(cfg.DataDir, "simkl_access_token", access); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "authenticated": true})
}

// SimklWatchlistImport handles POST /api/simkl/watchlist/import.
func SimklWatchlistImport(w http.ResponseWriter, r *http.Request, s *AppState) {
	live := latestConfig(s)
	cfg := *live
	cfg.Series = append([]SeriesConfig(nil), live.Series...)
	cfg.Movies = append([]MovieConfig(nil), live.Movies...)

	client := (&SimklClient{}).FromSettings(cfg.Settings)
	value, err := client.Watchlist(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	entries := [][3]string{}
	gh7_collect_watchlist(value, &entries)
	report := gh7_apply_watchlist_import(&cfg, entries)
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":     true,
		"source": "simkl",
		"found":  len(entries),
		"report": report,
	})
}

// TmdbSearch handles POST /api/tmdb/search.
func TmdbSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TmdbQuery
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	if cfg.TmdbAPIKey == nil {
		jsonError(w, http.StatusConflict, "TMDB API key is not configured")
		return
	}
	if input.Kind == "" {
		input.Kind = "series"
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, "query must contain 1-256 characters")
		return
	}
	isMovie := strings.EqualFold(input.Kind, "movie")
	tmdb := NewTmdbClient(cfg.TmdbAPIKey)

	ctx, cancel := context.WithTimeout(r.Context(), gh7_external_search_timeout)
	defer cancel()
	var items []TmdbItem
	var err error
	if isMovie {
		items, err = tmdb.SearchMovies(ctx, query)
	} else {
		items, err = tmdb.SearchSeries(ctx, query)
	}
	if ctx.Err() == context.DeadlineExceeded {
		jsonError(w, http.StatusGatewayTimeout,
			fmt.Sprintf("TMDB non ha risposto entro %ds", int(gh7_external_search_timeout.Seconds())))
		return
	}
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if len(items) > 0 {
		values := make([]any, 0, len(items))
		for _, item := range items {
			encoded, _ := json.Marshal(item)
			var mapped map[string]any
			_ = json.Unmarshal(encoded, &mapped)
			mapped["external"] = "tmdb"
			values = append(values, mapped)
		}
		jsonResponse(w, map[string]any{"ok": true, "kind": input.Kind, "items": values, "source": "tmdb"})
		return
	}
	if isMovie {
		jsonResponse(w, map[string]any{"ok": true, "kind": input.Kind, "items": []any{}, "source": "tmdb"})
		return
	}

	// Fallback TVDB (series only) when TMDB has no results.
	tvdb := WithLanguage(cfg.TvdbAPIKey(), cfg.TvdbLanguage())
	tvdbItems := []any{}
	tctx, tcancel := context.WithTimeout(r.Context(), gh7_external_search_timeout)
	defer tcancel()
	raw, tvdbErr := tvdb.SearchSeries(tctx, query)
	if tctx.Err() == context.DeadlineExceeded {
		logging.Debug("TVDB fallback search timed out")
	} else if tvdbErr != nil {
		logging.Debug("TVDB fallback search failed", "error", tvdbErr.Error())
	} else {
		for _, item := range raw {
			mapped, ok := item.(map[string]any)
			if !ok {
				continue
			}
			tvdbItems = append(tvdbItems, map[string]any{
				"id":             0,
				"name":           mapped["name"],
				"overview":       mapped["overview"],
				"poster":         mapped["image"],
				"first_air_date": mapped["year"],
				"external":       "tvdb",
				"tvdb_id":        mapped["tvdb_id"],
			})
		}
	}
	source := "tmdb"
	if len(tvdbItems) > 0 {
		source = "tvdb"
	}
	jsonResponse(w, map[string]any{"ok": true, "kind": "series", "items": tvdbItems, "source": source})
}

// TorrentPeersLegacy handles POST /api/torrents/peers.
func TorrentPeersLegacy(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TorrentHashInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	legacy := r.Clone(r.Context())
	legacy.URL.Path = "/api/torrents/" + input.Hash + "/peers"
	legacy.URL.RawPath = ""
	legacy.SetPathValue("hash", input.Hash)
	TorrentPeers(w, legacy, s)
}

// TraktAuthStart handles POST /api/trakt/auth/start.
func TraktAuthStart(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := (&TraktClient{}).FromSettings(cfg.Settings)
	if !client.Configured() {
		jsonError(w, http.StatusConflict, "Trakt non configurato")
		return
	}
	value, err := client.DeviceStart(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "data": value})
}

// TvdbSearch handles POST /api/tvdb/search.
func TvdbSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	// TvdbQuery is not declared by web.go; decode the single field into an
	// anonymous struct so no package-level type is (re)defined here.
	var input struct {
		Query string `json:"query"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	client := WithLanguage(cfg.TvdbAPIKey(), cfg.TvdbLanguage())
	if !client.Configured() {
		jsonError(w, http.StatusPreconditionRequired, "TVDB API key non configurata")
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, "query must contain 1-256 characters")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gh7_external_search_timeout)
	defer cancel()
	results, err := client.SearchSeries(ctx, query)
	if ctx.Err() == context.DeadlineExceeded {
		jsonError(w, http.StatusGatewayTimeout,
			fmt.Sprintf("TVDB non ha risposto entro %ds", int(gh7_external_search_timeout.Seconds())))
		return
	}
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "results": results})
}
