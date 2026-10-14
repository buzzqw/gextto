package gextto

import (
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
	"refresh_interval":        {Unit: "secondi"},
	"max_release_age_days":    {Unit: "giorni", Zero: "0 = nessun limite"},
	"gap_fill_max_per_series": {Zero: "0 = illimitato", DependsOn: "gap_filling"},
	"gap_deep_interval_hours": {Unit: "ore", DependsOn: "gap_filling"},
	"gap_deep_max_per_cycle":  {DependsOn: "gap_filling"},
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

	// Velocità e rete
	"torrent_dl_limit":             {Unit: "KiB/s", Zero: "0 = illimitato"},
	"torrent_ul_limit":             {Unit: "KiB/s", Zero: "0 = illimitato"},
	"torrent_sched_start":          {Kind: "time", DependsOn: "torrent_sched_enabled"},
	"torrent_sched_end":            {Kind: "time", DependsOn: "torrent_sched_enabled"},
	"torrent_sched_days":           {Kind: "days", Zero: "nessun giorno = mai attiva", DependsOn: "torrent_sched_enabled"},
	"torrent_sched_week":           {Kind: "week", Zero: "ogni giorno ha fascia e limiti propri (0 = illimitato); una fascia oltre la mezzanotte appartiene al giorno in cui inizia", DependsOn: "torrent_sched_enabled"},
	"torrent_sched_dl_limit":       {Unit: "KiB/s", Zero: "0 = illimitato", DependsOn: "torrent_sched_enabled"},
	"torrent_sched_ul_limit":       {Unit: "KiB/s", Zero: "0 = illimitato", DependsOn: "torrent_sched_enabled"},
	"libtorrent_announce_interval": {Unit: "secondi"},
	// Ricerca peer e tracker
	"torrent_holepunch": {DependsOn: "torrent_utp"},

	// Code e prestazioni
	"torrent_dynamic_queue_min":           {DependsOn: "torrent_dynamic_queue"},
	"torrent_dynamic_queue_max":           {DependsOn: "torrent_dynamic_queue"},
	"libtorrent_upload_slots_limit":       {Zero: "-1 = auto"},
	"libtorrent_half_open_limit":          {Zero: "-1 = auto"},
	"torrent_max_connections_per_torrent": {Zero: "-1 = illimitato"},
	"libtorrent_max_uploads_per_torrent":  {Zero: "-1 = illimitato"},
	"torrent_ramdisk_dir":                 {DependsOn: "torrent_ramdisk_enabled"},
	"torrent_ramdisk_threshold_gb":        {Unit: "GB", DependsOn: "torrent_ramdisk_enabled"},
	"torrent_ramdisk_margin_gb":           {Unit: "GB", DependsOn: "torrent_ramdisk_enabled"},
	"torrent_ramdisk_min_free_bytes":      {Unit: "byte", Zero: "0 = usa il margine", DependsOn: "torrent_ramdisk_enabled"},
	"libtorrent_aio_threads":              {Zero: "-1 = auto"},
	"torrent_cache_size":                  {Unit: "blocchi", Zero: "-1 = auto"},
	"torrent_cache_expiry":                {Unit: "secondi"},

	// Seed e completamento
	"torrent_seed_ratio":            {Zero: "0 = infinito"},
	"torrent_seed_time_days":        {Unit: "giorni", Zero: "0 = usa il limite in minuti"},
	"torrent_seed_time":             {Unit: "minuti"},
	"torrent_stall_after_min":       {Unit: "minuti"},
	"torrent_stall_retry_min":       {Unit: "minuti"},
	"torrent_stall_giveup_min":      {Unit: "minuti", Zero: "0 = mai"},
	"torrent_dead_swarm_giveup_min": {Unit: "minuti", Zero: "0 = usa la soglia generale"},

	// Libreria e rinomina
	"rename_verify_interval": {Unit: "ore"},

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

	// Accesso
	"auth_local_bypass": {DependsOn: "auth_enabled"},
	"auth_username":     {DependsOn: "auth_enabled"},
	"auth_password":     {DependsOn: "auth_enabled"},
}

// uiWeekRow is one day of the per-day bandwidth schedule editor.
type uiWeekRow struct {
	// Key is the stored day name (mon…sun), used in the form field names.
	Key      string
	Label    string
	On       bool
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
		rows = append(rows, uiWeekRow{Key: bwschedule.DayName(weekday), Label: uiWeekdays[index], On: day.On,
			Start: bwschedule.FormatClock(day.Start), End: bwschedule.FormatClock(day.End),
			Download: day.Download, Upload: day.Upload})
	}
	return rows
}

// uiWeekValue reads the per-day schedule editor of a settings form
// ("day-<name>-on|start|end|dl|ul"). It returns an Italian error message (the
// page translates it) for an active day without a valid window.
func uiWeekValue(form url.Values) (string, string) {
	var week bwschedule.Week
	for _, weekday := range bwschedule.MondayFirst {
		prefix := "day-" + bwschedule.DayName(weekday) + "-"
		on := form.Get(prefix+"on") != ""
		start, okStart := bwschedule.ParseClock(form.Get(prefix + "start"))
		end, okEnd := bwschedule.ParseClock(form.Get(prefix + "end"))
		if on && (!okStart || !okEnd || start == end) {
			return "", "ogni giorno attivo vuole un orario di inizio e uno di fine diversi"
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

// uiSettingSameValue compares a stored value with the default the way the
// field shows them: booleans by meaning, numbers numerically, text exactly.
func uiSettingSameValue(field uiSettingField, stored, def string) bool {
	stored, def = strings.TrimSpace(stored), strings.TrimSpace(def)
	switch field.Kind {
	case "bool":
		storedBool, _, _ := uiBoolValues(stored)
		defBool, _, _ := uiBoolValues(def)
		return storedBool == defBool
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
