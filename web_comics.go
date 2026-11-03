package gextto

// web_comics.go holds the HTTP handlers of comics: the monitored titles, the
// manual downloads (HTTP, mega, torrent and magnet), the weekly links, the
// search and the download history.

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/utils"
)

var (
	weeklyDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func anyPathStartsWith(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if pathStartsWith(path, prefix) {
			return true
		}
	}
	return false
}

func ComicDownloadHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.dryRun() {
		jsonError(w, http.StatusConflict, "download manuali disabilitati in dry-run")
		return
	}
	var input ComicDownloadInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Url) == "" || strings.TrimSpace(input.Title) == "" {
		jsonError(w, http.StatusBadRequest, "url e titolo sono obbligatori")
		return
	}
	cfg := latestConfig(s)
	monitoredPaths := []string{}
	if monitored, err := s.comics.ListMonitored(false); err == nil {
		for _, comic := range monitored {
			if strings.TrimSpace(comic.SavePath) != "" {
				monitoredPaths = append(monitoredPaths, comic.SavePath)
			}
		}
	}
	comicDir, hasComicDir := ComicDownloadDir(cfg)
	target := cfg.DownloadDir
	if strings.TrimSpace(input.SavePath) == "" {
		if hasComicDir {
			target = comicDir
		}
	} else {
		target = strings.TrimSpace(input.SavePath)
	}
	allowed := pathStartsWith(target, cfg.DataDir) ||
		pathStartsWith(target, cfg.DownloadDir) ||
		(hasComicDir && pathStartsWith(target, comicDir)) ||
		anyPathStartsWith(target, monitoredPaths)
	if !allowed {
		jsonError(w, http.StatusForbidden, "percorso comics non configurato")
		return
	}
	_ = os.MkdirAll(target, 0o755)

	client := NewGetComicsClient()
	method := strings.ToLower(input.Method)
	var result any
	var runErr error
	switch method {
	case "download_now", "direct", "http":
		historyURL := strings.TrimSpace(input.PostUrl)
		if historyURL == "" {
			historyURL = strings.TrimSpace(input.Url)
		}
		id, err := client.StartDirectDownloadWithCompletion(strings.TrimSpace(input.Url), target, strings.TrimSpace(input.Title), func(path string) {
			if s.comics == nil || historyURL == "" {
				return
			}
			if _, historyErr := s.comics.AddHistory(0, historyURL, strings.TrimSpace(input.Title), "", ""); historyErr != nil {
				logging.Warn(fmt.Sprintf("Could not record the manual download of «%s» in the comics history", input.Title), "error", historyErr)
			}
		})
		if err != nil {
			runErr = err
		} else {
			result = map[string]any{"id": id, "method": "http", "status": "downloading"}
		}
	case "mega":
		executable := os.Getenv("GEXTTO_MEGADL")
		if executable == "" {
			executable = "megadl"
		}
		resolved, err := client.ResolveMega(strings.TrimSpace(input.Url))
		if err != nil {
			runErr = err
		} else if path, err := DownloadMega(executable, resolved, target); err != nil {
			runErr = err
		} else {
			tagDownloadedComic(path, strings.TrimSpace(input.Title))
			result = map[string]any{"path": path, "method": "mega"}
		}
	case "torrent", "torrents":
		path, err := client.DownloadTorrent(strings.TrimSpace(input.Url), target)
		if err != nil {
			runErr = err
			break
		}
		hash, err := s.activeEngine().AddTorrentFile(path, target)
		if err != nil {
			runErr = err
			break
		}
		if hash == nil {
			runErr = fmt.Errorf("torrent not added")
			break
		}
		_ = os.Remove(path)
		_ = s.db.SetTorrentTag(*hash, "Comic")
		if strings.TrimSpace(input.PostUrl) != "" {
			_ = s.comics.AddTorrent(*hash, strings.TrimSpace(input.PostUrl), strings.TrimSpace(input.Title), target)
		}
		result = map[string]any{"hash": *hash, "method": "torrent"}
	case "magnet", "magnets":
		ok, err := s.activeEngine().AddWithPath(input.Url, cfg, &target)
		if err != nil {
			runErr = err
			break
		}
		if !ok {
			runErr = fmt.Errorf("magnet duplicato")
			break
		}
		if hash, ok := utils.MagnetHash(input.Url); ok {
			_ = s.db.SetTorrentTag(hash, "Comic")
			_ = s.comics.AddTorrent(hash, strings.TrimSpace(input.PostUrl), strings.TrimSpace(input.Title), target)
		}
		result = map[string]any{"method": "magnet"}
	default:
		runErr = fmt.Errorf("unsupported comics method")
	}
	if runErr != nil {
		jsonError(w, http.StatusBadGateway, runErr.Error())
		return
	}
	if method != "download_now" && method != "direct" && method != "http" && s.comics != nil {
		historyURL := strings.TrimSpace(input.PostUrl)
		if historyURL == "" {
			historyURL = strings.TrimSpace(input.Url)
		}
		if historyURL != "" {
			if _, err := s.comics.AddHistory(0, historyURL, strings.TrimSpace(input.Title), "", ""); err != nil {
				logging.Warn(fmt.Sprintf("Could not record the manual download of «%s» in the comics history", input.Title), "error", err)
			}
		}
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true, "result": result})
}

func ComicWeeklyLinks(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ComicWeeklyInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !weeklyDatePattern.MatchString(strings.TrimSpace(input.Date)) {
		jsonError(w, http.StatusBadRequest, "data weekly non valida")
		return
	}
	url, links, err := NewGetComicsClient().WeeklyLinks(strings.TrimSpace(input.Date))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "post_url": url, "links": links, "date": input.Date})
}

type gh1ComicWeeklySettings struct {
	Enabled      *bool   `json:"enabled"`
	FromDate     *string `json:"from_date"`
	HistoryLimit *int64  `json:"history_limit"`
}

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

func ComicDownloadPause(w http.ResponseWriter, r *http.Request, s *AppState) {
	id := pathParam(r, "id")
	if PauseHTTPDownload(id) {
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	jsonError(w, http.StatusConflict, "download non in scarico")
}

func ComicWeeklySettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input gh1ComicWeeklySettings
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.FromDate != nil {
		fromDate := strings.TrimSpace(*input.FromDate)
		if fromDate != "" && !validDate(fromDate) {
			jsonError(w, http.StatusBadRequest, "data weekly non valida: usare YYYY-MM-DD")
			return
		}
		if err := s.comics.SetSetting("weekly_from_date", fromDate); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if input.Enabled != nil {
		enabled := "no"
		if *input.Enabled {
			enabled = "yes"
		}
		if err := s.comics.SetSetting("weekly_enabled", enabled); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if input.HistoryLimit != nil {
		limit := clampInt64(*input.HistoryLimit, 1, 500)
		if err := s.comics.SetSetting("comics_history_limit", strconv.FormatInt(limit, 10)); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.comics.PruneHistory(limit); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	enabledValue, _ := s.comics.Setting("weekly_enabled", "no")
	fromDate, _ := s.comics.Setting("weekly_from_date", "")
	historyLimit := s.comics.HistoryLimit()
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":               true,
		"weekly_enabled":   settingTruthy(enabledValue),
		"weekly_from_date": fromDate,
		"history_limit":    historyLimit,
	})
}

// validDate reports whether value is a strict YYYY-MM-DD date.
func validDate(value string) bool {
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

// ComicDownloadRemove cancels a manual comics download, deleting its files
// when the caller asks for it.
func ComicDownloadRemove(w http.ResponseWriter, r *http.Request, s *AppState) {
	id := pathParam(r, "id")
	var input RemoveCompletedInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if CancelHTTPDownload(id, input.DeleteFiles) {
		jsonResponse(w, map[string]any{"ok": true})
		return
	}
	jsonError(w, http.StatusNotFound, "download non trovato")
}

func Comics(w http.ResponseWriter, r *http.Request, s *AppState) {
	items, err := s.comics.ListMonitored(false)
	if err != nil {
		items = []ComicMonitored{}
	}
	jsonResponse(w, items)
}

func ComicDownloadResume(w http.ResponseWriter, r *http.Request, s *AppState) {
	id := pathParam(r, "id")
	if ResumeHTTPDownload(id) {
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "download non in pausa"})
}

func ComicsHistory(w http.ResponseWriter, r *http.Request, s *AppState) {
	value, err := s.comics.History(s.comics.HistoryLimit())
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "items": value})
}

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

func ComicsWeekly(w http.ResponseWriter, r *http.Request, s *AppState) {
	enabledRaw, _ := s.comics.Setting("weekly_enabled", "no")
	enabled := settingTruthy(enabledRaw)
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

func ComicDownloads(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, HTTPDownloads())
}

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

// ComicCycle handles POST /api/comics/cycle.
func ComicCycle(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := NewGetComicsClient()
	defaultRoot := cfg.DownloadDir
	if s.cycle_lock != nil {
		s.cycle_lock.Lock()
		defer s.cycle_lock.Unlock()
	}
	// Bound to the daemon lifetime, not to the HTTP request: closing the page
	// must not abort a comics cycle halfway.
	ctx, cancel := s.BackgroundContext()
	defer cancel()
	count, err := RunComicsCycle(ctx, s.comics, client, s.notifier, defaultRoot, s.activeEngine(), s.db, cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "downloaded": count, "dry_run": cfg.DryRun})
}

// ComicLinksHandler handles POST /api/comics/links.
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

// ClearComicsHistory handles POST /api/comics/history/clear.
func ClearComicsHistory(w http.ResponseWriter, r *http.Request, s *AppState) {
	deleted, err := s.comics.ClearHistory()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "deleted": deleted})
}

// ClearComicsWeeklyHistory handles POST /api/comics/weekly/history/clear.
func ClearComicsWeeklyHistory(w http.ResponseWriter, r *http.Request, s *AppState) {
	deleted, err := s.comics.ClearWeeklyHistory()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "deleted": deleted})
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
