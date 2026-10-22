package gextto

import "github.com/buzzqw/gextto/internal/constants"

// uiweb_settings_defaults.go carries the default value of every setting the
// Configurazione page exposes, ported from rextto's `setting_tooltip` era and
// from Gextto's own config defaults (config.go, DefaultTransferSettings and
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
	"active":                  "true",
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
	// eD2k/Kad is off by default: it is an opt-in search provider and the
	// automatic fallback is a second, explicit switch.
	"ed2k_enabled":             "false",
	"amule_host":               "127.0.0.1",
	"amule_port":               "4712",
	"amule_password":           "",
	"ed2k_search_network":      "kad",
	"ed2k_fallback":            "false",
	"ed2k_min_sources":         "3",
	"ed2k_search_timeout_secs": "20",
	"ed2k_stall_days":          "7",

	// --- Transfer -----------------------------------------------------------
	"torrent_dynamic_queue":               "false",
	"torrent_preallocate":                 "true",
	"torrent_dynamic_queue_min":           "1",
	"torrent_dynamic_queue_max":           "10",
	"torrent_dont_count_slow_torrents":    "true",
	"torrent_stall_after_min":             "60",
	"torrent_stall_retry_min":             "60",
	"torrent_stall_giveup_min":            "20160",
	"torrent_dead_swarm_giveup_min":       "4320",
	"torrent_sequential":                  "false",
	"torrent_first_last":                  "false",
	"torrent_active_downloads":            "3",
	"torrent_active_seeds":                "3",
	"torrent_active_limit":                "5",
	"torrent_seed_ratio":                  "0",
	"torrent_seed_time":                   "0",
	"torrent_seed_time_days":              "0",
	"torrent_seed_idle_minutes":           "0",
	"auto_remove_completed":               "false",
	"torrent_max_connections_per_torrent": "-1",
	"torrent_connections_auto":            "yes",
	"autotune_adaptive":                   "true",
	"torrent_cache_size":                  "-1",
	"torrent_cache_expiry":                "300",
	"torrent_dht":                         "true",
	"torrent_pex":                         "true",
	"torrent_lsd":                         "true",
	"torrent_upnp":                        "true",
	"torrent_natpmp":                      "true",
	"torrent_utp":                         "true",
	"torrent_holepunch":                   "true",
	"torrent_dht_bootstrap_nodes":         "",
	"torrent_encryption":                  "1",
	"torrent_apply_ip_filter":             "true",
	"torrent_ipfilter_url":                constants.DefaultIPFilterURL,
	"torrent_listen_interfaces":           "0.0.0.0:6881-6891",
	"torrent_outgoing_interface":          "",
	"torrent_ramdisk_dir":                 "",
	"torrent_ramdisk_enabled":             "false",
	"torrent_ramdisk_threshold_gb":        "3.5",
	"torrent_ramdisk_margin_gb":           "0.5",
	"torrent_ramdisk_min_free_bytes":      "0",
	"torrent_port_min":                    "6881",
	"torrent_port_max":                    "6891",
	"torrent_dl_limit":                    "0",
	"torrent_ul_limit":                    "0",
	"torrent_speed_auto":                  "false",
	"torrent_speed_mode":                  "fixed",
	"torrent_auto_latency_guard":          "true",
	"torrent_auto_latency_source":         "both",
	"torrent_auto_latency_target":         "",
	"torrent_line_max_down_kib":           "0",
	"torrent_line_max_up_kib":             "0",
	"torrent_sched_enabled":               "false",
	"torrent_sched_start":                 "23:00",
	"torrent_sched_end":                   "08:00",
	"torrent_sched_days":                  "",
	"torrent_sched_dl_limit":              "0",
	"torrent_sched_ul_limit":              "0",
	"torrent_sched_week":                  "",

	// --- Motore torrent (backend) ------------------------------------------
	// gx-torrent è il default di installazione; qBittorrent-nox resta
	// selezionabile e fa da fallback automatico.
	"torrent_backend":                   "gx-torrent",
	"qbittorrent_url":                   "http://127.0.0.1:8080",
	"gxtorrent_url":                     "http://127.0.0.1:8890",
	"gxtorrent_listen":                  "127.0.0.1:8890",
	"gxtorrent_token":                   "",
	"gxtorrent_proxy":                   "",
	"gxtorrent_client_identity":         "gx-core",
	"gxtorrent_client_identity_version": "",
	"gxtorrent_request_timeout_secs":    "15",
	"gxtorrent_poll_interval_ms":        "1500",
	"gxtorrent_auto":                    "true",
	"gxtorrent_dlna":                    "false",
	"gxtorrent_dlna_listen":             "",
	"gxtorrent_dlna_name":               "",
	"gxtorrent_dlna_folders":            "",
	"qbittorrent_username":              "admin",
	"qbittorrent_request_timeout_secs":  "15",
	"qbittorrent_poll_interval_ms":      "1500",
	"qbittorrent_managed":               "false",

	// --- Rinomina -----------------------------------------------------------
	"rename_episodes":        "false",
	"tvdb_language":          "ita",
	"tmdb_language":          "it-IT",
	"default_language":       "ita",
	"quality_profiles":       "[]",
	"cleanup_upgrades":       "true",
	"movies_flat_files":      "yes",
	"upgrade_min_score_diff": "200",
	"upgrade_until_score":    "0",
	// A stuck download keeps retrying, but after this long without progress the
	// search may start an alternative that scores up to this many points less.
	"stall_alternative_after_min":  "360",
	"stall_alternative_score_drop": "100",

	// --- Avanzate -----------------------------------------------------------
	"min_free_space_gb":          "0",
	"trash_retention_days":       "0",
	"archive_cleanup_enabled":    "false",
	"archive_max_age_days":       "0",
	"archive_keep_min":           "0",
	"stop_on_old_page_threshold": "3",
	"rename_verify_interval":     "6",
	"move_episodes":              "false",
	"auth_enabled":               "false",
	"auth_local_bypass":          "true",
	"auth_username":              "admin",
	"hardlink_seeding":           "true",
	"pack_inspection":            "false",
	"pack_inspection_prioritize": "false",
	"debug_enabled":              "false",
	"update_check":               "true",
	"update_check_hours":         "6",

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
	"temp_orphan_cleanup_enabled":          "true",
	"temp_orphan_min_age_days":             "7",

	// --- Notifiche ----------------------------------------------------------
	"notify_telegram":       "false",
	"notify_email":          "false",
	"email_smtp":            "smtp.gmail.com:587",
	"notify_webhook_format": "gextto",

	// --- Percorsi -----------------------------------------------------------
	"cleanup_action": "move",
}

// uiSettingNoPrefill lists keys whose default must stay a placeholder: their
// value is a list/JSON blob, and pre-filling it in the textarea would make an
// accidental save overwrite the stored structure.
var uiSettingNoPrefill = map[string]bool{
	"blacklist":                      true,
	"content_filters":                true,
	"websearch_engines":              true,
	"torrent_dht_bootstrap_nodes":    true,
	"torrent_sched_days":             true,
	"torrent_ramdisk_min_free_bytes": true,
}

// uiSettingDefault returns the default value of a setting key (empty when the
// key has no registered default).
func uiSettingDefault(key string) string {
	return uiSettingDefaults[key]
}
