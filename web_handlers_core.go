package gextto

// Core web handlers: the entry page, magnet landing page, static assets, i18n,
// health/status/config views, log streaming and the shared middlewares. This is
// the Go implementation of the matching handlers .

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/messages"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Middlewares
// ---------------------------------------------------------------------------

// ApiAuth enforces the optional API token, mirroring `api_auth`. Public
// endpoints and non-API paths are passed through untouched.
func ApiAuth(s *AppState, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") ||
			path == "/api/auth" || path == "/api/health" || path == "/api/status" {
			next.ServeHTTP(w, r)
			return
		}
		// Read the token from the live configuration, not the startup
		// snapshot: setting `api_token` at runtime must take effect (both
		// enabling and rotating) without restarting the daemon.
		token := LatestConfig(s).APIToken
		if token == nil || *token == "" {
			next.ServeHTTP(w, r)
			return
		}
		supplied := r.Header.Get("x-gextto-token")
		if supplied == "" {
			if authorization := r.Header.Get("authorization"); strings.HasPrefix(authorization, "Bearer ") {
				supplied = strings.TrimPrefix(authorization, "Bearer ")
			}
		}
		// Constant-time comparison: a plain `==` short-circuits on the first
		// differing byte and leaks the shared prefix to a timing observer.
		if subtle.ConstantTimeCompare([]byte(supplied), []byte(*token)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		jsonError(w, http.StatusUnauthorized, "API token required")
	})
}

// noCacheWriter injects the Cache-Control header before the first write so it
// is present even when the wrapped handler writes the header itself ( can
// mutate the response after the fact, Go cannot).
type noCacheWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *noCacheWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.ResponseWriter.Header().Set("Cache-Control", "no-cache")
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *noCacheWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *noCacheWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *noCacheWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// UiNoCache disables browser caching for the UI shell (`/`) and its assets
// (`/pkg/*`), mirroring `ui_no_cache`.
func UiNoCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" && !strings.HasPrefix(path, "/pkg/") {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&noCacheWriter{ResponseWriter: w}, r)
	})
}

// ---------------------------------------------------------------------------
// Static pages
// ---------------------------------------------------------------------------

// Index serves the single-page UI shell. The compiled bundle is embedded in the
// binary and served from `/pkg`; a disk directory still overrides it.
func Index(w http.ResponseWriter, r *http.Request, s *AppState) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if uiBundleAvailable() {
		_, _ = io.WriteString(w, indexBundleHTML)
		return
	}
	_, _ = io.WriteString(w, indexMissingHTML)
}

const indexMissingHTML = `<!doctype html>
<html lang="it">
<head><meta charset="utf-8"><title>Gextto</title></head>
<body style="font-family:system-ui;background:#0b1120;color:#e6edf7;display:grid;place-items:center;height:100vh;margin:0">
<main style="max-width:520px;padding:24px;text-align:center">
<h1>Gextto</h1>
<p>La web interface non è inclusa in questo binario.</p>
<p>Compila con <code>make build</code> oppure imposta <code>GEXTTO_UI_DIR</code>.</p>
</main></body></html>`

const indexBundleHTML = `<!doctype html>
<html lang="it" data-theme="dark">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
  <meta name="color-scheme" content="dark light">
  <title>Gextto</title>
  <link rel="icon" href="/favicon.ico">
  <link rel="stylesheet" href="/pkg/ui.css">
  <style>
    html,body{margin:0;height:100%;background:#0b1120;color:#e6edf7;font-family:Inter,system-ui,sans-serif}
    #gextto-boot{display:grid;place-items:center;height:100vh;gap:14px;text-align:center}
    #gextto-boot .logo{width:64px;height:64px;border-radius:16px;background:linear-gradient(135deg,#3b82f6,#22d3ee);display:grid;place-items:center;font-weight:800;font-size:30px;color:#04121f}
    #gextto-boot .spinner{width:26px;height:26px;border:3px solid #1f2f47;border-top-color:#3b82f6;border-radius:50%;animation:gspin 1s linear infinite}
    @keyframes gspin{to{transform:rotate(360deg)}}
    #gextto-boot.hidden{display:none}
  </style>
</head>
<body>
  <div id="gextto-boot">
    <div class="logo">g</div>
    <div class="spinner"></div>
    <div>Avvio di Gextto…</div>
  </div>
  <script type="module">
    import init, { hydrate } from "/pkg/ui.js";
    init().then(() => {
      const boot = document.getElementById("gextto-boot");
      if (boot) boot.classList.add("hidden");
      hydrate();
    }).catch((error) => {
      document.body.innerHTML = '<pre style="padding:24px;color:#ff9fb0">Errore di avvio UI: ' + error + '</pre>';
    });
  </script>
</body>
</html>`

// MagnetHandler is the landing page for `magnet:` links.
func MagnetHandler(w http.ResponseWriter, r *http.Request, s *AppState) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, magnetPage)
}

const magnetPage = `<!doctype html>
<html lang="it">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
  <title>Gextto — aggiungi magnet</title>
  <style>
    body { font-family: Inter, system-ui, sans-serif; background: #0a0f1a; color: #e9f0fb; display: grid; place-items: center; min-height: 100vh; margin: 0; }
    main { max-width: 560px; padding: 28px; text-align: center; }
    h1 { font-size: 20px; margin: 0 0 8px; }
    p { color: #9aaccb; }
    a { color: #6cb8ff; }
    .ok { color: #7ce7bd; }
    .err { color: #ff9fb0; word-break: break-word; }
  </style>
</head>
<body>
  <main>
    <h1>Gextto</h1>
    <p id="status">Invio del magnet alla sessione…</p>
    <p><a href="/">Torna a Gextto</a></p>
  </main>
  <script>
    const magnet = new URLSearchParams(location.search).get("url") || decodeURIComponent(location.hash.slice(1));
    const status = document.getElementById("status");
    if (!magnet) {
      status.textContent = "Nessun magnet ricevuto.";
      status.className = "err";
    } else {
      fetch("/api/send-magnet", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ magnet }),
      })
        .then(async (response) => {
          const payload = await response.json().catch(() => ({}));
          if (!response.ok) throw payload.error || ("HTTP " + response.status);
          status.textContent = "Magnet aggiunto alla sessione.";
          status.className = "ok";
        })
        .catch((error) => {
          status.textContent = "Errore: " + error;
          status.className = "err";
        });
    }
  </script>
</body>
</html>`

// WasmAlias serves the WASM binary under its stable alias.
func WasmAlias(w http.ResponseWriter, r *http.Request, s *AppState) {
	data, ok := uiAsset("pkg/ui.wasm")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/wasm")
	_, _ = w.Write(data)
}

// Favicon serves the site favicon.
func Favicon(w http.ResponseWriter, r *http.Request, s *AppState) {
	data, ok := uiAsset("favicon.ico")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------------------
// Auth / i18n
// ---------------------------------------------------------------------------

// AuthStatus reports whether an API token is configured.
func AuthStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	token := LatestConfig(s).APIToken
	jsonResponse(w, map[string]any{"required": token != nil && *token != ""})
}

// I18nList returns the translations for a language.
func I18nList(w http.ResponseWriter, r *http.Request, s *AppState) {
	lang := queryParam(r, "lang")
	if lang == "" {
		if active, err := s.i18n.Language(); err == nil {
			lang = active
		} else {
			lang = "it"
		}
	}
	items, err := s.i18n.List(lang)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"lang": lang, "items": items})
}

// I18nSet stores one translation.
func I18nSet(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input I18nInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Lang) > 16 || strings.TrimSpace(input.Key) == "" || len(input.Key) > 256 || len(input.Value) > 4096 {
		jsonError(w, http.StatusBadRequest, "invalid translation")
		return
	}
	if err := s.i18n.Set(input.Lang, input.Key, input.Value); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// I18nLanguage switches the active UI language.
func I18nLanguage(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input LanguageInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Lang != "it" && input.Lang != "en" {
		jsonError(w, http.StatusBadRequest, "language must be it or en")
		return
	}
	if err := s.i18n.SetLanguage(input.Lang); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Keep backend notifications/log summaries in the same language.
	messages.SetLanguage(input.Lang)
	jsonResponse(w, map[string]any{"ok": true, "lang": input.Lang, "restart_required": true})
}

// I18nLanguages lists the supported languages.
func I18nLanguages(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, map[string]any{
		"ok": true,
		"items": []map[string]any{
			{"lang": "it", "name": "Italiano"},
			{"lang": "en", "name": "English"},
		},
	})
}

// I18nActive returns the active UI language.
func I18nActive(w http.ResponseWriter, r *http.Request, s *AppState) {
	lang, err := s.i18n.Language()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "lang": lang})
}

// I18nExport serialises a language to YAML.
func I18nExport(w http.ResponseWriter, r *http.Request, s *AppState) {
	lang := pathParam(r, "lang")
	items, err := s.i18n.List(lang)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	values := map[string]string{}
	for _, item := range items {
		values[item.Key] = item.Value
	}
	payload, err := yaml.Marshal(values)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

// I18nDeleteLang removes every translation of a language.
func I18nDeleteLang(w http.ResponseWriter, r *http.Request, s *AppState) {
	lang := pathParam(r, "lang")
	if strings.TrimSpace(lang) == "" || len(lang) > 32 {
		jsonError(w, http.StatusBadRequest, "invalid language code")
		return
	}
	removed, err := s.i18n.DeleteLang(lang)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "lang": lang, "removed": removed})
}

// I18nImport merges a YAML translation file into a language.
func I18nImport(w http.ResponseWriter, r *http.Request, s *AppState) {
	lang := pathParam(r, "lang")
	var input I18nYamlInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Yaml) > 2_000_000 {
		jsonError(w, http.StatusRequestEntityTooLarge, "translation file too large")
		return
	}
	values := map[string]string{}
	if err := yaml.Unmarshal([]byte(input.Yaml), &values); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.i18n.SetBulk(lang, values); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "imported": len(values)})
}

// ---------------------------------------------------------------------------
// Health / status
// ---------------------------------------------------------------------------

// healthCheckLimiter serialises the blocking health checks (NFS stats, trash
// walks) so the poll cannot spawn an unbounded number of probing goroutines.
var healthCheckLimiter = make(chan struct{}, 1)

// HealthApi returns the system health report, cached for 8 seconds.
func HealthApi(w http.ResponseWriter, r *http.Request, s *AppState) {
	if cached, ok := cacheGet("health", 8*time.Second); ok {
		jsonResponse(w, cached)
		return
	}
	dataDir := s.cfg.DataDir
	trashPath := filepath.Join(dataDir, "trash")
	if s.cfg.TrashPath != nil {
		trashPath = *s.cfg.TrashPath
	}
	archiveRoot := ""
	if s.cfg.ArchiveRoot != nil {
		archiveRoot = *s.cfg.ArchiveRoot
	}
	ramdiskPath := ""
	if value, ok := s.cfg.Settings["libtorrent_ramdisk_dir"]; ok && value != "" {
		ramdiskPath = value
	}
	healthCheckLimiter <- struct{}{}
	report := CheckWithPaths(&HealthPaths{
		DataDir:      dataDir,
		TrashPath:    trashPath,
		DownloadPath: s.cfg.LibtorrentDir,
		ArchiveRoot:  archiveRoot,
		RamdiskPath:  ramdiskPath,
	})
	<-healthCheckLimiter
	raw, err := json.Marshal(report)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	value := map[string]any{}
	if err := json.Unmarshal(raw, &value); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cachePut("health", value)
	jsonResponse(w, value)
}

// ProcessMetricsApi returns the lightweight process-only metrics.
func ProcessMetricsApi(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, ProcessMetrics())
}

// SetupStatus reports the first-run setup state.
func SetupStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	marker := SetupComplete(s.cfg)
	completed := marker
	autoCompleted := false
	if !marker {
		hasSeries := false
		if value, err := s.db.HasData(); err == nil {
			hasSeries = value
		}
		hasArchive := false
		if value, err := s.archive.Count(); err == nil {
			hasArchive = value > 0
		}
		if hasSeries || hasArchive {
			if err := CompleteSetup(s.cfg); err == nil {
				completed = true
				autoCompleted = true
			}
		}
	}
	jsonResponse(w, map[string]any{
		"completed":      completed,
		"import_source":  s.cfg.ImportSourceDir,
		"auto_completed": autoCompleted,
	})
}

// Status returns the daemon status summary.
func Status(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := LatestConfig(s)
	var seenMovies, seenSeries int64
	if movies, series, err := s.db.SeenCounts(); err == nil {
		seenMovies, seenSeries = movies, series
	}
	jsonResponse(w, map[string]any{
		"name":            "gextto",
		"version":         "1.0." + constants.Build,
		"active":          cfg.Active,
		"dry_run":         cfg.DryRun,
		"setup_completed": SetupComplete(cfg),
		"last_cycle":      s.last_cycle.Snapshot(),
		"torrent_stats":   s.torrents.Stats(),
		"seen": map[string]any{
			"movies": seenMovies,
			"series": seenSeries,
			"groups": seenMovies + seenSeries,
		},
	})
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

// rustLines splits text like the `str::lines()`: trailing newline does not
// create an empty final line, and a trailing `\r` is stripped.
func rustLines(text string) []string {
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for index, line := range lines {
		lines[index] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// coreTailLines reads the last `limit` lines of a file without loading it all:
// only a trailing window proportional to the requested line count is read.
func coreTailLines(path string, limit int) []string {
	empty := []string{}
	const maxWindow int64 = 2 * 1024 * 1024
	file, err := os.Open(path)
	if err != nil {
		return empty
	}
	defer file.Close()
	size := int64(0)
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	window := int64(limit)*512 + 4096
	if window > maxWindow {
		window = maxWindow
	}
	start := size - window
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return empty
	}
	data, err := io.ReadAll(file)
	if err != nil || !utf8.Valid(data) {
		return empty
	}
	lines := rustLines(string(data))
	if start > 0 && len(lines) > 0 {
		// The first line of the window may be truncated mid-way.
		lines = lines[1:]
	}
	skip := len(lines) - limit
	if skip < 0 {
		skip = 0
	}
	return lines[skip:]
}

// Logs returns the tail of the daemon log.
func Logs(w http.ResponseWriter, r *http.Request, s *AppState) {
	limit := int(queryInt(r, "limit", 200))
	if limit < 1 {
		limit = 1
	}
	if limit > 2000 {
		limit = 2000
	}
	current := filepath.Join(s.cfg.DataDir, "gextto.log")
	lines := coreTailLines(current, limit)
	if len(lines) < limit {
		// Include the most recent rotated backup to fill the requested window.
		backup := filepath.Join(s.cfg.DataDir, "gextto.log.1")
		previous := coreTailLines(backup, limit-len(lines))
		lines = append(previous, lines...)
	}
	start := len(lines) - limit
	if start < 0 {
		start = 0
	}
	jsonResponse(w, map[string]any{"items": lines[start:]})
}

func readFileFrom(path string, offset int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("invalid UTF-8 in log file")
	}
	return string(data), nil
}

func writeSSEEvent(w io.Writer, id string, event string, data string) {
	var builder strings.Builder
	if event != "" {
		builder.WriteString("event: ")
		builder.WriteString(event)
		builder.WriteByte('\n')
	}
	if id != "" {
		builder.WriteString("id: ")
		builder.WriteString(id)
		builder.WriteByte('\n')
	}
	builder.WriteString("data: ")
	builder.WriteString(data)
	builder.WriteByte('\n')
	builder.WriteByte('\n')
	_, _ = io.WriteString(w, builder.String())
}

func sseFlush(w http.ResponseWriter) {
	_ = http.NewResponseController(w).Flush()
}

// LogsStream streams the log tail over SSE and then follows the file, reading
// only the bytes appended since the previous tick.
func LogsStream(w http.ResponseWriter, r *http.Request, s *AppState) {
	path := filepath.Join(s.cfg.DataDir, "gextto.log")
	limit := int(queryInt(r, "limit", 200))
	if limit < 1 {
		limit = 1
	}
	if limit > 5000 {
		limit = 5000
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	snapshot := coreTailLines(path, limit)
	offset := int64(0)
	if info, err := os.Stat(path); err == nil {
		offset = info.Size()
	}
	snapshotJSON, _ := json.Marshal(map[string]any{"snapshot": snapshot})
	writeSSEEvent(w, strconv.FormatInt(offset, 10), "", string(snapshotJSON))
	sseFlush(w)

	lastActivity := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.Size() < offset {
			// Rotation or truncation: restart from the beginning of the new file.
			offset = 0
		}
		if info.Size() == offset {
			if time.Since(lastActivity) >= 15*time.Second {
				_, _ = io.WriteString(w, ":\n\n")
				sseFlush(w)
				lastActivity = time.Now()
			}
			continue
		}
		chunk, err := readFileFrom(path, offset)
		if err != nil {
			continue
		}
		// Emit only complete lines: the last line may still be mid-write.
		complete := ""
		if index := strings.LastIndex(chunk, "\n"); index >= 0 {
			complete = chunk[:index+1]
		}
		for _, line := range rustLines(complete) {
			data, _ := json.Marshal(map[string]any{"line": line})
			writeSSEEvent(w, strconv.FormatInt(offset, 10), "", string(data))
		}
		if complete == "" {
			continue
		}
		offset += int64(len(complete))
		sseFlush(w)
		lastActivity = time.Now()
	}
}

// NotificationsStream pushes torrent lifecycle events over SSE as they appear.
func NotificationsStream(w http.ResponseWriter, r *http.Request, s *AppState) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	cursor := len(s.torrent_events.Snapshot())
	lastActivity := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
		items := s.torrent_events.Snapshot()
		// The buffer drops its oldest entries once it exceeds its cap.
		if len(items) < cursor {
			cursor = 0
		}
		if cursor > len(items) {
			cursor = len(items)
		}
		batch := items[cursor:]
		cursor = len(items)
		for _, item := range batch {
			data, _ := json.Marshal(item)
			writeSSEEvent(w, "", "torrent", string(data))
		}
		if len(batch) > 0 {
			sseFlush(w)
			lastActivity = time.Now()
		} else if time.Since(lastActivity) >= 15*time.Second {
			_, _ = io.WriteString(w, ":\n\n")
			sseFlush(w)
			lastActivity = time.Now()
		}
	}
}

// ---------------------------------------------------------------------------
// Config view
// ---------------------------------------------------------------------------

func settingsOr(cfg *Config, key string, fallback string) string {
	if value, ok := cfg.Settings[key]; ok {
		return value
	}
	return fallback
}

func settingsBool(cfg *Config, key string, fallback bool) bool {
	if value, ok := cfg.Settings[key]; ok {
		return value == "yes" || value == "true" || value == "1"
	}
	return fallback
}

func settingsOrNil(cfg *Config, key string) *string {
	if value, ok := cfg.Settings[key]; ok {
		return &value
	}
	return nil
}

func settingsNonEmpty(cfg *Config, key string) bool {
	value, ok := cfg.Settings[key]
	return ok && strings.TrimSpace(value) != ""
}

func settingsParseInt(cfg *Config, key string, fallback int64) int64 {
	if value, ok := cfg.Settings[key]; ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func settingsParseUint(cfg *Config, key string, fallback int) int {
	if value, ok := cfg.Settings[key]; ok {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			return parsed
		}
	}
	return fallback
}

// coreStallGiveupMinutes implements `configured_stall_giveup_minutes`.
func coreStallGiveupMinutes(cfg *Config) float64 {
	if value, ok := cfg.Settings["libtorrent_stall_giveup_min"]; ok {
		if minutes, err := strconv.ParseFloat(value, 64); err == nil {
			return minutes
		}
	}
	if value, ok := cfg.Settings["libtorrent_stall_timeout_min"]; ok {
		if minutes, err := strconv.ParseFloat(value, 64); err == nil {
			if minutes > 0 {
				if minutes < 10080 {
					minutes = 10080
				}
				return minutes
			}
			return 0
		}
	}
	return 20160
}

// ConfigView returns the configuration snapshot shown by the settings UI.
func ConfigView(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := LatestConfig(s)

	indexers := make([]map[string]any, 0, len(cfg.Indexers))
	configuredKeys := 0
	for _, indexer := range cfg.Indexers {
		indexers = append(indexers, map[string]any{
			"name":    indexer.Name,
			"url":     indexer.URL,
			"api_key": indexer.APIKey,
			"enabled": indexer.Enabled,
		})
		if strings.TrimSpace(indexer.APIKey) != "" {
			configuredKeys++
		}
	}

	scoreSettings := map[string]string{}
	for key, value := range cfg.Settings {
		if strings.HasPrefix(key, "score_") {
			scoreSettings[key] = value
		}
	}

	tvdbKey := cfg.TvdbAPIKey()
	tvdbAPIKey := ""
	if tvdbKey != nil {
		tvdbAPIKey = *tvdbKey
	}
	tmdbAPIKey := ""
	if cfg.TmdbAPIKey != nil {
		tmdbAPIKey = *cfg.TmdbAPIKey
	}

	libtorrent := map[string]any{
		"enabled":                           cfg.LibtorrentEnabled,
		"version":                           LibtorrentVersion(),
		"active_downloads":                  cfg.Libtorrent.ActiveDownloads,
		"active_seeds":                      cfg.Libtorrent.ActiveSeeds,
		"active_limit":                      cfg.Libtorrent.ActiveLimit,
		"seed_ratio":                        cfg.Libtorrent.SeedRatio,
		"seed_time_minutes":                 cfg.Libtorrent.SeedTimeMinutes,
		"seed_time_days":                    cfg.Libtorrent.SeedTimeDays,
		"download_limit_kib":                cfg.Libtorrent.DownloadLimitKib,
		"upload_limit_kib":                  cfg.Libtorrent.UploadLimitKib,
		"stop_at_ratio":                     cfg.Libtorrent.StopAtRatio,
		"dynamic_queue":                     cfg.Libtorrent.DynamicQueue,
		"dynamic_queue_min":                 cfg.Libtorrent.DynamicQueueMin,
		"dynamic_queue_max":                 cfg.Libtorrent.DynamicQueueMax,
		"dont_count_slow_torrents":          cfg.Libtorrent.DontCountSlowTorrents,
		"stall_after_min":                   settingsOr(cfg, "libtorrent_stall_after_min", "60"),
		"stall_retry_min":                   settingsOr(cfg, "libtorrent_stall_retry_min", "60"),
		"stall_giveup_min":                  strconv.FormatFloat(coreStallGiveupMinutes(cfg), 'f', -1, 64),
		"auto_remove_completed":             cfg.Libtorrent.AutoRemoveCompleted,
		"dht":                               cfg.Libtorrent.Dht,
		"pex":                               cfg.Libtorrent.Pex,
		"lsd":                               cfg.Libtorrent.Lsd,
		"upnp":                              cfg.Libtorrent.Upnp,
		"natpmp":                            cfg.Libtorrent.Natpmp,
		"connections_limit":                 cfg.Libtorrent.ConnectionsLimit,
		"upload_slots_limit":                cfg.Libtorrent.UploadSlotsLimit,
		"half_open_limit":                   cfg.Libtorrent.HalfOpenLimit,
		"alert_queue_size":                  cfg.Libtorrent.AlertQueueSize,
		"max_connections_per_torrent":       cfg.Libtorrent.MaxConnectionsPerTorrent,
		"max_uploads_per_torrent":           cfg.Libtorrent.MaxUploadsPerTorrent,
		"aio_threads":                       cfg.Libtorrent.AioThreads,
		"cache_size":                        cfg.Libtorrent.CacheSize,
		"cache_expiry":                      cfg.Libtorrent.CacheExpiry,
		"announce_interval":                 cfg.Libtorrent.AnnounceInterval,
		"torrent_connect_boost":             cfg.Libtorrent.TorrentConnectBoost,
		"utp":                               cfg.Libtorrent.Utp,
		"prefer_rc4":                        cfg.Libtorrent.PreferRc4,
		"announce_to_all_trackers":          cfg.Libtorrent.AnnounceToAllTrackers,
		"announce_to_all_tiers":             cfg.Libtorrent.AnnounceToAllTiers,
		"allow_multiple_connections_per_ip": cfg.Libtorrent.AllowMultipleConnectionsPerIp,
		"apply_ip_filter":                   cfg.Libtorrent.ApplyIpFilter,
		"encryption":                        cfg.Libtorrent.Encryption,
		"proxy_type":                        cfg.Libtorrent.ProxyType,
		"proxy_host":                        cfg.Libtorrent.ProxyHost,
		"proxy_port":                        cfg.Libtorrent.ProxyPort,
		"ip_filter_path":                    cfg.Libtorrent.IpFilterPath,
		"listen_interfaces":                 cfg.Libtorrent.ListenInterfaces,
		"outgoing_interface":                cfg.Libtorrent.OutgoingInterface,
		"dht_bootstrap_nodes":               cfg.Libtorrent.DhtBootstrapNodes,
		"port_min":                          cfg.Libtorrent.PortMin,
		"port_max":                          cfg.Libtorrent.PortMax,
		"ramdisk_enabled":                   cfg.RamdiskEnabled(),
		"ramdisk_threshold_gb":              settingsOr(cfg, "libtorrent_ramdisk_threshold_gb", "3.5"),
		"ramdisk_margin_gb":                 settingsOr(cfg, "libtorrent_ramdisk_margin_gb", "0.5"),
		"ramdisk_min_free_bytes":            settingsOr(cfg, "libtorrent_ramdisk_min_free_bytes", ""),
		"auto_optimize":                     settingsOr(cfg, "libtorrent_auto_optimize", "false"),
	}

	paths := map[string]any{
		"data_dir":               cfg.DataDir,
		"libtorrent_dir":         cfg.LibtorrentDir,
		"libtorrent_temp_dir":    cfg.LibtorrentTempDir,
		"libtorrent_ramdisk_dir": settingsOrNil(cfg, "libtorrent_ramdisk_dir"),
		"state_dir":              cfg.StateDir,
		"archive_root":           cfg.ArchiveRoot,
		"trash_path":             cfg.TrashPath,
	}

	jsonResponse(w, map[string]any{
		"active":                               cfg.Active,
		"dry_run":                              cfg.DryRun,
		"refresh_secs":                         cfg.RefreshSecs,
		"series_count":                         len(cfg.Series),
		"movie_count":                          len(cfg.Movies),
		"feed_urls":                            cfg.FeedURLs,
		"indexers":                             indexers,
		"indexer_api_keys_configured":          configuredKeys,
		"flaresolverr_url":                     cfg.FlaresolverrURL,
		"websearch_engines":                    cfg.WebsearchEngines,
		"blacklist":                            cfg.Blacklist,
		"content_filters":                      cfg.ContentFilters,
		"source_filters":                       cfg.SourceFilters,
		"max_release_age_days":                 cfg.MaxReleaseAgeDays,
		"gap_fill_max_per_series":              settingsParseUint(cfg, "gap_fill_max_per_series", 0),
		"gap_fill_max_per_cycle":               settingsParseUint(cfg, "gap_fill_max_per_cycle", 30),
		"gap_filling":                          settingsBool(cfg, "gap_filling", true),
		"gap_deep_interval_hours":              settingsParseInt(cfg, "gap_deep_interval_hours", 6),
		"gap_deep_max_per_cycle":               settingsParseUint(cfg, "gap_deep_max_per_cycle", 5),
		"libtorrent_sched_enabled":             settingsOr(cfg, "libtorrent_sched_enabled", "false"),
		"libtorrent_sequential":                settingsOr(cfg, "libtorrent_sequential", "false"),
		"libtorrent_extra_settings":            settingsOr(cfg, "libtorrent_extra_settings", ""),
		"libtorrent_sched_start":               settingsOr(cfg, "libtorrent_sched_start", "23:00"),
		"libtorrent_sched_end":                 settingsOr(cfg, "libtorrent_sched_end", "08:00"),
		"libtorrent_sched_days":                settingsOr(cfg, "libtorrent_sched_days", ""),
		"libtorrent_sched_dl_limit":            settingsOr(cfg, "libtorrent_sched_dl_limit", "0"),
		"libtorrent_sched_ul_limit":            settingsOr(cfg, "libtorrent_sched_ul_limit", "0"),
		"libtorrent_temp_dl_limit":             settingsOr(cfg, "libtorrent_temp_dl_limit", "0"),
		"libtorrent_temp_ul_limit":             settingsOr(cfg, "libtorrent_temp_ul_limit", "0"),
		"libtorrent_temp_limit_enabled":        settingsOr(cfg, "libtorrent_temp_limit_enabled", "0"),
		"libtorrent_temp_limit_until":          settingsOr(cfg, "libtorrent_temp_limit_until", "0"),
		"score_settings":                       scoreSettings,
		"flaresolverr_configured":              cfg.FlaresolverrURL != nil,
		"tmdb_configured":                      cfg.TmdbAPIKey != nil,
		"jellyfin_configured":                  settingsNonEmpty(cfg, "jellyfin_url") && settingsNonEmpty(cfg, "jellyfin_api_key"),
		"jellyfin_url":                         settingsOr(cfg, "jellyfin_url", ""),
		"plex_configured":                      settingsNonEmpty(cfg, "plex_url") && settingsNonEmpty(cfg, "plex_token"),
		"plex_url":                             settingsOr(cfg, "plex_url", ""),
		"tmdb_api_key":                         tmdbAPIKey,
		"tmdb_language":                        cfg.TmdbLanguage(),
		"default_language":                     cfg.DefaultLanguage(),
		"tvdb_api_key":                         tvdbAPIKey,
		"tvdb_configured":                      tvdbKey != nil,
		"tvdb_language":                        cfg.TvdbLanguage(),
		"trakt_configured":                     settingsOr(cfg, "trakt_client_id", "") != "",
		"trakt_authenticated":                  settingsOr(cfg, "trakt_access_token", "") != "",
		"simkl_configured":                     settingsOr(cfg, "simkl_client_id", "") != "",
		"simkl_authenticated":                  settingsOr(cfg, "simkl_access_token", "") != "",
		"backup_send_telegram":                 settingsBool(cfg, "backup_send_telegram", false),
		"notify_telegram":                      cfg.NotifyTelegram,
		"notify_email":                         cfg.NotifyEmail,
		"telegram_configured":                  cfg.TelegramBotToken != nil && cfg.TelegramChatID != nil,
		"telegram_chat_id":                     cfg.TelegramChatID,
		"email_smtp":                           cfg.EmailSMTP,
		"email_from":                           cfg.EmailFrom,
		"email_to":                             cfg.EmailTo,
		"email_password_configured":            cfg.EmailPassword != nil,
		"webhook_configured":                   cfg.NotifyWebhookURL != nil,
		"notify_webhook_url":                   cfg.NotifyWebhookURL,
		"backup_retention":                     settingsOr(cfg, "backup_retention", "5"),
		"backup_cloud_dir":                     settingsOr(cfg, "backup_cloud_dir", ""),
		"min_free_space_gb":                    settingsOr(cfg, "min_free_space_gb", "0"),
		"trash_retention_days":                 settingsOr(cfg, "trash_retention_days", "0"),
		"archive_retention_days":               settingsOr(cfg, "archive_retention_days", "0"),
		"archive_cleanup_enabled":              settingsOr(cfg, "archive_cleanup_enabled", "false"),
		"archive_max_age_days":                 settingsOr(cfg, "archive_max_age_days", "0"),
		"archive_keep_min":                     settingsOr(cfg, "archive_keep_min", "0"),
		"stop_on_old_page_threshold":           settingsOr(cfg, "stop_on_old_page_threshold", "3"),
		"debug_enabled":                        settingsOr(cfg, "debug_enabled", "false"),
		"move_episodes":                        settingsOr(cfg, "move_episodes", "false"),
		"rename_verify_interval":               settingsOr(cfg, "rename_verify_interval", "6"),
		"cleanup_min_score_diff":               cfg.CleanupMinScoreDiff,
		"upgrade_min_score_diff":               cfg.UpgradeMinScoreDiff,
		"delay_torrent_minutes":                settingsOr(cfg, "delay_torrent_minutes", "0"),
		"delay_movies_minutes":                 settingsOr(cfg, "delay_movies_minutes", "0"),
		"delay_bypass_score":                   settingsOr(cfg, "delay_bypass_score", "0"),
		"housekeeping_enabled":                 settingsOr(cfg, "housekeeping_enabled", "true"),
		"housekeeping_interval_hours":          settingsOr(cfg, "housekeeping_interval_hours", "24"),
		"housekeeping_retain_cycles":           settingsOr(cfg, "housekeeping_retain_cycles", "200"),
		"housekeeping_error_age_days":          settingsOr(cfg, "housekeeping_error_age_days", "7"),
		"housekeeping_seen_days":               settingsOr(cfg, "housekeeping_seen_days", "30"),
		"housekeeping_gap_log_days":            settingsOr(cfg, "housekeeping_gap_log_days", "30"),
		"housekeeping_upgrade_backup_days":     settingsOr(cfg, "housekeeping_upgrade_backup_days", "30"),
		"housekeeping_history_days":            settingsOr(cfg, "housekeeping_history_days", "0"),
		"media_info_backfill_enabled":          settingsOr(cfg, "media_info_backfill_enabled", "true"),
		"media_info_backfill_interval_minutes": settingsOr(cfg, "media_info_backfill_interval_minutes", "60"),
		"media_info_backfill_batch":            settingsOr(cfg, "media_info_backfill_batch", "10"),
		"feed_max_pages":                       strconv.Itoa(cfg.FeedMaxPages()),
		"cleanup_upgrades":                     cfg.CleanupUpgrades,
		"cleanup_action":                       cfg.CleanupAction,
		"auto_remove_completed":                cfg.Libtorrent.AutoRemoveCompleted,
		"rename_format":                        cfg.RenameFormat,
		"rename_template":                      cfg.RenameTemplate,
		"rename_episodes":                      cfg.RenameEpisodes,
		"libtorrent":                           libtorrent,
		"paths":                                paths,
	})
}

// SaveConfigRoot accepts a full library configuration and delegates to the
// library saver, mirroring `save_config_root`.
func SaveConfigRoot(w http.ResponseWriter, r *http.Request, s *AppState) {
	SaveLibraryHandler(w, r, s)
}

// TagDirRules returns the configured tag directory rules.
func TagDirRules(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := LatestConfig(s)
	rules := []TagDirRule{}
	if value, ok := cfg.Settings["tag_dir_rules"]; ok {
		parsed := []TagDirRule{}
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			rules = parsed
		}
	}
	jsonResponse(w, map[string]any{"ok": true, "items": rules})
}

// SaveTagDirRules replaces the tag directory rules.
func SaveTagDirRules(w http.ResponseWriter, r *http.Request, s *AppState) {
	rules := []TagDirRule{}
	if err := decodeJSON(r, &rules); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(rules) > 100 {
		jsonError(w, http.StatusBadRequest, "invalid tag directory rules")
		return
	}
	for _, rule := range rules {
		if strings.TrimSpace(rule.Tag) == "" ||
			strings.TrimSpace(rule.FinalDir) == "" ||
			len(rule.Tag) > 128 ||
			len(rule.FinalDir) > 4096 ||
			len(rule.TempDir) > 4096 {
			jsonError(w, http.StatusBadRequest, "invalid tag directory rules")
			return
		}
	}
	payload, err := json.Marshal(rules)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := saveConfigSetting(s.cfg.DataDir, "tag_dir_rules", string(payload)); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "items": rules})
}
