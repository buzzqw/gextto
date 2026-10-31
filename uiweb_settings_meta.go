package gextto

import (
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/bwschedule"
)

// uiweb_settings_meta.go describes how each setting is edited, beyond its
// label: the unit shown next to the control, what the special value 0 (or -1)
// means, the control type when the value alone cannot tell, and the switch a
// setting depends on. Keeping this out of the labels keeps them short and lets
// the page render real controls (time pickers, day checkboxes, switches).

// uiSettingMeta is the presentation metadata of one setting.
type uiSettingMeta struct {
	// Unit is shown as a suffix of the control (secondi, minuti, GB, ...).
	Unit string
	// Scale converts between the stored value and the value shown: the page
	// shows value/Scale and saves value*Scale. 0 means the two are the same. It
	// lets a setting stored in seconds (refresh_interval) be edited in hours.
	Scale int64
	// Zero explains a special value, e.g. "0 = mai" or "-1 = auto".
	Zero string
	// Kind forces the control type: time, days, url or select.
	Kind string
	// Options are the choices of a forced select.
	Options []uiFormOption
	// DependsOn is the boolean setting that switches this one on: while it is
	// off the row is hidden, since its value has no effect.
	DependsOn string
}

var uiSettingMetaByKey = map[string]uiSettingMeta{
	"update_check_hours": {Unit: "ore", DependsOn: "update_check"},
	// Generale
	"refresh_interval":        {Unit: "ore", Scale: 3600},
	"max_release_age_days":    {Unit: "giorni", Zero: "0 = nessun limite"},
	"gap_fill_max_per_series": {Zero: "0 = illimitato", DependsOn: "gap_filling"},
	"gap_deep_interval_hours": {Unit: "ore", DependsOn: "gap_filling"},
	"gap_deep_max_per_cycle":  {DependsOn: "gap_filling"},
	"gap_last_resort":         {DependsOn: "gap_filling"},
	"cycle_title_search": {Kind: "select", Options: []uiFormOption{
		{Value: "auto", Label: "Automatica (solo se non ci sono feed)"},
		{Value: "yes", Label: "Sempre"},
		{Value: "no", Label: "Mai"},
	}},
	"delay_torrent_minutes": {Unit: "minuti", Zero: "0 = nessuno"},
	"delay_movies_minutes":  {Unit: "minuti", Zero: "0 = nessuno"},
	"delay_bypass_score":    {Zero: "0 = mai"},

	// Qualità e upgrade
	"upgrade_until_score":          {Zero: "0 = mai"},
	"stall_alternative_after_min":  {Unit: "minuti", Zero: "0 = mai"},
	"stall_alternative_score_drop": {Zero: "0 = disattivato"},

	// Motore torrent
	"qbittorrent_url":                  {Kind: "url"},
	"qbittorrent_request_timeout_secs": {Unit: "secondi"},
	"qbittorrent_poll_interval_ms":     {Unit: "ms"},
	"gxtorrent_url":                    {Kind: "url"},
	"gxtorrent_request_timeout_secs":   {Unit: "secondi"},
	"gxtorrent_poll_interval_ms":       {Unit: "ms"},
	"gxtorrent_dlna_listen":            {DependsOn: "gxtorrent_dlna"},
	"gxtorrent_dlna_name":              {DependsOn: "gxtorrent_dlna"},
	"gxtorrent_dlna_folders":           {Zero: "vuoto = automatico (archivio)", DependsOn: "gxtorrent_dlna"},

	// eD2k e Kad
	"amule_host":     {DependsOn: "ed2k_enabled"},
	"amule_port":     {DependsOn: "ed2k_enabled"},
	"amule_password": {DependsOn: "ed2k_enabled"},
	"ed2k_search_network": {Kind: "select", DependsOn: "ed2k_enabled", Options: []uiFormOption{
		{Value: "kad", Label: "Kad"},
		{Value: "both", Label: "Kad + server eD2k"},
		{Value: "global", Label: "Server eD2k (globale)"},
		{Value: "local", Label: "Server eD2k (connesso)"},
	}},
	"ed2k_fallback":            {DependsOn: "ed2k_enabled"},
	"ed2k_min_sources":         {Unit: "fonti", Zero: "0 = nessun minimo", DependsOn: "ed2k_fallback"},
	"ed2k_search_timeout_secs": {Unit: "secondi", DependsOn: "ed2k_enabled"},
	"ed2k_stall_days":          {Unit: "giorni", Zero: "0 = mai", DependsOn: "ed2k_enabled"},

	// Velocità e rete
	"torrent_speed_mode": {Kind: "select", Options: []uiFormOption{
		{Value: speedModeAuto, Label: "Auto: Gextto misura la linea e si adatta"},
		{Value: speedModeFixed, Label: "Limiti fissi"},
		{Value: speedModeSchedule, Label: "Programmazione oraria"},
	}},
	"torrent_dl_limit":   {Unit: "KiB/s", Zero: "0 = illimitato", DependsOn: "torrent_speed_mode=fixed|schedule"},
	"torrent_ul_limit":   {Unit: "KiB/s", Zero: "0 = illimitato", DependsOn: "torrent_speed_mode=fixed|schedule"},
	"torrent_speed_auto": {DependsOn: "torrent_speed_mode=schedule"},
	"torrent_auto_latency_source": {Kind: "select", Options: []uiFormOption{
		{Value: "both", Label: "Ping Google/Cloudflare + router (consigliato)"},
		{Value: "ping", Label: "Solo ping Google/Cloudflare (ignora il router)"},
		{Value: "router", Label: "Solo contatori del router (vecchio comportamento)"},
	}, DependsOn: "torrent_auto_latency_guard"},
	"torrent_auto_latency_target": {Zero: "vuoto = Google 8.8.8.8/8.8.4.4 e Cloudflare (porta 443)", DependsOn: "torrent_auto_latency_guard"},
	"torrent_line_max_down_kib":   {Unit: "KiB/s", Zero: "0 = non nota: la misura Auto", DependsOn: "torrent_auto_latency_guard"},
	"torrent_line_max_up_kib":     {Unit: "KiB/s", Zero: "0 = non nota: la misura Auto", DependsOn: "torrent_auto_latency_guard"},
	"torrent_sched_start":         {Kind: "time", DependsOn: "torrent_sched_enabled"},
	"torrent_sched_end":           {Kind: "time", DependsOn: "torrent_sched_enabled"},
	"torrent_sched_days":          {Kind: "days", Zero: "nessun giorno = mai attiva", DependsOn: "torrent_sched_enabled"},
	"torrent_sched_week":          {Kind: "week", Zero: "un giorno «Limite» ha i suoi valori (0 = illimitato); «Auto» lascia che Gextto misuri la linea e si adatti; una fascia oltre la mezzanotte appartiene al giorno in cui inizia", DependsOn: "torrent_speed_mode=schedule"},
	"torrent_sched_dl_limit":      {Unit: "KiB/s", Zero: "0 = illimitato", DependsOn: "torrent_sched_enabled"},
	"torrent_sched_ul_limit":      {Unit: "KiB/s", Zero: "0 = illimitato", DependsOn: "torrent_sched_enabled"},
	// Ricerca peer e tracker
	"torrent_holepunch": {DependsOn: "torrent_utp"},

	// Code e prestazioni
	"torrent_dynamic_queue_min":           {DependsOn: "torrent_dynamic_queue"},
	"torrent_dynamic_queue_max":           {DependsOn: "torrent_dynamic_queue"},
	"torrent_max_connections_per_torrent": {Zero: "-1 = nessun tetto"},
	"torrent_ramdisk_dir":                 {DependsOn: "torrent_ramdisk_enabled"},
	"torrent_ramdisk_threshold_gb":        {Unit: "GB", DependsOn: "torrent_ramdisk_enabled"},
	"torrent_ramdisk_margin_gb":           {Unit: "GB", DependsOn: "torrent_ramdisk_enabled"},
	"torrent_ramdisk_min_free_bytes":      {Unit: "byte", Zero: "0 = usa il margine", DependsOn: "torrent_ramdisk_enabled"},
	"torrent_cache_size":                  {Unit: "blocchi", Zero: "-1 = auto"},
	"torrent_cache_expiry":                {Unit: "secondi"},

	// Seed e completamento
	"torrent_seed_ratio":                {Zero: "0 = infinito"},
	"torrent_seed_time_days":            {Unit: "giorni", Zero: "0 = usa il limite in minuti"},
	"torrent_seed_time":                 {Unit: "minuti"},
	"torrent_seed_idle_minutes":         {Unit: "minuti", Zero: "0 = disattivato"},
	"torrent_stall_after_min":           {Unit: "minuti"},
	"torrent_stall_retry_min":           {Unit: "minuti"},
	"torrent_stall_giveup_min":          {Unit: "minuti", Zero: "0 = mai"},
	"torrent_dead_swarm_giveup_min":     {Unit: "minuti", Zero: "0 = usa la soglia generale"},
	"torrent_dead_swarm_blocklist_days": {Unit: "giorni", Zero: "0 = per sempre"},

	// Identità annunciata dal motore gx-torrent.
	"gxtorrent_client_identity": {Kind: "select", Options: []uiFormOption{
		{Value: "gx-core", Label: "gx-core"},
		{Value: "qbittorrent", Label: "qBittorrent"},
		{Value: "transmission", Label: "Transmission"},
		{Value: "rain", Label: "Rain"},
		{Value: "anacrolix", Label: "anacrolix/torrent"},
	}},

	// Libreria e rinomina
	"rename_verify_interval": {Unit: "ore"},
	"audio_cleanup_language": {Kind: "langcombo", DependsOn: "audio_cleanup", Options: []uiFormOption{
		{Value: "ita", Label: "Italiano"},
		{Value: "eng", Label: "English"},
		{Value: "fra", Label: "Français"},
		{Value: "deu", Label: "Deutsch"},
		{Value: "spa", Label: "Español"},
		{Value: "por", Label: "Português"},
		{Value: "nld", Label: "Nederlands"},
		{Value: "pol", Label: "Polski"},
		{Value: "swe", Label: "Svenska"},
		{Value: "nor", Label: "Norsk"},
		{Value: "dan", Label: "Dansk"},
		{Value: "fin", Label: "Suomi"},
		{Value: "ces", Label: "Čeština"},
		{Value: "hun", Label: "Magyar"},
		{Value: "ron", Label: "Română"},
		{Value: "ell", Label: "Ελληνικά"},
		{Value: "tur", Label: "Türkçe"},
		{Value: "rus", Label: "Русский"},
		{Value: "ukr", Label: "Українська"},
		{Value: "ara", Label: "العربية"},
		{Value: "heb", Label: "עברית"},
		{Value: "hin", Label: "हिन्दी"},
		{Value: "jpn", Label: "日本語"},
		{Value: "kor", Label: "한국어"},
		{Value: "zho", Label: "中文"},
	}},
	"audio_cleanup_policy": {Kind: "select", DependsOn: "audio_cleanup", Options: []uiFormOption{
		{Value: "all", Label: "Tutte"},
		{Value: "best", Label: "Solo la migliore"},
		{Value: "first", Label: "Solo la prima"},
	}},

	// Archivio e spazio
	"trash_retention_days": {Unit: "giorni", Zero: "0 = svuota tutto"},
	"min_free_space_gb":    {Unit: "GB", Zero: "0 = nessun controllo"},

	// Manutenzione automatica
	"housekeeping_interval_hours":          {Unit: "ore", DependsOn: "housekeeping_enabled"},
	"housekeeping_retain_cycles":           {DependsOn: "housekeeping_enabled"},
	"housekeeping_seen_days":               {Unit: "giorni", Zero: "0 = mai", DependsOn: "housekeeping_enabled"},
	"housekeeping_history_days":            {Unit: "giorni", Zero: "0 = conserva sempre", DependsOn: "housekeeping_enabled"},
	"housekeeping_error_age_days":          {Unit: "giorni", DependsOn: "housekeeping_enabled"},
	"housekeeping_gap_log_days":            {Unit: "giorni", Zero: "0 = mai", DependsOn: "housekeeping_enabled"},
	"housekeeping_upgrade_backup_days":     {Unit: "giorni", Zero: "0 = mai", DependsOn: "housekeeping_enabled"},
	"archive_max_age_days":                 {Unit: "giorni", Zero: "0 = nessun limite", DependsOn: "archive_cleanup_enabled"},
	"archive_keep_min":                     {DependsOn: "archive_cleanup_enabled"},
	"media_info_backfill_interval_minutes": {Unit: "minuti", DependsOn: "media_info_backfill_enabled"},
	"media_info_backfill_batch":            {DependsOn: "media_info_backfill_enabled"},
	"media_integrity_interval_minutes":     {Unit: "minuti", DependsOn: "media_integrity_enabled"},
	"media_integrity_batch":                {DependsOn: "media_integrity_enabled"},
	"media_integrity_quick_4k":             {DependsOn: "media_integrity_enabled"},
	"temp_orphan_min_age_days":             {Unit: "giorni", DependsOn: "temp_orphan_cleanup_enabled"},

	// Notifiche
	"telegram_bot_token": {DependsOn: "notify_telegram"},
	"telegram_chat_id":   {DependsOn: "notify_telegram"},
	"email_smtp":         {DependsOn: "notify_email"},
	"email_from":         {DependsOn: "notify_email"},
	"email_to":           {DependsOn: "notify_email"},
	"email_password":     {DependsOn: "notify_email"},
	"notify_webhook_url": {Kind: "url"},
	"notify_webhook_format": {Kind: "select", Options: []uiFormOption{
		{Value: "gextto", Label: "Gextto (JSON firmato)"},
		{Value: "discord", Label: "Discord"},
		{Value: "slack", Label: "Slack"},
		{Value: "ntfy", Label: "ntfy"},
		{Value: "gotify", Label: "Gotify"},
		{Value: "pushover", Label: "Pushover"},
	}},
	"notify_discord_url":      {Kind: "url"},
	"notify_slack_url":        {Kind: "url"},
	"notify_gotify_url":       {Kind: "url"},
	"notify_ntfy_url":         {Kind: "url"},
	"notify_apprise_url":      {Kind: "url"},
	"notify_cooldown_minutes": {Unit: "minuti", Zero: "0 = nessun limite"},
	"notify_language": {Kind: "select", Options: []uiFormOption{
		{Value: "", Label: "Automatica (lingua dell'interfaccia)"},
		{Value: "it", Label: "Italiano"},
		{Value: "en", Label: "Inglese"},
		{Value: "de", Label: "Tedesco"},
		{Value: "fr", Label: "Francese"},
		{Value: "es", Label: "Spagnolo"},
		{Value: "pl", Label: "Polacco"},
	}},

	// Accesso
	"auth_local_bypass": {DependsOn: "auth_enabled"},
	"auth_username":     {DependsOn: "auth_enabled"},
	"auth_password":     {DependsOn: "auth_enabled"},
}

// uiWeekRow is one day of the per-day bandwidth schedule editor. Mode is the
// single choice per day: "off" (no window), "limit" (the window with the two
// numbers) or "auto" (the window with no limit, left to the guided tuning so the
// line is measured there). One choice, so there is no "Attivo" checkbox to
// combine with an "Auto" one.
type uiWeekRow struct {
	// Key is the stored day name (mon…sun), used in the form field names.
	Key      string
	Label    string
	Mode     string
	Start    string
	End      string
	Download int64
	Upload   int64
}

// uiWeekRows renders a stored libtorrent_sched_week value as editor rows,
// Monday first. An empty or unreadable value gives every day off on the
// default night window.
func uiWeekRows(value string) []uiWeekRow {
	week, ok := bwschedule.Parse(value)
	if !ok {
		week = bwschedule.Uniform([7]bool{}, 23*60, 8*60, 0, 0)
	}
	return uiWeekRowsFrom(week)
}

func uiWeekRowsFrom(week bwschedule.Week) []uiWeekRow {
	rows := make([]uiWeekRow, 0, 7)
	for index, weekday := range bwschedule.MondayFirst {
		day := week[weekday]
		mode := "off"
		if day.Auto() {
			mode = "auto"
		} else if day.On {
			mode = "limit"
		}
		rows = append(rows, uiWeekRow{Key: bwschedule.DayName(weekday), Label: uiWeekdays[index], Mode: mode,
			Start: bwschedule.FormatClock(day.Start), End: bwschedule.FormatClock(day.End),
			Download: day.Download, Upload: day.Upload})
	}
	return rows
}

// uiWeekValue reads the per-day schedule editor of a settings form
// ("day-<name>-mode|start|end|dl|ul"; the older "on"/"auto" checkboxes are still
// read). It returns an Italian error message (the page translates it) for an
// active day without a valid window.
func uiWeekValue(form url.Values) (string, string) {
	var week bwschedule.Week
	for _, weekday := range bwschedule.MondayFirst {
		prefix := "day-" + bwschedule.DayName(weekday) + "-"
		// One choice per day. Older forms used two checkboxes ("on"/"auto"); keep
		// reading them so a page loaded before the change still saves.
		mode := strings.ToLower(strings.TrimSpace(form.Get(prefix + "mode")))
		switch {
		case mode == "off" || mode == "limit" || mode == "auto":
		case form.Get(prefix+"auto") != "":
			mode = "auto"
		case form.Get(prefix+"on") != "":
			mode = "limit"
		default:
			mode = "off"
		}
		on := mode == "limit" || mode == "auto"
		start, okStart := bwschedule.ParseClock(form.Get(prefix + "start"))
		end, okEnd := bwschedule.ParseClock(form.Get(prefix + "end"))
		if on && (!okStart || !okEnd || start == end) {
			return "", "ogni giorno attivo vuole un orario di inizio e uno di fine diversi"
		}
		// "Auto" hands the window to the guided tuning (unlimited): the two
		// numbers are kept out of the way and stored as the AutoLimit sentinel.
		if mode == "auto" {
			week[weekday] = bwschedule.Day{On: true, Start: start, End: end, Download: bwschedule.AutoLimit, Upload: bwschedule.AutoLimit}
			continue
		}
		limit := func(name string) int64 {
			parsed, err := strconv.ParseInt(strings.TrimSpace(form.Get(prefix+name)), 10, 64)
			if err != nil || parsed < 0 {
				return 0
			}
			return parsed
		}
		week[weekday] = bwschedule.Day{On: on, Start: start, End: end, Download: limit("dl"), Upload: limit("ul")}
	}
	return week.String(), ""
}

// uiWeekdays are the values of libtorrent_sched_days: 0 = Monday … 6 = Sunday.
var uiWeekdays = []string{"Lunedì", "Martedì", "Mercoledì", "Giovedì", "Venerdì", "Sabato", "Domenica"}

// uiDaysOptions turns a "0,1,4" day list into one checkbox option per weekday.
func uiDaysOptions(value string) []uiFormOption {
	selected := map[string]bool{}
	for _, day := range strings.Split(value, ",") {
		if parsed, err := strconv.Atoi(strings.TrimSpace(day)); err == nil {
			selected[strconv.Itoa(parsed)] = true
		}
	}
	options := make([]uiFormOption, 0, len(uiWeekdays))
	for index, label := range uiWeekdays {
		value := strconv.Itoa(index)
		options = append(options, uiFormOption{Value: value, Label: label, Selected: selected[value]})
	}
	return options
}

// uiDaysValue joins the checked weekdays back into the stored "0,1,4" form,
// in weekday order and without duplicates or foreign values.
func uiDaysValue(values []string) string {
	checked := map[int]bool{}
	for _, value := range values {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed >= 0 && parsed < len(uiWeekdays) {
			checked[parsed] = true
		}
	}
	parts := []string{}
	for day := range uiWeekdays {
		if checked[day] {
			parts = append(parts, strconv.Itoa(day))
		}
	}
	return strings.Join(parts, ",")
}

// uiIsNumber reports whether value is a plain decimal number.
func uiIsNumber(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	_, err := strconv.ParseFloat(trimmed, 64)
	return err == nil
}

// uiIsBoolSpelling reports whether value is one of the boolean spellings the
// settings readers accept.
func uiIsBoolSpelling(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "false", "yes", "no", "on", "off", "1", "0":
		return true
	}
	return false
}

// uiSettingDisplayNumber converts a stored numeric value into the unit the page
// shows (a setting stored in seconds edited in hours). Keys without a scale are
// returned unchanged.
func uiSettingDisplayNumber(key, stored string) string {
	scale := uiSettingMetaByKey[key].Scale
	if scale <= 1 || strings.TrimSpace(stored) == "" {
		return stored
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(stored), 64)
	if err != nil {
		return stored
	}
	return formatScaledNumber(value / float64(scale))
}

// uiSettingStoredNumber converts a value typed in the shown unit back into the
// stored unit (hours to seconds), rounded to a whole number.
func uiSettingStoredNumber(key, shown string) string {
	scale := uiSettingMetaByKey[key].Scale
	if scale <= 1 || strings.TrimSpace(shown) == "" {
		return shown
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(shown), 64)
	if err != nil {
		return shown
	}
	return strconv.FormatInt(int64(math.Round(value*float64(scale))), 10)
}

// formatScaledNumber prints a scaled number without a trailing ".0".
func formatScaledNumber(value float64) string {
	if value == math.Trunc(value) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// uiSettingSameValue compares a stored value with the default the way the
// field shows them: booleans by meaning, numbers numerically, text exactly. A
// scaled unit (hours shown, seconds stored) is compared in the shown unit.
func uiSettingSameValue(field uiSettingField, stored, def string) bool {
	stored, def = strings.TrimSpace(stored), strings.TrimSpace(def)
	if uiSettingMetaByKey[field.Key].Scale > 1 {
		stored = uiSettingDisplayNumber(field.Key, stored)
		def = uiSettingDisplayNumber(field.Key, def)
	}
	switch field.Kind {
	case "bool":
		storedBool, _, _ := uiBoolValues(stored)
		defBool, _, _ := uiBoolValues(def)
		return storedBool == defBool
	case "select":
		// A two-choice combo over a boolean (Attivo/Non attivo) is compared by
		// meaning, so a stored "1" is not "modified" against a default "true".
		if uiIsBoolSpelling(stored) && uiIsBoolSpelling(def) {
			storedBool, _, _ := uiBoolValues(stored)
			defBool, _, _ := uiBoolValues(def)
			return storedBool == defBool
		}
	case "number":
		a, errA := strconv.ParseFloat(stored, 64)
		b, errB := strconv.ParseFloat(def, 64)
		if errA == nil && errB == nil {
			return a == b
		}
	case "days":
		return uiDaysValue(strings.Split(stored, ",")) == uiDaysValue(strings.Split(def, ","))
	}
	return stored == def
}
