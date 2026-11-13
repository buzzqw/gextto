package gextto

// web_maintenance.go holds the maintenance API: the archive (browse, add,
// delete, batch download), database maintenance (vacuum, analyze, integrity
// check, pruning), the trash, duplicate cleanup and housekeeping, the MediaInfo
// backfill, and the helpers that queue a release coming from the archive.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

type backfillMediaInfoInput struct {
	Limit *int `json:"limit"`
}

func derefI64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func DbAction(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input DbActionInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action != "vacuum" && action != "analyze" && action != "check" {
		jsonError(w, http.StatusBadRequest, "action must be vacuum, analyze or check")
		return
	}
	if action == "check" {
		checks := runDbCheck(s)
		ok := true
		for _, check := range checks {
			if !check.OK {
				ok = false
				break
			}
		}
		jsonStatus(w, http.StatusOK, map[string]any{"ok": ok, "action": action, "checks": checks})
		return
	}
	result, err := runDbAction(s, action)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":              true,
		"action":          action,
		"before":          map[string]any{"size_bytes": result.BeforeSize, "rows": result.BeforeRows},
		"after":           map[string]any{"size_bytes": result.AfterSize, "rows": result.AfterRows},
		"reclaimed_bytes": result.BeforeSize - result.AfterSize,
	})
}

// dbCheck is the integrity result of one SQLite database. integrity_check
// also verifies ordinary SQLite indexes; the archive additionally checks its
// external-content FTS5 index against the archive table.
type dbCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func checkSQLite(db *sql.DB, checkFTS bool) (bool, string) {
	if db == nil {
		return false, "connessione database non disponibile"
	}
	rows, err := db.Query("PRAGMA integrity_check")
	if err != nil {
		return false, err.Error()
	}
	defer rows.Close()
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return false, err.Error()
		}
		if !strings.EqualFold(strings.TrimSpace(result), "ok") {
			return false, result
		}
	}
	if err := rows.Err(); err != nil {
		return false, err.Error()
	}
	foreignKeys, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return false, err.Error()
	}
	defer foreignKeys.Close()
	if foreignKeys.Next() {
		return false, "violazione di chiave esterna rilevata"
	}
	if err := foreignKeys.Err(); err != nil {
		return false, err.Error()
	}
	if checkFTS {
		if _, err := db.Exec("INSERT INTO archive_fts(archive_fts, rank) VALUES('integrity-check', 1)"); err != nil {
			return false, "indice FTS: " + err.Error()
		}
	}
	return true, "integrità e indici verificati"
}

// runDbCheck verifies the four SQLite files without modifying them.
func runDbCheck(s *AppState) []dbCheck {
	checks := make([]dbCheck, 0, 4)
	add := func(name string, db *sql.DB, fts bool) {
		ok, detail := checkSQLite(db, fts)
		checks = append(checks, dbCheck{Name: name, OK: ok, Detail: detail})
	}
	add("Serie e download", s.db.db, false)
	add("Archivio", s.archive.db, true)
	add("Fumetti", s.comics.db, false)
	config, err := OpenConfigDB(filepath.Join(s.cfg.DataDir, "gextto_config.db"))
	if err != nil {
		checks = append(checks, dbCheck{Name: "Configurazione", OK: false, Detail: err.Error()})
	} else {
		defer config.Close()
		add("Configurazione", config, false)
	}
	return checks
}

// dbActionResult carries the stable logical database size
// (page_count × page_size) before and after a maintenance action.
type dbActionResult struct {
	BeforeSize int64
	BeforeRows int64
	AfterSize  int64
	AfterRows  int64
}

func runDbAction(s *AppState, action string) (dbActionResult, error) {
	var result dbActionResult
	start := time.Now()
	logging.Debug("db maintenance action started", "action", action)
	configPath := filepath.Join(s.cfg.DataDir, "gextto_config.db")
	configSize := func(path string) int64 {
		conn, err := OpenConfigDB(path)
		if err != nil {
			return 0
		}
		defer conn.Close()
		return ConnectionSizeBytes(conn)
	}
	// collect intentionally excludes the transient WAL/SHM files. Their size can
	// spike during VACUUM and does not represent durable database usage.
	collect := func() (int64, int64) {
		size := s.db.DBSizeBytes() + s.archive.SizeBytes() + s.comics.SizeBytes() + configSize(configPath)
		rows := s.db.DBTotalRows()
		if count, err := s.archive.Count(); err == nil {
			rows += count
		}
		return size, rows
	}

	result.BeforeSize, result.BeforeRows = collect()

	if err := s.db.Optimize(action); err != nil {
		logging.Error("db maintenance failed on series db", "action", action, "error", err)
		return result, err
	}
	if err := s.archive.Optimize(action); err != nil {
		logging.Error("db maintenance failed on archive db", "action", action, "error", err)
		return result, err
	}
	if err := s.comics.Optimize(action); err != nil {
		logging.Error("Could not optimise the comics database", "error", err)
		return result, err
	}
	if conn, err := OpenConfigDB(configPath); err == nil {
		_ = OptimizeConnection(conn, action)
		conn.Close()
	}

	result.AfterSize, result.AfterRows = collect()
	if reclaimed := result.BeforeSize - result.AfterSize; reclaimed > 0 {
		logging.Info(fmt.Sprintf("🧹 Databases compacted: %s freed", logging.HumanBytesI64(int64(reclaimed))))
	}
	logging.Debug("db maintenance action finished",
		"action", action,
		"duration", time.Since(start).String(),
		"before_bytes", result.BeforeSize,
		"after_bytes", result.AfterSize,
		"reclaimed_bytes", result.BeforeSize-result.AfterSize)
	return result, nil
}

func AddArchiveEntry(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ArchiveAddInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	source := "archive"
	if strings.TrimSpace(input.Source) != "" {
		source = input.Source
	}
	release := ParseRelease(input.Title, input.Magnet, source)
	if release == nil {
		status, body := gh0AddRawMagnet(r.Context(), s, input.Magnet)
		jsonStatus(w, status, body)
		return
	}
	status, body := addRelease(r.Context(), s, *release)
	jsonStatus(w, status, body)
}

func gh0DownloadAndAdd(ctx context.Context, s *AppState, url string) (*string, string) {
	cfg := latestConfig(s)
	url = strings.TrimSpace(url)
	addMagnet := func(m string) (*string, string) {
		hash, ok := utils.MagnetHash(m)
		if !ok {
			return nil, "invalid magnet link"
		}
		added, err := s.activeEngine().AddWithOptions(m, cfg, nil, AddOptions{})
		if err != nil {
			return nil, err.Error()
		}
		if !added {
			return nil, ""
		}
		return &hash, ""
	}
	if strings.HasPrefix(url, "magnet:?") {
		return addMagnet(url)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	payload, magnet, err := s.engine.FetchTorrent(requestCtx, url)
	if err != nil {
		return nil, err.Error()
	}
	if magnet != "" {
		return addMagnet(magnet)
	}
	path := filepath.Join(cfg.StateDir, fmt.Sprintf(".manual-%d.torrent", time.Now().UnixNano()))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return nil, err.Error()
	}
	defer os.Remove(path)
	result, err := s.activeEngine().AddTorrentFileWithOptions(path, cfg, nil, AddOptions{})
	if err != nil {
		return nil, err.Error()
	}
	return result, ""
}

func gh0AddParsedRelease(ctx context.Context, s *AppState, release models.Release) (int, map[string]any) {
	source := strings.TrimSpace(release.Magnet)
	if isHTTPURL(source) {
		hash, errMessage := gh0DownloadAndAdd(ctx, s, source)
		if errMessage != "" {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": errMessage}
		}
		if hash == nil {
			return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
		}
		release.Magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s", *hash)
		score := latestConfig(s).ReleaseScore(&release)
		if err := s.db.RegisterTorrentScored(&release, score); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
		_ = s.db.SetTorrentReason(*hash, manualReason)
		return http.StatusAccepted, map[string]any{"ok": true, "title": release.Title}
	}

	score := latestConfig(s).ReleaseScore(&release)
	if err := s.db.RegisterTorrentScored(&release, score); err != nil {
		return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
	}
	if hash, ok := utils.MagnetHash(release.Magnet); ok {
		_ = s.db.SetTorrentReason(hash, manualReason)
	}
	title := release.Title
	engine := s.activeEngine()
	cfg := latestConfig(s)
	go func() {
		defer recoverGoroutine("g0 background")
		added, err := engine.Add(source, cfg)
		if err != nil {
			logging.Error(fmt.Sprintf("❌ Could not add «%s» to the downloads", title), "error", err)
		} else if added {
			logging.Info(fmt.Sprintf("📥 «%s» added to the downloads by hand", title))
		} else {
			logging.Info(fmt.Sprintf("«%s» is already among the downloads", title))
		}
	}()
	return http.StatusAccepted, map[string]any{"ok": true, "title": release.Title, "queued": true}
}

func addRelease(ctx context.Context, s *AppState, release models.Release) (int, map[string]any) {
	if !SetupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	isURL := isHTTPURL(release.Magnet)
	status, body := gh0AddParsedRelease(ctx, s, release)
	if isURL && status == http.StatusBadRequest {
		if found := resolveBySearch(ctx, s, release.Title); found != nil {
			retryStatus, retryBody := gh0AddParsedRelease(ctx, s, *found)
			if retryStatus != http.StatusBadRequest {
				return retryStatus, retryBody
			}
		}
	}
	return status, body
}

func gh0AddRawMagnet(ctx context.Context, s *AppState, source string) (int, map[string]any) {
	if !SetupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	trimmed := strings.TrimSpace(source)
	if isHTTPURL(trimmed) {
		hash, errMessage := gh0DownloadAndAdd(ctx, s, trimmed)
		if errMessage != "" {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": errMessage}
		}
		if hash == nil {
			return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
		}
		return http.StatusAccepted, map[string]any{"ok": true}
	}
	added, err := s.activeEngine().Add(trimmed, latestConfig(s))
	if err != nil {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
	}
	if !added {
		return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
	}
	return http.StatusAccepted, map[string]any{"ok": true}
}

func resolveBySearch(ctx context.Context, s *AppState, title string) *models.Release {
	query := gh0SearchQueryFromTitle(title)
	normalizedQuery := gh0NormalizeSearch(query)
	if normalizedQuery == "" {
		return nil
	}
	results := s.engine.SearchQueryManual(ctx, latestConfig(s), query)
	for i := range results {
		if sourceIsUsable(results[i].Magnet) && strings.HasPrefix(gh0NormalizeSearch(results[i].Title), normalizedQuery) {
			return &results[i]
		}
	}
	return nil
}

func gh0SearchQueryFromTitle(title string) string {
	cleaned := []rune{}
	beforeBracket := strings.SplitN(title, "[", 2)[0]
	for _, character := range beforeBracket {
		if (character >= '0' && character <= '9') || (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') {
			cleaned = append(cleaned, character)
		} else {
			cleaned = append(cleaned, ' ')
		}
	}
	words := []string{}
	for _, word := range strings.Fields(string(cleaned)) {
		lower := strings.ToLower(word)
		isYear := len(word) == 4
		if isYear {
			for _, character := range word {
				if character < '0' || character > '9' {
					isYear = false
					break
				}
			}
		}
		isMarker := strings.Contains(lower, "1080p") ||
			strings.Contains(lower, "720p") ||
			strings.Contains(lower, "2160p") ||
			strings.Contains(lower, "480p") ||
			isQualityMarker(lower)
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

func isQualityMarker(lower string) bool {
	switch lower {
	case "4k", "uhd", "bluray", "bdrip", "web", "webdl", "webrip", "hdtv", "dvdrip",
		"h264", "h265", "x264", "x265", "hevc", "avc", "aac", "ac3", "eac3", "ddp",
		"dts", "proper", "repack":
		return true
	}
	return false
}

func gh0NormalizeSearch(value string) string {
	beforeBracket := strings.ToLower(strings.SplitN(value, "[", 2)[0])
	cleaned := []rune{}
	for _, character := range beforeBracket {
		if (character >= '0' && character <= '9') || (character >= 'a' && character <= 'z') {
			cleaned = append(cleaned, character)
		} else {
			cleaned = append(cleaned, ' ')
		}
	}
	return strings.Join(strings.Fields(string(cleaned)), " ")
}

func BackfillMediaInfo(w http.ResponseWriter, r *http.Request, s *AppState) {
	limit := 100
	var input backfillMediaInfoInput
	if has, err := decodeOptional(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	} else if has && input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 5000 {
		limit = 5000
	}

	// The backfill runs ffprobe on many files and used to block the request.
	// It is now a background job: the request returns 202 immediately while the
	// detailed report stays available on /api/jobs/{id} (job result).
	job, created := s.jobs.Create("media-info-backfill", "media-info-backfill")
	if !created {
		jsonError(w, http.StatusConflict, "a MediaInfo backfill is already running")
		return
	}
	if err := s.jobs.Start(job.ID, func(ctx context.Context, jobID string) {
		report, err := runMediaInfoBackfill(ctx, s, limit, func(done, total int) {
			if total > 0 {
				s.jobs.SetProgress(jobID, float64(done)/float64(total), "")
			}
		})
		if err != nil {
			s.jobs.Fail(jobID, err)
			return
		}
		s.jobs.SetResult(jobID, report)
		logging.Info("media info backfill completed", "job_id", jobID)
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"job_id":  job.ID,
		"message": "Aggiornamento MediaInfo avviato",
	})
}

func logMediaInfoFailure(target MediaInfoBackfillTarget, reason string, items *[]any) {
	logging.Warn("🔬 MediaInfo backfill: file not analysed",
		"path", target.Path, "kind", target.Kind, "series", target.Series,
		"season", target.Season, "episode", target.Episode, "name", target.Name,
		"year", target.Year, "reason", reason)
	*items = append(*items, map[string]any{
		"path":    target.Path,
		"kind":    target.Kind,
		"series":  target.Series,
		"season":  target.Season,
		"episode": target.Episode,
		"name":    target.Name,
		"year":    target.Year,
		"reason":  reason,
	})
}

func RunHousekeepingNow(w http.ResponseWriter, r *http.Request, s *AppState) {
	report, ok := runHousekeeping(s)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "housekeeping failed")
		return
	}
	// The manual button applies the release-archive retention too, exactly like
	// the periodic worker: only catalogue rows, never files or downloaded
	// torrents.
	archiveRemoved, err := applyArchiveRetention(latestConfig(s), s.archive)
	if err != nil {
		logging.Warn("⚠️ The release archive cleanup failed", "error", err)
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "report": report, "archive_removed": archiveRemoved})
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

// protectedTorrentPaths is the set of files a live torrent is still using, so a
// duplicate cleanup never removes one of them.
func protectedTorrentPaths(torrents TorrentEngine) map[string]struct{} {
	protected := map[string]struct{}{}
	for _, torrent := range torrents.List() {
		files, ok, err := torrents.Files(torrent.Hash)
		if err != nil || !ok {
			continue
		}
		for _, file := range files {
			protected[filepath.Join(torrent.SavePath, file.Path)] = struct{}{}
		}
	}
	return protected
}

// CleanDuplicates scans the series archives for inferior duplicates: it lists
// them, or removes them when execute is set.
func CleanDuplicates(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input DuplicatesInput
	if err := decodeOptionalJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	execute := input.Execute
	cfg := latestConfig(s)
	protected := protectedTorrentPaths(s.activeEngine())
	candidates := []DuplicateCandidate{}
	removed := 0
	for index := range cfg.Series {
		series := &cfg.Series[index]
		resolved := cfg.ResolveArchivePath(series)
		if resolved == nil || strings.TrimSpace(*resolved) == "" {
			continue
		}
		directory := *resolved
		if execute {
			count, err := CleanupInferiorDuplicatesInDir(cfg, series.Name, directory, protected)
			if err != nil {
				logging.Warn("duplicate cleanup failed", "series", series.Name, "error", err)
			} else {
				removed += count
			}
		} else {
			found, err := FindSeriesDuplicatesInDir(cfg, series.Name, directory, protected)
			if err != nil {
				logging.Warn("duplicate scan failed", "series", series.Name, "error", err)
			} else {
				candidates = append(candidates, found...)
			}
		}
	}
	if execute {
		logging.Info("duplicate cleanup completed", "removed", removed)
		jsonResponse(w, map[string]any{"ok": true, "execute": true, "removed": removed})
		return
	}
	// Record what the scan found, with enough detail to locate each file, so the
	// log answers "what, where and why" without opening the UI again.
	for _, candidate := range candidates {
		logging.Warn("inferior duplicate found",
			"series", candidate.Series,
			"season", candidate.Season,
			"episode", candidate.Episode,
			"file", candidate.Path,
			"rank", candidate.ResolutionRank,
			"best", candidate.BestRank,
		)
	}
	logging.Info("duplicate scan completed", "found", len(candidates))
	jsonResponse(w, map[string]any{"ok": true, "execute": false, "count": len(candidates), "items": candidates})
}

// DbPrune prunes the series database (old cycles, old errors, seen entries) and
// reports what it removed.
func DbPrune(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input PruneInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	retain := int64(50)
	if input.RetainCycles != nil {
		retain = *input.RetainCycles
	}
	retain = clampInt64(retain, 1, 10000)
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
	if input.Preview {
		preview, err := s.db.PrunePreview(retain, errorAge, seenDays)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonStatus(w, http.StatusOK, map[string]any{
			"ok":           true,
			"preview":      true,
			"report":       preview,
			"seen_removed": preview.SeenToRemove,
		})
		return
	}
	if s.dryRun() {
		jsonError(w, http.StatusConflict, "dry-run does not modify the database")
		return
	}
	report, err := s.db.Cleanup(retain, errorAge)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	seenRemoved, err := s.db.PruneSeenOlderThan(seenDays)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "report": report, "seen_removed": seenRemoved})
}

// CleanTrash empties the trash folder, honouring the retention days unless force
// is set.
func CleanTrash(w http.ResponseWriter, r *http.Request, s *AppState) {
	// Use the live configuration, not the startup snapshot, so this endpoint
	// empties the same trash folder the UI lists and the single-entry delete
	// uses. The fallback also matches those handlers when trash_path is unset.
	cfg := latestConfig(s)
	if cfg.DryRun {
		jsonError(w, http.StatusConflict, "dry-run does not delete trash")
		return
	}
	trashPath := filepath.Join(cfg.DataDir, "trash")
	if cfg.TrashPath != nil {
		trashPath = *cfg.TrashPath
	}
	if !safeTrashRoot(cfg, trashPath) {
		jsonError(w, http.StatusConflict, "trash_path non valido: svuotamento rifiutato")
		return
	}
	var input CleanTrashInput
	if !DecodeOptionalJSON(w, r, &input) {
		return
	}
	retentionDays := int64(0)
	if !input.Force {
		if parsed, err := strconv.ParseInt(cfg.Settings[settingTrashRetentionDays], 10, 64); err == nil {
			retentionDays = parsed
		}
		if retentionDays < 0 {
			jsonError(w, http.StatusBadRequest, "trash_retention_days non può essere negativo")
			return
		}
	}
	files, byteCount, err := RemoveTrashContents(trashPath, retentionDays)
	if err != nil {
		logging.Warn("trash cleanup failed", "path", trashPath, "error", err.Error())
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if files > 0 {
		logging.Info(fmt.Sprintf("🗑️ Trash emptied — %d items removed · %s", files, logging.HumanBytes(byteCount)), "path", trashPath)
	} else {
		logging.Info("🗑️ Trash already empty", "path", trashPath)
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":             true,
		"files":          files,
		"bytes":          byteCount,
		"retention_days": retentionDays,
	})
}

// DbPruneByIds prunes the given seen rows, movies and series, by id.
func DbPruneByIds(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input PruneByIdsInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.dryRun() {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "dry-run does not modify the database"})
		return
	}
	movies, series, err := s.db.PruneSeenByIDs(input.MovieSeen, input.SeriesSeen)
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"movie_seen_removed":  movies,
		"series_seen_removed": series,
	})
}

// DeleteTrashEntries deletes the named entries of the trash, or all of them.
func DeleteTrashEntries(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input TrashDeleteInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	if cfg.DryRun {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "dry-run does not delete trash"})
		return
	}
	root := filepath.Join(cfg.DataDir, "trash")
	if cfg.TrashPath != nil {
		root = *cfg.TrashPath
	}
	if !safeTrashRoot(cfg, root) {
		jsonError(w, http.StatusConflict, "trash_path non valido: eliminazione rifiutata")
		return
	}
	var names []string
	if input.All {
		names = []string{}
		if entries, err := os.ReadDir(root); err == nil {
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
		}
	} else {
		names = input.Names
	}
	if len(names) == 0 {
		jsonError(w, http.StatusBadRequest, "nessun elemento da eliminare")
		return
	}
	removed := 0
	var byteCount uint64
	errors := []string{}
	removedNames := []string{}
	for _, name := range names {
		if strings.TrimSpace(name) == "" || !SingleComponent(name) || !filepath.IsLocal(name) {
			errors = append(errors, name)
			continue
		}
		target := filepath.Join(root, name)
		info, err := os.Lstat(target)
		if err != nil {
			errors = append(errors, name)
			continue
		}
		var size uint64
		if info.IsDir() {
			size = DirectorySize(target)
		} else {
			size = uint64(info.Size())
		}
		var outcome error
		if info.IsDir() {
			outcome = os.RemoveAll(target)
		} else {
			outcome = os.Remove(target)
		}
		if outcome != nil {
			errors = append(errors, name)
			continue
		}
		removed++
		byteCount += size
		removedNames = append(removedNames, name)
	}
	if removed > 0 {
		logging.Info(fmt.Sprintf("🗑️ Trash emptied — %d items removed · %s", removed, logging.HumanBytes(byteCount)), "path", root)
	}
	if len(errors) > 0 {
		logging.Warn("trash cleanup left some entries", "path", root, "failed", len(errors), "names", strings.Join(errors, ", "))
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":      true,
		"removed": removed,
		"bytes":   byteCount,
		"names":   removedNames,
		"errors":  errors,
	})
}

// SingleComponent reports whether `name` is exactly one path component.
func SingleComponent(name string) bool {
	if name == "" {
		return false
	}
	if strings.Contains(name, "/") {
		return false
	}
	return filepath.Clean(name) == name
}

// DirectorySize is the total size in bytes of a directory and everything in it.
func DirectorySize(path string) uint64 {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	var total uint64
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		info, err := os.Lstat(child)
		if err != nil {
			continue
		}
		if info.IsDir() {
			total += DirectorySize(child)
		} else {
			total += uint64(info.Size())
		}
	}
	return total
}

// RemoveTrashContents empties a trash folder, keeping the entries modified less
// than olderThanDays ago when the value is positive; it returns how many files
// it removed and their size.
func RemoveTrashContents(root string, olderThanDays int64) (int, uint64, error) {
	files := 0
	var byteCount uint64
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return 0, 0, nil
	}
	var cutoff time.Time
	if olderThanDays > 0 {
		cutoff = time.Now().Add(-time.Duration(olderThanDays) * 24 * time.Hour)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return files, byteCount, err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		linkInfo, err := os.Lstat(path)
		if err != nil {
			return files, byteCount, err
		}
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			// Remove the link itself, not its target, so a trash full of
			// symlinks can still be emptied.
			files++
			if err := os.Remove(path); err != nil {
				return files, byteCount, err
			}
			continue
		}
		if linkInfo.IsDir() {
			nestedFiles, nestedBytes, err := RemoveTrashContents(path, olderThanDays)
			files += nestedFiles
			byteCount += nestedBytes
			if err != nil {
				return files, byteCount, err
			}
			remaining, err := os.ReadDir(path)
			if err != nil {
				return files, byteCount, err
			}
			if len(remaining) == 0 {
				if err := os.Remove(path); err != nil {
					return files, byteCount, err
				}
			}
		} else {
			if olderThanDays > 0 {
				metadata, err := os.Stat(path)
				tooNew := err == nil && metadata.ModTime().After(cutoff)
				if tooNew {
					continue
				}
			}
			metadata, err := os.Stat(path)
			if err == nil {
				byteCount += uint64(metadata.Size())
			}
			files++
			if err := os.Remove(path); err != nil {
				return files, byteCount, err
			}
		}
	}
	return files, byteCount, nil
}

// gh4_addRelease queues a release for download, falling back to a search of its
// title when the URL cannot be fetched.
func gh4AddRelease(s *AppState, release models.Release) (int, any) {
	if !SetupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	isURL := isHTTPURL(release.Magnet)
	status, value := addParsedRelease(s, release)
	if isURL && status == http.StatusBadRequest {
		if found := gh4ResolveBySearch(s, release.Title); found != nil {
			retryStatus, retryValue := addParsedRelease(s, *found)
			if retryStatus != http.StatusBadRequest {
				return retryStatus, retryValue
			}
		}
	}
	return status, value
}

// gh4_addParsedRelease registers a release and queues it: a magnet goes to the
// engine at once, an HTTP URL is fetched first.
func addParsedRelease(s *AppState, release models.Release) (int, any) {
	source := strings.TrimSpace(release.Magnet)
	if isHTTPURL(source) {
		hash, err := downloadAndAdd(s, source, AddOptions{})
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
		_ = s.db.SetTorrentReason(*hash, manualReason)
		return http.StatusAccepted, map[string]any{"ok": true, "title": release.Title}
	}
	score := latestConfig(s).ReleaseScore(&release)
	if err := s.db.RegisterTorrentScored(&release, score); err != nil {
		return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
	}
	if hash, ok := utils.MagnetHash(release.Magnet); ok {
		_ = s.db.SetTorrentReason(hash, manualReason)
	}
	title := release.Title
	logHash, _ := utils.MagnetHash(release.Magnet)
	cfg := LatestConfig(s)
	go func() {
		defer recoverGoroutine("g4 background")
		added, err := s.activeEngine().Add(source, cfg)
		switch {
		case err != nil:
			logging.Error(fmt.Sprintf("❌ Could not add «%s» to the downloads", title), "hash", logHash, "error", err)
		case added:
			logging.Info(fmt.Sprintf("📥 «%s» added to the downloads by hand (score %v)", title, score), "hash", logHash)
		default:
			logging.Info(fmt.Sprintf("«%s» is already among the downloads", title), "hash", logHash)
		}
	}()
	return http.StatusAccepted, map[string]any{"ok": true, "title": title, "queued": true}
}

// gh4_downloadAndAdd adds a magnet link, or fetches and adds a .torrent file; it
// returns the info hash, and nil when the torrent was already there.
func downloadAndAdd(s *AppState, rawURL string, options AddOptions) (*string, error) {
	cfg := LatestConfig(s)
	rawURL = strings.TrimSpace(rawURL)
	addMagnet := func(m string) (*string, error) {
		hash, ok := utils.MagnetHash(m)
		if !ok {
			return nil, fmt.Errorf("invalid magnet link")
		}
		added, err := s.activeEngine().AddWithOptions(m, cfg, nil, options)
		if err != nil {
			return nil, err
		}
		if !added {
			return nil, nil
		}
		return &hash, nil
	}
	if strings.HasPrefix(rawURL, "magnet:?") {
		return addMagnet(rawURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	payload, magnet, err := s.engine.FetchTorrent(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if magnet != "" {
		return addMagnet(magnet)
	}
	path := filepath.Join(cfg.StateDir, fmt.Sprintf(".manual-%s.torrent", newUUID()))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return nil, err
	}
	defer os.Remove(path)
	return s.activeEngine().AddTorrentFileWithOptions(path, cfg, nil, options)
}

// gh4_resolveBySearch searches for a release whose title starts with the one
// given and whose source can be used.
func gh4ResolveBySearch(s *AppState, title string) *models.Release {
	query := searchQueryFromTitle(title)
	normalized := normalizeSearch(query)
	if normalized == "" {
		return nil
	}
	results := s.engine.SearchQueryManual(context.Background(), LatestConfig(s), query)
	for i := range results {
		release := &results[i]
		if sourceIsUsable(release.Magnet) && strings.HasPrefix(normalizeSearch(release.Title), normalized) {
			return release
		}
	}
	return nil
}

// gh4_searchQueryFromTitle turns a release title into a search query: it keeps
// the leading words and stops at the year or at the first quality marker.
func searchQueryFromTitle(title string) string {
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
		isYear := len(word) == 4 && allASCIIDigits(word)
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

// gh4_sourceIsUsable reports whether the source is an HTTP URL or a magnet link
// with a valid info hash.
func sourceIsUsable(source string) bool {
	source = strings.TrimSpace(source)
	if isHTTPURL(source) {
		return true
	}
	if !strings.HasPrefix(source, "magnet:") {
		return false
	}
	_, ok := utils.MagnetHash(source)
	return ok
}

// gh4_addRawMagnet queues a raw magnet or an HTTP URL, refusing a duplicate.
func addRawMagnet(s *AppState, source string) (int, any) {
	if !SetupComplete(s.cfg) {
		return http.StatusConflict, map[string]any{"ok": false, "error": "complete the initial setup first"}
	}
	source = strings.TrimSpace(source)
	if isHTTPURL(source) {
		hash, err := downloadAndAdd(s, source, AddOptions{})
		if err != nil {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
		}
		if hash == nil {
			return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
		}
		return http.StatusAccepted, map[string]any{"ok": true}
	}
	added, err := s.activeEngine().Add(source, LatestConfig(s))
	if err != nil {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}
	}
	if !added {
		return http.StatusConflict, map[string]any{"ok": false, "error": "torrent duplicate"}
	}
	return http.StatusAccepted, map[string]any{"ok": true}
}

// BatchArchiveDownload queues 1-100 archive results, each one by eD2k link or by
// torrent, and reports which ones were accepted.
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
		var status int
		var value any
		if strings.TrimSpace(item.Ed2kLink) != "" {
			// An eD2k result has no magnet: queue its ed2k:// link through the
			// same path as the search results, so aMule gets it. The release is
			// parsed like a torrent's, so the download keeps its series, season
			// and episode (the episode search sends only title, link and source).
			link := strings.TrimSpace(item.Ed2kLink)
			release := models.Release{Title: item.Title, Source: source, Ed2kLink: link}
			if parsed := ParseEd2kRelease(item.Title, link, source, 0, 0, time.Now().UTC()); parsed != nil {
				release = *parsed
			}
			status, value = AddRelease(s, release)
		} else {
			release := ParseRelease(item.Title, item.Magnet, source)
			if release == nil && IsTorrentURL(item.Magnet) {
				// An archive listing may hold only a `.torrent` link: keep it as
				// the source so the add path resolves the magnet and registers
				// the release against its film instead of adding it untracked.
				link := strings.TrimSpace(item.Magnet)
				release = ParseReleaseSource(item.Title, "", &link, source, time.Now().UTC())
			}
			if release == nil {
				status, value = addRawMagnet(s, item.Magnet)
			} else {
				status, value = gh4AddRelease(s, *release)
			}
		}
		if (status >= 200 && status < 300) || status == http.StatusAccepted {
			accepted++
		} else {
			// Carry the reason: «already present» and «invalid link» must not
			// read the same in the answer of an action.
			reason := ""
			if payload, ok := value.(map[string]any); ok {
				reason, _ = payload["error"].(string)
			}
			rejected = append(rejected, map[string]any{"title": item.Title, "status": status, "error": reason})
		}
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "accepted": accepted, "rejected": rejected})
}

// DbPruneKeyword removes the archived releases matching the given keywords; with
// preview it only counts and lists them.
func DbPruneKeyword(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input KeywordPruneInput
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
	if s.dryRun() {
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

// DbPrunePreview reports what DbPrune would remove without modifying the
// database.
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

// RestoreSourceInput carries the execute flag of RestoreSource: when it is false
// the renames are only listed.
type RestoreSourceInput struct {
	Execute bool `json:"execute"`
}

func directorySize(path string) uint64 {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	var total uint64
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		if directoryExists(child) {
			total += directorySize(child)
			continue
		}
		// A non-directory counts with the size lstat reports for its directory
		// entry, so a symlink does not pull in the size of its target.
		if info, err := entry.Info(); err == nil {
			total += uint64(info.Size())
		}
	}
	return total
}

// DeleteArchiveEntry removes the archive entries given by id, or the one matching
// the magnet.
func DeleteArchiveEntry(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ArchiveDeleteInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.dryRun() {
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

// RestoreSource puts the source token back into the names of the archived
// episodes: it lists the renames, or applies them when execute is set.
func RestoreSource(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input RestoreSourceInput
	if err := gh6DecodeOptionalJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	execute := input.Execute
	if execute && s.dryRun() {
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
			// rename(2) replaces an existing file without a word: a library
			// file already holding the restored name must not be overwritten.
			if _, err := os.Lstat(target); err == nil {
				errors++
				logging.Warn(fmt.Sprintf("Did not restore the source in the name of %s: %s already exists", archivePath, target))
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

// TrashEntries lists the trash folder with the size of every entry, largest
// first.
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
			isDir := directoryExists(child)
			var size uint64
			if isDir {
				size = directorySize(child)
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
	retentionDays := parseSettingInt(cfg, "trash_retention_days")
	jsonResponse(w, map[string]any{
		"ok":             true,
		"path":           path,
		"exists":         directoryExists(path),
		"count":          len(items),
		"total_bytes":    totalBytes,
		"retention_days": retentionDays,
		"items":          items,
	})
}

// ArchiveEntries handles GET /api/archive.
func ArchiveEntries(w http.ResponseWriter, r *http.Request, s *AppState) {
	term := queryParam(r, "q")
	if term == "" {
		term = queryParam(r, "query")
	}
	page := queryIntVal(r, "page", 1)
	if page < 1 {
		page = 1
	}
	limit := queryIntVal(r, "limit", 100)
	if limit < 1 {
		limit = 1
	} else if limit > 1000 {
		limit = 1000
	}
	if s.archive == nil {
		jsonError(w, http.StatusInternalServerError, "archivio non disponibile")
		return
	}
	webParam := queryParam(r, "web")
	webSearch := webParam == "1" || strings.EqualFold(webParam, "true")
	if webSearch && strings.TrimSpace(term) != "" && page == 1 && s.engine != nil {
		cfg := latestConfig(s)
		if cfg != nil {
			searchCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			results := s.engine.SearchQueryManualAll(searchCtx, cfg, strings.TrimSpace(term))
			cancel()
			if len(results) > 0 {
				if err := s.archive.SaveBatch(results, cfg); err != nil {
					logging.Warn("failed to save web search results to archive", "query", term, "error", err)
				}
				if s.db != nil {
					if err := s.db.RecordSeenBatch(results, cfg); err != nil {
						logging.Warn("archive web search seen recording failed", "error", err)
					}
				}
				logging.Info(fmt.Sprintf("🔎 The web search for «%s» found %s", term, countLabel(len(results), "result", "results")))
			}
		}
	}
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

// archiveFileTitle is the episode title a scanned file carries: its name
// without the extension, or the bare name when nothing is left.
func archiveFileTitle(name string) string {
	title := strings.TrimSuffix(name, filepath.Ext(name))
	if strings.TrimSpace(title) == "" {
		return name
	}
	return title
}

// archiveFileSize is the size of a scanned file, or 0 when it cannot be read.
func archiveFileSize(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}
