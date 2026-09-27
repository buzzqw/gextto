package gextto

// web_handlers_g1.go ports the `group g1` HTTP handlers from gextto's
// the daemon. Handler signatures follow the web.go contract:
//
//	func Name(w http.ResponseWriter, r *http.Request, s *AppState)
//
// Shared helpers and core input structs live in web.go. Only file-local
// private helpers/types are declared here and prefixed `gh1_`.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/rules"
	"github.com/buzzqw/gextto/internal/utils"
)

// ---------------------------------------------------------------------------
// File-local private types ( structs declared outside the web module lines 372-955,
// so they are not part of the core web.go input set).
// ---------------------------------------------------------------------------

type gh1_ComicWeeklySettings struct {
	Enabled  bool    `json:"enabled"`
	FromDate *string `json:"from_date"`
}

// gh1_decodeOptionalJSON decodes an optional JSON body: an empty body leaves
// target untouched ( `Option<Json<T>>` = None), a malformed body is an
// error.
func gh1_decodeOptionalJSON(r *http.Request, target any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, target)
}

// ---------------------------------------------------------------------------
// File-local private helpers.
// ---------------------------------------------------------------------------

// gh1_torrentAction mirrors the web module `torrent_action`.
func gh1_torrentAction(ok bool, err error) (int, any) {
	if err != nil {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
	}
	if ok {
		return http.StatusOK, map[string]any{"ok": true}
	}
	return http.StatusNotFound, map[string]any{"ok": false, "error": "torrent unavailable in dry-run"}
}

// gh1_torrentDisplayName mirrors the web module `torrent_display_name`.
func gh1_torrentDisplayName(torrents TorrentSession, hash string) string {
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

// gh1_torrentFilesAreDisposable mirrors the web module `torrent_files_are_disposable`.
func gh1_torrentFilesAreDisposable(db *Database, hash string) bool {
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

// gh1_mismatchedPackRelease mirrors the web module `mismatched_pack_release`.
func gh1_mismatchedPackRelease(torrents TorrentSession, db *Database, hash string) *models.Release {
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return nil
	}
	release := meta.Release
	if release.Kind != "series" || !release.IsPack {
		return nil
	}
	name := gh1_torrentDisplayName(torrents, hash)
	corrected := ReconcilePackIdentity(&release, name)
	if corrected == nil {
		return nil
	}
	sameSeason := (corrected.Season == nil) == (release.Season == nil)
	if sameSeason && corrected.Season != nil && release.Season != nil {
		sameSeason = *corrected.Season == *release.Season
	}
	if sameSeason {
		return nil
	}
	result := release
	return &result
}

// gh1_blocklistMismatchedPack mirrors the web module `blocklist_mismatched_pack`.
func gh1_blocklistMismatchedPack(torrents TorrentSession, db *Database, hash string) bool {
	release := gh1_mismatchedPackRelease(torrents, db, hash)
	if release == nil {
		return false
	}
	if blocked, _ := db.IsBlocklisted(hash); blocked {
		return true
	}
	if err := db.Blocklist(release, "season_pack_identity_mismatch"); err != nil {
		logging.Warn("could not blocklist mismatched season pack", "hash", hash, "error", err.Error())
		return false
	}
	logging.Warn("mismatched season pack permanently blocklisted", "hash", hash, "title", release.Title)
	return true
}

// gh1_mediaTestError mirrors the web module `media_test_error`.
func gh1_mediaTestError(provider string, status int) string {
	switch status {
	case 401, 403:
		return fmt.Sprintf("%s: credenziali non valide", provider)
	case 404:
		return fmt.Sprintf("%s: endpoint non trovato, controlla l'URL", provider)
	default:
		return fmt.Sprintf("%s: HTTP %d", provider, status)
	}
}

// gh1_purgeRemovedLibrary mirrors the web module `purge_removed_library`.
func gh1_purgeRemovedLibrary(db *Database, previousSeries []SeriesConfig, previousMovies []MovieConfig, series []SeriesConfig, movies []MovieConfig) {
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
				logging.Warn("impossibile rimuovere lo stato della serie eliminata", "error", err.Error(), "series", old.Name)
			}
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
		if !present {
			if _, err := db.PurgeMovie(old.Name); err != nil {
				logging.Warn("impossibile rimuovere lo stato del film eliminato", "error", err.Error(), "movie", old.Name)
			}
		}
	}
}

// gh1_archiveReleasesForQuery mirrors the web module `archive_releases_for_query`.
func gh1_archiveReleasesForQuery(s *AppState, cfg *Config, query string) []models.Release {
	entries, _ := s.archive.Search(query)
	results := make([]models.Release, 0, len(entries))
	for _, entry := range entries {
		release := ParseRelease(entry[0], entry[1], "archive:"+entry[2])
		if release == nil {
			continue
		}
		if reason := cfg.AllReleaseDeniedReason(release); reason != "" {
			if cfg.ReleaseIsMonitored(release) {
				rules.LogRejection(release, reason)
			}
		} else {
			results = append(results, *release)
		}
	}
	return results
}

var (
	gh1_feedStatusMu    sync.Mutex
	gh1_feedStatusAt    time.Time
	gh1_feedStatusValue map[string]any
)

// gh1_computeFeedStatus mirrors the web module `compute_feed_status`.
func gh1_computeFeedStatus(s *AppState) map[string]any {
	cfg := latestConfig(s)
	entries, _ := s.archive.RecentEntries(40000)
	type feedEntry struct {
		lower  string
		title  string
		magnet string
		source string
	}
	list := make([]feedEntry, 0, len(entries))
	for _, entry := range entries {
		title, magnet, source := entry[0], entry[1], entry[2]
		if strings.HasPrefix(source, "archive:") || strings.HasPrefix(source, "timeframe") {
			continue
		}
		list = append(list, feedEntry{strings.ToLower(title), title, magnet, source})
	}
	find := func(query string) []map[string]any {
		words := strings.Fields(strings.ToLower(query))
		matches := []map[string]any{}
		if len(words) == 0 {
			return matches
		}
		for _, entry := range list {
			all := true
			for _, word := range words {
				if !strings.Contains(entry.lower, word) {
					all = false
					break
				}
			}
			if !all {
				continue
			}
			matches = append(matches, map[string]any{
				"title":  entry.title,
				"magnet": entry.magnet,
				"source": entry.source,
			})
			if len(matches) >= 8 {
				break
			}
		}
		return matches
	}
	items := []map[string]any{}
	seriesCount := 0
	for _, series := range cfg.Series {
		if !series.Enabled {
			continue
		}
		if seriesCount >= 120 {
			break
		}
		seriesCount++
		items = append(items, map[string]any{
			"kind":    "series",
			"name":    series.Name,
			"matches": find(series.Name),
		})
	}
	movieCount := 0
	for _, movie := range cfg.Movies {
		if !movie.Enabled {
			continue
		}
		if movieCount >= 120 {
			break
		}
		movieCount++
		query := fmt.Sprintf("%s %s", movie.Name, movie.Year)
		items = append(items, map[string]any{
			"kind":    "movie",
			"name":    movie.Name,
			"matches": find(query),
		})
	}
	value := map[string]any{"ok": true, "items": items}
	gh1_feedStatusMu.Lock()
	gh1_feedStatusAt = time.Now()
	gh1_feedStatusValue = value
	gh1_feedStatusMu.Unlock()
	return value
}

// ---------------------------------------------------------------------------
// Handlers.
// ---------------------------------------------------------------------------

func AddComic(w http.ResponseWriter, r *http.Request, s *AppState) {
	if !SetupComplete(s.cfg) {
		jsonError(w, http.StatusConflict, "complete the initial setup first")
		return
	}
	var input ComicInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.TagUrl) == "" {
		jsonError(w, http.StatusBadRequest, "title and tag_url are required")
		return
	}
	id, err := s.comics.AddMonitoredWithMetadata(
		input.Title,
		input.TagUrl,
		input.PostUrl,
		input.CoverUrl,
		input.Publisher,
		input.Description,
		input.FromDate,
		input.SavePath,
	)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

func BackupSendTelegram(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	notifier := FromConfig(cfg)
	dataDir := cfg.DataDir
	root := filepath.Join(dataDir, "backups")
	retain := 5
	if value, ok := cfg.Settings["backup_retention"]; ok {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			retain = int(parsed)
		}
	}
	if retain < 1 {
		retain = 1
	}
	if retain > 100 {
		retain = 100
	}
	path, err := CreateSnapshot(dataDir, root, retain)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sent, err := notifier.NotifyBackupDocument(path, "Gextto backup")
	if err != nil {
		sent = false
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "path": path, "sent": sent})
}

func CheckPorts(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	ports := []map[string]any{}
	for port := int(cfg.Libtorrent.PortMin); port <= int(cfg.Libtorrent.PortMax); port++ {
		address := fmt.Sprintf("0.0.0.0:%d", port)
		tcpAvailable := false
		if listener, err := net.Listen("tcp", address); err == nil {
			tcpAvailable = true
			_ = listener.Close()
		}
		udpAvailable := false
		if packet, err := net.ListenPacket("udp", address); err == nil {
			udpAvailable = true
			_ = packet.Close()
		}
		ports = append(ports, map[string]any{
			"port":          port,
			"available":     tcpAvailable && udpAvailable,
			"tcp_available": tcpAvailable,
			"udp_available": udpAvailable,
		})
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":       true,
		"port_min": cfg.Libtorrent.PortMin,
		"port_max": cfg.Libtorrent.PortMax,
		"ports":    ports,
	})
}

func ComicDownloadPause(w http.ResponseWriter, r *http.Request, s *AppState) {
	id := pathParam(r, "id")
	if PauseHTTPDownload(id) {
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	jsonError(w, http.StatusConflict, "download non in scarico")
}

func ComicWeeklySettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input gh1_ComicWeeklySettings
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	fromDate := ""
	if input.FromDate != nil {
		fromDate = strings.TrimSpace(*input.FromDate)
	}
	if fromDate != "" && !gh1_validDate(fromDate) {
		jsonError(w, http.StatusBadRequest, "data weekly non valida: usare YYYY-MM-DD")
		return
	}
	enabled := "no"
	if input.Enabled {
		enabled = "yes"
	}
	if err := s.comics.SetSetting("weekly_enabled", enabled); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.comics.SetSetting("weekly_from_date", fromDate); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":               true,
		"weekly_enabled":   input.Enabled,
		"weekly_from_date": fromDate,
	})
}

// gh1_validDate reports whether value is a strict YYYY-MM-DD date.
func gh1_validDate(value string) bool {
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func DbInfo(w http.ResponseWriter, r *http.Request, s *AppState) {
	info, err := s.db.Info()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files := []map[string]any{}
	for _, name := range []string{
		"gextto_series.db",
		"gextto_archive.db",
		"gextto_config.db",
		"gextto_comics.db",
	} {
		path := filepath.Join(s.cfg.DataDir, name)
		sizeBytes := int64(0)
		exists := false
		if stat, err := os.Stat(path); err == nil {
			sizeBytes = stat.Size()
			exists = !stat.IsDir()
		}
		files = append(files, map[string]any{
			"name":       name,
			"path":       path,
			"size_bytes": sizeBytes,
			"exists":     exists,
		})
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "info": info, "files": files})
}

func DeleteMovie(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cfg := *latestConfig(s)
	previousMovies := append([]MovieConfig(nil), cfg.Movies...)
	before := len(cfg.Movies)
	filtered := make([]MovieConfig, 0, len(cfg.Movies))
	for _, movie := range cfg.Movies {
		if movie.ID != id {
			filtered = append(filtered, movie)
		}
	}
	cfg.Movies = filtered
	if before == len(cfg.Movies) {
		jsonError(w, http.StatusNotFound, "movie not found")
		return
	}
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gh1_purgeRemovedLibrary(s.db, cfg.Series, previousMovies, cfg.Series, cfg.Movies)
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

func FeedStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	gh1_feedStatusMu.Lock()
	cached := gh1_feedStatusValue
	computedAt := gh1_feedStatusAt
	gh1_feedStatusMu.Unlock()
	if cached != nil && time.Since(computedAt) < 60*time.Second {
		jsonResponse(w, cached)
		return
	}
	jsonResponse(w, gh1_computeFeedStatus(s))
}

func JellyfinTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	url := cfg.Settings["jellyfin_url"]
	key := cfg.Settings["jellyfin_api_key"]
	if strings.TrimSpace(url) == "" || strings.TrimSpace(key) == "" {
		jsonError(w, http.StatusBadRequest, "Jellyfin non configurato")
		return
	}
	endpoint := strings.TrimRight(url, "/") + "/System/Info"
	client := &http.Client{Timeout: 15 * time.Second}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	request.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=\"%s\"", key))
	request.Header.Set("X-Emby-Token", key)
	response, err := client.Do(request)
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		var info map[string]any
		_ = json.NewDecoder(response.Body).Decode(&info)
		server, _ := info["ServerName"].(string)
		version, _ := info["Version"].(string)
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "server": server, "version": version})
		return
	}
	jsonStatus(w, http.StatusBadGateway, map[string]any{
		"ok":    false,
		"error": gh1_mediaTestError("Jellyfin", response.StatusCode),
	})
}

func ManualArchiveSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	if !SetupComplete(s.cfg) {
		jsonError(w, http.StatusConflict, "complete the initial setup first")
		return
	}
	var input SearchInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, "query must contain 1-256 characters")
		return
	}
	cfg := latestConfig(s)
	results := gh1_archiveReleasesForQuery(s, cfg, query)
	seen := map[string]struct{}{}
	kept := results[:0]
	for _, release := range results {
		hash, ok := utils.MagnetHash(release.Magnet)
		if !ok {
			continue
		}
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		kept = append(kept, release)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return cfg.ReleaseScore(&kept[i]) > cfg.ReleaseScore(&kept[j])
	})
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "query": query, "results": kept})
}

func MovieHistoryHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.db.DownloadedMovies(500)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func PauseTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	status, value := gh1_torrentAction(s.activeEngine().Pause(hash))
	jsonStatus(w, status, value)
}

func RecheckTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	name := hash
	for _, torrent := range s.activeEngine().List() {
		if strings.EqualFold(torrent.Hash, hash) {
			name = torrent.Name
			break
		}
	}
	logging.Info("manual torrent integrity check requested", "hash", hash, "name", name)
	ok, err := s.activeEngine().ForceRecheck(hash)
	switch {
	case err != nil:
		logging.Error("manual torrent integrity check failed to start", "hash", hash, "name", name, "error", err.Error())
	case ok:
		logging.Info("manual torrent integrity check accepted by libtorrent", "hash", hash, "name", name)
	default:
		logging.Warn("manual torrent integrity check not started: torrent session unavailable", "hash", hash, "name", name)
	}
	status, value := gh1_torrentAction(ok, err)
	jsonStatus(w, status, value)
}

func RemoveTorrentWithOptions(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input RemoveOptionsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	identityMismatch := gh1_blocklistMismatchedPack(s.activeEngine(), s.db, hash)
	if input.Blocklist && !identityMismatch {
		if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
			_ = s.db.Blocklist(&meta.Release, "manual")
		}
	}
	deleteFiles := input.DeleteFiles || gh1_torrentFilesAreDisposable(s.db, hash)
	removed, removeErr := s.activeEngine().Remove(hash, deleteFiles)
	if removeErr == nil && removed {
		_ = s.db.MarkTorrentRemoved(hash)
		_ = s.db.ForgetRemovedTorrent(hash)
	}
	status, value := gh1_torrentAction(removed, removeErr)
	jsonStatus(w, status, value)
}

func RunNow(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg, err := LoadConfig(s.config_path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.cfg.DryRun {
		cfg.DryRun = true
	}
	if !SetupComplete(&cfg) {
		jsonError(w, http.StatusConflict, "complete the initial setup first")
		return
	}
	if !cfg.Active {
		jsonError(w, http.StatusConflict, "daemon inactive")
		return
	}
	var domain *string
	switch queryParam(r, "domain") {
	case "series", "movies", "comics":
		value := queryParam(r, "domain")
		domain = &value
	}
	running := true
	if s.cycle_lock.TryLock() {
		s.cycle_lock.Unlock()
		running = false
	}
	logging.Info("manual cycle requested", "domain", gh1_domainLabel(domain), "queued", running)
	taskDomain := domain
	go func() {
		s.cycle_lock.Lock()
		defer s.cycle_lock.Unlock()
		logging.Info("manual cycle started", "domain", gh1_domainLabel(taskDomain))
		now := time.Now().UTC()
		s.last_cycle.Set(models.CycleStats{LastStartedAt: &now, ErrorDetails: map[string]int{}})
		notifier := FromConfig(&cfg)
		stats, runErr := RunCycleDomain(
			context.Background(),
			&cfg,
			s.engine,
			s.db,
			s.archive,
			s.comics,
			notifier,
			s.activeEngine(),
			taskDomain,
		)
		if runErr != nil {
			logging.Error("manual cycle failed", "error", runErr.Error())
			return
		}
		if stats != nil {
			logging.Info("manual cycle completed",
				"downloads_started", stats.DownloadsStarted,
				"gaps_filled", stats.GapsFilled)
			s.last_cycle.Set(*stats)
		}
	}()
	message := "Ciclo avviato"
	if running {
		message = "Ciclo già in corso: richiesta accodata"
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"started": !running,
		"queued":  running,
		"message": message,
	})
}

// gh1_domainLabel mirrors the `unwrap_or("full")` label used in logs.
func gh1_domainLabel(domain *string) string {
	if domain == nil || *domain == "" {
		return "full"
	}
	return *domain
}

func SaveSourceFilters(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SourceFiltersInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Filters) > 200 {
		jsonError(w, http.StatusBadRequest, "too many source filters (max 200)")
		return
	}
	filters := input.Filters
	if filters == nil {
		filters = []SourceFilter{}
	}
	encoded, err := json.Marshal(filters)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload := string(encoded)
	if len(payload) > 100000 {
		jsonError(w, http.StatusRequestEntityTooLarge, "source filters too large")
		return
	}
	if err := SaveSetting(s.cfg.DataDir, "source_filters", payload); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "filters": filters})
}

func SelectRamdisk(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RamDiskSelectInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := strings.TrimSpace(input.Path)
	info, statErr := os.Stat(path)
	if statErr != nil || !info.IsDir() || !DirectoryWritable(path) {
		jsonError(w, http.StatusBadRequest, "il percorso RAM disk non esiste o non è scrivibile")
		return
	}
	recommended, err := SaveRamdiskSettings(s.cfg.DataDir, path)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("configurazione RAM disk fallita: %v", err),
		})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":          true,
		"path":        path,
		"recommended": recommended,
	})
}

func SeriesRenamePreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	var input RenameInput
	if err := gh1_decodeOptionalJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, value := seriesRenameApply(s, name, false, input.Force, input.SourceOnly)
	jsonStatus(w, status, value)
}

func SetLogLevel(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input LogLevel
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Level) > 128 || strings.TrimSpace(input.Level) == "" {
		jsonError(w, http.StatusBadRequest, "invalid log level")
		return
	}
	logging.SetLevel(input.Level)
	logging.Info("runtime log level changed", "level", input.Level)
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "level": input.Level})
}

func SetTorrentNoRename(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input NoRenameInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.SetTorrentNoRename(hash, input.Value); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "no_rename": input.Value})
}

func SimklAuthStart(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := new(SimklClient).FromSettings(cfg.Settings)
	if !client.Configured() {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "Simkl non configurato"})
		return
	}
	value, err := client.PinStart(r.Context())
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "data": value})
}

func SourcesHealth(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	rawTerm := queryParam(r, "q")
	var term *string
	if strings.TrimSpace(rawTerm) != "" {
		value := rawTerm
		term = &value
	}
	client := &http.Client{Timeout: 45 * time.Second}
	// Health probes are request-bound: a client that navigates away stops them.
	ctx := r.Context()

	tasks := []func() map[string]any{}
	if term != nil {
		feedMaxPages := cfg.FeedMaxPages()
		maxAgeDays := cfg.MaxReleaseAgeDays
		oldRatio := cfg.StopOnOldPageRatio()
		for _, feed := range cfg.FeedURLs {
			feed := feed
			tasks = append(tasks, func() map[string]any {
				items, err := FetchFeed(ctx, feed, cfg.FlaresolverrURL, feedMaxPages, maxAgeDays, oldRatio)
				if err != nil {
					return map[string]any{"kind": "feed", "name": feed, "ok": false, "results": int64(0), "error": err.Error()}
				}
				return map[string]any{"kind": "feed", "name": feed, "ok": true, "results": int64(len(items)), "error": nil}
			})
		}
		for _, indexer := range cfg.Indexers {
			if !indexer.Enabled {
				continue
			}
			indexer := indexer
			tasks = append(tasks, func() map[string]any {
				items, err := FetchTorznab(ctx, indexer, *term)
				if err != nil {
					return map[string]any{"kind": "indexer", "name": indexer.Name, "ok": false, "results": int64(0), "error": err.Error()}
				}
				return map[string]any{"kind": "indexer", "name": indexer.Name, "ok": true, "results": int64(len(items)), "error": nil}
			})
		}
		for _, engine := range cfg.WebsearchEngines {
			engine := engine
			tasks = append(tasks, func() map[string]any {
				items, err := runWebEngine(ctx, engine, *term, cfg.FlaresolverrURL)
				if err != nil {
					return map[string]any{"kind": "engine", "name": engine, "ok": false, "results": int64(0), "error": err.Error()}
				}
				return map[string]any{"kind": "engine", "name": engine, "ok": true, "results": int64(len(items)), "error": nil}
			})
		}
	} else {
		for _, feed := range cfg.FeedURLs {
			feed := feed
			tasks = append(tasks, func() map[string]any {
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
				if err != nil {
					return map[string]any{"kind": "feed", "name": feed, "url": feed, "ok": false, "status": int64(0), "error": err.Error()}
				}
				response, err := client.Do(request)
				if err != nil {
					return map[string]any{"kind": "feed", "name": feed, "url": feed, "ok": false, "status": int64(0), "error": err.Error()}
				}
				_ = response.Body.Close()
				ok := response.StatusCode >= 200 && response.StatusCode < 300
				return map[string]any{"kind": "feed", "name": feed, "url": feed, "ok": ok, "status": int64(response.StatusCode), "error": nil}
			})
		}
		for _, indexer := range cfg.Indexers {
			if !indexer.Enabled {
				continue
			}
			indexer := indexer
			tasks = append(tasks, func() map[string]any {
				items, err := FetchTorznab(ctx, indexer, "ita")
				if err != nil {
					return map[string]any{"kind": "indexer", "name": indexer.Name, "url": indexer.URL, "ok": false, "results": int64(0), "error": err.Error()}
				}
				return map[string]any{"kind": "indexer", "name": indexer.Name, "url": indexer.URL, "ok": true, "results": int64(len(items)), "error": nil}
			})
		}
		for _, engine := range cfg.WebsearchEngines {
			engine := engine
			tasks = append(tasks, func() map[string]any {
				items, err := runWebEngine(ctx, engine, "ita", cfg.FlaresolverrURL)
				if err != nil {
					return map[string]any{"kind": "engine", "name": engine, "ok": false, "results": int64(0), "error": err.Error()}
				}
				return map[string]any{"kind": "engine", "name": engine, "ok": true, "results": int64(len(items)), "error": nil}
			})
		}
	}

	var mu sync.Mutex
	items := []map[string]any{}
	var wait sync.WaitGroup
	for _, task := range tasks {
		wait.Add(1)
		go func(task func() map[string]any) {
			defer wait.Done()
			item := task()
			mu.Lock()
			items = append(items, item)
			mu.Unlock()
		}(task)
	}
	wait.Wait()

	var query any
	if term != nil {
		query = *term
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":           true,
		"query":        query,
		"items":        items,
		"flaresolverr": cfg.FlaresolverrURL != nil,
		"tmdb":         cfg.TmdbAPIKey != nil,
	})
}

func TorrentDetails(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	magnet := ""
	if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
		magnet = meta.Release.Magnet
	}
	for _, torrent := range s.activeEngine().List() {
		if !strings.EqualFold(torrent.Hash, hash) {
			continue
		}
		noRename, _ := s.db.TorrentNoRename(hash)
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":        true,
			"torrent":   torrent,
			"magnet":    magnet,
			"no_rename": noRename,
		})
		return
	}
	jsonError(w, http.StatusNotFound, "torrent not found")
}

func TorrentTags(w http.ResponseWriter, r *http.Request, s *AppState) {
	pairs, _ := s.db.TorrentTags()
	items := []map[string]any{}
	for _, pair := range pairs {
		items = append(items, map[string]any{"hash": pair[0], "tag": pair[1]})
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func TraktScrobble(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not scrobble")
		return
	}
	if !(&TraktClient{}).FromSettings(latestConfig(s).Settings).Configured() {
		jsonError(w, http.StatusConflict, "Trakt non configurato")
		return
	}
	var input ScrobbleInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	action := "stop"
	if input.Action != nil {
		action = *input.Action
	}
	cfg := latestConfig(s)
	value, err := new(TraktClient).FromSettings(cfg.Settings).Scrobble(r.Context(), action, input.Payload)
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, value)
}

func UnpinTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	ok, err := s.activeEngine().SetPin("", false)
	if err == nil {
		_ = SaveSetting(s.cfg.DataDir, "libtorrent_pinned_hash", "")
	}
	status, value := gh1_torrentAction(ok, err)
	jsonStatus(w, status, value)
}
