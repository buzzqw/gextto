package gextto

// uiweb_settings_defaults.go carries the default value of every setting the
// Configurazione page exposes, ported from rextto's `setting_tooltip` era and
// from Gextto's own config defaults (config.go, DefaultLibtorrentSettings and
// the config_view mapping in web_handlers_core.go).
//
// The settings page reads raw `cfg.Settings`, which only contains the keys the
// user already saved: without this map a fresh install shows empty inputs and
// booleans render as text. Filling Value/Placeholder from here makes the form
// show the same starting values as rextto/extto.

// uiSettingDefaults maps a setting key to the value shown when the user has not
// saved anything yet. Boolean-looking values (true/false, yes/no, on/off) make
// uiSettingKind render a Sì/No select, so keep the spelling consistent with the
// backend readers.
var uiSettingDefaults = map[string]string{
	// --- Punteggi -----------------------------------------------------------
	"score_res_2160p":     "2000",
	"score_res_1080p":     "1000",
	"score_res_720p":      "400",
	"score_res_576p":      "80",
	"score_res_480p":      "40",
	"score_res_360p":      "20",
	"score_source_bluray": "300",
	"score_source_remux":  "400",
	"score_source_webdl":  "200",
	"score_source_webrip": "150",
	"score_source_hdtv":   "50",
	"score_source_dvdrip": "20",
	"score_codec_h265":    "200",
	"score_codec_h264":    "50",
	"score_audio_truehd":  "150",
	"score_audio_dts-hd":  "120",
	"score_audio_dts":     "100",
	"score_audio_ddp":     "80",
	"score_audio_ac3":     "50",
	"score_audio_5.1":     "50",
	"score_audio_aac":     "30",
	"score_audio_mp3":     "10",
	"score_bonus_dv":      "300",
	"score_bonus_hdr":     "100",
	"score_bonus_proper":  "75",
	"score_bonus_repack":  "100",
	"score_bonus_real":    "100",

	// --- Daemon -------------------------------------------------------------
	"active":                  "false",
	"refresh_interval":        "21600",
	"max_release_age_days":    "0",
	"gap_fill_max_per_series": "0",
	"gap_filling":             "yes",
	"gap_deep_interval_hours": "6",
	"gap_deep_max_per_cycle":  "5",
	"cycle_title_search":      "auto",

	// --- Sorgenti -----------------------------------------------------------
	"blacklist":         "cam\ncamrip\nts\ntelesync\ntelecine\nscr\nscreener\nworkprint\nsample",
	"websearch_engines": "",
	"content_filters":   "",

	// --- Libtorrent ---------------------------------------------------------
	"libtorrent_enabled":                           "true",
	"libtorrent_dynamic_queue":                     "false",
	"libtorrent_auto_optimize":                     "false",
	"libtorrent_preallocate":                       "false",
	"libtorrent_dynamic_queue_min":                 "1",
	"libtorrent_dynamic_queue_max":                 "10",
	"libtorrent_dont_count_slow_torrents":          "true",
	"libtorrent_stall_after_min":                   "60",
	"libtorrent_stall_retry_min":                   "60",
	"libtorrent_stall_giveup_min":                  "20160",
	"libtorrent_sequential":                        "false",
	"libtorrent_active_downloads":                  "3",
	"libtorrent_active_seeds":                      "3",
	"libtorrent_active_limit":                      "5",
	"libtorrent_seed_ratio":                        "0",
	"libtorrent_seed_time":                         "0",
	"libtorrent_seed_time_days":                    "0",
	"auto_remove_completed":                        "false",
	"libtorrent_connections_limit":                 "200",
	"libtorrent_upload_slots_limit":                "-1",
	"libtorrent_half_open_limit":                   "-1",
	"libtorrent_max_connections_per_torrent":       "-1",
	"libtorrent_max_uploads_per_torrent":           "-1",
	"libtorrent_aio_threads":                       "-1",
	"libtorrent_cache_size":                        "-1",
	"libtorrent_cache_expiry":                      "300",
	"libtorrent_alert_queue_size":                  "1000",
	"libtorrent_dht":                               "true",
	"libtorrent_pex":                               "true",
	"libtorrent_lsd":                               "true",
	"libtorrent_upnp":                              "true",
	"libtorrent_natpmp":                            "true",
	"libtorrent_utp":                               "true",
	"libtorrent_prefer_rc4":                        "false",
	"libtorrent_announce_to_all_trackers":          "false",
	"libtorrent_announce_to_all_tiers":             "false",
	"libtorrent_allow_multiple_connections_per_ip": "true",
	"libtorrent_announce_interval":                 "1800",
	"libtorrent_torrent_connect_boost":             "50",
	"libtorrent_dht_bootstrap_nodes":               "",
	"libtorrent_encryption":                        "1",
	"libtorrent_apply_ip_filter":                   "true",
	"libtorrent_ipfilter_url":                      "",
	"libtorrent_listen_interfaces":                 "0.0.0.0:6881-6891",
	"libtorrent_outgoing_interface":                "",
	"libtorrent_ramdisk_dir":                       "",
	"libtorrent_ramdisk_enabled":                   "false",
	"libtorrent_ramdisk_threshold_gb":              "3.5",
	"libtorrent_ramdisk_margin_gb":                 "0.5",
	"libtorrent_ramdisk_min_free_bytes":            "0",
	"libtorrent_port_min":                          "6881",
	"libtorrent_port_max":                          "6891",
	"libtorrent_dl_limit":                          "0",
	"libtorrent_ul_limit":                          "0",
	"libtorrent_sched_enabled":                     "false",
	"libtorrent_sched_start":                       "23:00",
	"libtorrent_sched_end":                         "08:00",
	"libtorrent_sched_days":                        "",
	"libtorrent_sched_dl_limit":                    "0",
	"libtorrent_sched_ul_limit":                    "0",
	"libtorrent_extra_settings":                    "Una per riga: chiave=valore\nmax_peerlist_size=4000\nmax_queued_disk_bytes=104857600\nactive_downloads=6\nsmooth_connects=true\nrequest_timeout=10",

	// --- Motore torrent (backend) ------------------------------------------
	"torrent_backend":                  "embedded",
	"qbittorrent_url":                  "http://127.0.0.1:8080",
	"qbittorrent_username":             "admin",
	"qbittorrent_request_timeout_secs": "15",
	"qbittorrent_poll_interval_ms":     "1500",
	"qbittorrent_managed":              "false",

	// --- Rinomina -----------------------------------------------------------
	"rename_episodes":        "false",
	"tvdb_language":          "ita",
	"tmdb_language":          "it-IT",
	"default_language":       "ita",
	"cleanup_upgrades":       "false",
	"cleanup_min_score_diff": "0",
	"upgrade_min_score_diff": "200",

	// --- Avanzate -----------------------------------------------------------
	"min_free_space_gb":          "0",
	"trash_retention_days":       "0",
	"archive_cleanup_enabled":    "false",
	"archive_max_age_days":       "0",
	"archive_keep_min":           "0",
	"stop_on_old_page_threshold": "3",
	"rename_verify_interval":     "6",
	"move_episodes":              "false",
	"debug_enabled":              "false",

	// --- Acquisizione -------------------------------------------------------
	"delay_torrent_minutes":                "0",
	"delay_movies_minutes":                 "0",
	"delay_bypass_score":                   "0",
	"housekeeping_enabled":                 "true",
	"housekeeping_interval_hours":          "24",
	"housekeeping_retain_cycles":           "200",
	"housekeeping_seen_days":               "30",
	"housekeeping_history_days":            "0",
	"housekeeping_error_age_days":          "7",
	"housekeeping_gap_log_days":            "30",
	"housekeeping_upgrade_backup_days":     "30",
	"media_info_backfill_enabled":          "true",
	"media_info_backfill_interval_minutes": "60",
	"media_info_backfill_batch":            "10",

	// --- Notifiche ----------------------------------------------------------
	"notify_telegram": "false",
	"notify_email":    "false",
	"email_smtp":      "smtp.gmail.com:587",

	// --- Percorsi -----------------------------------------------------------
	"cleanup_action": "move",
}

// uiSettingNoPrefill lists keys whose default must stay a placeholder: their
// value is a list/JSON blob, and pre-filling it in the textarea would make an
// accidental save overwrite the stored structure.
var uiSettingNoPrefill = map[string]bool{
	"blacklist":                         true,
	"content_filters":                   true,
	"websearch_engines":                 true,
	"libtorrent_dht_bootstrap_nodes":    true,
	"libtorrent_sched_days":             true,
	"libtorrent_extra_settings":         true,
	"libtorrent_ramdisk_min_free_bytes": true,
}

// uiSettingDefault returns the default value of a setting key (empty when the
// key has no registered default).
func uiSettingDefault(key string) string {
	return uiSettingDefaults[key]
}
