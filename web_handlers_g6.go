package gextto

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// gh6ExternalSearchTimeout implements `EXTERNAL_SEARCH_TIMEOUT`.
const gh6ExternalSearchTimeout = 12 * time.Second

// MediaProbeInput is the input of `media_info_probe` ( line 2000).
type MediaProbeInput struct {
	Path string `json:"path"`
}

// RestoreSourceInput is the input of `restore_source` ( line 8264).
type RestoreSourceInput struct {
	Execute bool `json:"execute"`
}

// TempLimitsInput is the input of `set_temp_limits` ( line 11012).
type TempLimitsInput struct {
	DownloadKib int64 `json:"download_kib"`
	UploadKib   int64 `json:"upload_kib"`
	Minutes     int64 `json:"minutes"`
	Clear       bool  `json:"clear"`
}

// ---------------------------------------------------------------------------
// Private helpers (gh6_ prefixed)
// ---------------------------------------------------------------------------

func gh6_decodeOptionalJSON(r *http.Request, target any) error {
	if r.ContentLength == 0 {
		return nil
	}
	err := decodeJSON(r, target)
	if err == io.EOF {
		return nil
	}
	return err
}

func gh6_isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func gh6_fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func gh6_purgeRemovedLibrary(
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
				logging.Warn("impossibile rimuovere lo stato della serie eliminata", "error", err, "series", old.Name)
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
				logging.Warn("impossibile rimuovere lo stato del film eliminato", "error", err, "movie", old.Name)
			}
		}
	}
}

// gh6_revokeIntegrationTokens mirrors `revoke_integration_tokens`.
func gh6_revokeIntegrationTokens(s *AppState, keys []string) (int, map[string]any) {
	if s.cfg.DryRun {
		return http.StatusConflict, map[string]any{
			"ok":    false,
			"error": "dry-run does not modify integration tokens",
		}
	}
	for _, key := range keys {
		if err := saveConfigSetting(s.cfg.DataDir, key, ""); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
	}
	return http.StatusOK, map[string]any{"ok": true, "authenticated": false}
}

func gh6_parseSettingInt(cfg *Config, key string) int64 {
	value, ok := cfg.Settings[key]
	if !ok {
		return 0
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func gh6_truthySetting(value string) bool {
	switch strings.ToLower(value) {
	case "yes", "true", "1":
		return true
	}
	return false
}

func gh6_scheduledSpeedLimits(cfg *Config) (int64, int64, bool) {
	if !gh6_truthySetting(cfg.Settings["libtorrent_sched_enabled"]) {
		return 0, 0, false
	}
	now := time.Now()
	// `%u` is Monday=1..Sunday=7; `-1` makes it Monday=0..Sunday=6.
	weekday := (int(now.Weekday()) + 6) % 7
	activeDays := map[int]bool{}
	for _, day := range strings.Split(cfg.Settings["libtorrent_sched_days"], ",") {
		if parsed, err := strconv.Atoi(strings.TrimSpace(day)); err == nil {
			activeDays[parsed] = true
		}
	}
	dayOK := activeDays[weekday]
	parseTime := func(key string, defaultHour, defaultMinute int) (int, int) {
		if value, ok := cfg.Settings[key]; ok {
			parts := strings.SplitN(value, ":", 2)
			if len(parts) == 2 {
				hour, errHour := strconv.Atoi(strings.TrimSpace(parts[0]))
				minute, errMinute := strconv.Atoi(strings.TrimSpace(parts[1]))
				if errHour == nil && errMinute == nil {
					return hour, minute
				}
			}
		}
		return defaultHour, defaultMinute
	}
	startHour, startMinute := parseTime("libtorrent_sched_start", 23, 0)
	endHour, endMinute := parseTime("libtorrent_sched_end", 8, 0)
	nowMinutes := now.Hour()*60 + now.Minute()
	startMinutes := startHour*60 + startMinute
	endMinutes := endHour*60 + endMinute
	inTime := false
	if startMinutes <= endMinutes {
		inTime = nowMinutes >= startMinutes && nowMinutes < endMinutes
	} else {
		inTime = nowMinutes >= startMinutes || nowMinutes < endMinutes
	}
	if dayOK && inTime {
		return gh6_parseSettingInt(cfg, "libtorrent_sched_dl_limit"),
			gh6_parseSettingInt(cfg, "libtorrent_sched_ul_limit"), true
	}
	return 0, 0, false
}

func gh6_currentSpeedLimits(cfg *Config) (int64, int64) {
	now := time.Now().Unix()
	tempUntil := gh6_parseSettingInt(cfg, "libtorrent_temp_limit_until")
	tempEnabled := false
	if value, ok := cfg.Settings["libtorrent_temp_limit_enabled"]; ok {
		tempEnabled = gh6_truthySetting(value)
	}
	if tempEnabled && (tempUntil == 0 || tempUntil > now) {
		return gh6_parseSettingInt(cfg, "libtorrent_temp_dl_limit"),
			gh6_parseSettingInt(cfg, "libtorrent_temp_ul_limit")
	}
	if download, upload, ok := gh6_scheduledSpeedLimits(cfg); ok {
		return download, upload
	}
	baseDownload := cfg.Libtorrent.DownloadLimitKib
	if baseDownload < 0 {
		baseDownload = 0
	}
	baseUpload := cfg.Libtorrent.UploadLimitKib
	if baseUpload < 0 {
		baseUpload = 0
	}
	return baseDownload, baseUpload
}

func gh6_applySpeedPolicy(cfg *Config, torrents TorrentEngine) {
	download, upload := gh6_currentSpeedLimits(cfg)
	if _, err := torrents.SetGlobalSpeedLimits(download, upload); err != nil {
		logging.Debug("speed policy apply failed", "error", err)
	}
}

func gh6_directorySize(path string) uint64 {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	var total uint64
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		if gh6_isDir(child) {
			total += gh6_directorySize(child)
			continue
		}
		// uses `entry.metadata()` (lstat) for non-directory sizes.
		if info, err := entry.Info(); err == nil {
			total += uint64(info.Size())
		}
	}
	return total
}

func gh6_jsonString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case *string:
		if typed != nil {
			return *typed
		}
	}
	return ""
}

func gh6_effectiveDownloadForRatio(torrent models.TorrentView) float64 {
	if torrent.AllTimeDownload > 0 {
		return float64(torrent.AllTimeDownload)
	}
	if torrent.TotalSize > 0 {
		return float64(torrent.TotalSize)
	}
	return 0
}

func gh6_torrentFilesAreDisposable(db *Database, hash string) bool {
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

func gh6_countIpfilterRules(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "-") {
			continue
		}
		count++
	}
	return count
}

func gh6_seenEntries(w http.ResponseWriter, r *http.Request, s *AppState, kind string) {
	group := queryParam(r, "key")
	if group == "" {
		group = queryParam(r, "group")
	}
	if strings.TrimSpace(group) == "" {
		jsonError(w, http.StatusBadRequest, "key required")
		return
	}
	limit := queryIntVal(r, "limit", 500)
	items, err := s.db.SeenByGroup(kind, strings.TrimSpace(group), limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

func gh6_systemctlField(scope []string, verb, unit string) string {
	args := append(append([]string{}, scope...), verb, unit)
	output, _ := exec.Command("systemctl", args...).Output()
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "unknown"
	}
	return value
}

// gh6_serviceScope returns the systemd scope arguments for a local unit. When
// the daemon itself runs as a `systemctl --user` unit (the checkout installer),
// querying the system scope reports "inactive"/"not-found" even though the
// service is healthy; prefer the user scope when its unit file is present.
func gh6_serviceScope(unit string) ([]string, string) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return nil, "system"
		}
		base = filepath.Join(home, ".config")
	}
	if _, err := os.Stat(filepath.Join(base, "systemd", "user", unit)); err == nil {
		return []string{"--user"}, "user"
	}
	return nil, "system"
}

func gh6_servicesProbe(rawURL string, timeout time.Duration) (int, bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := HTTPGet(ctx, rawURL, nil)
	if err != nil {
		return 0, false, ""
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, true, string(body)
}

// IndexerTest implements `indexer_test`: probe a single indexer as typed in the
// editor (which may not be saved yet) and report reachability plus any Torznab
// application error. It is the per-row "Testa" action of the Indexer editor.
func IndexerTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input IndexerConfig
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" || len(rawURL) > 2048 {
		jsonError(w, http.StatusBadRequest, "a valid indexer url is required")
		return
	}
	if len(input.APIKey) > 512 {
		jsonError(w, http.StatusBadRequest, "api key is too long")
		return
	}
	status, reachable, body := gh6_servicesProbe(HealthProbeURL(input), 10*time.Second)
	torznabError := ""
	if reachable {
		torznabError = TorznabError(body)
	}
	ok := reachable && status >= 200 && status < 300 && torznabError == ""
	message := ""
	switch {
	case !reachable:
		message = "irraggiungibile"
	case torznabError != "":
		message = torznabError
	case !ok:
		message = fmt.Sprintf("HTTP %d", status)
	}
	logging.Info("indexer test",
		"kind", managerKind(input),
		"name", strings.TrimSpace(input.Name),
		"host", domain_of(rawURL),
		"ok", ok,
		"status", status,
		"error", message)
	jsonResponse(w, map[string]any{
		"ok":        ok,
		"reachable": reachable,
		"status":    status,
		"error":     message,
	})
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// ApplyMovieMetadata implements `apply_movie_metadata`.
func ApplyMovieMetadata(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	var input MovieMetadataApplyInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	var current *MovieConfig
	for index := range cfg.Movies {
		if cfg.Movies[index].ID == id {
			current = &cfg.Movies[index]
			break
		}
	}
	if current == nil {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	source := strings.ToLower(strings.TrimSpace(input.Source))
	externalID := strings.TrimSpace(input.Id)
	if externalID == "" || len(externalID) > 64 {
		jsonError(w, http.StatusBadRequest, "invalid metadata id")
		return
	}
	var details map[string]any
	if source == "tmdb" {
		if cfg.TmdbAPIKey == nil {
			jsonError(w, http.StatusConflict, "TMDB API key is not configured")
			return
		}
		tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
		ctx, cancel := context.WithTimeout(r.Context(), gh6ExternalSearchTimeout)
		defer cancel()
		item, err := tmdb.MovieDetails(ctx, externalID)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				jsonError(w, http.StatusGatewayTimeout, "TMDB timed out")
				return
			}
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		details = map[string]any{
			"title":          item.Title,
			"original_title": item.OriginalTitle,
			"overview":       item.Overview,
			"poster_path":    item.PosterPath,
			"release_date":   item.ReleaseDate,
		}
	} else if source == "tvdb" {
		tvdb := WithLanguage(cfg.TvdbAPIKey(), cfg.TvdbLanguage())
		if !tvdb.Configured() {
			jsonError(w, http.StatusConflict, "TVDB API key is not configured")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), gh6ExternalSearchTimeout)
		defer cancel()
		item, err := tvdb.MovieDetails(ctx, externalID)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				jsonError(w, http.StatusGatewayTimeout, "TVDB timed out")
				return
			}
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		itemMap, _ := item.(map[string]any)
		poster := any(nil)
		if value, present := itemMap["image"]; present {
			poster = value
		} else if value, present := itemMap["image_url"]; present {
			poster = value
		}
		details = map[string]any{
			"title":          itemMap["name"],
			"original_title": itemMap["originalName"],
			"overview":       itemMap["overview"],
			"poster_path":    poster,
			"release_date":   itemMap["year"],
		}
	} else {
		jsonError(w, http.StatusBadRequest, "unsupported metadata source")
		return
	}
	title := strings.TrimSpace(gh6_jsonString(details["title"]))
	if title == "" {
		title = current.Name
	}
	date := gh6_jsonString(details["release_date"])
	metadataYear := ""
	if len(date) >= 4 {
		candidate := date[:4]
		digits := true
		for _, character := range candidate {
			if character < '0' || character > '9' {
				digits = false
				break
			}
		}
		if digits {
			metadataYear = candidate
		}
	}
	year := current.Year
	if metadataYear != "" {
		year = metadataYear
	}
	updated := *latestConfig(s)
	// Deep-copy the slice: `LatestConfig` may return a cached Config whose
	// backing array must not be mutated in place.
	updated.Movies = append([]MovieConfig(nil), updated.Movies...)
	index := -1
	for position := range updated.Movies {
		if updated.Movies[position].ID == id {
			index = position
			break
		}
	}
	if index < 0 {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	movie := &updated.Movies[index]
	movie.Name = title
	movie.Year = year
	movie.OriginalTitle = gh6_jsonString(details["original_title"])
	movie.Overview = gh6_jsonString(details["overview"])
	movie.PosterPath = gh6_jsonString(details["poster_path"])
	if source == "tmdb" {
		movie.TmdbID = externalID
		movie.TvdbID = ""
	} else {
		movie.TvdbID = externalID
		movie.TmdbID = ""
	}
	if err := SaveLibrary(s.cfg.DataDir, updated.Series, updated.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.db.RenameMovieIdentity(current.Name, current.Year, title, year); err != nil {
		logging.Error("movie metadata saved but download identity update failed", "error", err, "movie_id", id)
		jsonError(w, http.StatusInternalServerError, "metadata saved, but could not preserve download history")
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "movie": updated.Movies[index]})
}

// BrowseDir implements `browse_dir`.
func BrowseDir(w http.ResponseWriter, r *http.Request, s *AppState) {
	requested := strings.TrimSpace(queryParam(r, "path"))
	if requested == "" {
		if home := os.Getenv("HOME"); home != "" {
			requested = home
		} else {
			requested = "/"
		}
	}
	absolute, err := filepath.Abs(requested)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	if !gh6_isDir(canonical) {
		jsonError(w, http.StatusBadRequest, "not a directory")
		return
	}
	dirs := []string{}
	if entries, err := os.ReadDir(canonical); err == nil {
		for _, entry := range entries {
			child := filepath.Join(canonical, entry.Name())
			if gh6_isDir(child) {
				dirs = append(dirs, child)
			}
		}
	}
	sort.Strings(dirs)
	var parent any
	if canonical != "/" {
		parent = filepath.Dir(canonical)
	}
	jsonResponse(w, map[string]any{
		"ok":     true,
		"path":   canonical,
		"parent": parent,
		"dirs":   dirs,
	})
}

// ComicCheckLinks implements `comic_check_links`.
func ComicCheckLinks(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ComicCheckLinksInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Urls) == 0 || len(input.Urls) > 20 {
		jsonError(w, http.StatusBadRequest, "provide 1-20 comic post URLs")
		return
	}
	client := NewGetComicsClient()
	items := []map[string]any{}
	for _, url := range input.Urls {
		links, err := client.Links(strings.TrimSpace(url))
		if err != nil {
			items = append(items, map[string]any{"url": url, "ok": false, "error": err.Error()})
			continue
		}
		items = append(items, map[string]any{"url": url, "ok": true, "links": links})
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

// ComicExplore implements `comic_explore`.
func ComicExplore(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ComicExploreInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(input.Query) > 256 {
		jsonError(w, http.StatusBadRequest, "query di ricerca non valida")
		return
	}
	url, posts, err := NewGetComicsClient().SearchPosts(query)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "query_url": url, "items": posts})
}

// CreateRamdisk implements `create_ramdisk`.
func CreateRamdisk(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RamDiskCreateInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	defaultPath := "/dev/shm/gextto"
	requested := defaultPath
	if input.Path != nil {
		requested = *input.Path
	}
	if requested != defaultPath || filepath.Dir(requested) != "/dev/shm" {
		jsonError(w, http.StatusBadRequest, "il RAM disk automatico può essere creato solo in /dev/shm/gextto")
		return
	}
	if err := os.MkdirAll(requested, 0o755); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("creazione RAM disk fallita: %v", err))
		return
	}
	if err := os.Chmod(requested, 0o700); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("permessi RAM disk non impostati: %v", err))
		return
	}
	recommendation, err := SaveRamdiskSettings(s.cfg.DataDir, requested)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("RAM disk creato ma configurazione non salvata: %v", err))
		return
	}
	jsonResponse(w, map[string]any{
		"ok":          true,
		"path":        requested,
		"recommended": recommendation,
		"warning":     "Il contenuto di /dev/shm non sopravvive al riavvio della macchina.",
	})
}

// DeleteArchiveEntry implements `delete_archive_entry`.
func DeleteArchiveEntry(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ArchiveDeleteInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not modify archive")
		return
	}
	if len(input.Ids) == 0 && strings.TrimSpace(input.Magnet) == "" {
		jsonError(w, http.StatusBadRequest, "magnet or ids is required")
		return
	}
	var deleted int
	var err error
	if len(input.Ids) > 0 {
		deleted, err = s.archive.DeleteIDs(input.Ids)
	} else {
		var ok bool
		ok, err = s.archive.Delete(strings.TrimSpace(input.Magnet))
		if ok {
			deleted = 1
		}
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if deleted > 0 {
		jsonResponse(w, map[string]any{"ok": true, "deleted": deleted})
		return
	}
	jsonError(w, http.StatusNotFound, "archive entry not found")
}

// ExplainRelease implements `explain_release`.
func ExplainRelease(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ExplainReleaseInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Release.Title) == "" {
		jsonError(w, http.StatusBadRequest, "release senza titolo")
		return
	}
	cfg := latestConfig(s)
	scanRelease := input.Release
	disk := ArchiveQuality(cfg, &scanRelease)
	trace, err := ExplainWithArchive(cfg, s.db, &input.Release, disk)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "trace": trace})
}

// IpfilterStatus implements `ipfilter_status`.
func IpfilterStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	target := strings.TrimSpace(cfg.Libtorrent.IpFilterPath)
	cached := filepath.Join(cfg.DataDir, "ipfilter.dat")
	isURL := strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
	path := cached
	if !isURL && target != "" {
		path = target
	}
	_, statErr := os.Stat(path)
	active := target != "" && statErr == nil
	rules := 0
	if active {
		rules = gh6_countIpfilterRules(path)
	}
	jsonResponse(w, map[string]any{
		"ok":         true,
		"configured": target != "",
		"url":        target,
		"path":       path,
		"active":     active,
		"rules":      rules,
	})
}

// LogLevelGet implements `log_level_get`.
func LogLevelGet(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{"ok": true, "level": "runtime"})
}

// MediaInfoProbe implements `media_info_probe`.
func MediaInfoProbe(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input MediaProbeInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := strings.TrimSpace(input.Path)
	if path == "" {
		jsonError(w, http.StatusBadRequest, "empty path")
		return
	}
	info := Probe(path)
	if info == nil {
		jsonError(w, http.StatusUnprocessableEntity, "ffprobe unavailable or the file is unreadable")
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "info": info})
}

// MoviesSeenView implements `movies_seen_view`.
func MoviesSeenView(w http.ResponseWriter, r *http.Request, s *AppState) {
	gh6_seenEntries(w, r, s, "movie")
}

// RamdiskView implements `ramdisk_view`.
func RamdiskView(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	configured := cfg.RamdiskDir()
	items := []map[string]any{}
	for _, mount := range DetectedRamdiskMounts() {
		items = append(items, RamdiskEntry(mount[0], mount[1], configured))
	}
	if configured != nil && *configured != "" {
		found := false
		for _, item := range items {
			if item["path"] == *configured {
				found = true
				break
			}
		}
		if !found {
			items = append(items, RamdiskEntry(*configured, "configured", configured))
		}
	}
	var configuredValue any
	configuredOK := false
	problem := ""
	if configured != nil && *configured != "" {
		configuredValue = *configured
		// Make the panel self-diagnosing: a configured RAM disk that vanished
		// after a reboot (tmpfs is volatile) or lost its mount must be obvious
		// instead of silently disabling the RAM-disk tier.
		mountType, isRamdisk := ramdiskMountType(*configured)
		switch {
		case !gh6_isDir(*configured):
			problem = fmt.Sprintf("La cartella RAM disk configurata non esiste più: %s", *configured)
		case !DirectoryWritable(*configured):
			problem = fmt.Sprintf("La cartella RAM disk configurata non è scrivibile: %s", *configured)
		case !isRamdisk:
			problem = fmt.Sprintf("La cartella configurata è su un filesystem '%s', non tmpfs/ramfs: i download andranno su disco.", mountType)
		default:
			configuredOK = true
		}
	}
	createRoot := "/dev/shm"
	jsonResponse(w, map[string]any{
		"ok":               true,
		"enabled":          cfg.RamdiskEnabled(),
		"configured":       configuredValue,
		"configured_ok":    configuredOK,
		"problem":          problem,
		"paths":            items,
		"create_path":      filepath.Join(createRoot, "gextto"),
		"create_available": gh6_isDir(createRoot) && DirectoryWritable(createRoot),
		"warning":          "Il contenuto di /dev/shm e degli altri tmpfs non sopravvive al riavvio della macchina.",
	})
}

// gh6_archivedCopyFinalized reports whether the DB points at an archived copy
// that already went through renaming and quality checks. A post-seed relocation
// can leave the file in the archive folder under its original torrent name when
// the import step never ran, so that copy must be finalized instead of removed.
func gh6_archivedCopyFinalized(db *Database, torrentName, hash string) bool {
	if db == nil {
		return false
	}
	processed, err := db.TorrentProcessed(hash)
	if err != nil || processed == nil || strings.TrimSpace(*processed) == "" {
		return false
	}
	if _, statErr := os.Stat(*processed); statErr != nil {
		return false
	}
	name := strings.TrimSpace(torrentName)
	if name == "" {
		return true
	}
	return !strings.EqualFold(filepath.Base(filepath.Clean(*processed)), filepath.Base(name))
}

// RemoveCompletedTorrents implements `remove_completed_torrents`.
func RemoveCompletedTorrents(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RemoveCompletedInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	var globalRatio *float64
	if cfg.Libtorrent.SeedRatio > 0.0 {
		value := cfg.Libtorrent.SeedRatio
		globalRatio = &value
	}
	var globalTime *int64
	if cfg.Libtorrent.SeedTimeDays > 0 {
		value := cfg.Libtorrent.SeedTimeDays * 86_400
		globalTime = &value
	} else if cfg.Libtorrent.SeedTimeMinutes > 0 {
		value := cfg.Libtorrent.SeedTimeMinutes * 60
		globalTime = &value
	}
	removed := []string{}
	removedNames := []string{}
	skipped := 0
	// Skips caused by an error (finalization or engine removal failed), as
	// opposed to the routine ones (seed limit not reached, move in progress,
	// archive pending): only these are worth surfacing in the summary log.
	failed := []string{}
	for _, torrent := range s.activeEngine().List() {
		label := strings.TrimSpace(torrent.Name)
		if label == "" {
			label = "unnamed torrent"
		}
		status, _ := s.db.TorrentStatus(torrent.Hash)
		processed, _ := s.db.TorrentProcessed(torrent.Hash)
		isArchived := processed != nil && strings.TrimSpace(*processed) != ""
		isCompletedDB := status != nil && *status == "completed"
		completed := torrent.Progress >= 99.99 || torrent.State == "finished" || torrent.State == "seeding" || isCompletedDB || isArchived
		if !completed {
			continue
		}
		// Never interfere with a move/rename in progress: a torrent libtorrent is
		// moving, or a tracked release whose library copy is not in place yet
		// (end-of-seed archive pending), must be left alone. Removing it now would
		// abort the move and could leave the file behind.
		if torrent.State == "moving" {
			skipped++
			continue
		}
		if meta, err := s.db.TorrentMeta(torrent.Hash); err == nil && meta != nil {
			if processed == nil || strings.TrimSpace(*processed) == "" {
				skipped++
				continue
			}
		}
		if torrent.SeedRatio == 0.0 || torrent.SeedDays == 0 {
			skipped++
			continue
		}
		var ratioLimit *float64
		if torrent.SeedRatio > 0.0 {
			value := torrent.SeedRatio
			ratioLimit = &value
		} else {
			ratioLimit = globalRatio
		}
		var timeLimit *int64
		if torrent.SeedDays > 0 {
			value := torrent.SeedDays * 86_400
			timeLimit = &value
		} else {
			timeLimit = globalTime
		}
		ratioReached := false
		if ratioLimit != nil {
			download := gh6_effectiveDownloadForRatio(torrent)
			ratioReached = download > 0.0 && float64(torrent.AllTimeUpload)/download >= *ratioLimit
		}
		timeReached := timeLimit != nil && torrent.SeedingSeconds >= *timeLimit
		canRemove := false
		if ratioReached || timeReached {
			canRemove = true
		} else if isArchived && torrent.State == "paused" {
			canRemove = true
		} else if ratioLimit == nil && timeLimit == nil && (torrent.State == "paused" || torrent.State == "finished" || isArchived) {
			canRemove = true
		}
		if !canRemove {
			skipped++
			continue
		}
		// A completed torrent whose archive import never ran (post-seed move
		// interrupted, or the file still carries the torrent name) is finalized
		// here: moved into the library, renamed, quality-checked and deduplicated
		// before being dropped from the session. A bare Remove would leave the
		// file unchecked and under the wrong name.
		if !gh6_archivedCopyFinalized(s.db, torrent.Name, torrent.Hash) {
			logging.Debug("completed download still needs archiving; finalizing it before cleanup",
				"hash", torrent.Hash, "name", torrent.Name)
			if ok, err := ArchiveAndRemoveTorrent(s, cfg, torrent.Hash); err != nil {
				logging.Warn("completed torrent finalization failed",
					"hash", torrent.Hash, "name", torrent.Name, "error", err)
				skipped++
				failed = append(failed, label+" (archiviazione non riuscita: "+err.Error()+")")
				continue
			} else if !ok {
				skipped++
				failed = append(failed, label+" (archiviazione non completata)")
				continue
			}
			removed = append(removed, torrent.Hash)
			removedNames = append(removedNames, label)
			continue
		}
		deleteFiles := input.DeleteFiles || gh6_torrentFilesAreDisposable(s.db, torrent.Hash)
		source := CompletionPath(&models.TorrentEvent{
			Kind:     "torrent_finished",
			Hash:     torrent.Hash,
			Name:     torrent.Name,
			SavePath: torrent.SavePath,
		})
		if deleteFiles && !input.DeleteFiles {
			if !tev_completedSourceDisposable(s.db, torrent.Hash, torrent.SavePath) {
				deleteFiles = false
			}
		}
		if deleteFiles && cfg.TrashPath != nil && strings.TrimSpace(*cfg.TrashPath) != "" && cfg.CleanupAction != "delete" {
			if _, statErr := os.Stat(source); statErr == nil {
				if target, moveErr := MoveToTrash(source, *cfg.TrashPath); moveErr != nil {
					logging.Error("could not move completed torrent source to trash",
						"hash", torrent.Hash, "name", torrent.Name, "source", source, "error", moveErr.Error())
				} else {
					logging.Info("completed torrent source moved to trash",
						"hash", torrent.Hash, "name", torrent.Name, "source", source, "trash", target)
				}
			}
			deleteFiles = false
		}
		ok, err := s.activeEngine().Remove(torrent.Hash, deleteFiles)
		if err != nil {
			logging.Warn("completed torrent removal failed", "hash", torrent.Hash, "name", torrent.Name, "error", err)
			skipped++
			failed = append(failed, label+" (rimozione non riuscita: "+err.Error()+")")
			continue
		}
		if ok {
			_ = s.db.MarkTorrentRemoved(torrent.Hash)
			removed = append(removed, torrent.Hash)
			removedNames = append(removedNames, label)
		} else {
			skipped++
			failed = append(failed, label+" (rimozione rifiutata dal motore torrent)")
		}
	}
	summary := []any{"removed", len(removed)}
	if len(removedNames) > 0 {
		summary = append(summary, "torrents", strings.Join(removedNames, ", "))
	}
	logging.Info("completed torrents cleanup finished", summary...)
	if len(failed) > 0 {
		logging.Warn("completed torrents cleanup: some torrents could not be removed",
			"failed", len(failed), "details", strings.Join(failed, "; "))
	}
	logging.Debug("completed torrents cleanup skipped torrents", "skipped", skipped, "with_errors", len(failed))
	jsonResponse(w, map[string]any{
		"ok":      true,
		"success": true,
		"removed": len(removed),
		"skipped": skipped,
		// HTTP comic downloads are not torrents and must remain visible until
		// the user removes them explicitly from the unified Scarico page.
		"http_removed": 0,
		"items":        removed,
	})
}

// RestoreSource implements `restore_source`.
func RestoreSource(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RestoreSourceInput
	if err := gh6_decodeOptionalJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	execute := input.Execute
	if execute && s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not modify files")
		return
	}
	cfg := latestConfig(s)
	items := []map[string]any{}
	renamed := 0
	errors := 0
	seriesCount := 0
	for _, series := range cfg.Series {
		if !series.Enabled || strings.TrimSpace(series.ArchivePath) == "" {
			continue
		}
		seriesCount++
		episodes, err := s.db.EpisodesForSeries(series.Name, series.IgnoredSeasons)
		if err != nil {
			episodes = nil
		}
		for _, episode := range episodes {
			if episode.ArchivePath == nil || strings.TrimSpace(*episode.ArchivePath) == "" {
				continue
			}
			archivePath := *episode.ArchivePath
			source := ParseQuality(episode.Title).Source
			if strings.TrimSpace(source) == "" || strings.EqualFold(source, "unknown") {
				continue
			}
			info, err := os.Stat(archivePath)
			if err != nil || info.IsDir() {
				continue
			}
			name := filepath.Base(archivePath)
			newName, ok := RestoreSourceToken(name, source)
			if !ok {
				continue
			}
			target := filepath.Join(filepath.Dir(archivePath), newName)
			if target == archivePath {
				continue
			}
			if execute {
				if _, err := RenameSidecars(archivePath, target, cfg); err != nil {
					logging.Warn("restore source: sidecar rename failed", "error", err)
				}
				if err := os.Rename(archivePath, target); err != nil {
					errors++
					logging.Warn("restore source: rename failed", "error", err)
					continue
				}
				_ = s.db.SetEpisodeArchivePath(cfg, series.Name, episode.Season, episode.Episode, target)
				renamed++
				items = append(items, map[string]any{
					"series":  series.Name,
					"season":  episode.Season,
					"episode": episode.Episode,
					"from":    archivePath,
					"to":      target,
				})
			} else {
				items = append(items, map[string]any{
					"series":  series.Name,
					"season":  episode.Season,
					"episode": episode.Episode,
					"from":    archivePath,
					"to":      target,
				})
			}
		}
	}
	jsonResponse(w, map[string]any{
		"ok":      true,
		"execute": execute,
		"series":  seriesCount,
		"renamed": renamed,
		"errors":  errors,
		"count":   len(items),
		"items":   items,
	})
}

// SaveSeriesConfig implements `save_series_config`.
func SaveSeriesConfig(w http.ResponseWriter, r *http.Request, s *AppState) {
	var series []SeriesConfig
	if err := decodeJSON(r, &series); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(series) > 500 {
		jsonError(w, http.StatusBadRequest, "too many series")
		return
	}
	cfg := latestConfig(s)
	if err := SaveLibrary(s.cfg.DataDir, series, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gh6_purgeRemovedLibrary(s.db, cfg.Series, cfg.Movies, series, cfg.Movies)
	jsonResponse(w, map[string]any{"ok": true, "series": len(series)})
}

// ScorePreview implements `score_preview`.
func ScorePreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ScorePreviewInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	title := strings.TrimSpace(input.Title)
	if title == "" || len(title) > 512 {
		jsonError(w, http.StatusBadRequest, "titolo non valido (1-512 caratteri)")
		return
	}
	cfg := latestConfig(s)
	const magnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	release := ParseRelease(title, magnet, "simulator")
	if release == nil {
		jsonError(w, http.StatusBadRequest, "impossibile analizzare il titolo")
		return
	}
	quality := release.Quality
	breakdown := []map[string]any{}
	for _, item := range quality.ScoreBreakdownWithSettings(cfg.Settings) {
		breakdown = append(breakdown, map[string]any{"label": item.Label, "value": item.Value})
	}
	qualityScore := cfg.QualityScore(&quality)
	releaseScore := cfg.ReleaseScore(release)
	if delta := releaseScore - qualityScore; delta != 0 {
		breakdown = append(breakdown, map[string]any{"label": "Modificatori release", "value": delta})
	}
	probe := release.Title
	if release.Series != nil {
		probe = *release.Series
	}
	var matchedSeries any
	for index := range cfg.Series {
		series := &cfg.Series[index]
		if !series.Enabled {
			continue
		}
		matched := SeriesNamesMatch(series.Name, probe)
		if !matched {
			for _, alias := range series.Aliases {
				if SeriesNamesMatch(alias, probe) {
					matched = true
					break
				}
			}
		}
		if matched {
			matchedSeries = series.Name
			break
		}
	}
	var matchedMovie any
	for index := range cfg.Movies {
		movie := &cfg.Movies[index]
		if movie.Enabled && SeriesNamesMatch(movie.Name, probe) {
			matchedMovie = movie.Name
			break
		}
	}
	jsonResponse(w, map[string]any{
		"ok":             true,
		"kind":           release.Kind,
		"series":         release.Series,
		"season":         release.Season,
		"episode":        release.Episode,
		"is_pack":        release.IsPack,
		"year":           release.Year,
		"quality":        quality,
		"base_score":     qualityScore,
		"score":          releaseScore,
		"breakdown":      breakdown,
		"allowed":        cfg.ReleaseAllowed(release),
		"matched_series": matchedSeries,
		"matched_movie":  matchedMovie,
	})
}

// SeriesList implements `series_list`.
func SeriesList(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	items := []map[string]any{}
	for index, series := range cfg.Series {
		items = append(items, map[string]any{
			"id":           index + 1,
			"name":         series.Name,
			"seasons":      series.Seasons,
			"quality":      series.Quality,
			"language":     series.Language,
			"archive_path": series.ArchivePath,
			"enabled":      series.Enabled,
			"aliases":      series.Aliases,
			"tmdb_id":      series.TmdbID,
			"timeframe":    series.Timeframe,
		})
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

// ServicesStatus implements `services_status`.
func ServicesStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	const unit = "gextto.service"
	scope, scopeName := gh6_serviceScope(unit)
	service := map[string]any{
		"unit":    unit,
		"scope":   scopeName,
		"active":  gh6_systemctlField(scope, "is-active", unit),
		"enabled": gh6_systemctlField(scope, "is-enabled", unit),
	}
	cfg := latestConfig(s)
	indexers := []map[string]any{}
	for _, indexer := range cfg.Indexers {
		if !indexer.Enabled {
			continue
		}
		status, reachable, body := gh6_servicesProbe(HealthProbeURL(indexer), 5*time.Second)
		var statusValue any
		if reachable {
			statusValue = status
		}
		errorText := ""
		if reachable {
			errorText = TorznabError(body)
		}
		var errorValue any
		if errorText != "" {
			errorValue = errorText
		}
		healthy := reachable && status >= 200 && status < 300 && errorText == ""
		indexers = append(indexers, map[string]any{
			"name":      indexer.Name,
			"url":       indexer.URL,
			"reachable": reachable,
			"healthy":   healthy,
			"status":    statusValue,
			"error":     errorValue,
		})
	}
	if cfg.FlaresolverrURL != nil && strings.TrimSpace(*cfg.FlaresolverrURL) != "" {
		url := *cfg.FlaresolverrURL
		status, reachable, _ := gh6_servicesProbe(url, 5*time.Second)
		var statusValue any
		if reachable {
			statusValue = status
		}
		indexers = append(indexers, map[string]any{
			"kind":      "flaresolverr",
			"name":      "FlareSolverr",
			"url":       url,
			"reachable": reachable,
			"status":    statusValue,
		})
	}
	jsonResponse(w, map[string]any{
		"ok":       true,
		"services": []map[string]any{service},
		"indexers": indexers,
	})
}

// SetTempLimits implements `set_temp_limits`.
func SetTempLimits(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TempLimitsInput
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
	minutes := input.Minutes
	if minutes < 0 {
		minutes = 0
	} else if minutes > 24*60 {
		minutes = 24 * 60
	}
	until := int64(0)
	if !input.Clear && minutes != 0 {
		until = time.Now().Unix() + minutes*60
	}
	enabled := "1"
	if input.Clear {
		enabled = "0"
	}
	pairs := [][2]string{
		{"libtorrent_temp_dl_limit", strconv.FormatInt(download, 10)},
		{"libtorrent_temp_ul_limit", strconv.FormatInt(upload, 10)},
		{"libtorrent_temp_limit_enabled", enabled},
		{"libtorrent_temp_limit_until", strconv.FormatInt(until, 10)},
	}
	for _, pair := range pairs {
		if err := saveConfigSetting(s.cfg.DataDir, pair[0], pair[1]); err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("salvataggio limite temporaneo: %v", err))
			return
		}
	}
	cfg := latestConfig(s)
	gh6_applySpeedPolicy(cfg, s.activeEngine())
	jsonResponse(w, map[string]any{
		"ok":           true,
		"until":        until,
		"enabled":      !input.Clear,
		"permanent":    !input.Clear && minutes == 0,
		"download_kib": download,
		"upload_kib":   upload,
	})
}

// GetTempLimits reports the speed policy in force: the temporary limit (with
// its expiry), the bandwidth schedule and the base limits, plus which one is
// applied now. The TUI shows it on the downloads tab.
func GetTempLimits(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	now := time.Now().Unix()
	until := gh6_parseSettingInt(cfg, "libtorrent_temp_limit_until")
	tempActive := gh6_truthySetting(cfg.Settings["libtorrent_temp_limit_enabled"]) && (until == 0 || until > now)
	remaining := int64(0)
	if tempActive && until > 0 {
		remaining = until - now
	}
	schedDownload, schedUpload, schedActive := gh6_scheduledSpeedLimits(cfg)
	download, upload := gh6_currentSpeedLimits(cfg)
	source := "base"
	if tempActive {
		source = "temp"
	} else if schedActive {
		source = "schedule"
	}
	jsonResponse(w, map[string]any{
		"source":             source,
		"download_kib":       download,
		"upload_kib":         upload,
		"temp_active":        tempActive,
		"temp_download_kib":  gh6_parseSettingInt(cfg, "libtorrent_temp_dl_limit"),
		"temp_upload_kib":    gh6_parseSettingInt(cfg, "libtorrent_temp_ul_limit"),
		"temp_until":         until,
		"temp_remaining_sec": remaining,
		"sched_active":       schedActive,
		"sched_download_kib": schedDownload,
		"sched_upload_kib":   schedUpload,
		"base_download_kib":  max(cfg.Libtorrent.DownloadLimitKib, 0),
		"base_upload_kib":    max(cfg.Libtorrent.UploadLimitKib, 0),
		// Shown next to the limits on the TUI downloads tab.
		"auto_remove_completed": cfg.Libtorrent.AutoRemoveCompleted,
	})
}

// SimklWatchlist implements `simkl_watchlist`.
func SimklWatchlist(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := (&SimklClient{}).FromSettings(cfg.Settings)
	if !client.Configured() || !client.Authenticated() {
		jsonError(w, http.StatusConflict, "Simkl non configurato")
		return
	}
	value, err := client.Watchlist(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, value)
}

// TmdbDiscover implements `tmdb_discover`.
func TmdbDiscover(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if cfg.TmdbAPIKey == nil {
		jsonError(w, http.StatusConflict, "TMDB API key is not configured")
		return
	}
	var input DiscoverInput
	if err := gh6_decodeOptionalJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	kind := DefaultTmdbKind()
	if input.Kind != nil {
		kind = *input.Kind
	}
	window := "week"
	if input.Window != nil {
		window = *input.Window
	}
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	ctx := r.Context()
	mode := ""
	if input.Mode != nil {
		mode = *input.Mode
	}
	var (
		items []TmdbItem
		err   error
	)
	switch {
	case mode == "popular":
		items, err = tmdb.Popular(ctx, kind)
	case mode == "top_rated" || mode == "now_playing" || mode == "upcoming":
		items, err = tmdb.Category(ctx, kind, mode)
	case mode == "trending" || input.Mode == nil:
		items, err = tmdb.Trending(ctx, kind, window)
	default:
		err = fmt.Errorf("unsupported TMDB discovery mode: %s", mode)
	}
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	for index := range items {
		items[index].InLibrary = gh_tmdbItemInLibrary(cfg, kind, items[index])
	}
	jsonResponse(w, map[string]any{"ok": true, "kind": kind, "items": items})
}

// TorrentPeers implements `torrent_peers`.
func TorrentPeers(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	peers, found, err := s.activeEngine().Peers(hash)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !found {
		jsonError(w, http.StatusNotFound, "torrent unavailable in dry-run")
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "peers": peers})
}

// TrashEntries implements `trash_entries`.
func TrashEntries(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	path := ""
	if cfg.TrashPath != nil {
		path = *cfg.TrashPath
	} else {
		path = filepath.Join(cfg.DataDir, "trash")
	}
	items := []map[string]any{}
	var totalBytes uint64
	if entries, err := os.ReadDir(path); err == nil {
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			isDir := gh6_isDir(child)
			var size uint64
			if isDir {
				size = gh6_directorySize(child)
			} else if info, err := entry.Info(); err == nil {
				size = uint64(info.Size())
			}
			totalBytes += size
			items = append(items, map[string]any{
				"name":       entry.Name(),
				"path":       child,
				"size_bytes": size,
				"is_dir":     isDir,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left, _ := items[i]["size_bytes"].(uint64)
		right, _ := items[j]["size_bytes"].(uint64)
		return left > right
	})
	retentionDays := gh6_parseSettingInt(cfg, "trash_retention_days")
	jsonResponse(w, map[string]any{
		"ok":             true,
		"path":           path,
		"exists":         gh6_isDir(path),
		"count":          len(items),
		"total_bytes":    totalBytes,
		"retention_days": retentionDays,
		"items":          items,
	})
}
