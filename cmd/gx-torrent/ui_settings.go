package main

// ui_settings.go is the standalone settings page: folders, network, protocols,
// queue/cache, bandwidth and scheduler, language and notifications.
//
// In standalone settings.json is authoritative (read at startup; see optStr in
// main.go). Folders, network and protocol changes apply from the next start;
// the queue limits live in the queue config (state.json) and apply at once,
// like the bandwidth and the scheduler.

import (
	"encoding/json"
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

	store := d.opts.Settings

	// Queue cache and limits (queue config, applied at once). A value equal to
	// the current one is left out of the patch.
	cfg, _, _ := d.config()
	patch := map[string]json.RawMessage{}
	putInt := func(key string, value int64) {
		raw, _ := json.Marshal(value)
		patch[key] = raw
	}
	if v, ok := parseFormKib(r.FormValue("speed-download")); ok && v != cfg.SpeedLimitDownload {
		putInt("speed_limit_download", v)
	}
	if v, ok := parseFormKib(r.FormValue("speed-upload")); ok && v != cfg.SpeedLimitUpload {
		putInt("speed_limit_upload", v)
	}
	if v := formInt(r, "active-downloads", int64(cfg.ActiveDownloads)); v > 0 && int(v) != cfg.ActiveDownloads {
		putInt("active_downloads", v)
	}
	if v := formInt(r, "active-seeds", int64(cfg.ActiveSeeds)); v >= 0 && int(v) != cfg.ActiveSeeds {
		putInt("active_seeds", v)
	}
	if v := formInt(r, "active-limit", int64(cfg.ActiveLimit)); v > 0 && int(v) != cfg.ActiveLimit {
		putInt("active_limit", v)
	}
	if v := formInt(r, "cache-mb", cfg.CacheMB); v != cfg.CacheMB {
		putInt("cache_mb", v)
	}
	if v := formBool(r, "preallocate"); v != cfg.Preallocate {
		raw, _ := json.Marshal(v)
		patch["preallocate"] = raw
	}
	if v := formBool(r, "sequential"); v != cfg.Sequential {
		raw, _ := json.Marshal(v)
		patch["sequential"] = raw
	}
	if len(patch) > 0 {
		if _, err := d.setConfig(patch); err != nil {
			d.renderSettings(w, r, "The queue settings could not be applied: "+err.Error(), true)
			return
		}
		// Re-read the config: the upload limit below may have just changed.
		cfg, _, _ = d.config()
	}

	// Scheduler window (settings.json).
	enabled := formBool(r, "schedule-enabled")
	start := strings.TrimSpace(r.FormValue("schedule-start"))
	end := strings.TrimSpace(r.FormValue("schedule-end"))
	scheduleDL, dlSet := parseFormKib(r.FormValue("schedule-download"))
	scheduleUL, ulSet := parseFormKib(r.FormValue("schedule-upload"))
	if !dlSet {
		scheduleDL = int64FromSetting(store.Get(scheduleDLKey, "0"))
	}
	if !ulSet {
		scheduleUL = int64FromSetting(store.Get(scheduleULKey, "0"))
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
	store.Set(scheduleEnabledKey, strconv.FormatBool(enabled))
	store.Set(scheduleStartKey, start)
	store.Set(scheduleEndKey, end)
	store.Set(scheduleDLKey, strconv.FormatInt(scheduleDL, 10))
	store.Set(scheduleULKey, strconv.FormatInt(scheduleUL, 10))

	// Latency auto-speed (settings.json, applied at once). It needs a positive
	// upload ceiling (global or scheduled) to have something to lower.
	autoEnabled := formBool(r, "autospeed-enabled")
	autoTarget := strings.TrimSpace(r.FormValue("autospeed-target"))
	autoMin := formInt(r, "autospeed-min", autoSpeedDefaultMinKiB)
	if autoMin < 1 {
		autoMin = autoSpeedDefaultMinKiB
	}
	if autoEnabled && cfg.SpeedLimitUpload <= 0 && scheduleUL <= 0 {
		d.renderSettings(w, r, "Latency auto-speed needs a positive upload limit (global or scheduled).", true)
		return
	}
	store.Set(autospeedEnabledKey, strconv.FormatBool(autoEnabled))
	store.Set(autospeedTargetKey, autoTarget)
	store.Set(autospeedMinKey, strconv.FormatInt(autoMin, 10))

	// Folders, network, protocols, language and notifications (settings.json,
	// read at the next start).
	for _, key := range []string{
		"download-dir", "temp-dir", "listen", "peer-ports", "listen-interface",
		"outgoing-interface", "proxy", "token", "ipfilter", "dht-bootstrap",
		"notify-url", "lang",
	} {
		store.Set(key, strings.TrimSpace(r.FormValue(key)))
	}
	encryption := int(formInt(r, "encryption", 1))
	if encryption < 0 || encryption > 2 {
		encryption = 1
	}
	store.Set("encryption", strconv.Itoa(encryption))
	store.Set("ipfilter-trackers", strconv.FormatBool(formBool(r, "ipfilter-trackers")))
	if hours := formInt(r, ipFilterRefreshKey, ipFilterDefaultRefreshHours); hours >= 0 && hours <= 24*30 {
		store.Set(ipFilterRefreshKey, strconv.FormatInt(hours, 10))
	}
	store.Set("dht-read-only", strconv.FormatBool(formBool(r, "dht-read-only")))
	store.Set("web-sources-allow-private", strconv.FormatBool(formBool(r, "web-sources-allow-private")))
	for _, toggle := range []string{"dht", "pex", "utp", "holepunch", "lsd", "upnp", "natpmp"} {
		store.Set("no-"+toggle, strconv.FormatBool(!formBool(r, toggle)))
	}
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

// encryptionOptions renders the encryption select, marking the current value.
func encryptionOptions(selected string) string {
	selected = strings.TrimSpace(selected)
	var b strings.Builder
	for _, option := range []struct{ value, label string }{
		{"0", "Disabled (0)"},
		{"1", "Enabled (1)"},
		{"2", "Forced (2)"},
	} {
		mark := ""
		if option.value == selected {
			mark = " selected"
		}
		b.WriteString(`<option value="` + option.value + `"` + mark + `>` + html.EscapeString(option.label) + `</option>`)
	}
	return b.String()
}

// languageOptions renders the UI language select with the endonyms.
func languageOptions(selected string) string {
	selected = strings.ToLower(strings.TrimSpace(selected))
	names := []struct{ code, name string }{
		{"en", "English"}, {"it", "Italiano"}, {"de", "Deutsch"},
		{"fr", "Français"}, {"es", "Español"}, {"pl", "Polski"},
	}
	var b strings.Builder
	for _, item := range names {
		mark := ""
		if item.code == selected || (selected == "" && item.code == "en") {
			mark = " selected"
		}
		b.WriteString(`<option value="` + item.code + `"` + mark + `>` + item.name + `</option>`)
	}
	return b.String()
}

// renderSettings writes the settings page, localized like the daemon page.
func (d *Daemon) renderSettings(w http.ResponseWriter, r *http.Request, errKey string, isError bool) {
	cfg, _, _ := d.config()
	store := d.opts.Settings
	page := assets.SettingsPage()
	repl := func(placeholder, value string) {
		page = strings.Replace(page, placeholder, value, 1)
	}
	checkedIf := func(cond bool) string {
		if cond {
			return " checked"
		}
		return ""
	}
	boolSetting := func(key string) bool {
		return strings.EqualFold(strings.TrimSpace(store.Get(key, "")), "true")
	}
	// A protocol is enabled unless its "no-<x>" flag is set.
	featureOn := func(key string) bool { return !boolSetting("no-" + key) }
	get := func(key string) string { return html.EscapeString(store.Get(key, "")) }

	repl("{{SPEED_DL}}", strconv.FormatInt(cfg.SpeedLimitDownload, 10))
	repl("{{SPEED_UL}}", strconv.FormatInt(cfg.SpeedLimitUpload, 10))
	repl("{{SCHED_START}}", html.EscapeString(store.Get(scheduleStartKey, "")))
	repl("{{SCHED_END}}", html.EscapeString(store.Get(scheduleEndKey, "")))
	repl("{{SCHED_DL}}", strconv.FormatInt(int64FromSetting(store.Get(scheduleDLKey, "0")), 10))
	repl("{{SCHED_UL}}", strconv.FormatInt(int64FromSetting(store.Get(scheduleULKey, "0")), 10))
	repl("{{SCHED_ENABLED}}", checkedIf(boolSetting(scheduleEnabledKey)))
	repl("{{AUTOSPEED_ENABLED}}", checkedIf(boolSetting(autospeedEnabledKey)))
	repl("{{AUTOSPEED_TARGET}}", get(autospeedTargetKey))
	autoSpeedMin := int64FromSetting(store.Get(autospeedMinKey, strconv.Itoa(autoSpeedDefaultMinKiB)))
	if autoSpeedMin < 1 {
		autoSpeedMin = autoSpeedDefaultMinKiB
	}
	repl("{{AUTOSPEED_MIN}}", strconv.FormatInt(autoSpeedMin, 10))
	repl("{{AUTOSPEED_STATUS}}", html.EscapeString(d.autoSpeedStatus(time.Now())))

	repl("{{DOWNLOAD_DIR}}", get("download-dir"))
	repl("{{TEMP_DIR}}", get("temp-dir"))
	repl("{{LISTEN}}", get("listen"))
	repl("{{PEER_PORTS}}", get("peer-ports"))
	repl("{{LISTEN_IFACE}}", get("listen-interface"))
	repl("{{OUT_IFACE}}", get("outgoing-interface"))
	repl("{{PROXY}}", get("proxy"))
	repl("{{TOKEN}}", get("token"))
	repl("{{IPFILTER}}", get("ipfilter"))
	repl("{{IPFILTER_REFRESH}}", strconv.Itoa(int(d.ipFilterRefreshEvery()/time.Hour)))
	repl("{{IPFILTER_STATUS}}", d.ipFilterStatusHTML())
	repl("{{OPT_STATUS}}", d.optimizationStatusHTML())
	repl("{{IPFILTER_TRACKERS}}", checkedIf(!strings.EqualFold(strings.TrimSpace(store.Get("ipfilter-trackers", "true")), "false")))
	repl("{{ENC_OPTIONS}}", encryptionOptions(store.Get("encryption", "1")))
	repl("{{DHT_BOOTSTRAP}}", get("dht-bootstrap"))
	repl("{{DHT_ON}}", checkedIf(featureOn("dht")))
	repl("{{PEX_ON}}", checkedIf(featureOn("pex")))
	repl("{{UTP_ON}}", checkedIf(featureOn("utp")))
	repl("{{HOLEPUNCH_ON}}", checkedIf(featureOn("holepunch")))
	repl("{{LSD_ON}}", checkedIf(featureOn("lsd")))
	repl("{{UPNP_ON}}", checkedIf(featureOn("upnp")))
	repl("{{NATPMP_ON}}", checkedIf(featureOn("natpmp")))
	repl("{{DHT_READONLY}}", checkedIf(boolSetting("dht-read-only")))
	repl("{{WEB_PRIVATE}}", checkedIf(boolSetting("web-sources-allow-private")))

	repl("{{ACTIVE_DL}}", strconv.Itoa(cfg.ActiveDownloads))
	repl("{{ACTIVE_SEEDS}}", strconv.Itoa(cfg.ActiveSeeds))
	repl("{{ACTIVE_LIMIT}}", strconv.Itoa(cfg.ActiveLimit))
	cacheMB := cfg.CacheMB
	if cacheMB < 0 {
		cacheMB = 0
	}
	repl("{{CACHE_MB}}", strconv.FormatInt(cacheMB, 10))
	repl("{{PREALLOCATE}}", checkedIf(cfg.Preallocate))
	repl("{{SEQUENTIAL}}", checkedIf(cfg.Sequential))

	repl("{{LANG_OPTIONS}}", languageOptions(store.Get("lang", "")))
	repl("{{NOTIFY_URL}}", get("notify-url"))

	errorHTML := ""
	if errKey != "" {
		class := "muted"
		if isError {
			class = "err"
		}
		errorHTML = `<p class="` + class + `">` + html.EscapeString(errKey) + `</p>`
	}
	repl("<!--ERROR-->", errorHTML)

	lang := d.uiLang(r)
	if lang != "" && lang != "en" {
		page = strings.Replace(page, `lang="en"`, `lang="`+lang+`"`, 1)
	}
	page = uiTranslateHTML(page, uiDictionary(lang))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}
