package gextto

import (
	"strconv"
	"strings"
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

	// Velocità e rete
	"libtorrent_dl_limit":          {Unit: "KiB/s", Zero: "0 = illimitato"},
	"libtorrent_ul_limit":          {Unit: "KiB/s", Zero: "0 = illimitato"},
	"libtorrent_sched_start":       {Kind: "time", DependsOn: "libtorrent_sched_enabled"},
	"libtorrent_sched_end":         {Kind: "time", DependsOn: "libtorrent_sched_enabled"},
	"libtorrent_sched_days":        {Kind: "days", Zero: "nessun giorno = mai attiva", DependsOn: "libtorrent_sched_enabled"},
	"libtorrent_sched_dl_limit":    {Unit: "KiB/s", Zero: "0 = illimitato", DependsOn: "libtorrent_sched_enabled"},
	"libtorrent_sched_ul_limit":    {Unit: "KiB/s", Zero: "0 = illimitato", DependsOn: "libtorrent_sched_enabled"},
	"libtorrent_announce_interval": {Unit: "secondi"},
	// Ricerca peer e tracker
	"libtorrent_holepunch": {DependsOn: "libtorrent_utp"},

	// Code e prestazioni
	"libtorrent_dynamic_queue_min":           {DependsOn: "libtorrent_dynamic_queue"},
	"libtorrent_dynamic_queue_max":           {DependsOn: "libtorrent_dynamic_queue"},
	"libtorrent_upload_slots_limit":          {Zero: "-1 = auto"},
	"libtorrent_half_open_limit":             {Zero: "-1 = auto"},
	"libtorrent_max_connections_per_torrent": {Zero: "-1 = illimitato"},
	"libtorrent_max_uploads_per_torrent":     {Zero: "-1 = illimitato"},
	"libtorrent_ramdisk_dir":                 {DependsOn: "libtorrent_ramdisk_enabled"},
	"libtorrent_ramdisk_threshold_gb":        {Unit: "GB", DependsOn: "libtorrent_ramdisk_enabled"},
	"libtorrent_ramdisk_margin_gb":           {Unit: "GB", DependsOn: "libtorrent_ramdisk_enabled"},
	"libtorrent_ramdisk_min_free_bytes":      {Unit: "byte", Zero: "0 = usa il margine", DependsOn: "libtorrent_ramdisk_enabled"},
	"libtorrent_aio_threads":                 {Zero: "-1 = auto"},
	"libtorrent_cache_size":                  {Unit: "blocchi", Zero: "-1 = auto"},
	"libtorrent_cache_expiry":                {Unit: "secondi"},

	// Seed e completamento
	"libtorrent_seed_ratio":            {Zero: "0 = infinito"},
	"libtorrent_seed_time_days":        {Unit: "giorni", Zero: "0 = usa il limite in minuti"},
	"libtorrent_seed_time":             {Unit: "minuti"},
	"libtorrent_stall_after_min":       {Unit: "minuti"},
	"libtorrent_stall_retry_min":       {Unit: "minuti"},
	"libtorrent_stall_giveup_min":      {Unit: "minuti", Zero: "0 = mai"},
	"libtorrent_dead_swarm_giveup_min": {Unit: "minuti", Zero: "0 = usa la soglia generale"},

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
