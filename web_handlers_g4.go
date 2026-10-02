package gextto

// web_handlers_g4.go ports the `group g4` HTTP handlers from gextto's
// the daemon. Handler signatures follow the web.go contract:
//
//	func Name(w http.ResponseWriter, r *http.Request, s *AppState)
//
// Shared helpers and core input structs live in web.go. Only file-local
// private helpers/types are declared here and prefixed `gh4_`.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// ---------------------------------------------------------------------------
// File-local private input types ( structs declared outside the web module lines
// 372-955, so they are not part of the core web.go input set).
// ---------------------------------------------------------------------------

type gh4_KeywordPruneInput struct {
	Keyword  string   `json:"keyword"`
	Keywords []string `json:"keywords"`
	Preview  bool     `json:"preview"`
}

// gh4_historyDisplayName keeps provider suffixes out of the history title when
// the same provider is already exposed in the dedicated source column.
func gh4_historyDisplayName(name, source string) string {
	name = strings.TrimSpace(name)
	source = strings.TrimSpace(source)
	if name == "" || source == "" {
		return name
	}
	labels := []string{source}
	if parts := strings.Split(source, ":"); len(parts) > 1 {
		labels = append(labels, parts[len(parts)-1])
	}
	for _, part := range strings.Split(source, " - ") {
		labels = append(labels, part)
	}
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

type gh4_WebSeedsInput struct {
	Urls   []string `json:"urls"`
	Remove bool     `json:"remove"`
}

type gh4_SpeedLimitsInput struct {
	DownloadKib int64 `json:"download_kib"`
	UploadKib   int64 `json:"upload_kib"`
}

// gh4_truthy ports the `matches!(value, "yes" | "true" | "1")` setting check.
func gh4_truthy(value string) bool {
	return value == "yes" || value == "true" || value == "1"
}

// ---------------------------------------------------------------------------
// Small private helpers based on the web module.
// ---------------------------------------------------------------------------

// gh4_findSeries mirrors the web module `find_series`: exact name or alias match.
func gh4_findSeries(cfg *Config, name string) *SeriesConfig {
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

// gh4_purgeRemovedLibrary mirrors the web module `purge_removed_library`.
func gh4_purgeRemovedLibrary(db *Database, previousSeries []SeriesConfig, previousMovies []MovieConfig, series []SeriesConfig, movies []MovieConfig) {
	for _, old := range previousSeries {
		present := false
		for _, item := range series {
			if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) {
				present = true
				break
			}
		}
		if present {
			continue
		}
		if _, err := db.PurgeSeries(old.Name); err != nil {
			logging.Warn("impossibile rimuovere lo stato della serie eliminata", "error", err.Error(), "series", old.Name)
		}
	}
	for _, old := range previousMovies {
		present := false
		if old.ID > 0 {
			for _, item := range movies {
				if item.ID == old.ID {
					present = true
					break
				}
			}
		} else {
			for _, item := range movies {
				if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(old.Name)) &&
					strings.TrimSpace(item.Year) == strings.TrimSpace(old.Year) {
					present = true
					break
				}
			}
		}
		if present {
			continue
		}
		if _, err := db.PurgeMovie(old.Name); err != nil {
			logging.Warn("impossibile rimuovere lo stato del film eliminato", "error", err.Error(), "movie", old.Name)
		}
	}
}

// gh4_seenEntries mirrors the web module `seen_entries`.
func gh4_seenEntries(s *AppState, kind string, r *http.Request) (int, any) {
	group := queryParam(r, "key")
	if group == "" {
		group = queryParam(r, "group")
	}
	if strings.TrimSpace(group) == "" {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": "key required"}
	}
	limit := int(queryInt(r, "limit", 500))
	items, err := s.db.SeenByGroup(kind, strings.TrimSpace(group), limit)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
	}
	return http.StatusOK, map[string]any{"ok": true, "items": items}
}

// gh4_storedDownloadTags mirrors the web module `stored_download_tags`.
func gh4_storedDownloadTags(cfg *Config) []string {
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

// gh4_xmlAttribute mirrors the web module `xml_attribute`.
func gh4_xmlAttribute(body, name string) *string {
	needle := name + "=\""
	start := strings.Index(body, needle)
	if start < 0 {
		return nil
	}
	rest := body[start+len(needle):]
	end := strings.Index(rest, "\"")
	if end < 0 {
		return nil
	}
	value := rest[:end]
	return &value
}

// gh4_mediaTestError mirrors the web module `media_test_error`.
func gh4_mediaTestError(provider string, status int) string {
	switch status {
	case 401, 403:
		return fmt.Sprintf("%s: credenziali non valide", provider)
	case 404:
		return fmt.Sprintf("%s: endpoint non trovato, controlla l'URL", provider)
	default:
		return fmt.Sprintf("%s: HTTP %d", provider, status)
	}
}

// gh4_versionParts mirrors the web module `version_parts`.
func gh4_versionParts(value string) []int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "v")
	fields := strings.FieldsFunc(trimmed, func(character rune) bool {
		return character < '0' || character > '9'
	})
	parts := []int{}
	for _, field := range fields {
		if field == "" {
			continue
		}
		if parsed, err := strconv.Atoi(field); err == nil {
			parts = append(parts, parsed)
		}
	}
	return parts
}

// gh4_versionIsNewer mirrors the web module `version_is_newer`.
func gh4_versionIsNewer(candidate, current string) bool {
	left := gh4_versionParts(candidate)
	right := gh4_versionParts(current)
	for len(left) < len(right) {
		left = append(left, 0)
	}
	for len(right) < len(left) {
		right = append(right, 0)
	}
	for i := range left {
		if left[i] != right[i] {
			return left[i] > right[i]
		}
	}
	return false
}

// gh4_fetchLatestLibtorrentRelease mirrors the web module `fetch_latest_libtorrent_release`.
func gh4_fetchLatestLibtorrentRelease() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	request, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/arvidn/libtorrent/releases/latest", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "gextto-libtorrent-check")
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub ha risposto %s", response.Status)
	}
	var value map[string]any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		return "", err
	}
	tag, _ := value["tag_name"].(string)
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if tag == "" {
		return "", fmt.Errorf("risposta GitHub senza tag_name")
	}
	return tag, nil
}

// gh4_libtorrentAptCandidate mirrors the web module `libtorrent_apt_candidate`.
func gh4_libtorrentAptCandidate() *string {
	for _, packageName := range []string{
		"libtorrent-rasterbar-dev",
		"libtorrent-rasterbar2.0t64",
		"libtorrent-rasterbar2.0",
	} {
		command := exec.Command("apt-cache", "policy", packageName)
		command.Env = append(os.Environ(), "LC_ALL=C")
		output, err := command.Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(output), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "Candidate:") {
				continue
			}
			candidate := strings.TrimSpace(strings.TrimPrefix(trimmed, "Candidate:"))
			if candidate != "" && candidate != "(none)" {
				value := candidate
				return &value
			}
		}
	}
	return nil
}

// gh4_torrentDisplayName mirrors the web module `torrent_display_name`.
func gh4_torrentDisplayName(torrents TorrentSession, hash string) string {
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

// gh4_removeFailedTorrent mirrors the web module `remove_failed_torrent`.
func gh4_removeFailedTorrent(torrents TorrentSession, hash string) bool {
	name := gh4_torrentDisplayName(torrents, hash)
	incomplete := false
	for _, torrent := range torrents.List() {
		if strings.EqualFold(torrent.Hash, hash) {
			incomplete = torrent.Progress < 99.99 && torrent.State != "seeding" && torrent.State != "finished"
			break
		}
	}
	removed, err := torrents.Remove(hash, incomplete)
	if err != nil {
		logging.Warn("failed torrent removal failed", "hash", hash, "name", name, "error", err.Error())
		return false
	}
	if removed {
		logging.Info("failed download removed from the session", "name", name)
	} else {
		logging.Debug("failed torrent already removed", "hash", hash, "name", name)
	}
	return true
}

// ---------------------------------------------------------------------------
// Archive scan helpers (the web module `parse_season_episode`, `scan_archive_path`).
// ---------------------------------------------------------------------------

var gh4_seasonEpisodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bs(?:tagione)?\s*(\d{1,2})\s*e(?:p(?:isodio)?)?\.?\s*(\d{1,4})`),
	regexp.MustCompile(`(?i)\b(\d{1,2})\s*x\s*(\d{1,4})`),
	regexp.MustCompile(`(?i)stagione\s*(\d{1,2}).*?episodio\s*(\d{1,4})`),
}

// gh4_parseSeasonEpisode mirrors the web module `parse_season_episode`.
func gh4_parseSeasonEpisode(name string) (int64, int64, bool) {
	for _, pattern := range gh4_seasonEpisodePatterns {
		match := pattern.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		season, errSeason := strconv.ParseInt(match[1], 10, 64)
		episode, errEpisode := strconv.ParseInt(match[2], 10, 64)
		if errSeason == nil && errEpisode == nil {
			return season, episode, true
		}
	}
	return 0, 0, false
}

// gh4_scanArchivePath mirrors the web module `scan_archive_path`.
func gh4_scanArchivePath(db *Database, series *SeriesConfig, path string, cfg *Config) (int, int, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return 0, 0, fmt.Errorf("archive path is not a directory: %s", path)
	}
	files, err := VideoFiles(path)
	if err != nil {
		return 0, 0, err
	}
	found := 0
	updated := 0
	for _, file := range files {
		name := filepath.Base(file)
		season, episode, ok := gh4_parseSeasonEpisode(name)
		if !ok || season <= 0 || episode <= 0 {
			continue
		}
		// Never register a broken file as the archived copy.
		if invalidErr := validateCompletedFile(file); invalidErr != nil {
			logging.Warn("archive scan skipped an invalid video file",
				"path", file, "error", invalidErr.Error())
			continue
		}
		found++
		var size int64
		if fileInfo, statErr := os.Stat(file); statErr == nil {
			size = fileInfo.Size()
		}
		title := strings.TrimSuffix(name, filepath.Ext(name))
		if strings.TrimSpace(title) == "" {
			title = name
		}
		qualityScore := cfg.FileScore(file, "series", series.Name)
		if syncErr := db.SyncArchiveFileScored(series.Name, season, episode, title, file, size, qualityScore); syncErr != nil {
			return found, updated, syncErr
		}
		updated++
	}
	return found, updated, nil
}

// gh4_archiveImportBusyContains reports whether a series archive is currently
// being written by a post-processing run (shared `archiveImportBusy` set).
func gh4_archiveImportBusyContains(name string) bool {
	archiveImportBusyMu.Lock()
	defer archiveImportBusyMu.Unlock()
	_, ok := archiveImportBusy[name]
	return ok
}

// ---------------------------------------------------------------------------
// Add-release helpers (the web module `add_release`, `add_raw_magnet`, `add_parsed_release`,
// `download_and_add`, `resolve_by_search` and their helpers).
// ---------------------------------------------------------------------------

// gh4_addRelease mirrors the web module `add_release`.
func gh4_addRelease(s *AppState, release models.Release) (int, any) {
	if !SetupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	isURL := gh4_isTorrentURL(release.Magnet)
	status, value := gh4_addParsedRelease(s, release)
	if isURL && status == http.StatusBadRequest {
		if found := gh4_resolveBySearch(s, release.Title); found != nil {
			retryStatus, retryValue := gh4_addParsedRelease(s, *found)
			if retryStatus != http.StatusBadRequest {
				return retryStatus, retryValue
			}
		}
	}
	return status, value
}

// gh4_addParsedRelease mirrors the web module `add_parsed_release`.
func gh4_addParsedRelease(s *AppState, release models.Release) (int, any) {
	source := strings.TrimSpace(release.Magnet)
	if gh4_isTorrentURL(source) {
		hash, err := gh4_downloadAndAdd(s, source, AddOptions{})
		if err != nil {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
		}
		if hash == nil {
			return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
		}
		release.Magnet = "magnet:?xt=urn:btih:" + *hash
		score := latestConfig(s).ReleaseScore(&release)
		if err := s.db.RegisterTorrentScored(&release, score); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
		_ = s.db.SetTorrentReason(*hash, "manual")
		return http.StatusAccepted, map[string]any{"ok": true, "title": release.Title}
	}
	score := latestConfig(s).ReleaseScore(&release)
	if err := s.db.RegisterTorrentScored(&release, score); err != nil {
		return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
	}
	if hash, ok := utils.MagnetHash(release.Magnet); ok {
		_ = s.db.SetTorrentReason(hash, "manual")
	}
	title := release.Title
	go func() {
		added, err := s.activeEngine().Add(source, s.cfg)
		switch {
		case err != nil:
			logging.Error("manual torrent add failed", "title", title, "error", err.Error())
		case added:
			logging.Info("manual torrent queued", "title", title)
		default:
			logging.Info("manual torrent already queued", "title", title)
		}
	}()
	return http.StatusAccepted, map[string]any{"ok": true, "title": title, "queued": true}
}

// gh4_isTorrentURL mirrors the web module `is_torrent_url`.
func gh4_isTorrentURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// gh4_downloadAndAdd mirrors the web module `download_and_add`.
func gh4_downloadAndAdd(s *AppState, rawURL string, options AddOptions) (*string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "gextto/0.1")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.cfg.StateDir, fmt.Sprintf(".manual-%s.torrent", gh4_uuid()))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return nil, err
	}
	hash, err := s.activeEngine().AddTorrentFileWithOptions(path, s.cfg, nil, options)
	_ = os.Remove(path)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// gh4_resolveBySearch mirrors the web module `resolve_by_search`.
func gh4_resolveBySearch(s *AppState, title string) *models.Release {
	query := gh4_searchQueryFromTitle(title)
	normalized := gh4_normalizeSearch(query)
	if normalized == "" {
		return nil
	}
	results := s.engine.SearchQueryManual(context.Background(), s.cfg, query)
	for i := range results {
		release := &results[i]
		if gh4_sourceIsUsable(release.Magnet) && strings.HasPrefix(gh4_normalizeSearch(release.Title), normalized) {
			return release
		}
	}
	return nil
}

// gh4_searchQueryFromTitle mirrors the web module `search_query_from_title`.
func gh4_searchQueryFromTitle(title string) string {
	head := title
	if index := strings.Index(title, "["); index >= 0 {
		head = title[:index]
	}
	cleaned := make([]rune, 0, len(head))
	for _, character := range head {
		if (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') {
			cleaned = append(cleaned, character)
		} else {
			cleaned = append(cleaned, ' ')
		}
	}
	markers := map[string]struct{}{
		"4k": {}, "uhd": {}, "bluray": {}, "bdrip": {}, "web": {}, "webdl": {},
		"webrip": {}, "hdtv": {}, "dvdrip": {}, "h264": {}, "h265": {}, "x264": {},
		"x265": {}, "hevc": {}, "avc": {}, "aac": {}, "ac3": {}, "eac3": {},
		"ddp": {}, "dts": {}, "proper": {}, "repack": {},
	}
	words := []string{}
	for _, word := range strings.Fields(string(cleaned)) {
		lower := strings.ToLower(word)
		isYear := len(word) == 4 && gh4_allASCIIDigits(word)
		_, isMarker := markers[lower]
		if !isMarker {
			isMarker = strings.Contains(lower, "1080p") ||
				strings.Contains(lower, "720p") ||
				strings.Contains(lower, "2160p") ||
				strings.Contains(lower, "480p")
		}
		if isMarker {
			break
		}
		words = append(words, word)
		if isYear || len(words) >= 6 {
			break
		}
	}
	return strings.Join(words, " ")
}

// gh4_allASCIIDigits reports whether every character is an ASCII digit.
func gh4_allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// gh4_normalizeSearch mirrors the web module `normalize_search`.
func gh4_normalizeSearch(value string) string {
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

// gh4_sourceIsUsable mirrors the web module `source_is_usable`.
func gh4_sourceIsUsable(source string) bool {
	source = strings.TrimSpace(source)
	if gh4_isTorrentURL(source) {
		return true
	}
	if !strings.HasPrefix(source, "magnet:") {
		return false
	}
	_, ok := utils.MagnetHash(source)
	return ok
}

// gh4_addRawMagnet mirrors the web module `add_raw_magnet`.
func gh4_addRawMagnet(s *AppState, source string) (int, any) {
	if !SetupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	source = strings.TrimSpace(source)
	if gh4_isTorrentURL(source) {
		hash, err := gh4_downloadAndAdd(s, source, AddOptions{})
		if err != nil {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
		}
		if hash == nil {
			return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
		}
		return http.StatusAccepted, map[string]any{"ok": true}
	}
	added, err := s.activeEngine().Add(source, s.cfg)
	if err != nil {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
	}
	if !added {
		return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
	}
	return http.StatusAccepted, map[string]any{"ok": true}
}

// gh4_uuid returns a random RFC 4122 version 4 UUID string.
func gh4_uuid() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

// gh4_derefString returns the pointed-to string or "".
func gh4_derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ---------------------------------------------------------------------------
// Handlers.
// ---------------------------------------------------------------------------

// AddTorrent implements `add_torrent`. NOTE: web.go currently declares an input
// struct named `AddTorrent`, which collides with this handler; the struct must
// be renamed (e.g. `AddTorrentInput`) for the package to build.
func AddTorrentHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	if !SetupComplete(s.cfg) {
		jsonError(w, http.StatusConflict, "complete the initial setup first")
		return
	}
	var input AddTorrentInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	preallocate := cfg.LibtorrentPreallocate()
	if input.Preallocate != nil {
		preallocate = *input.Preallocate
	}
	options := AddOptions{
		Paused:         input.StartPaused,
		Sequential:     input.Sequential,
		SeedMode:       input.SeedMode,
		QueueTop:       input.QueueTop,
		FirstLast:      input.FirstLast,
		StopAtMetadata: input.StopAtMetadata,
		Preallocate:    preallocate,
	}
	var preferred *string
	if input.SavePath != nil {
		trimmed := strings.TrimSpace(*input.SavePath)
		if trimmed != "" {
			preferred = &trimmed
		}
	}
	added, err := s.activeEngine().AddWithOptions(input.Magnet, cfg, preferred, options)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !added {
		jsonError(w, http.StatusConflict, "duplicate")
		return
	}
	if input.NoRename {
		if hash, ok := utils.MagnetHash(input.Magnet); ok {
			_ = s.db.SetTorrentNoRename(hash, true)
		}
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true})
}

// BatchArchiveDownload implements `batch_archive_download`.
func BatchArchiveDownload(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ArchiveBatchInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Items) == 0 || len(input.Items) > 100 {
		jsonError(w, http.StatusBadRequest, "batch must contain 1-100 releases")
		return
	}
	accepted := 0
	rejected := []map[string]any{}
	for _, item := range input.Items {
		source := "archive"
		if strings.TrimSpace(item.Source) != "" {
			source = item.Source
		}
		release := ParseRelease(item.Title, item.Magnet, source)
		var status int
		if release == nil {
			status, _ = gh4_addRawMagnet(s, item.Magnet)
		} else {
			status, _ = gh4_addRelease(s, *release)
		}
		if (status >= 200 && status < 300) || status == http.StatusAccepted {
			accepted++
		} else {
			rejected = append(rejected, map[string]any{"title": item.Title, "status": status})
		}
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "accepted": accepted, "rejected": rejected})
}

// CleanupDatabase implements `cleanup_database`.
func CleanupDatabase(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not clean database")
		return
	}
	cfg := latestConfig(s)
	archiveDays := int64(0)
	archiveDaysSet := false
	if value, ok := cfg.Settings["archive_max_age_days"]; ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			archiveDays = parsed
			archiveDaysSet = true
		}
	}
	if !archiveDaysSet {
		// Legacy fallback: `archive_retention_days` was the original name of
		// `archive_max_age_days`. Kept only so an old database still works; it is
		// no longer exposed in the settings UI.
		if value, ok := cfg.Settings["archive_retention_days"]; ok {
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
				archiveDays = parsed
			}
		}
	}
	archiveKeepMin := int64(0)
	if value, ok := cfg.Settings["archive_keep_min"]; ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			archiveKeepMin = parsed
		}
	}
	archiveCleanupEnabled := false
	if value, ok := cfg.Settings["archive_cleanup_enabled"]; ok {
		archiveCleanupEnabled = gh4_truthy(value)
	}
	archiveRemoved := 0
	if archiveCleanupEnabled && archiveDays > 0 {
		archiveRemoved, _ = s.archive.CleanupOlderThanKeeping(archiveDays, archiveKeepMin)
	}
	report, err := s.db.Cleanup(1000, 30)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "report": report, "archive_removed": archiveRemoved})
}

// ComicDownloadTag implements `comic_download_tag`.
func ComicDownloadTag(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ComicDownloadTagInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Id) == "" {
		jsonError(w, http.StatusBadRequest, "download id is required")
		return
	}
	if SetHTTPDownloadTag(strings.TrimSpace(input.Id), strings.TrimSpace(input.Tag)) {
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "id": input.Id})
		return
	}
	jsonError(w, http.StatusNotFound, "download not found")
}

// ComicsWeekly implements `comics_weekly`.
func ComicsWeekly(w http.ResponseWriter, r *http.Request, s *AppState) {
	enabledRaw, _ := s.comics.Setting("weekly_enabled", "no")
	enabled := gh4_truthy(enabledRaw)
	fromDate, _ := s.comics.Setting("weekly_from_date", "")
	checkIntervalSecs := int64(604800)
	if value, err := s.comics.Setting("comics_check_interval", "604800"); err == nil {
		if parsed, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil {
			checkIntervalSecs = parsed
		}
	}
	if checkIntervalSecs < 0 {
		checkIntervalSecs = 0
	}
	lastCheckTs := int64(0)
	if value, err := s.comics.Setting("last_comics_check_ts", "0"); err == nil {
		if parsed, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil {
			lastCheckTs = parsed
		}
	}
	if lastCheckTs < 0 {
		lastCheckTs = 0
	}
	items, err := s.comics.Weekly(s.comics.HistoryLimit())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"weekly_enabled":      enabled,
		"weekly_from_date":    fromDate,
		"items":               items,
		"check_interval_secs": checkIntervalSecs,
		"last_check_ts":       lastCheckTs,
		"history_limit":       s.comics.HistoryLimit(),
	})
}

// DbPruneKeyword implements `db_prune_keyword`.
func DbPruneKeyword(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input gh4_KeywordPruneInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keywords := input.Keywords
	if len(keywords) == 0 {
		split := strings.FieldsFunc(input.Keyword, func(character rune) bool {
			return character == ',' || character == ';' || character == '|' || character == '\n'
		})
		for _, value := range split {
			keywords = append(keywords, strings.TrimSpace(value))
		}
	}
	kept := keywords[:0]
	for _, value := range keywords {
		if strings.TrimSpace(value) != "" && len(value) <= 128 {
			kept = append(kept, value)
		}
	}
	keywords = kept
	if len(keywords) == 0 || len(keywords) > 64 {
		jsonError(w, http.StatusBadRequest, "inserire da 1 a 64 parole, ciascuna di massimo 128 caratteri")
		return
	}
	if input.Preview {
		count, err := s.db.CountKeywords(keywords)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items, err := s.db.SearchKeywords(keywords, 300)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":       true,
			"preview":  true,
			"count":    count,
			"keywords": keywords,
			"items":    items,
		})
		return
	}
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not modify the database")
		return
	}
	removed, err := s.db.PruneKeywords(keywords)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "removed": removed, "keywords": keywords})
}

// DownloadTagsList implements `download_tags_list`.
func DownloadTagsList(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": gh4_storedDownloadTags(cfg)})
}

// Gaps implements `gaps`.
func Gaps(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.ArchiveGaps()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg := latestConfig(s)
	airDates, _ := s.db.EpisodeAirDates()
	result := []map[string]any{}
	for _, gap := range items {
		season := gap.Season
		if cfg.FindSeriesMatch(gap.Series, &season) == nil {
			continue
		}
		airDate := airDates[airDateKey{Series: gap.Series, Season: gap.Season, Episode: gap.Episode}]
		result = append(result, map[string]any{
			"series":   gap.Series,
			"season":   gap.Season,
			"episode":  gap.Episode,
			"air_date": airDate,
		})
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "count": len(result), "items": result})
}

// LibtorrentCheckUpdate implements `libtorrent_check_update`.
func LibtorrentCheckUpdate(w http.ResponseWriter, r *http.Request, s *AppState) {
	installed := LibtorrentVersion()
	latest, err := gh4_fetchLatestLibtorrentRelease()
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{
			"ok":        false,
			"installed": installed,
			"error":     err.Error(),
		})
		return
	}
	aptCandidate := gh4_libtorrentAptCandidate()
	updateAvailable := gh4_versionIsNewer(latest, installed)
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":               true,
		"installed":        installed,
		"latest":           latest,
		"apt_candidate":    aptCandidate,
		"update_available": updateAvailable,
		"releases_url":     "https://github.com/arvidn/libtorrent/releases",
	})
}

// MarkTorrentFailed implements `mark_torrent_failed`.
func MarkTorrentFailed(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	metadata, err := s.db.TorrentMeta(hash)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if metadata != nil {
		_ = s.db.Blocklist(&metadata.Release, "manual_failed")
	}
	restored, _ := s.db.RestoreUpgrade(hash)
	_ = s.db.MarkTorrentError(hash, "manual failure")
	removed := false
	for _, torrent := range s.activeEngine().List() {
		if strings.EqualFold(torrent.Hash, hash) {
			removed = true
			break
		}
	}
	if gh4_removeFailedTorrent(s.activeEngine(), hash) {
		_ = s.db.MarkTorrentRemovedAt(hash)
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "removed": removed, "upgrade_restored": restored})
}

// MovieSearch implements `movie_search`.
func MovieSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cfg := latestConfig(s)
	var movie *MovieConfig
	for index := range cfg.Movies {
		if cfg.Movies[index].ID == id {
			movie = &cfg.Movies[index]
			break
		}
	}
	if movie == nil {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	query := strings.TrimSpace(movie.Name + " " + movie.Year)
	results := s.engine.SearchMovie(r.Context(), cfg, movie, true, true)
	entries, _ := s.archive.Search(query)
	for _, entry := range entries {
		release := ParseRelease(entry[0], entry[1], "archive:"+entry[2])
		if release != nil {
			results = append(results, *release)
		}
	}
	seen := map[string]struct{}{}
	kept := results[:0]
	for index := range results {
		release := &results[index]
		if release.Kind != "movie" {
			continue
		}
		matched := cfg.FindMovieMatchForRelease(release, true)
		if matched == nil || matched.ID != movie.ID || !cfg.MovieReleaseAllowedForTitle(movie, &release.Quality, release.Title) {
			continue
		}
		hash, ok := releaseDedupKey(release)
		if !ok {
			continue
		}
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		kept = append(kept, *release)
	}
	results = kept
	sort.SliceStable(results, func(i, j int) bool {
		return cfg.ReleaseScoreForMovie(&results[i], movie) > cfg.ReleaseScoreForMovie(&results[j], movie)
	})
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "query": query, "results": results})
}

// PlexTest implements `plex_test`.
func PlexTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	base := cfg.Settings["plex_url"]
	token := cfg.Settings["plex_token"]
	if strings.TrimSpace(base) == "" || strings.TrimSpace(token) == "" {
		jsonError(w, http.StatusBadRequest, "Plex non configurato")
		return
	}
	base = strings.TrimRight(base, "/")
	token = strings.TrimSpace(token)
	client := &http.Client{Timeout: 15 * time.Second}
	get := func(path string) (*http.Response, error) {
		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, base+path, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("X-Plex-Token", token)
		return client.Do(request)
	}
	response, err := get("/identity")
	if err != nil {
		logging.Info("plex test", "host", domain_of(base), "ok", false, "error", err.Error())
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		identity, _ := readLimitedBody(response.Body, maxAPIResponseBytes)
		machine := gh4_derefString(gh4_xmlAttribute(string(identity), "machineIdentifier"))
		server := ""
		if root, rootErr := get("/"); rootErr == nil {
			defer root.Body.Close()
			if root.StatusCode >= 200 && root.StatusCode < 300 {
				body, _ := readLimitedBody(root.Body, maxAPIResponseBytes)
				server = gh4_derefString(gh4_xmlAttribute(string(body), "friendlyName"))
			}
		}
		logging.Info("plex test", "host", domain_of(base), "ok", true, "status", response.StatusCode, "server", server)
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "server": server, "version": "", "machine": machine})
		return
	}
	mediaError := gh4_mediaTestError("Plex", response.StatusCode)
	logging.Info("plex test", "host", domain_of(base), "ok", false, "status", response.StatusCode, "error", mediaError)
	jsonStatus(w, http.StatusBadGateway, map[string]any{
		"ok":    false,
		"error": mediaError,
	})
}

// RemoveBlocklistEntry implements `remove_blocklist_entry`.
func RemoveBlocklistEntry(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	removed, err := s.db.RemoveBlocklist(hash)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		jsonError(w, http.StatusNotFound, "hash not blocklisted")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

// RescoreDatabase implements `rescore_database`.
func RescoreDatabase(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not modify database scores")
		return
	}
	cfg := latestConfig(s)
	updated, err := s.db.Rescore(cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "updated": updated})
}

// SaveLibrary implements `save_library`.
func SaveLibraryHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input LibraryInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	invalid := len(input.Series) > 500 || len(input.Movies) > 500
	if !invalid {
		for _, series := range input.Series {
			if strings.TrimSpace(series.Name) == "" {
				invalid = true
				break
			}
		}
	}
	if !invalid {
		for _, movie := range input.Movies {
			if strings.TrimSpace(movie.Name) == "" {
				invalid = true
				break
			}
		}
	}
	if invalid {
		jsonError(w, http.StatusBadRequest, "invalid or oversized library configuration")
		return
	}
	previous := latestConfig(s)
	// config.go currently exports this helper as `SaveLibrary`, which collides
	// with this handler; it must be renamed (e.g. `SaveLibraryConfig`).
	if err := SaveLibraryConfig(s.cfg.DataDir, input.Series, input.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gh4_purgeRemovedLibrary(s.db, previous.Series, previous.Movies, input.Series, input.Movies)
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":               true,
		"restart_required": true,
		"series":           len(input.Series),
		"movies":           len(input.Movies),
	})
}

// ScanAllArchives implements `scan_all_archives`.
//
// The scan is long (it walks every archive folder) and used to run inside the
// HTTP request. It is now a background job: the request validates quickly and
// returns 202 with a job id, while /api/jobs/{id} exposes progress, result and
// errors. Cancellation is observed between series; a single folder scan is never
// interrupted half-way.
func ScanAllArchives(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	type scanTarget struct {
		name string
		path string
	}
	targets := []scanTarget{}
	for index := range cfg.Series {
		series := &cfg.Series[index]
		if !series.Enabled || strings.TrimSpace(series.ArchivePath) == "" {
			continue
		}
		if gh4_archiveImportBusyContains(series.Name) {
			continue
		}
		targets = append(targets, scanTarget{name: series.Name, path: series.ArchivePath})
	}

	job, created := s.jobs.Create("scan-archives", "scan-archives")
	if !created {
		jsonError(w, http.StatusConflict, "an archive scan is already running")
		return
	}
	if err := s.jobs.Start(job.ID, func(ctx context.Context, jobID string) {
		updated := 0
		errorsList := []map[string]any{}
		for index, target := range targets {
			if ctx.Err() != nil {
				return
			}
			s.jobs.SetProgress(jobID, float64(index)/float64(len(targets)), target.name)
			series := gh4_findSeries(cfg, target.name)
			if series == nil {
				continue
			}
			_, count, err := gh4_scanArchivePath(s.db, series, target.path, cfg)
			if err != nil {
				errorsList = append(errorsList, map[string]any{"series": target.name, "error": err.Error()})
				continue
			}
			updated += count
		}
		if len(errorsList) > 0 {
			logging.Warn("archive scan finished with errors", "updated", updated, "errors", len(errorsList), "job_id", jobID)
			s.jobs.Fail(jobID, fmt.Errorf("archive scan finished with %d error(s); updated=%d", len(errorsList), updated))
			return
		}
		logging.Info("archive scan completed", "updated", updated, "job_id", jobID)
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"job_id":  job.ID,
		"total":   len(targets),
		"message": "Scansione archivi avviata",
	})
}

// SeriesEpisodes implements `series_episodes`.
func SeriesEpisodes(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	cfg := latestConfig(s)
	ignored := []int64{}
	if series := gh4_findSeries(cfg, name); series != nil {
		ignored = series.IgnoredSeasons
	}
	items, err := s.db.EpisodesForSeries(name, ignored)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

// SeriesSeenView implements `series_seen_view`.
func SeriesSeenView(w http.ResponseWriter, r *http.Request, s *AppState) {
	status, value := gh4_seenEntries(s, "series", r)
	jsonStatus(w, status, value)
}

// SetSpeedLimits implements `set_speed_limits`.
func SetSpeedLimits(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input gh4_SpeedLimitsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	download := input.DownloadKib
	if download < 0 {
		download = 0
	}
	upload := input.UploadKib
	if upload < 0 {
		upload = 0
	}
	if err := SaveSetting(s.cfg.DataDir, "libtorrent_dl_limit", strconv.FormatInt(download, 10)); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = SaveSetting(s.cfg.DataDir, "libtorrent_ul_limit", strconv.FormatInt(upload, 10))
	if _, err := s.activeEngine().SetGlobalSpeedLimits(download, upload); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "download_kib": download, "upload_kib": upload})
}

// SetWebSeeds implements `set_web_seeds`.
func SetWebSeeds(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input gh4_WebSeedsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	urls := []string{}
	for _, raw := range input.Urls {
		url := strings.TrimSpace(raw)
		if url == "" {
			continue
		}
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "ftp://") {
			urls = append(urls, url)
		}
	}
	joined := strings.Join(urls, "\n")
	if joined == "" {
		jsonError(w, http.StatusBadRequest, "no valid web seed URL")
		return
	}
	ok, err := s.activeEngine().WebSeeds(hash, joined, input.Remove)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusConflict, "torrent unavailable in dry-run")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

// SimklSettings implements `simkl_settings`.
func SimklSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := new(SimklClient).FromSettings(cfg.Settings)
	clientID, hasClientID := cfg.Settings["simkl_client_id"]
	clientIDConfigured := hasClientID && clientID != ""
	watchlistStatus, ok := cfg.Settings["simkl_watchlist_status"]
	if !ok {
		watchlistStatus = "plantowatch"
	}
	calendarDays, ok := cfg.Settings["simkl_calendar_days"]
	if !ok {
		calendarDays = "7"
	}
	markWatched := false
	if value, ok := cfg.Settings["simkl_mark_watched"]; ok {
		markWatched = gh4_truthy(value)
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"configured":           client.Configured(),
		"authenticated":        client.Authenticated(),
		"client_id":            clientID,
		"client_id_configured": clientIDConfigured,
		"watchlist_status":     watchlistStatus,
		"calendar_days":        calendarDays,
		"mark_watched":         markWatched,
	})
}

// TestNotification implements `test_notification`.
func TestNotification(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TestNotificationInput
	_ = decodeJSON(r, &input)
	message := "Gextto: notifica di test"
	if input.Message != nil && strings.TrimSpace(*input.Message) != "" {
		message = *input.Message
	}
	notifier := FromConfig(latestConfig(s))
	if err := notifier.Notify(message); err != nil {
		logging.Info("notification test", "ok", false, "error", err.Error())
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	logging.Info("notification test", "ok", true, "enabled", notifier.Enabled())
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "enabled": notifier.Enabled()})
}

// TorrentHistory implements `torrent_history`.
func TorrentHistory(w http.ResponseWriter, r *http.Request, s *AppState) {
	limit := queryInt(r, "limit", 10)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	page := queryInt(r, "page", 1)
	if page < 1 {
		page = 1
	}
	query := queryParam(r, "q")
	// Fetch both sources before paginating: otherwise comic rows would be
	// prepended to every torrent page and the reported total would not match
	// the visible rows.
	_, torrentTotal, err := s.db.CompletedTorrents(0, 1, query)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	torrentLimit := int(torrentTotal)
	if torrentLimit < 1 {
		torrentLimit = 1
	}
	if torrentLimit > 2000 {
		torrentLimit = 2000
	}
	torrents, _, err := s.db.CompletedTorrents(0, torrentLimit, query)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type historyItem struct {
		value any
		at    string
	}
	combined := make([]historyItem, 0, len(torrents))
	for _, torrent := range torrents {
		torrent.Name = gh4_historyDisplayName(torrent.Name, torrent.Source)
		at := torrent.CompletedAt
		if at == "" {
			at = torrent.UpdatedAt
		}
		combined = append(combined, historyItem{value: torrent, at: at})
	}
	// Comic HTTP downloads are not libtorrent rows, but users expect every
	// completed download in the same Storico download view. Keep their source
	// record in comics_history and expose it with the common history shape.
	if s.comics != nil {
		if history, historyErr := s.comics.History(s.comics.HistoryLimit()); historyErr == nil {
			needle := strings.ToLower(strings.TrimSpace(query))
			for _, item := range history {
				name := fmt.Sprint(item["title"])
				postURL := fmt.Sprint(item["post_url"])
				if needle != "" && !strings.Contains(strings.ToLower(name), needle) && !strings.Contains(strings.ToLower(postURL), needle) {
					continue
				}
				comic := map[string]any{
					"name":           name,
					"kind":           "comic",
					"tag":            "Comic",
					"quality_score":  0,
					"status":         "completed",
					"processed_path": "",
					"completed_at":   item["sent_at"],
					"source":         postURL,
				}
				combined = append(combined, historyItem{value: comic, at: fmt.Sprint(item["sent_at"])})
			}
		}
	}
	sort.SliceStable(combined, func(i, j int) bool {
		return combined[i].at > combined[j].at
	})
	total := int64(len(combined))
	if torrentTotal > int64(len(torrents)) {
		// Keep pagination honest if the database contains more rows than the
		// defensive fetch cap; the visible page is still correctly ordered for
		// the rows we loaded.
		total = torrentTotal + int64(len(combined)-len(torrents))
	}
	pages := (total + limit - 1) / limit
	if pages < 1 {
		pages = 1
	}
	offset := (page - 1) * limit
	start := int(offset)
	if start > len(combined) {
		start = len(combined)
	}
	end := start + int(limit)
	if end > len(combined) {
		end = len(combined)
	}
	items := make([]any, 0, end-start)
	for _, item := range combined[start:end] {
		items = append(items, item.value)
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":    true,
		"items": items,
		"total": total,
		"page":  page,
		"pages": pages,
	})
}

// TraktAuthPoll implements `trakt_auth_poll`.
func TraktAuthPoll(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not save integration tokens")
		return
	}
	var input AuthCode
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	value, err := new(TraktClient).FromSettings(cfg.Settings).DevicePoll(r.Context(), input.Code)
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	access, accessErr := TokenString(value, "access_token")
	refresh, refreshErr := TokenString(value, "refresh_token")
	if accessErr != nil || refreshErr != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{
			"ok":    false,
			"error": "Trakt response did not contain tokens",
			"data":  value,
		})
		return
	}
	if err := SaveSetting(cfg.DataDir, "trakt_access_token", access); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := SaveSetting(cfg.DataDir, "trakt_refresh_token", refresh); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "authenticated": true})
}

// TraktWatchlist implements `trakt_watchlist`.
func TraktWatchlist(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := new(TraktClient).FromSettings(cfg.Settings)
	if !client.Configured() || !client.Authenticated() {
		jsonError(w, http.StatusConflict, "Trakt non configurato")
		return
	}
	value, err := client.Watchlist(r.Context())
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, value)
}

// WatchedFoldersView implements `watched_folders_view`.
func WatchedFoldersView(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":    true,
		"items": LoadWatchedFolders(cfg.Settings),
	})
}
