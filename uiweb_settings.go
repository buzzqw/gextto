package gextto

import "strings"

// uiweb_settings.go contains the curated search index and tabs used by the
// server-rendered settings page.

// uiSettingDef is one entry of the settings index. Group is the titled panel
// the setting belongs to inside its tab; it is explicit so a setting never ends
// up in the wrong panel because of how its key happens to be spelled.
type uiSettingDef struct{ Key, Label, Tab, Group string }

// uiSettingsTab is one section of the Configurazione page. Area groups the
// sections in the navigation; Intro is the one-line purpose shown under the
// section title.
type uiSettingsTab struct{ ID, Label, Area, Intro string }

// uiSettingsArea is a heading of the settings navigation.
type uiSettingsArea struct{ Name, Label string }

// uiScoreSettingDefs is the canonical score editor. Score keys are normally
// absent until edited, so deriving the UI from persisted settings would leave a
// fresh installation with no score controls at all.
var uiScoreSettingDefs = []uiSettingDef{
	{Key: "score_res_2160p", Label: "2160p", Tab: "scores"}, {Key: "score_res_1080p", Label: "1080p", Tab: "scores"},
	{Key: "score_res_720p", Label: "720p", Tab: "scores"}, {Key: "score_res_576p", Label: "576p", Tab: "scores"},
	{Key: "score_res_480p", Label: "480p", Tab: "scores"}, {Key: "score_res_360p", Label: "360p", Tab: "scores"},
	{Key: "score_source_bluray", Label: "BluRay", Tab: "scores"}, {Key: "score_source_remux", Label: "Remux", Tab: "scores"},
	{Key: "score_source_webdl", Label: "WEB-DL", Tab: "scores"}, {Key: "score_source_webrip", Label: "WEBRip", Tab: "scores"},
	{Key: "score_source_hdtv", Label: "HDTV", Tab: "scores"}, {Key: "score_source_dvdrip", Label: "DVDRip", Tab: "scores"},
	{Key: "score_codec_h265", Label: "H.265 / HEVC", Tab: "scores"}, {Key: "score_codec_h264", Label: "H.264 / AVC", Tab: "scores"},
	{Key: "score_audio_truehd", Label: "Dolby TrueHD", Tab: "scores"}, {Key: "score_audio_dts-hd", Label: "DTS-HD", Tab: "scores"},
	{Key: "score_audio_dts", Label: "DTS", Tab: "scores"}, {Key: "score_audio_ddp", Label: "Dolby Digital Plus", Tab: "scores"},
	{Key: "score_audio_ac3", Label: "AC3", Tab: "scores"}, {Key: "score_audio_5.1", Label: "5.1", Tab: "scores"},
	{Key: "score_audio_aac", Label: "AAC", Tab: "scores"}, {Key: "score_audio_mp3", Label: "MP3", Tab: "scores"},
	{Key: "score_bonus_dv", Label: "Dolby Vision", Tab: "scores"}, {Key: "score_bonus_hdr", Label: "HDR", Tab: "scores"},
	{Key: "score_bonus_proper", Label: "PROPER", Tab: "scores"}, {Key: "score_bonus_repack", Label: "REPACK", Tab: "scores"},
	{Key: "score_bonus_real", Label: "REAL", Tab: "scores"},
}

var uiScoreSettingKeys = func() map[string]bool {
	keys := make(map[string]bool, len(uiScoreSettingDefs))
	for _, def := range uiScoreSettingDefs {
		keys[def.Key] = true
	}
	return keys
}()

// Score keys that are deliberately not editable in the Punteggi tab.
//
// The aliases (x265/hevc/x264/avc/eac3) are folded into one canonical key per
// parsed token. The *_unknown/_mult keys are legacy or orphan values the scorer
// never reads: showing them would offer a control that does nothing.
var uiDeprecatedScoreSettingKeys = map[string]bool{
	"score_codec_x265": true, "score_codec_hevc": true, "score_codec_x264": true,
	"score_codec_avc": true, "score_audio_eac3": true,
	"score_res_mult": true, "score_res_unknown": true,
	"score_codec_unknown": true, "score_audio_unknown": true,
	"score_source_unknown": true,
}

// uiSettingsAreas orders the navigation by the question each section answers:
// what to look for, how to download it, where to store it, and the system.
var uiSettingsAreas = []uiSettingsArea{
	{Name: "search", Label: "Cosa cercare"},
	{Name: "download", Label: "Come scaricare"},
	{Name: "storage", Label: "Dove salvare"},
	{Name: "system", Label: "Sistema"},
}

var uiSettingsTabs = []uiSettingsTab{
	{ID: "daemon", Label: "Generale", Area: "search", Intro: "Quando e quanto spesso Gextto cerca nuove release e gli episodi mancanti."},
	{ID: "sources", Label: "Sorgenti", Area: "search", Intro: "Da dove arrivano le release: feed RSS, motori web, filtri e cartelle osservate."},
	{ID: "scores", Label: "Qualità e upgrade", Area: "search", Intro: "Come vengono confrontate le release e quando una versione migliore sostituisce quella in libreria."},
	{ID: "backend", Label: "Motore torrent", Area: "download", Intro: "Quale motore scarica i torrent e come Gextto lo raggiunge."},
	{ID: "network", Label: "Velocità e rete", Area: "download", Intro: "Limiti di banda, fasce orarie, porte e protocolli della sessione torrent."},
	{ID: "performance", Label: "Code e prestazioni", Area: "download", Intro: "Quanti torrent restano attivi, connessioni, RAM disk e cache."},
	{ID: "seeding", Label: "Seed e completamento", Area: "download", Intro: "Quanto restare in seed, cosa fare a download finito e come gestire i torrent bloccati."},
	{ID: "rename", Label: "Libreria e rinomina", Area: "storage", Intro: "Come vengono nominati e organizzati i file in libreria."},
	{ID: "paths", Label: "Archivio e spazio", Area: "storage", Intro: "Cartelle di lavoro, cestino e spazio minimo su disco."},
	{ID: "maintenance", Label: "Manutenzione automatica", Area: "system", Intro: "Pulizie periodiche dei dati tecnici, analisi MediaInfo e file orfani."},
	{ID: "notify", Label: "Notifiche", Area: "system", Intro: "Telegram, email, webhook e programmi da eseguire sugli eventi."},
	{ID: "access", Label: "Accesso e servizi", Area: "system", Intro: "Login, chiavi API e credenziali dei servizi esterni."},
	{ID: "system", Label: "Diagnostica e traduzioni", Area: "system", Intro: "Log dettagliati e testi dell'interfaccia."},
}

// uiSettingsTabAliases keeps links and bookmarks to the sections of the
// previous layout working after their content moved.
var uiSettingsTabAliases = map[string]string{
	"acquisition": "maintenance",
	"advanced":    "system",
	"i18n":        "system",
}

// uiSettingsCanonicalTab resolves a requested tab through the aliases and
// returns the server-owned tab ID, or "" when the tab is unknown.
func uiSettingsCanonicalTab(tab string) string {
	tab = strings.TrimSpace(tab)
	if alias, ok := uiSettingsTabAliases[tab]; ok {
		tab = alias
	}
	for _, known := range uiSettingsTabs {
		if known.ID == tab {
			return known.ID
		}
	}
	return ""
}

// The order of the entries is the order of the panels and of the rows on the
// page: the first setting of a group places the whole group.
var uiSettingsIndex = []uiSettingDef{
	// --- Generale -----------------------------------------------------------
	{Key: "active", Label: "Ricerca e download automatici", Tab: "daemon", Group: "Ciclo di ricerca"},
	{Key: "refresh_interval", Label: "Intervallo della ricerca automatica", Tab: "daemon", Group: "Ciclo di ricerca"},
	{Key: "cycle_title_search", Label: "Ricerca titoli online nel ciclo", Tab: "daemon", Group: "Ciclo di ricerca"},
	{Key: "max_release_age_days", Label: "Età massima delle release", Tab: "daemon", Group: "Ciclo di ricerca"},
	{Key: "gap_filling", Label: "Cerca gli episodi mancanti", Tab: "daemon", Group: "Episodi mancanti"},
	{Key: "gap_fill_max_per_series", Label: "Episodi mancanti per serie a ogni ciclo", Tab: "daemon", Group: "Episodi mancanti"},
	{Key: "gap_deep_interval_hours", Label: "Intervallo della ricerca approfondita", Tab: "daemon", Group: "Episodi mancanti"},
	{Key: "gap_deep_max_per_cycle", Label: "Ricerche approfondite per ciclo", Tab: "daemon", Group: "Episodi mancanti"},
	{Key: "delay_torrent_minutes", Label: "Ritardo per le serie", Tab: "daemon", Group: "Ritardo prima di scaricare"},
	{Key: "delay_movies_minutes", Label: "Ritardo per i film", Tab: "daemon", Group: "Ritardo prima di scaricare"},
	{Key: "delay_bypass_score", Label: "Ignora il ritardo da questo punteggio", Tab: "daemon", Group: "Ritardo prima di scaricare"},

	// --- Sorgenti -----------------------------------------------------------
	{Key: "blacklist", Label: "Blacklist (una parola per riga)", Tab: "sources", Group: "Feed e blacklist"},
	{Key: "stop_on_old_page_threshold", Label: "Pagine feed da leggere", Tab: "sources", Group: "Feed e blacklist"},

	// --- Qualità e upgrade (the score weights come from uiScoreSettingDefs) --
	{Key: "upgrade_min_score_diff", Label: "Differenza minima score per upgrade", Tab: "scores", Group: "Upgrade e sostituzione"},
	{Key: "upgrade_until_score", Label: "Smetti di migliorare oltre questo punteggio", Tab: "scores", Group: "Upgrade e sostituzione"},
	{Key: "cleanup_upgrades", Label: "Sostituisci le versioni già archiviate", Tab: "scores", Group: "Upgrade e sostituzione"},
	{Key: "cleanup_min_score_diff", Label: "Differenza minima score per cleanup", Tab: "scores", Group: "Upgrade e sostituzione"},
	{Key: "stall_alternative_after_min", Label: "Prova un'alternativa dopo (stallo)", Tab: "scores", Group: "Download bloccati"},
	{Key: "stall_alternative_score_drop", Label: "Punteggio in meno per l'alternativa", Tab: "scores", Group: "Download bloccati"},

	// --- Motore torrent: one panel per engine -------------------------------
	{Key: "torrent_backend", Label: "Motore torrent", Tab: "backend", Group: "Motore torrent"},
	{Key: "gxtorrent_url", Label: "gx-torrent — URL Web API", Tab: "backend", Group: "gx-torrent"},
	{Key: "gxtorrent_listen", Label: "gx-torrent — indirizzo di ascolto (LAN)", Tab: "backend", Group: "gx-torrent"},
	{Key: "gxtorrent_auto", Label: "gx-torrent — gestione automatica (cache e coda)", Tab: "backend", Group: "gx-torrent"},
	{Key: "gxtorrent_proxy", Label: "gx-torrent — proxy (socks5:// o http://)", Tab: "backend", Group: "gx-torrent"},
	{Key: "gxtorrent_request_timeout_secs", Label: "gx-torrent — timeout richieste", Tab: "backend", Group: "gx-torrent"},
	{Key: "gxtorrent_poll_interval_ms", Label: "gx-torrent — intervallo polling", Tab: "backend", Group: "gx-torrent"},
	{Key: "gxtorrent_dlna", Label: "gx-torrent — server DLNA/UPnP", Tab: "backend", Group: "gx-torrent"},
	{Key: "gxtorrent_dlna_listen", Label: "gx-torrent — DLNA: indirizzo HTTP", Tab: "backend", Group: "gx-torrent"},
	{Key: "gxtorrent_dlna_name", Label: "gx-torrent — DLNA: nome del server", Tab: "backend", Group: "gx-torrent"},
	{Key: "gxtorrent_dlna_folders", Label: "gx-torrent — DLNA: cartelle della libreria", Tab: "backend", Group: "gx-torrent"},
	{Key: "qbittorrent_managed", Label: "qBittorrent-nox — scaricato e aggiornato da Gextto", Tab: "backend", Group: "qBittorrent-nox"},
	{Key: "qbittorrent_url", Label: "qBittorrent-nox — URL Web API", Tab: "backend", Group: "qBittorrent-nox"},
	{Key: "qbittorrent_username", Label: "qBittorrent-nox — utente", Tab: "backend", Group: "qBittorrent-nox"},
	{Key: "qbittorrent_password", Label: "qBittorrent-nox — password", Tab: "backend", Group: "qBittorrent-nox"},
	{Key: "qbittorrent_category", Label: "qBittorrent-nox — categoria", Tab: "backend", Group: "qBittorrent-nox"},
	{Key: "qbittorrent_tag", Label: "qBittorrent-nox — tag", Tab: "backend", Group: "qBittorrent-nox"},
	{Key: "qbittorrent_request_timeout_secs", Label: "qBittorrent-nox — timeout richieste", Tab: "backend", Group: "qBittorrent-nox"},
	{Key: "qbittorrent_poll_interval_ms", Label: "qBittorrent-nox — intervallo polling", Tab: "backend", Group: "qBittorrent-nox"},
	{Key: "qbittorrent_path_mappings", Label: "qBittorrent-nox — mappatura percorsi", Tab: "backend", Group: "qBittorrent-nox"},

	// --- Velocità e rete ----------------------------------------------------
	{Key: "torrent_dl_limit", Label: "Limite download globale", Tab: "network", Group: "Limiti di velocità"},
	{Key: "torrent_ul_limit", Label: "Limite upload globale", Tab: "network", Group: "Limiti di velocità"},
	{Key: "torrent_sched_enabled", Label: "Programmazione velocità attiva", Tab: "network", Group: "Programmazione velocità"},
	{Key: "torrent_sched_start", Label: "Programmazione — ora inizio", Tab: "network", Group: "Programmazione velocità"},
	{Key: "torrent_sched_end", Label: "Programmazione — ora fine", Tab: "network", Group: "Programmazione velocità"},
	{Key: "torrent_sched_days", Label: "Programmazione — giorni", Tab: "network", Group: "Programmazione velocità"},
	{Key: "torrent_sched_dl_limit", Label: "Programmazione — download", Tab: "network", Group: "Programmazione velocità"},
	{Key: "torrent_sched_ul_limit", Label: "Programmazione — upload", Tab: "network", Group: "Programmazione velocità"},
	{Key: "torrent_sched_week", Label: "Programmazione — per giorno", Tab: "network", Group: "Programmazione velocità"},
	{Key: "torrent_port_min", Label: "Porta minima", Tab: "network", Group: "Porte e interfacce"},
	{Key: "torrent_port_max", Label: "Porta massima", Tab: "network", Group: "Porte e interfacce"},
	{Key: "torrent_listen_interfaces", Label: "Interfacce listen", Tab: "network", Group: "Porte e interfacce"},
	{Key: "torrent_outgoing_interface", Label: "Interfaccia uscente", Tab: "network", Group: "Porte e interfacce"},
	{Key: "torrent_dht", Label: "DHT", Tab: "network", Group: "Ricerca peer e tracker"},
	{Key: "torrent_pex", Label: "PEX", Tab: "network", Group: "Ricerca peer e tracker"},
	{Key: "torrent_lsd", Label: "LSD", Tab: "network", Group: "Ricerca peer e tracker"},
	{Key: "torrent_upnp", Label: "UPnP", Tab: "network", Group: "Ricerca peer e tracker"},
	{Key: "torrent_natpmp", Label: "NAT-PMP", Tab: "network", Group: "Ricerca peer e tracker"},
	{Key: "torrent_utp", Label: "uTP", Tab: "network", Group: "Ricerca peer e tracker"},
	{Key: "torrent_holepunch", Label: "Holepunching (BEP 55)", Tab: "network", Group: "Ricerca peer e tracker"},
	{Key: "torrent_dht_bootstrap_nodes", Label: "Nodi bootstrap DHT", Tab: "network", Group: "Ricerca peer e tracker"},
	{Key: "torrent_encryption", Label: "Cifratura", Tab: "network", Group: "Cifratura e filtro IP"},
	{Key: "torrent_apply_ip_filter", Label: "Applica IP filter", Tab: "network", Group: "Cifratura e filtro IP"},
	{Key: "torrent_ipfilter_url", Label: "IP filter (file/URL)", Tab: "network", Group: "Cifratura e filtro IP"},

	// --- Code e prestazioni -------------------------------------------------
	{Key: "torrent_dynamic_queue", Label: "Auto-gestione dinamica coda e risorse", Tab: "performance", Group: "Coda e slot"},
	{Key: "torrent_dynamic_queue_min", Label: "Slot download dinamici minimi", Tab: "performance", Group: "Coda e slot"},
	{Key: "torrent_dynamic_queue_max", Label: "Slot download dinamici massimi", Tab: "performance", Group: "Coda e slot"},
	{Key: "torrent_dont_count_slow_torrents", Label: "Non contare i torrent fermi negli slot attivi", Tab: "performance", Group: "Coda e slot"},
	{Key: "torrent_active_downloads", Label: "Download attivi", Tab: "performance", Group: "Coda e slot"},
	{Key: "torrent_active_seeds", Label: "Seed attivi", Tab: "performance", Group: "Coda e slot"},
	{Key: "torrent_active_limit", Label: "Limite torrent attivi", Tab: "performance", Group: "Coda e slot"},
	{Key: "torrent_sequential", Label: "Download sequenziale", Tab: "performance", Group: "Modalità di download"},
	{Key: "torrent_first_last", Label: "Prima/ultima parte dei file", Tab: "performance", Group: "Modalità di download"},
	{Key: "torrent_preallocate", Label: "Prealloca lo spazio su disco", Tab: "performance", Group: "Modalità di download"},
	{Key: "torrent_max_connections_per_torrent", Label: "Connessioni max per torrent", Tab: "performance", Group: "Connessioni"},
	{Key: "torrent_ramdisk_enabled", Label: "Usa il RAM disk", Tab: "performance", Group: "RAM disk"},
	{Key: "torrent_ramdisk_dir", Label: "Cartella RAM disk", Tab: "performance", Group: "RAM disk"},
	{Key: "torrent_ramdisk_threshold_gb", Label: "Dimensione massima per torrent", Tab: "performance", Group: "RAM disk"},
	{Key: "torrent_ramdisk_margin_gb", Label: "Margine libero da mantenere", Tab: "performance", Group: "RAM disk"},
	{Key: "torrent_ramdisk_min_free_bytes", Label: "Spazio minimo libero", Tab: "performance", Group: "RAM disk"},
	{Key: "torrent_cache_size", Label: "Cache disco", Tab: "performance", Group: "Per esperti"},
	{Key: "torrent_cache_expiry", Label: "Scadenza cache", Tab: "performance", Group: "Per esperti"},

	// --- Seed e completamento -----------------------------------------------
	{Key: "torrent_seed_ratio", Label: "Seed ratio globale", Tab: "seeding", Group: "Seed"},
	{Key: "torrent_seed_time_days", Label: "Seed massimo", Tab: "seeding", Group: "Seed"},
	{Key: "torrent_seed_time", Label: "Seed massimo (fallback)", Tab: "seeding", Group: "Seed"},
	{Key: "auto_remove_completed", Label: "Elimina i completati dopo il seed", Tab: "seeding", Group: "A download completato"},
	{Key: "move_episodes", Label: "Sposta gli episodi/pack in archivio (non copiare)", Tab: "seeding", Group: "A download completato"},
	{Key: "hardlink_seeding", Label: "Hardlink invece della copia durante il seed", Tab: "seeding", Group: "A download completato"},
	{Key: "torrent_stall_after_min", Label: "Considera bloccato dopo", Tab: "seeding", Group: "Torrent bloccati"},
	{Key: "torrent_stall_retry_min", Label: "Riprova i torrent bloccati ogni", Tab: "seeding", Group: "Torrent bloccati"},
	{Key: "torrent_stall_giveup_min", Label: "Rimuovi i torrent bloccati dopo", Tab: "seeding", Group: "Torrent bloccati"},
	{Key: "torrent_dead_swarm_giveup_min", Label: "Rimuovi i torrent senza seeder dopo", Tab: "seeding", Group: "Torrent bloccati"},

	// --- Libreria e rinomina ------------------------------------------------
	{Key: "rename_episodes", Label: "Rinomina episodi", Tab: "rename", Group: "Rinomina"},
	{Key: "movies_flat_files", Label: "Film come file singoli (spiana le cartelle)", Tab: "rename", Group: "Rinomina"},
	{Key: "rename_verify_interval", Label: "Verifica dei file rinominati ogni", Tab: "rename", Group: "Rinomina"},
	{Key: "default_language", Label: "Lingua predefinita", Tab: "rename", Group: "Lingue dei metadati"},
	{Key: "tvdb_language", Label: "Lingua TVDB", Tab: "rename", Group: "Lingue dei metadati"},
	{Key: "tmdb_language", Label: "Lingua TMDB", Tab: "rename", Group: "Lingue dei metadati"},

	// --- Archivio e spazio --------------------------------------------------
	{Key: "archive_root", Label: "Cartella archivio", Tab: "paths", Group: "Cartelle"},
	{Key: "torrent_dir", Label: "Cartella download", Tab: "paths", Group: "Cartelle"},
	{Key: "torrent_temp_dir", Label: "Cartella temporanea", Tab: "paths", Group: "Cartelle"},
	{Key: "torrent_copy_dir", Label: "Copia i file .torrent in", Tab: "paths", Group: "Cartelle"},
	{Key: "trash_path", Label: "Cartella cestino", Tab: "paths", Group: "Cestino"},
	{Key: "cleanup_action", Label: "Cosa fare con i file sostituiti", Tab: "paths", Group: "Cestino"},
	{Key: "trash_retention_days", Label: "Conservazione nel cestino", Tab: "paths", Group: "Cestino"},
	{Key: "min_free_space_gb", Label: "Spazio libero minimo per scaricare", Tab: "paths", Group: "Spazio su disco"},

	// --- Manutenzione automatica --------------------------------------------
	{Key: "housekeeping_enabled", Label: "Housekeeping periodico attivo", Tab: "maintenance", Group: "Housekeeping"},
	{Key: "housekeeping_interval_hours", Label: "Housekeeping — intervallo", Tab: "maintenance", Group: "Housekeeping"},
	{Key: "housekeeping_retain_cycles", Label: "Housekeeping — statistiche cicli di ricerca conservate", Tab: "maintenance", Group: "Housekeeping"},
	{Key: "housekeeping_seen_days", Label: "Housekeeping — visti nel feed", Tab: "maintenance", Group: "Housekeeping"},
	{Key: "housekeeping_history_days", Label: "Housekeeping — storico download", Tab: "maintenance", Group: "Housekeeping"},
	{Key: "housekeeping_error_age_days", Label: "Housekeeping — schede errore", Tab: "maintenance", Group: "Housekeeping"},
	{Key: "housekeeping_gap_log_days", Label: "Housekeeping — log ricerche gap", Tab: "maintenance", Group: "Housekeeping"},
	{Key: "housekeeping_upgrade_backup_days", Label: "Housekeeping — backup upgrade", Tab: "maintenance", Group: "Housekeeping"},
	{Key: "archive_cleanup_enabled", Label: "Pulizia automatica archivio", Tab: "maintenance", Group: "Pulizia archivio release"},
	{Key: "archive_max_age_days", Label: "Archivio — età massima", Tab: "maintenance", Group: "Pulizia archivio release"},
	{Key: "archive_keep_min", Label: "Archivio — mantieni almeno N voci", Tab: "maintenance", Group: "Pulizia archivio release"},
	{Key: "media_info_backfill_enabled", Label: "Backfill MediaInfo automatico", Tab: "maintenance", Group: "Analisi MediaInfo"},
	{Key: "media_info_backfill_interval_minutes", Label: "Backfill MediaInfo — intervallo", Tab: "maintenance", Group: "Analisi MediaInfo"},
	{Key: "media_info_backfill_batch", Label: "Backfill MediaInfo — file per volta", Tab: "maintenance", Group: "Analisi MediaInfo"},
	{Key: "temp_orphan_cleanup_enabled", Label: "Sposta nel cestino i dati orfani della cartella temporanea", Tab: "maintenance", Group: "File orfani"},
	{Key: "temp_orphan_min_age_days", Label: "Dati orfani — fermi da almeno", Tab: "maintenance", Group: "File orfani"},

	// --- Notifiche ----------------------------------------------------------
	{Key: "notify_telegram", Label: "Telegram attivo", Tab: "notify", Group: "Telegram"},
	{Key: "telegram_bot_token", Label: "Telegram bot token", Tab: "notify", Group: "Telegram"},
	{Key: "telegram_chat_id", Label: "Telegram chat ID", Tab: "notify", Group: "Telegram"},
	{Key: "notify_email", Label: "Email attiva", Tab: "notify", Group: "Email"},
	{Key: "email_smtp", Label: "SMTP", Tab: "notify", Group: "Email"},
	{Key: "email_from", Label: "Email mittente", Tab: "notify", Group: "Email"},
	{Key: "email_to", Label: "Email destinatario", Tab: "notify", Group: "Email"},
	{Key: "email_password", Label: "Password email", Tab: "notify", Group: "Email"},
	{Key: "notify_webhook_url", Label: "Webhook URL", Tab: "notify", Group: "Webhook"},
	{Key: "notify_webhook_secret", Label: "Webhook secret", Tab: "notify", Group: "Webhook"},
	{Key: "notify_webhook_format", Label: "Formato webhook", Tab: "notify", Group: "Webhook"},
	{Key: "notify_webhook_token", Label: "Webhook token", Tab: "notify", Group: "Webhook"},
	{Key: "notify_webhook_user", Label: "Webhook user (Pushover)", Tab: "notify", Group: "Webhook"},

	// --- Accesso e servizi --------------------------------------------------
	{Key: "auth_enabled", Label: "Richiedi l'accesso (login)", Tab: "access", Group: "Login"},
	{Key: "auth_local_bypass", Label: "Nessun login dalla rete locale", Tab: "access", Group: "Login"},
	{Key: "auth_username", Label: "Utente", Tab: "access", Group: "Login"},
	{Key: "auth_password", Label: "Password", Tab: "access", Group: "Login"},
	{Key: "auth_api_key", Label: "Chiave API (script, TUI, calendario)", Tab: "access", Group: "Chiavi API"},
	{Key: "gxtorrent_token", Label: "gx-torrent — token di accesso (pagina e API in LAN)", Tab: "access", Group: "Chiavi API"},
	{Key: "tmdb_api_key", Label: "TMDB API key (consigliata)", Tab: "access", Group: "Servizi metadati"},
	{Key: "tvdb_api_key", Label: "TVDB API key (alternativa a TMDB)", Tab: "access", Group: "Servizi metadati"},
	{Key: "tvdb_pin", Label: "TVDB PIN abbonato", Tab: "access", Group: "Servizi metadati"},

	// --- Diagnostica e traduzioni -------------------------------------------
	{Key: "debug_enabled", Label: "Debug (log dettagliati)", Tab: "system", Group: "Diagnostica"},
	{Key: "update_check", Label: "Controlla gli aggiornamenti", Tab: "system", Group: "Aggiornamenti"},
	{Key: "update_check_hours", Label: "Intervallo controllo aggiornamenti", Tab: "system", Group: "Aggiornamenti"},
}

// uiSettingIndexed holds every key the settings page shows (index and score
// weights). gh7_setting_key_allowed accepts all of them, so a field on the page
// is always savable.
var uiSettingIndexed = func() map[string]bool {
	keys := make(map[string]bool, len(uiSettingsIndex)+len(uiScoreSettingDefs))
	for _, def := range uiSettingsIndex {
		keys[def.Key] = true
	}
	for key := range uiScoreSettingKeys {
		keys[key] = true
	}
	return keys
}()

// uiSettingGroupByKey maps an indexed setting to its explicit panel.
var uiSettingGroupByKey = func() map[string]string {
	groups := make(map[string]string, len(uiSettingsIndex))
	for _, def := range uiSettingsIndex {
		groups[def.Key] = def.Group
	}
	return groups
}()

// uiSettingSearchTerms contains common concepts that are useful when searching
// for a setting but are intentionally not repeated in its visible label.
var uiSettingSearchTerms = map[string]string{
	"torrent_dynamic_queue":               "memoria memo memory ram coda queue risorse resources",
	"torrent_dynamic_queue_min":           "memoria memo memory ram coda queue risorse resources",
	"torrent_dynamic_queue_max":           "memoria memo memory ram coda queue risorse resources",
	"torrent_dont_count_slow_torrents":    "memoria memo memory ram coda queue risorse resources",
	"torrent_active_downloads":            "memoria memo memory ram coda queue risorse resources",
	"torrent_active_seeds":                "memoria memo memory ram coda queue risorse resources",
	"torrent_active_limit":                "memoria memo memory ram coda queue risorse resources",
	"torrent_cache_size":                  "memoria memo memory ram cache buffer disco disk",
	"torrent_cache_expiry":                "memoria memo memory ram cache buffer disco disk",
	"torrent_ramdisk_enabled":             "memoria memo memory ram ramdisk tmpfs",
	"torrent_ramdisk_threshold_gb":        "memoria memo memory ram ramdisk tmpfs spazio space",
	"torrent_ramdisk_margin_gb":           "memoria memo memory ram ramdisk tmpfs spazio space",
	"torrent_ramdisk_min_free_bytes":      "memoria memo memory ram ramdisk tmpfs spazio space",
	"torrent_preallocate":                 "memoria memo memory ram spazio space disco disk",
	"torrent_max_connections_per_torrent": "memoria memo memory ram risorse resources connessioni connections",
	"min_free_space_gb":                   "memoria memo memory ram spazio space disco disk",
	// Words of the labels used before the reorganization.
	"active":                        "attivo daemon",
	"delay_torrent_minutes":         "delay",
	"delay_movies_minutes":          "delay",
	"delay_bypass_score":            "delay bypassa",
	"gap_filling":                   "gap filling",
	"gap_deep_interval_hours":       "deep search",
	"gap_deep_max_per_cycle":        "deep search",
	"torrent_stall_after_min":       "stalled stallo",
	"torrent_stall_retry_min":       "stalled stallo retry",
	"torrent_stall_giveup_min":      "stalled stallo rimozione",
	"torrent_dead_swarm_giveup_min": "dead swarm stallo rimozione",
	"trash_path":                    "trash",
	"trash_retention_days":          "trash",
	"cleanup_action":                "cleanup azione trash",
	"cleanup_upgrades":              "cleanup upgrade",
	"torrent_dl_limit":              "velocità download",
	"torrent_ul_limit":              "velocità upload",
}
