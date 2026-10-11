package main

// ui_settings.go is the standalone settings page: the global bandwidth limits
// and the bandwidth scheduler (an alternate daily window, like qBittorrent).
// The limits live in the queue config (state.json), the scheduler window in
// settings.json; both are applied to the running session at once.

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/webui/assets"
)

// uiSettingsPath is the standalone settings page.
const uiSettingsPath = "/ui/settings"

// handleUISettings renders the settings page (GET) and saves it (POST).
func (d *Daemon) handleUISettings(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		msg := strings.TrimSpace(r.URL.Query().Get("msg"))
		d.renderSettings(w, r, msg, r.URL.Query().Get("ok") == "0")
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()

	// Global limits (state.json): a blank field keeps the current value.
	cfg, _, _ := d.config()
	download, downloadSet := parseFormKib(r.FormValue("speed-download"))
	upload, uploadSet := parseFormKib(r.FormValue("speed-upload"))
	if !downloadSet {
		download = cfg.SpeedLimitDownload
	}
	if !uploadSet {
		upload = cfg.SpeedLimitUpload
	}
	patch := map[string]json.RawMessage{}
	if download != cfg.SpeedLimitDownload {
		raw, _ := json.Marshal(download)
		patch["speed_limit_download"] = raw
	}
	if upload != cfg.SpeedLimitUpload {
		raw, _ := json.Marshal(upload)
		patch["speed_limit_upload"] = raw
	}
	if len(patch) > 0 {
		if _, err := d.setConfig(patch); err != nil {
			d.renderSettings(w, r, "The bandwidth limits could not be applied: "+err.Error(), true)
			return
		}
	}

	// Scheduler window (settings.json).
	enabled := formBool(r, "schedule-enabled")
	start := strings.TrimSpace(r.FormValue("schedule-start"))
	end := strings.TrimSpace(r.FormValue("schedule-end"))
	scheduleDL, dlSet := parseFormKib(r.FormValue("schedule-download"))
	scheduleUL, ulSet := parseFormKib(r.FormValue("schedule-upload"))
	if !dlSet {
		scheduleDL = int64FromSetting(d.opts.Settings.Get(scheduleDLKey, "0"))
	}
	if !ulSet {
		scheduleUL = int64FromSetting(d.opts.Settings.Get(scheduleULKey, "0"))
	}
	if enabled {
		startMinute, okStart := parseClock(start)
		endMinute, okEnd := parseClock(end)
		if !okStart || !okEnd {
			d.renderSettings(w, r, "The scheduler window must be two times in HH:MM.", true)
			return
		}
		if startMinute == endMinute {
			d.renderSettings(w, r, "The scheduler start and end must differ.", true)
			return
		}
	}
	store := d.opts.Settings
	store.Set(scheduleEnabledKey, fmt.Sprintf("%t", enabled))
	store.Set(scheduleStartKey, start)
	store.Set(scheduleEndKey, end)
	store.Set(scheduleDLKey, strconv.FormatInt(scheduleDL, 10))
	store.Set(scheduleULKey, strconv.FormatInt(scheduleUL, 10))
	if err := store.Save(); err != nil {
		d.renderSettings(w, r, "Cannot save the settings: "+err.Error(), true)
		return
	}
	d.applyScheduleNow()
	http.Redirect(w, r, uiSettingsPath+"?msg="+url.QueryEscape("Settings saved")+"&ok=1", http.StatusSeeOther)
}

// parseFormKib reads a non-negative KiB/s value; ok is false when the field is
// blank or invalid (the caller then keeps the current value).
func parseFormKib(raw string) (value int64, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

// applyScheduleNow pushes the effective limits (scheduled or global) onto the
// running session, so a change on the settings page takes effect at once.
func (d *Daemon) applyScheduleNow() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.applyEffectiveSpeedLimitsLocked(time.Now())
}

// renderSettings writes the settings page, localized like the daemon page.
func (d *Daemon) renderSettings(w http.ResponseWriter, r *http.Request, errKey string, isError bool) {
	cfg, _, _ := d.config()
	page := assets.SettingsPage()
	page = strings.Replace(page, "{{SPEED_DL}}", strconv.FormatInt(cfg.SpeedLimitDownload, 10), 1)
	page = strings.Replace(page, "{{SPEED_UL}}", strconv.FormatInt(cfg.SpeedLimitUpload, 10), 1)
	page = strings.Replace(page, "{{SCHED_START}}", html.EscapeString(d.opts.Settings.Get(scheduleStartKey, "")), 1)
	page = strings.Replace(page, "{{SCHED_END}}", html.EscapeString(d.opts.Settings.Get(scheduleEndKey, "")), 1)
	page = strings.Replace(page, "{{SCHED_DL}}", strconv.FormatInt(int64FromSetting(d.opts.Settings.Get(scheduleDLKey, "0")), 10), 1)
	page = strings.Replace(page, "{{SCHED_UL}}", strconv.FormatInt(int64FromSetting(d.opts.Settings.Get(scheduleULKey, "0")), 10), 1)
	checked := ""
	if strings.EqualFold(strings.TrimSpace(d.opts.Settings.Get(scheduleEnabledKey, "false")), "true") {
		checked = " checked"
	}
	page = strings.Replace(page, "{{SCHED_ENABLED}}", checked, 1)

	errorHTML := ""
	if errKey != "" {
		class := "muted"
		if isError {
			class = "err"
		}
		errorHTML = `<p class="` + class + `">` + html.EscapeString(errKey) + `</p>`
	}
	page = strings.Replace(page, "<!--ERROR-->", errorHTML, 1)

	lang := d.uiLang(r)
	if lang != "" && lang != "en" {
		page = strings.Replace(page, `lang="en"`, `lang="`+lang+`"`, 1)
	}
	page = uiTranslateHTML(page, uiDictionary(lang))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}
