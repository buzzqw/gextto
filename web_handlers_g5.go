package gextto

// Port of the gextto HTTP handlers assigned to this group (the web module).
//
// The handlers keep the shared `HandlerFunc` shape
// `func(w http.ResponseWriter, r *http.Request, s *AppState)` and reuse the
// helpers/types provided by web.go. Every private helper defined here carries
// the `gh5_` prefix to avoid clashing with the sibling handler files.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
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
)

// ---------------------------------------------------------------------------
// Private helpers (gh5_ prefix)
// ---------------------------------------------------------------------------

// gh5_readOptionalBody returns the raw request body, tolerating an absent one.
func gh5_readOptionalBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	return io.ReadAll(r.Body)
}

// gh5_asInt64 mirrors encoding/json's `Value::as_i64`.
func gh5_asInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case int:
		return int64(number), true
	case float64:
		if number != math.Trunc(number) {
			return 0, false
		}
		return int64(number), true
	case json.Number:
		parsed, err := number.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// gh5_torrentAction maps a libtorrent action result to the shared JSON
// response used by the torrent endpoints.
func gh5_torrentAction(w http.ResponseWriter, result bool, err error) {
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

// gh5_findSeries mirrors the web module `find_series`: match by name or alias.
func gh5_findSeries(cfg *Config, name string) *SeriesConfig {
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

// gh5_purgeRemovedLibrary removes the DB state of series/movies that are no
// longer part of the saved library.
func gh5_purgeRemovedLibrary(db *Database, previousSeries []SeriesConfig, previousMovies []MovieConfig, series []SeriesConfig, movies []MovieConfig) {
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
			logging.Warn("impossibile rimuovere lo stato della serie eliminata", "error", err, "series", old.Name)
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
			logging.Warn("impossibile rimuovere lo stato del film eliminato", "error", err, "movie", old.Name)
		}
	}
}

func gh5_archiveImportBusyContains(name string) bool {
	return ArchiveImportBusyContains(name)
}

var gh5_seasonEpisodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bs(?:tagione)?\s*(\d{1,2})\s*e(?:p(?:isodio)?)?\.?\s*(\d{1,4})`),
	regexp.MustCompile(`(?i)\b(\d{1,2})\s*x\s*(\d{1,4})`),
	regexp.MustCompile(`(?i)stagione\s*(\d{1,2}).*?episodio\s*(\d{1,4})`),
}

// gh5_parseSeasonEpisode mirrors the web module `parse_season_episode`.
func gh5_parseSeasonEpisode(name string) (int64, int64, bool) {
	for _, pattern := range gh5_seasonEpisodePatterns {
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

// gh5_scanArchivePath mirrors the web module `scan_archive_path`.
func gh5_scanArchivePath(db *Database, series *SeriesConfig, path string, cfg *Config) (int, int, error) {
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
		season, episode, ok := gh5_parseSeasonEpisode(name)
		if !ok || season <= 0 || episode <= 0 {
			continue
		}
		// Never register a broken file as the archived copy: a zero-filled or
		// unrecognised video must not make the episode look present.
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

// ---------------------------------------------------------------------------
// Backup helpers
// ---------------------------------------------------------------------------

type gh5_ftpConfig struct {
	host     string
	user     string
	password string
	remote   string
}

type gh5_backupSteps struct {
	path             string
	ftpUploaded      bool
	ftpHost          string
	ftpRemote        string
	ftpError         *string
	cloudCopied      bool
	cloudDestination string
	cloudError       *string
}

// gh5_backupFtpConfig mirrors the web module `backup_ftp_config`.
func gh5_backupFtpConfig(cfg *Config) *gh5_ftpConfig {
	host := cfg.Settings["backup_ftp_host"]
	user := cfg.Settings["backup_ftp_user"]
	password := cfg.Settings["backup_ftp_password"]
	remote := cfg.Settings["backup_ftp_path"]
	if strings.TrimSpace(host) != "" && strings.TrimSpace(user) != "" && password != "" {
		return &gh5_ftpConfig{host: host, user: user, password: password, remote: remote}
	}
	return nil
}

// gh5_backupCloudDir mirrors the web module `backup_cloud_dir`.
func gh5_backupCloudDir(cfg *Config) *string {
	value := strings.TrimSpace(cfg.Settings["backup_cloud_dir"])
	if value == "" {
		return nil
	}
	return &value
}

// gh5_backupRetention mirrors the web module `backup_retention`.
func gh5_backupRetention(cfg *Config) int {
	retain := 5
	if parsed, err := strconv.ParseUint(cfg.Settings["backup_retention"], 10, 64); err == nil {
		retain = int(parsed)
	}
	if retain < 1 {
		retain = 1
	}
	if retain > 100 {
		retain = 100
	}
	return retain
}

// gh5_runBackupSteps mirrors the web module `run_backup_steps`.
func gh5_runBackupSteps(dataDir, root string, retain int, ftp *gh5_ftpConfig, cloudDir *string) (gh5_backupSteps, error) {
	path, err := CreateSnapshot(dataDir, root, retain)
	if err != nil {
		return gh5_backupSteps{}, err
	}
	var size int64
	if info, statErr := os.Stat(path); statErr == nil {
		size = info.Size()
	}
	logging.Info("backup snapshot created", "path", path, "size_bytes", size)

	steps := gh5_backupSteps{path: path}
	if ftp != nil {
		steps.ftpHost = ftp.host
		steps.ftpRemote = ftp.remote
		if err := UploadFTP(path, ftp.host, ftp.user, ftp.password, ftp.remote); err != nil {
			message := err.Error()
			steps.ftpError = &message
			logging.Warn("backup FTP upload failed", "path", path, "host", ftp.host, "error", message)
		} else {
			steps.ftpUploaded = true
		}
	}
	if cloudDir != nil {
		if destination, err := CopySnapshot(path, *cloudDir); err != nil {
			message := err.Error()
			steps.cloudError = &message
			logging.Warn("backup cloud copy failed", "directory", *cloudDir, "error", message)
		} else {
			steps.cloudCopied = true
			steps.cloudDestination = destination
		}
	}
	return steps, nil
}

// gh5_backupLabel mirrors the web module `backup_label`.
func gh5_backupLabel(name string, modified *uint64) string {
	var seconds *int64
	if strings.HasPrefix(name, "snapshot-") && strings.HasSuffix(name, ".zip") {
		raw := name[len("snapshot-") : len(name)-len(".zip")]
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			seconds = &value
		}
	}
	if seconds == nil && modified != nil {
		value := int64(*modified)
		seconds = &value
	}
	if seconds != nil {
		return time.Unix(*seconds, 0).Format("02/01/2006 15:04")
	}
	return "data sconosciuta"
}

func gh5_modifiedSeconds(value any) uint64 {
	if number, ok := value.(uint64); ok {
		return number
	}
	return 0
}

// ---------------------------------------------------------------------------
// Integration/watchlist helpers
// ---------------------------------------------------------------------------

type gh5_watchlistTitle struct {
	title string
	year  string
}

type gh5_watchlistEntry struct {
	kind  string
	title string
	year  string
}

// gh5_collectWatchlistEntries mirrors the web module `collect_watchlist_entries`.
func gh5_collectWatchlistEntries(value any, out *[]gh5_watchlistTitle) {
	switch node := value.(type) {
	case []any:
		for _, item := range node {
			gh5_collectWatchlistEntries(item, out)
		}
	case map[string]any:
		if title, ok := node["title"].(string); ok {
			year := ""
			if parsed, ok := gh5_asInt64(node["year"]); ok {
				year = strconv.FormatInt(parsed, 10)
			}
			*out = append(*out, gh5_watchlistTitle{title: title, year: year})
			return
		}
		for _, child := range node {
			gh5_collectWatchlistEntries(child, out)
		}
	}
}

// gh5_collectWatchlistInto mirrors the web module `collect_watchlist`.
func gh5_collectWatchlistInto(value any, out *[]gh5_watchlistEntry) {
	switch node := value.(type) {
	case []any:
		for _, item := range node {
			gh5_collectWatchlistInto(item, out)
		}
	case map[string]any:
		typed := false
		for key, child := range node {
			var kind string
			switch key {
			case "show", "shows", "anime", "series", "tv":
				kind = "series"
			case "movie", "movies":
				kind = "movie"
			default:
				continue
			}
			typed = true
			titles := []gh5_watchlistTitle{}
			gh5_collectWatchlistEntries(child, &titles)
			for _, title := range titles {
				*out = append(*out, gh5_watchlistEntry{kind: kind, title: title.title, year: title.year})
			}
		}
		if !typed {
			for _, child := range node {
				gh5_collectWatchlistInto(child, out)
			}
		}
	}
}

// gh5_applyWatchlistImport mirrors the web module `apply_watchlist_import`.
func gh5_applyWatchlistImport(cfg *Config, entries []gh5_watchlistEntry) map[string]any {
	seriesAdded := 0
	moviesAdded := 0
	for _, entry := range entries {
		if strings.TrimSpace(entry.title) == "" {
			continue
		}
		if entry.kind == "series" {
			exists := false
			for index := range cfg.Series {
				if strings.EqualFold(cfg.Series[index].Name, entry.title) {
					exists = true
					break
				}
			}
			if exists {
				continue
			}
			cfg.Series = append(cfg.Series, SeriesConfig{
				Name:     entry.title,
				Seasons:  "1+",
				Language: "ita",
				Enabled:  true,
			})
			seriesAdded++
		} else {
			exists := false
			for index := range cfg.Movies {
				if strings.EqualFold(cfg.Movies[index].Name, entry.title) {
					exists = true
					break
				}
			}
			if exists {
				continue
			}
			cfg.Movies = append(cfg.Movies, MovieConfig{
				Name:     entry.title,
				Year:     entry.year,
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

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// ApplyLibtorrentSettings implements `apply_libtorrent_settings`.
func ApplyLibtorrentSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	client, err := s.requireEmbedded("apply_settings")
	if err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	cfg := latestConfig(s)
	result, err := client.ApplySettings(cfg)
	gh5_torrentAction(w, result, err)
}

// BlocklistEntries implements `blocklist_entries`.
func BlocklistEntries(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.BlocklistEntries(500)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

// ClearProvidersStatus implements `clear_providers_status`.
func ClearProvidersStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input struct {
		Provider *string `json:"provider"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var provider *string
	if input.Provider != nil {
		trimmed := strings.TrimSpace(*input.Provider)
		if trimmed != "" {
			provider = &trimmed
		}
	}
	if err := s.db.ClearProviderStatus(provider); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// ComicDownloads implements `comic_downloads`.
func ComicDownloads(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, HTTPDownloads())
}

// CreateBackup implements `create_backup`.
func CreateBackup(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	notifier := FromConfig(cfg)
	dataDir := cfg.DataDir
	root := filepath.Join(dataDir, "backups")
	retain := gh5_backupRetention(cfg)
	ftp := gh5_backupFtpConfig(cfg)
	cloudDir := gh5_backupCloudDir(cfg)
	sendTelegram := false
	if value, ok := cfg.Settings["backup_send_telegram"]; ok {
		sendTelegram = value == "yes" || value == "true" || value == "1"
	}

	steps, err := gh5_runBackupSteps(dataDir, root, retain, ftp, cloudDir)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	telegramUploaded := false
	if sendTelegram {
		caption := "Gextto backup: " + filepath.Base(steps.path)
		uploaded, uploadErr := notifier.NotifyBackupDocument(steps.path, caption)
		telegramUploaded = uploadErr == nil && uploaded
		logging.Info("backup Telegram send completed", "uploaded", telegramUploaded)
	}
	logging.Info(
		"backup completed",
		"path", steps.path,
		"ftp_uploaded", steps.ftpUploaded,
		"ftp_host", steps.ftpHost,
		"ftp_remote", steps.ftpRemote,
		"ftp_error", gh5_optionalString(steps.ftpError),
		"cloud_copied", steps.cloudCopied,
		"cloud_destination", steps.cloudDestination,
		"cloud_error", gh5_optionalString(steps.cloudError),
		"telegram_uploaded", telegramUploaded,
	)
	_ = notifier.NotifyEvent("backup_completed", map[string]any{
		"path":              steps.path,
		"ftp_uploaded":      steps.ftpUploaded,
		"ftp_error":         steps.ftpError,
		"cloud_copied":      steps.cloudCopied,
		"cloud_error":       steps.cloudError,
		"telegram_uploaded": telegramUploaded,
	})
	jsonResponse(w, map[string]any{
		"ok":                true,
		"path":              steps.path,
		"ftp_uploaded":      steps.ftpUploaded,
		"ftp_error":         steps.ftpError,
		"cloud_copied":      steps.cloudCopied,
		"cloud_error":       steps.cloudError,
		"telegram_uploaded": telegramUploaded,
	})
}

func gh5_optionalString(value *string) string {
	if value == nil {
		return "none"
	}
	return *value
}

// DbPrunePreview implements `db_prune_preview`.
func DbPrunePreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input PruneInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	retain := int64(50)
	if input.RetainCycles != nil {
		retain = *input.RetainCycles
	}
	if retain < 1 {
		retain = 1
	}
	if retain > 10000 {
		retain = 10000
	}
	errorAge := int64(7)
	if input.ErrorAgeDays != nil {
		errorAge = *input.ErrorAgeDays
	}
	if errorAge < 1 {
		errorAge = 1
	}
	seenDays := int64(0)
	if input.SeenRetentionDays != nil {
		seenDays = *input.SeenRetentionDays
	}
	preview, err := s.db.PrunePreview(retain, errorAge, seenDays)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":           true,
		"preview":      true,
		"report":       preview,
		"seen_removed": preview.SeenToRemove,
	})
}

// EventHooksView implements `event_hooks_view`.
func EventHooksView(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{"ok": true, "items": s.notifier.EventHooks()})
}

// IgnoreEpisode implements `ignore_episode`.
func IgnoreEpisode(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := pathParam(r, "series")
	season, okSeason := pathInt(r, "season")
	episode, okEpisode := pathInt(r, "episode")
	if !okSeason || !okEpisode {
		jsonError(w, http.StatusBadRequest, "invalid episode path")
		return
	}
	var input IgnoreEpisodeInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.SetEpisodeIgnored(series, season, episode, input.Ignored, input.Reason); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "ignored": input.Ignored})
}

// ListBackups implements `list_backups`.
func ListBackups(w http.ResponseWriter, r *http.Request, s *AppState) {
	root := filepath.Join(s.cfg.DataDir, "backups")
	items := []map[string]any{}
	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".zip" {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				continue
			}
			name := entry.Name()
			var modified any
			var modifiedSeconds *uint64
			modTime := info.ModTime()
			if !modTime.IsZero() && !modTime.Before(time.Unix(0, 0)) {
				seconds := uint64(modTime.Unix())
				modified = seconds
				modifiedSeconds = &seconds
			}
			items = append(items, map[string]any{
				"name":       name,
				"path":       filepath.Join(root, name),
				"size_bytes": info.Size(),
				"modified":   modified,
				"label":      gh5_backupLabel(name, modifiedSeconds),
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left := gh5_modifiedSeconds(items[i]["modified"])
		right := gh5_modifiedSeconds(items[j]["modified"])
		if left != right {
			return left > right
		}
		return items[i]["name"].(string) > items[j]["name"].(string)
	})
	jsonResponse(w, map[string]any{"items": items})
}

// ValidateBackup verifies a named local backup before a recovery operation.
// It never extracts the archive: ZIP names are untrusted and a validation must
// not be able to write into the data directory.
func ValidateBackup(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, "Nome backup non valido")
		return
	}
	name := filepath.Base(strings.TrimSpace(input.Name))
	if name == "." || name == "" || name != strings.TrimSpace(input.Name) || filepath.Ext(name) != ".zip" {
		jsonError(w, http.StatusBadRequest, "Seleziona un file ZIP dalla lista dei backup")
		return
	}
	path := filepath.Join(latestConfig(s).DataDir, "backups", name)
	if err := verifyZipArchive(path); err != nil {
		logging.Warn("backup verification failed", "path", path, "error", err)
		jsonError(w, http.StatusUnprocessableEntity, "Il backup non è leggibile o è danneggiato; usa un’altra copia.")
		return
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "Il backup non è leggibile o è danneggiato; usa un’altra copia.")
		return
	}
	defer reader.Close()
	archiveDatabases := make([]string, 0, len(databases))
	known := make(map[string]struct{}, len(databases))
	for _, database := range databases {
		known[database] = struct{}{}
	}
	for _, entry := range reader.File {
		if _, ok := known[entry.Name]; ok {
			archiveDatabases = append(archiveDatabases, entry.Name)
		}
	}
	sort.Strings(archiveDatabases)
	jsonResponse(w, map[string]any{
		"ok":        true,
		"name":      name,
		"valid":     true,
		"entries":   len(reader.File),
		"databases": archiveDatabases,
		"message":   "Backup verificato: lo ZIP è leggibile e tutti i file interni hanno superato il controllo d’integrità.",
	})
}

// MediaInfoGet implements `media_info_get`.
func MediaInfoGet(w http.ResponseWriter, r *http.Request, s *AppState) {
	query := r.URL.Query()
	var info map[string]any
	var err error
	if query.Has("series") && query.Has("season") && query.Has("episode") {
		season, seasonErr := strconv.ParseInt(query.Get("season"), 10, 64)
		if seasonErr != nil {
			jsonError(w, http.StatusBadRequest, seasonErr.Error())
			return
		}
		episode, episodeErr := strconv.ParseInt(query.Get("episode"), 10, 64)
		if episodeErr != nil {
			jsonError(w, http.StatusBadRequest, episodeErr.Error())
			return
		}
		info, err = s.db.EpisodeMediaInfo(query.Get("series"), season, episode)
	} else if query.Has("movie") {
		var year *int64
		if query.Has("year") {
			parsed, yearErr := strconv.ParseInt(query.Get("year"), 10, 64)
			if yearErr != nil {
				jsonError(w, http.StatusBadRequest, yearErr.Error())
				return
			}
			year = &parsed
		}
		info, err = s.db.MovieMediaInfo(query.Get("movie"), year)
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "info": info})
}

// MoviesSeenGroupedView implements `movies_seen_grouped_view`.
func MoviesSeenGroupedView(w http.ResponseWriter, r *http.Request, s *AppState) {
	gh5_seenGrouped(w, r, s, "movie")
}

func gh5_seenGrouped(w http.ResponseWriter, r *http.Request, s *AppState, kind string) {
	limit := queryInt(r, "limit", 50)
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
	offset := (page - 1) * limit
	queryText := queryParam(r, "q")
	var (
		groups []FeedSeenGroup
		total  int64
		err    error
	)
	if kind == "series" {
		groups, total, err = s.db.SeriesSeenGrouped(int(offset), int(limit), queryText)
	} else {
		groups, total, err = s.db.MoviesSeenGrouped(int(offset), int(limit), queryText)
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pages := int64(1)
	if limit > 0 {
		pages = (total + limit - 1) / limit
	}
	if pages < 1 {
		pages = 1
	}
	jsonResponse(w, map[string]any{
		"ok":     true,
		"groups": groups,
		"total":  total,
		"page":   page,
		"pages":  pages,
	})
}

// ProvidersStatusView implements `providers_status_view`.
func ProvidersStatusView(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.ProviderStatuses()
	if err != nil {
		items = []models.ProviderStatus{}
	}
	for index := range items {
		items[index].UserMessage, items[index].SuggestedAction = gh5_providerGuidance(items[index])
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

// gh5_providerGuidance keeps connection details in the diagnostic field while
// giving the person operating Gextto an immediately useful explanation. The
// provider is retried automatically after its backoff; "Azzera" is only useful
// once the external cause has been fixed.
func gh5_providerGuidance(status models.ProviderStatus) (string, string) {
	errText := strings.ToLower(status.LastError)
	switch {
	case strings.Contains(errText, "429") || strings.Contains(errText, "rate limit"):
		return "Il provider ha limitato temporaneamente le richieste.", "Attendi il nuovo tentativo automatico; riduci le ricerche manuali se il problema continua."
	case strings.Contains(errText, "401") || strings.Contains(errText, "403") || strings.Contains(errText, "unauthorized") || strings.Contains(errText, "forbidden"):
		return "Il provider ha rifiutato l’accesso.", "Controlla credenziali, API key o cookie del provider; poi usa Azzera per riprovare subito."
	case strings.Contains(errText, "404"):
		return "L’indirizzo configurato del provider non è stato trovato.", "Controlla URL e percorso del provider; poi usa Azzera per riprovare subito."
	case strings.Contains(errText, "timeout") || strings.Contains(errText, "deadline exceeded"):
		return "Il provider non ha risposto entro il tempo previsto.", "Verifica rete e disponibilità del provider; Gextto riproverà automaticamente."
	case strings.Contains(errText, "no such host") || strings.Contains(errText, "network is unreachable") || strings.Contains(errText, "connection refused") || strings.Contains(errText, "x509"):
		return "Il provider non è raggiungibile dalla rete di Gextto.", "Verifica DNS, rete, certificato e URL; poi usa Azzera per riprovare subito."
	case strings.Contains(errText, "5") && strings.Contains(errText, "http"):
		return "Il provider ha segnalato un errore temporaneo del proprio servizio.", "Attendi il nuovo tentativo automatico; non è necessaria una modifica locale."
	default:
		return "Il provider non ha completato l’ultima richiesta.", "Consulta il dettaglio tecnico, verifica la configurazione e usa Azzera dopo la correzione."
	}
}

// RemoveComic implements `remove_comic`.
func RemoveComic(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid comic id")
		return
	}
	removed, err := s.comics.RemoveMonitored(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		jsonError(w, http.StatusNotFound, "comic not found")
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// RestartTorrent implements `restart_torrent`.
func RestartTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	result, err := s.activeEngine().Restart(hash)
	gh5_torrentAction(w, result, err)
}

// SaveMoviesConfig implements `save_movies_config`.
func SaveMoviesConfig(w http.ResponseWriter, r *http.Request, s *AppState) {
	var movies []MovieConfig
	if err := decodeJSON(r, &movies); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(movies) > 500 {
		jsonError(w, http.StatusBadRequest, "too many movies")
		return
	}
	cfg := latestConfig(s)
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gh5_purgeRemovedLibrary(s.db, cfg.Series, cfg.Movies, cfg.Series, movies)
	jsonResponse(w, map[string]any{"ok": true, "movies": len(movies)})
}

// ScanSeriesArchive implements `scan_series_archive`.
func ScanSeriesArchive(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	name := pathParam(r, "name")
	series := gh5_findSeries(cfg, name)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	if gh5_archiveImportBusyContains(series.Name) {
		jsonResponse(w, map[string]any{
			"ok":      true,
			"skipped": true,
			"reason":  "archive import in progress",
		})
		return
	}
	var requestedPath *string
	body, bodyErr := gh5_readOptionalBody(r)
	if bodyErr != nil {
		jsonError(w, http.StatusBadRequest, bodyErr.Error())
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		var input ArchiveScanInput
		if err := json.Unmarshal(body, &input); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		requestedPath = input.Path
	}
	path := series.ArchivePath
	if requestedPath != nil && strings.TrimSpace(*requestedPath) != "" {
		path = *requestedPath
	}
	found, updated, err := gh5_scanArchivePath(s.db, series, path, cfg)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":      true,
		"found":   found,
		"updated": updated,
		"series":  series.Name,
		"path":    path,
	})
}

// SeriesInfo implements `series_info`.
func SeriesInfo(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	name := pathParam(r, "name")
	series := gh5_findSeries(cfg, name)
	if series == nil {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	ctx := r.Context()
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	var tmdbID *string
	if strings.TrimSpace(series.TmdbID) != "" {
		value := series.TmdbID
		tmdbID = &value
	}
	value, err := tmdb.SeriesInfo(ctx, series.Name, tmdbID)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if value == nil {
		jsonResponse(w, map[string]any{"ok": true, "info": nil})
		return
	}

	var poster *string
	if path, ok := value["poster_path"].(string); ok {
		address := "https://image.tmdb.org/t/p/w300" + path
		poster = &address
	}
	var network *string
	if items, ok := value["networks"].([]any); ok && len(items) > 0 {
		if item, ok := items[0].(map[string]any); ok {
			if label, ok := item["name"].(string); ok {
				network = &label
			}
		}
	}
	var year *string
	if text, ok := value["first_air_date"].(string); ok {
		runes := []rune(text)
		if len(runes) > 4 {
			runes = runes[:4]
		}
		trimmed := string(runes)
		year = &trimmed
	}
	var country *string
	if items, ok := value["origin_country"].([]any); ok && len(items) > 0 {
		if text, ok := items[0].(string); ok {
			country = &text
		}
	}
	if country == nil {
		if items, ok := value["production_countries"].([]any); ok && len(items) > 0 {
			if item, ok := items[0].(map[string]any); ok {
				if text, ok := item["iso_3166_1"].(string); ok {
					country = &text
				}
			}
		}
	}
	var lastAirDate *string
	if text, ok := value["last_air_date"].(string); ok {
		lastAirDate = &text
	}

	tvdb := WithLanguage(cfg.TvdbAPIKey(), cfg.TvdbLanguage())
	var tvdbID *int64
	if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(series.TvdbID), 10, 64); parseErr == nil {
		tvdbID = &parsed
	}
	if tvdbID == nil {
		if results, searchErr := tvdb.SearchSeries(ctx, series.Name); searchErr == nil && len(results) > 0 {
			if item, ok := results[0].(map[string]any); ok {
				if parsed, ok := gh5_asInt64(item["tvdb_id"]); ok {
					tvdbID = &parsed
				} else if text, ok := item["tvdb_id"].(string); ok {
					if parsed, parseErr := strconv.ParseInt(text, 10, 64); parseErr == nil {
						tvdbID = &parsed
					}
				}
			}
		}
	}

	cast := []any{}
	if tvdbID != nil {
		if characters, charErr := tvdb.SeriesCharacters(ctx, *tvdbID); charErr == nil {
			for _, character := range characters {
				item, _ := character.(map[string]any)
				personName, hasName := item["name"].(string)
				person, hasPerson := gh5_asInt64(item["tvdb_id"])
				if hasName && hasPerson {
					cast = append(cast, map[string]any{
						"name": personName,
						"url":  fmt.Sprintf("https://thetvdb.com/dereferrer/people/%d", person),
					})
				}
			}
		}
	}
	if len(cast) == 0 {
		if id, ok := gh5_asInt64(value["id"]); ok {
			if personCast, castErr := tmdb.SeriesCast(ctx, strconv.FormatInt(id, 10)); castErr == nil {
				for _, entry := range personCast {
					cast = append(cast, map[string]any{
						"name": entry.Name,
						"url":  fmt.Sprintf("https://www.themoviedb.org/person/%d", entry.ID),
					})
				}
			}
		}
	}

	genres := []string{}
	if items, ok := value["genres"].([]any); ok {
		for _, item := range items {
			if entry, ok := item.(map[string]any); ok {
				if label, ok := entry["name"].(string); ok {
					genres = append(genres, label)
				}
			}
		}
	}
	tvdbURL := ""
	if tvdbID != nil {
		tvdbURL = fmt.Sprintf("https://thetvdb.com/dereferrer/series/%d", *tvdbID)
	} else {
		tvdbURL = "https://thetvdb.com/search?query=" + url.QueryEscape(series.Name)
	}

	jsonResponse(w, map[string]any{
		"ok": true,
		"info": map[string]any{
			"name":           value["name"],
			"overview":       value["overview"],
			"poster":         poster,
			"network":        network,
			"year":           year,
			"seasons":        value["number_of_seasons"],
			"episodes_total": value["number_of_episodes"],
			"vote":           value["vote_average"],
			"status":         value["status"],
			"country":        country,
			"last_air_date":  lastAirDate,
			"last_episode":   value["last_episode_to_air"],
			"next_episode":   value["next_episode_to_air"],
			"tmdb_id":        value["id"],
			"tvdb_id":        series.TvdbID,
			"tvdb_url":       tvdbURL,
			"cast":           cast,
			"genres":         genres,
		},
	})
}

// ServiceRestart implements `service_restart`.
func ServiceRestart(w http.ResponseWriter, r *http.Request, s *AppState) {
	action := "restart"
	body, bodyErr := gh5_readOptionalBody(r)
	if bodyErr != nil {
		jsonError(w, http.StatusBadRequest, bodyErr.Error())
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		var input struct {
			Action *string `json:"action"`
		}
		if err := json.Unmarshal(body, &input); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.Action != nil {
			candidate := strings.ToLower(strings.TrimSpace(*input.Action))
			if candidate == "restart" || candidate == "start" || candidate == "stop" {
				action = candidate
			}
		}
	}

	// Validate the action once and map it to a literal so the command never
	// receives a value derived from the request.
	verb := "restart"
	switch action {
	case "start":
		verb = "start"
	case "stop":
		verb = "stop"
	}

	// A `systemctl --user` unit runs under the same user as the daemon, so it
	// can be controlled directly without the root helper.
	if scope, scopeName := gh6_serviceScope("gextto.service"); scopeName == "user" {
		go func() {
			time.Sleep(800 * time.Millisecond)
			_ = exec.Command("systemctl", append(append([]string{}, scope...), verb, "gextto.service")...).Run()
		}()
		jsonResponse(w, map[string]any{
			"ok":      true,
			"action":  action,
			"scope":   "user",
			"message": "azione sul servizio richiesta",
		})
		return
	}

	const restartHelper = "/usr/local/bin/gextto-restart"
	if _, err := os.Stat(restartHelper); err != nil {
		jsonError(w, http.StatusPreconditionRequired, "Helper di riavvio non installato. Da root, una volta: sudo install -m 0755 scripts/gextto-restart /usr/local/bin/gextto-restart && sudo install -m 0440 systemd/gextto.sudoers /etc/sudoers.d/gextto")
		return
	}
	allowed := exec.Command("sudo", "-n", "-l", restartHelper).Run() == nil
	if !allowed {
		jsonError(w, http.StatusForbidden, "Utente non autorizzato al riavvio: installa la regola sudoers systemd/gextto.sudoers.")
		return
	}
	go func() {
		time.Sleep(800 * time.Millisecond)
		_ = exec.Command("sudo", "-n", restartHelper, verb).Run()
	}()
	jsonResponse(w, map[string]any{
		"ok":      true,
		"action":  action,
		"message": "azione sul servizio richiesta",
	})
}

// SetSuperSeeding implements `set_super_seeding`.
func SetSuperSeeding(w http.ResponseWriter, r *http.Request, s *AppState) {
	client, err := s.requireEmbedded("super_seeding")
	if err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	hash := pathParam(r, "hash")
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := client.SetSuperSeeding(hash, input.Enabled)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !result {
		jsonError(w, http.StatusConflict, "torrent unavailable in dry-run")
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// SetupCompleteExisting implements `setup_complete_existing`.
func SetupCompleteExisting(w http.ResponseWriter, r *http.Request, s *AppState) {
	if err := CompleteSetup(s.cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// SimklStatus implements `simkl_status`.
func SimklStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := new(SimklClient).FromSettings(cfg.Settings)
	jsonResponse(w, map[string]any{
		"configured":    client.Configured(),
		"authenticated": client.Authenticated(),
	})
}

// TmdbAdd implements `tmdb_add`.
func TmdbAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TmdbAddInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.TmdbId) == "" {
		jsonError(w, http.StatusBadRequest, "TMDB item is incomplete")
		return
	}
	cfg, err := LoadConfig(s.config_path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.EqualFold(input.Kind, "movie") {
		for index := range cfg.Movies {
			if strings.EqualFold(cfg.Movies[index].Name, strings.TrimSpace(input.Name)) && cfg.Movies[index].Year == input.Year {
				jsonError(w, http.StatusConflict, "film già presente")
				return
			}
		}
		language := strings.TrimSpace(input.Language)
		if language == "" {
			language = "ita"
		}
		cfg.Movies = append(cfg.Movies, MovieConfig{
			ID:                   0,
			Name:                 strings.TrimSpace(input.Name),
			Year:                 strings.TrimSpace(input.Year),
			TmdbID:               strings.TrimSpace(input.TmdbId),
			TvdbID:               "",
			OriginalTitle:        "",
			Overview:             "",
			PosterPath:           "",
			Quality:              strings.TrimSpace(input.Quality),
			Language:             language,
			Enabled:              true,
			Subtitle:             strings.TrimSpace(input.Subtitle),
			Exclude:              strings.TrimSpace(input.Exclude),
			LanguageRequirements: "",
			SubtitleRequirements: "",
			DisableUpgrades:      false,
		})
	} else {
		for index := range cfg.Series {
			if strings.EqualFold(cfg.Series[index].Name, strings.TrimSpace(input.Name)) {
				jsonError(w, http.StatusConflict, "serie già presente")
				return
			}
		}
		seasons := strings.TrimSpace(input.Seasons)
		if seasons == "" {
			seasons = "1+"
		}
		language := strings.TrimSpace(input.Language)
		if language == "" {
			language = "ita"
		}
		cfg.Series = append(cfg.Series, SeriesConfig{
			Name:             strings.TrimSpace(input.Name),
			Seasons:          seasons,
			Quality:          strings.TrimSpace(input.Quality),
			Language:         language,
			ArchivePath:      strings.TrimSpace(input.ArchivePath),
			Timeframe:        0,
			Aliases:          splitAliases(input.Aliases),
			TmdbID:           strings.TrimSpace(input.TmdbId),
			TvdbID:           strings.TrimSpace(input.TvdbId),
			Subtitle:         strings.TrimSpace(input.Subtitle),
			Exclude:          strings.TrimSpace(input.Exclude),
			Enabled:          true,
			IgnoredSeasons:   nil,
			SeasonSubfolders: false,
			DisableUpgrades:  false,
		})
	}
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":               true,
		"name":             input.Name,
		"restart_required": true,
	})
}

// splitAliases parses a comma separated alias list into a clean slice.
func splitAliases(raw string) []string {
	aliases := []string{}
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			aliases = append(aliases, trimmed)
		}
	}
	if len(aliases) == 0 {
		return nil
	}
	return aliases
}

// TorrentNoRenameList implements `torrent_no_rename_list`.
func TorrentNoRenameList(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.NoRenameTorrents()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	torrents := make([]map[string]any, 0, len(items))
	for _, item := range items {
		torrents = append(torrents, map[string]any{"hash": item[0], "name": item[1]})
	}
	jsonResponse(w, map[string]any{"ok": true, "torrents": torrents})
}

// TraktAuthRefresh implements `trakt_auth_refresh`.
func TraktAuthRefresh(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not save integration tokens")
		return
	}
	cfg := latestConfig(s)
	client := new(TraktClient).FromSettings(cfg.Settings)
	if !client.Configured() || !client.Authenticated() {
		jsonError(w, http.StatusConflict, "Trakt non configurato")
		return
	}
	value, err := client.Refresh(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	access, accessErr := TokenString(value, "access_token")
	refresh, refreshErr := TokenString(value, "refresh_token")
	if accessErr != nil || refreshErr != nil {
		jsonError(w, http.StatusBadGateway, "Trakt response did not contain refresh tokens")
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
	jsonResponse(w, map[string]any{"ok": true})
}

// TraktWatchlistImport implements `trakt_watchlist_import`.
func TraktWatchlistImport(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := *latestConfig(s)
	value, err := new(TraktClient).FromSettings(cfg.Settings).Watchlist(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	entries := []gh5_watchlistEntry{}
	gh5_collectWatchlistInto(value, &entries)
	report := gh5_applyWatchlistImport(&cfg, entries)
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":     true,
		"source": "trakt",
		"found":  len(entries),
		"report": report,
	})
}
