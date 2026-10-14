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
	"fmt"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/bwschedule"
	"github.com/buzzqw/gextto/internal/notify"
	"github.com/buzzqw/gextto/internal/settings"
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
	if v := formInt(r, "min-free-mb", cfg.MinFree()>>20); v >= 0 && v != cfg.MinFree()>>20 {
		putInt("min_free_mb", v)
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
	days := parseScheduleDayForm(r.Form["schedule-day"])
	// The day group carries a marker so a form posted without it (an older
	// client) keeps the stored days instead of clearing them.
	daysProvided := r.FormValue("schedule-days-form") == "1"
	if !daysProvided {
		days = scheduleDaysSet(store.Get(scheduleDaysKey, ""))
	}
	// The per-day table (one window and limits per weekday) supersedes the
	// single window; a form without it (an older client) keeps the old path.
	if r.FormValue("schedule-week-form") == "1" {
		week, errKey := parseScheduleWeekForm(r)
		if errKey == "" && enabled && !week.AnyOn() {
			errKey = "Select at least one day for the scheduler window."
		}
		if errKey != "" {
			d.renderSettings(w, r, errKey, true)
			return
		}
		store.Set(scheduleEnabledKey, strconv.FormatBool(enabled))
		store.Set(scheduleWeekKey, week.String())
		scheduleUL = 0
		for _, day := range week {
			if day.On && day.Upload > scheduleUL {
				scheduleUL = day.Upload
			}
		}
	} else {
		if errKey := saveScheduleSingleWindow(store, enabled, start, end, scheduleDL, scheduleUL, days, daysProvided); errKey != "" {
			d.renderSettings(w, r, errKey, true)
			return
		}
	}

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

	// Global seeding limits (settings.json, enforced by the queue tick). The
	// per-torrent limits set through the API always win.
	store.Set(maxRatioEnabledKey, strconv.FormatBool(formBool(r, "max-ratio-enabled")))
	if v, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("max-ratio")), 64); err == nil && v >= 0 {
		store.Set(maxRatioKey, strconv.FormatFloat(v, 'f', -1, 64))
	}
	act := "0"
	if formBool(r, "max-ratio-remove") {
		act = "1"
	}
	store.Set(maxRatioActKey, act)
	store.Set(maxSeedingTimeEnabledKey, strconv.FormatBool(formBool(r, "max-seeding-time-enabled")))
	if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("max-seeding-time"))); err == nil && v >= 0 {
		store.Set(maxSeedingTimeKey, strconv.Itoa(v))
	}

	// Alternative limits and the manual toggle (settings.json, applied at once).
	altDL, altUL, _ := d.altSpeedSettings()
	if v, ok := parseFormKib(r.FormValue("alt-speed-download")); ok && v != altDL {
		store.Set(altSpeedDLKey, strconv.FormatInt(v, 10))
	}
	if v, ok := parseFormKib(r.FormValue("alt-speed-upload")); ok && v != altUL {
		store.Set(altSpeedULKey, strconv.FormatInt(v, 10))
	}
	// Only touch the toggle when the form carries it, so an older client that
	// does not post the checkbox does not clear the mode set from the API.
	if r.Form.Has("alt-speed-limits-on") {
		store.Set(altSpeedEnabledKey, strconv.FormatBool(formBool(r, "alt-speed-limits-on")))
	}

	// Folders, network, protocols, language and notifications (settings.json,
	// read at the next start).
	for _, key := range []string{
		"download-dir", "temp-dir", "listen", "peer-ports", "listen-interface",
		"outgoing-interface", "proxy", "token", "ipfilter", "dht-bootstrap",
		"watch-folders", "export-dir", "export-dir-fin",
		"notify-url", "lang", notifyWebhookTokenKey, notifyWebhookUserKey,
		notifyTelegramTokenKey, notifyTelegramChatKey, notifyEmailSMTPKey,
		notifyEmailFromKey, notifyEmailToKey,
	} {
		store.Set(key, strings.TrimSpace(r.FormValue(key)))
	}
	encryption := int(formInt(r, "encryption", 1))
	if encryption < 0 || encryption > 2 {
		encryption = 1
	}
	store.Set("encryption", strconv.Itoa(encryption))
	// Notifications: the password is kept as typed (it may hold spaces).
	store.Set(notifyEmailPasswordKey, r.FormValue(notifyEmailPasswordKey))
	format := strings.TrimSpace(r.FormValue(notifyFormatKey))
	if !slices.Contains(notify.Formats, format) {
		format = "json"
	}
	store.Set(notifyFormatKey, format)
	for _, key := range []string{notifyOnCompleteKey, notifyOnFeedMatchKey, notifyOnFeedErrorKey} {
		store.Set(key, strconv.FormatBool(formBool(r, key)))
	}
	store.Set("ipfilter-trackers", strconv.FormatBool(formBool(r, "ipfilter-trackers")))
	if hours := formInt(r, ipFilterRefreshKey, ipFilterDefaultRefreshHours); hours >= 0 && hours <= 24*30 {
		store.Set(ipFilterRefreshKey, strconv.FormatInt(hours, 10))
	}
	store.Set("dht-read-only", strconv.FormatBool(formBool(r, "dht-read-only")))
	store.Set("web-sources-allow-private", strconv.FormatBool(formBool(r, "web-sources-allow-private")))
	// DLNA media server (settings.json, applied by the DLNA loop).
	store.Set(dlnaEnabledKey, strconv.FormatBool(formBool(r, "dlna-enabled")))
	store.Set(dlnaListenKey, strings.TrimSpace(r.FormValue("dlna-listen")))
	store.Set(dlnaNameKey, strings.TrimSpace(r.FormValue("dlna-name")))
	store.Set(dlnaFoldersKey, strings.Join(dlnaSplitList(r.FormValue("dlna-folders")), ";"))
	store.Set(updateCheckKey, strconv.FormatBool(formBool(r, updateCheckKey)))
	if hours, err := strconv.Atoi(strings.TrimSpace(r.FormValue(updateHoursKey))); err == nil && hours >= 1 && hours <= updateMaxHours {
		store.Set(updateHoursKey, strconv.Itoa(hours))
	}
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

// saveScheduleSingleWindow stores the older single-window scheduler form (one
// window and one pair of limits on the selected days). It returns a catalog
// key when the form is invalid.
func saveScheduleSingleWindow(store *settings.Store, enabled bool, start, end string, scheduleDL, scheduleUL int64, days [7]bool, daysProvided bool) string {
	if enabled {
		startMinute, okStart := parseClock(start)
		endMinute, okEnd := parseClock(end)
		if !okStart || !okEnd {
			return "The scheduler window must be two times in HH:MM."
		}
		if startMinute == endMinute {
			return "The scheduler start and end must differ."
		}
		if scheduleDaysSelected(days) == 0 {
			return "Select at least one day for the scheduler window."
		}
	}
	store.Set(scheduleEnabledKey, strconv.FormatBool(enabled))
	store.Set(scheduleStartKey, start)
	store.Set(scheduleEndKey, end)
	store.Set(scheduleDLKey, strconv.FormatInt(scheduleDL, 10))
	store.Set(scheduleULKey, strconv.FormatInt(scheduleUL, 10))
	if daysProvided {
		store.Set(scheduleDaysKey, formatScheduleDays(days))
	}
	// A single-window form replaces any per-day schedule.
	store.Set(scheduleWeekKey, "")
	return ""
}

// parseScheduleWeekForm reads the per-day scheduler table: for every weekday
// "schedule-<day>-on", "-start", "-end", "-dl" and "-ul" (day = sun…sat). A
// day that is on needs two different HH:MM times; blank limits mean 0
// (unlimited). The error is a catalog key.
func parseScheduleWeekForm(r *http.Request) (bwschedule.Week, string) {
	var week bwschedule.Week
	for n := range week {
		prefix := "schedule-" + bwschedule.DayName(time.Weekday(n)) + "-"
		day := bwschedule.Day{On: formBool(r, prefix+"on")}
		start, okStart := parseClock(r.FormValue(prefix + "start"))
		end, okEnd := parseClock(r.FormValue(prefix + "end"))
		if day.On && (!okStart || !okEnd) {
			return week, "The scheduler window must be two times in HH:MM."
		}
		if day.On && start == end {
			return week, "The scheduler start and end must differ."
		}
		day.Start, day.End = start, end
		day.Download, _ = parseFormKib(r.FormValue(prefix + "dl"))
		day.Upload, _ = parseFormKib(r.FormValue(prefix + "ul"))
		week[n] = day
	}
	return week, ""
}

// scheduleWeekRows renders the per-day scheduler table body, Monday first.
// The day names and column headers are catalog strings, translated with the
// page; every input is named by its row header plus its column header.
func scheduleWeekRows(week bwschedule.Week) string {
	names := map[time.Weekday]string{time.Monday: "Mon", time.Tuesday: "Tue", time.Wednesday: "Wed",
		time.Thursday: "Thu", time.Friday: "Fri", time.Saturday: "Sat", time.Sunday: "Sun"}
	var b strings.Builder
	for _, weekday := range bwschedule.MondayFirst {
		day := week[weekday]
		key := bwschedule.DayName(weekday)
		prefix := "schedule-" + key + "-"
		row := "sched-row-" + key
		checked := ""
		if day.On {
			checked = " checked"
		}
		fmt.Fprintf(&b, `<tr><th scope="row" id="%s">%s</th>`, row, names[weekday])
		fmt.Fprintf(&b, `<td><input type="checkbox" name="%son" value="1"%s aria-labelledby="%s sched-col-on" title="Apply the scheduled limits on this day of the week"></td>`, prefix, checked, row)
		fmt.Fprintf(&b, `<td><input type="time" name="%sstart" value="%s" aria-labelledby="%s sched-col-start" title="Start time of the scheduled bandwidth window (HH:MM)"></td>`, prefix, bwschedule.FormatClock(day.Start), row)
		fmt.Fprintf(&b, `<td><input type="time" name="%send" value="%s" aria-labelledby="%s sched-col-end" title="End time of the scheduled bandwidth window (HH:MM)"></td>`, prefix, bwschedule.FormatClock(day.End), row)
		fmt.Fprintf(&b, `<td><input type="number" min="0" name="%sdl" value="%d" aria-labelledby="%s sched-col-dl" title="Download speed limit during scheduled window in KiB/s"></td>`, prefix, day.Download, row)
		fmt.Fprintf(&b, `<td><input type="number" min="0" name="%sul" value="%d" aria-labelledby="%s sched-col-ul" title="Upload speed limit during scheduled window in KiB/s"></td></tr>`, prefix, day.Upload, row)
	}
	return b.String()
}

// parseScheduleDayForm reads the day checkboxes (weekday numbers, 0=Sunday) into
// a per-weekday set.
func parseScheduleDayForm(values []string) [7]bool {
	var days [7]bool
	for _, value := range values {
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n >= 0 && n <= 6 {
			days[n] = true
		}
	}
	return days
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
	altDL, altUL, altManual := d.altSpeedSettings()
	repl("{{ALT_SPEED_DL}}", strconv.FormatInt(altDL, 10))
	repl("{{ALT_SPEED_UL}}", strconv.FormatInt(altUL, 10))
	repl("{{ALT_SPEED_ON}}", checkedIf(altManual))
	repl("{{SCHED_ENABLED}}", checkedIf(boolSetting(scheduleEnabledKey)))
	week, ok := d.scheduleWeek()
	if !ok {
		// Nothing usable saved yet: a night window, every day off, as a start.
		week = bwschedule.Uniform([7]bool{}, 1*60, 8*60, 0, 0)
	}
	repl("{{SCHED_WEEK_ROWS}}", scheduleWeekRows(week))
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
	repl("{{EXPORT_DIR}}", get("export-dir"))
	repl("{{EXPORT_FIN_DIR}}", get("export-dir-fin"))
	repl("{{MAX_RATIO_ON}}", checkedIf(boolSetting(maxRatioEnabledKey)))
	repl("{{MAX_RATIO}}", strconv.FormatFloat(d.settingFloat(maxRatioKey, -1), 'f', -1, 64))
	repl("{{MAX_RATIO_REMOVE}}", checkedIf(d.settingInt(maxRatioActKey, 0) == 1))
	repl("{{MAX_TIME_ON}}", checkedIf(boolSetting(maxSeedingTimeEnabledKey)))
	repl("{{MAX_TIME}}", strconv.Itoa(d.settingInt(maxSeedingTimeKey, -1)))
	repl("{{WATCH_FOLDERS}}", get("watch-folders"))
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
	repl("{{MIN_FREE_MB}}", strconv.FormatInt(cfg.MinFree()>>20, 10))
	repl("{{ACTIVE_LIMIT}}", strconv.Itoa(cfg.ActiveLimit))
	cacheMB := cfg.CacheMB
	if cacheMB < 0 {
		cacheMB = 0
	}
	repl("{{CACHE_MB}}", strconv.FormatInt(cacheMB, 10))
	repl("{{PREALLOCATE}}", checkedIf(cfg.Preallocate))
	repl("{{SEQUENTIAL}}", checkedIf(cfg.Sequential))
	repl("{{DLNA_ENABLED}}", checkedIf(boolSetting(dlnaEnabledKey)))
	repl("{{DLNA_LISTEN}}", get(dlnaListenKey))
	repl("{{DLNA_NAME}}", get(dlnaNameKey))
	// One folder per line in the textarea; stored ";"-separated.
	repl("{{DLNA_FOLDERS}}", html.EscapeString(strings.Join(dlnaSplitList(store.Get(dlnaFoldersKey, "")), "\n")))
	dlnaStatus := "Media server off."
	if base := d.dlnaStatusURL(); base != "" {
		dlnaStatus = "Media server running at <code>" + html.EscapeString(base) + "</code>"
	}
	repl("{{DLNA_STATUS}}", dlnaStatus)

	repl("{{UPDATE_CHECK}}", checkedIf(d.updateCheckEnabled()))
	repl("{{UPDATE_HOURS}}", strconv.Itoa(d.updateHours()))
	repl("{{UPDATE_STATUS}}", d.updateSectionHTML())
	repl("<!--UPDATE_NOTES-->", d.updateNotesDialogHTML())

	repl("{{LANG_OPTIONS}}", languageOptions(store.Get("lang", "")))
	repl("{{NOTIFY_URL}}", get("notify-url"))
	repl("{{NOTIFY_TG_TOKEN}}", get(notifyTelegramTokenKey))
	repl("{{NOTIFY_TG_CHAT}}", get(notifyTelegramChatKey))
	repl("{{NOTIFY_WH_TOKEN}}", get(notifyWebhookTokenKey))
	repl("{{NOTIFY_WH_USER}}", get(notifyWebhookUserKey))
	repl("{{NOTIFY_SMTP}}", get(notifyEmailSMTPKey))
	repl("{{NOTIFY_FROM}}", get(notifyEmailFromKey))
	repl("{{NOTIFY_TO}}", get(notifyEmailToKey))
	repl("{{NOTIFY_PASSWORD}}", get(notifyEmailPasswordKey))
	repl("{{NOTIFY_ON_COMPLETE}}", checkedIf(d.notifyEventOn("download_complete")))
	repl("{{NOTIFY_ON_MATCH}}", checkedIf(d.notifyEventOn("feed_match")))
	repl("{{NOTIFY_ON_ERROR}}", checkedIf(d.notifyEventOn("feed_error")))
	formatLabels := map[string]string{"json": "Generic JSON", "discord": "Discord", "slack": "Slack", "gotify": "Gotify", "ntfy": "ntfy", "pushover": "Pushover"}
	var formats strings.Builder
	selectedFormat := store.Get(notifyFormatKey, "json")
	for _, format := range notify.Formats {
		mark := ""
		if format == selectedFormat {
			mark = " selected"
		}
		formats.WriteString(`<option value="` + format + `"` + mark + `>` + formatLabels[format] + `</option>`)
	}
	repl("{{NOTIFY_FORMAT_OPTIONS}}", formats.String())

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
