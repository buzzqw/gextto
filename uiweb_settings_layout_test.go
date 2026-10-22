package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// uiSettingsKeysBeforeReorganization is the settings index as it was before
// the Configurazione page was reorganized into areas (146 options, plus the
// 27 score weights). Every one of them must still be on the page.
var uiSettingsKeysBeforeReorganization = []string{
	"refresh_interval", "max_release_age_days", "gap_fill_max_per_series", "gap_filling", "gap_deep_interval_hours", "gap_deep_max_per_cycle",
	"cycle_title_search", "active", "blacklist", "torrent_dynamic_queue", "torrent_preallocate", "torrent_dynamic_queue_min",
	"torrent_dynamic_queue_max", "torrent_dont_count_slow_torrents", "torrent_stall_after_min", "torrent_stall_retry_min",
	"torrent_stall_giveup_min", "torrent_dead_swarm_giveup_min", "torrent_sequential", "torrent_active_downloads", "torrent_active_seeds",
	"torrent_active_limit", "torrent_seed_ratio", "torrent_seed_time", "torrent_seed_time_days", "auto_remove_completed",
	"torrent_max_connections_per_torrent", "torrent_cache_size", "torrent_cache_expiry", "torrent_dht", "torrent_pex", "torrent_lsd",
	"torrent_upnp", "torrent_natpmp", "torrent_utp", "torrent_dht_bootstrap_nodes", "torrent_encryption", "torrent_apply_ip_filter",
	"torrent_ipfilter_url", "torrent_listen_interfaces", "torrent_outgoing_interface", "torrent_ramdisk_dir", "torrent_ramdisk_enabled",
	"torrent_ramdisk_threshold_gb", "torrent_ramdisk_margin_gb", "torrent_ramdisk_min_free_bytes", "torrent_port_min", "torrent_port_max",
	"torrent_dl_limit", "torrent_ul_limit", "torrent_sched_enabled", "torrent_sched_start", "torrent_sched_end", "torrent_sched_days",
	"torrent_sched_dl_limit", "torrent_sched_ul_limit", "torrent_backend", "qbittorrent_url", "qbittorrent_username", "qbittorrent_password",
	"qbittorrent_category", "qbittorrent_tag", "qbittorrent_request_timeout_secs", "qbittorrent_poll_interval_ms",
	"qbittorrent_path_mappings", "qbittorrent_managed", "gxtorrent_url", "gxtorrent_listen", "gxtorrent_request_timeout_secs",
	"gxtorrent_poll_interval_ms", "gxtorrent_proxy", "gxtorrent_auto", "rename_episodes", "tvdb_language", "tmdb_language",
	"default_language", "cleanup_upgrades", "movies_flat_files", "upgrade_min_score_diff", "upgrade_until_score",
	"tmdb_api_key", "tvdb_api_key", "min_free_space_gb", "trash_retention_days", "archive_cleanup_enabled", "archive_max_age_days",
	"archive_keep_min", "stop_on_old_page_threshold", "rename_verify_interval", "auth_enabled", "auth_local_bypass", "auth_username",
	"auth_password", "auth_api_key", "gxtorrent_token", "move_episodes", "hardlink_seeding", "debug_enabled", "delay_torrent_minutes",
	"delay_movies_minutes", "delay_bypass_score", "housekeeping_enabled", "housekeeping_interval_hours", "housekeeping_retain_cycles",
	"housekeeping_seen_days", "housekeeping_history_days", "housekeeping_error_age_days", "housekeeping_gap_log_days",
	"housekeeping_upgrade_backup_days", "media_info_backfill_enabled", "media_info_backfill_interval_minutes", "media_info_backfill_batch",
	"temp_orphan_cleanup_enabled", "temp_orphan_min_age_days", "notify_telegram", "telegram_bot_token", "telegram_chat_id",
	"notify_webhook_url", "notify_webhook_secret", "notify_webhook_format", "notify_webhook_token", "notify_webhook_user", "notify_email",
	"email_smtp", "email_from", "email_to", "email_password", "archive_root", "trash_path", "cleanup_action", "torrent_dir",
	"torrent_temp_dir", "torrent_copy_dir",
}

// TestSettingsEveryPreviousOptionIsStillAvailable counts the options and
// checks that each one is rendered, editable, in the section the index
// assigns it to.
func TestSettingsEveryPreviousOptionIsStillAvailable(t *testing.T) {
	if got := len(uiSettingsKeysBeforeReorganization); got != 133 {
		t.Fatalf("frozen list has %d keys, want 133", got)
	}
	if got := len(uiSettingsIndex); got != 168 {
		t.Fatalf("settings index has %d options, want the 149 of before plus the 3 stall-alternative/update options, holepunch, the TVDB PIN, the first/last default, the 4 DLNA media server options, the update check interval, the per-day speed schedule, the adaptive tuning switch, the automatic speed policy and its latency guard (switch, source, target and line maxima), the idle-seed limit, the in-download pack inspection (abort and file priorities), the speed rule choice, the 9 eD2k/Kad (aMule) options, the two gx-torrent client-identity options and the automatic peer cap, minus cleanup_min_score_diff (folded into upgrade_min_score_diff)", got)
	}
	if got := len(uiScoreSettingDefs); got != 27 {
		t.Fatalf("score weights = %d, want 27", got)
	}
	tabOf := map[string]string{}
	for _, def := range uiSettingsIndex {
		tabOf[def.Key] = def.Tab
	}
	for _, key := range uiSettingsKeysBeforeReorganization {
		if tabOf[key] == "" {
			t.Errorf("option %q is no longer in the settings index", key)
		}
	}

	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	bodies := map[string]string{}
	render := func(tab string) string {
		if body, ok := bodies[tab]; ok {
			return body
		}
		code, body := v2Request(t, server, http.MethodGet, "/settings/body?tab="+tab, nil)
		if code != http.StatusOK {
			t.Fatalf("tab %s -> %d", tab, code)
		}
		bodies[tab] = body
		return body
	}
	found := 0
	for _, def := range append(append([]uiSettingDef{}, uiSettingsIndex...), uiScoreSettingDefs...) {
		body := render(def.Tab)
		if !strings.Contains(body, `id="v2-setting-`+def.Key+`"`) || !strings.Contains(body, `id="input-`+def.Key+`"`) {
			t.Errorf("option %q missing from tab %q", def.Key, def.Tab)
			continue
		}
		found++
	}
	if found != 195 {
		t.Fatalf("rendered %d options, want 195", found)
	}

	// Editors, panels and actions without a single key.
	for tab, markers := range map[string][]string{
		"sources":     {"Feed RSS", "Motori web", "Filtri contenuto esclusi", "Filtri per sorgente", "Cartelle osservate", "Pulisci i risultati già archiviati"},
		"scores":      {"Gruppi custom", "Aggiungi gruppo"},
		"backend":     {"Applica motore", "Installa / Ottimizza qBittorrent-nox", "Stato qBittorrent-nox", "Test connessione"},
		"network":     {"v2-ipfilter-panel", "Carica / aggiorna ora"},
		"performance": {"Ottimizza"},
		"rename":      {"Composizione del nome", "Regole tag → cartella"},
		"notify":      {"Hook eventi"},
		"seeding":     {"Regole di seed"},
		"system":      {"v2-i18n-table", "Esporta YAML", "Importa YAML"},
	} {
		body := render(tab)
		for _, marker := range markers {
			if !strings.Contains(body, marker) {
				t.Errorf("tab %s lost %q", tab, marker)
			}
		}
	}
}

func TestSettingsNavigationAreasAndAliases(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=settings&tab=notify", nil)
	if code != http.StatusOK {
		t.Fatalf("settings page -> %d", code)
	}
	for _, area := range uiSettingsAreas {
		if !strings.Contains(body, area.Label) {
			t.Errorf("navigation area %q missing", area.Label)
		}
	}
	for _, tab := range uiSettingsTabs {
		if !strings.Contains(body, `href="/?view=settings&amp;tab=`+tab.ID+`"`) {
			t.Errorf("navigation link to %q missing", tab.ID)
		}
	}
	nav := body[strings.Index(body, `class="settings-nav"`):strings.Index(body, `class="settings-main"`)]
	if strings.Count(nav, `aria-current="page"`) != 1 || !strings.Contains(body, `<h2 id="v2-settings-heading" tabindex="-1">Notifiche</h2>`) {
		t.Fatalf("active section must be marked once and titled: %s", nav)
	}

	for alias, want := range map[string]string{"acquisition": "maintenance", "advanced": "system", "i18n": "system", "bogus": "daemon"} {
		request, _ := http.NewRequest(http.MethodGet, server.URL+"/settings/body?tab="+alias, nil)
		request.Header.Set("HX-Request", "true")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if got := response.Header.Get("HX-Push-Url"); got != "/?view=settings&tab="+want {
			t.Errorf("tab %s pushes %q, want tab %s", alias, got, want)
		}
	}
	// The heading of the swapped body follows the section (it used to stay
	// on «Daemon»).
	if _, body := v2Request(t, server, http.MethodGet, "/settings/body?tab=seeding", nil); !strings.Contains(body, ">Seed e completamento</h2>") {
		t.Fatalf("swapped body must carry its own heading")
	}
}

func TestSettingsRowsControlsAndAccessibility(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	_, body := v2Request(t, server, http.MethodGet, "/settings/body?tab=network", nil)
	for _, marker := range []string{
		// Time pickers and one checkbox per weekday.
		`type="time" name="value" id="input-torrent_sched_start"`,
		`class="setting-days" role="group" aria-labelledby="label-torrent_sched_days"`,
		// Units and special values outside the label.
		`<span class="setting-unit" id="unit-torrent_dl_limit">KiB/s</span>`,
		`<small class="setting-zero" id="zero-torrent_dl_limit">0 = illimitato</small>`,
		`aria-describedby="hint-torrent_dl_limit unit-torrent_dl_limit zero-torrent_dl_limit"`,
		// Switches and named save buttons.
		`role="switch" name="value" id="input-torrent_dht"`,
		`aria-labelledby="save-torrent_dht label-torrent_dht"`,
		// The schedule rows hide while the schedule is off.
		`data-v2-depends-on="torrent_speed_mode=schedule" hidden`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("libtorrent tab lacks %s", marker)
		}
	}
	if strings.Contains(body, "Nella v2 ogni campo") || strings.Contains(body, "ⓘ") {
		t.Fatalf("developer jargon or inert icon still shown")
	}

	// IP filter switch and URL share one panel now.
	panel := func(title string) string {
		start := strings.Index(body, "<h3>"+title+"</h3>")
		if start < 0 {
			t.Fatalf("panel %q missing", title)
		}
		rest := body[start+4:]
		if next := strings.Index(rest, "<h3>"); next >= 0 {
			rest = rest[:next]
		}
		return rest
	}
	for _, key := range []string{"torrent_encryption", "torrent_apply_ip_filter", "torrent_ipfilter_url"} {
		if !strings.Contains(panel("Cifratura e filtro IP"), `id="v2-setting-`+key+`"`) {
			t.Errorf("%s is not in the Cifratura e filtro IP panel", key)
		}
	}

	// Weekdays are saved back as the "0,1,4" list the scheduler reads.
	code, saved := v2Request(t, server, http.MethodPost, "/settings/save", url.Values{"key": {"torrent_sched_days"}, "value": {"4", "0", "9", "2"}})
	if code != http.StatusOK || !strings.Contains(saved, "salvata") {
		t.Fatalf("days save -> %d: %s", code, saved)
	}
	if got := latestConfig(state).Settings["torrent_sched_days"]; got != "0,2,4" {
		t.Fatalf("days stored as %q, want 0,2,4", got)
	}
	// A switch posts its value first and the hidden "off" value after it.
	if _, saved = v2Request(t, server, http.MethodPost, "/settings/save", url.Values{"key": {"torrent_dht"}, "value": {"false"}}); latestConfig(state).Settings["torrent_dht"] != "false" {
		t.Fatalf("switch off not saved: %s", saved)
	}
	// A value different from the default is marked and can be restored.
	if !strings.Contains(saved, `class="setting-badge">modificato`) || !strings.Contains(saved, `data-v2-reset="true"`) {
		t.Fatalf("modified switch must show its default: %s", saved)
	}
	if _, saved = v2Request(t, server, http.MethodPost, "/settings/save", url.Values{"key": {"torrent_dht"}, "value": {"true", "false"}}); latestConfig(state).Settings["torrent_dht"] != "true" || strings.Contains(saved, "setting-badge\">modificato") {
		t.Fatalf("switch on not saved or still marked modified: %s", saved)
	}
	// The «Predefinito» button must be available on every resetting field, even
	// one already equal to its default (here: saved "true" == default "true").
	if !strings.Contains(saved, `data-v2-reset="true"`) {
		t.Fatalf("a field equal to its default must still offer «Predefinito»: %s", saved)
	}
	// Errors are tied to their control.
	if _, saved = v2Request(t, server, http.MethodPost, "/settings/save", url.Values{"key": {"score_res_1080p"}, "value": {"abc"}}); !strings.Contains(saved, `aria-invalid="true"`) || !strings.Contains(saved, "status-score_res_1080p") {
		t.Fatalf("score error must mark the control invalid: %s", saved)
	}
}

func TestSettingsEnginePanelsAndUnusedOptions(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// gx-torrent is the default engine: its panel is open, qBittorrent-nox's
	// is closed.
	_, body := v2Request(t, server, http.MethodGet, "/settings/body?tab=backend", nil)
	open := strings.Index(body, "<h3>gx-torrent</h3>")
	closed := strings.Index(body, `<summary class="panel-head"><h3>qBittorrent-nox</h3>`)
	if open < 0 || closed < 0 {
		t.Fatalf("engine panels not split by active engine: %s", body)
	}
	// The action buttons reopen the same section.
	if !strings.Contains(body, `name="redirect" value="/?view=settings&amp;tab=backend"`) {
		t.Fatalf("engine actions must come back to the Motore torrent section")
	}
	if !strings.Contains(body, `select class="input" name="value" id="input-gxtorrent_client_identity"`) ||
		!strings.Contains(body, `<option value="qbittorrent"`) ||
		!strings.Contains(body, `<option value="transmission"`) {
		t.Fatalf("client identity must be a select with the presets: %s", body)
	}
	// Expert knobs start closed.
	_, body = v2Request(t, server, http.MethodGet, "/settings/body?tab=performance", nil)
	if !strings.Contains(body, `<summary class="panel-head"><h3>Per esperti</h3>`) {
		t.Fatalf("expert panel must be collapsed")
	}
}

func TestSettingsSearchCoversScoresAndEditors(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	if _, body := v2Request(t, server, http.MethodGet, "/settings/search?q=dolby", nil); !strings.Contains(body, "Dolby TrueHD") || !strings.Contains(body, "tab=scores") {
		t.Fatalf("score weights must be searchable: %s", body)
	}
	if _, body := v2Request(t, server, http.MethodGet, "/settings/search?q=event", nil); !strings.Contains(body, "Hook eventi") || !strings.Contains(body, "tab=notify") {
		t.Fatalf("editors must be searchable: %s", body)
	}
	// Words of the previous labels still find the moved settings.
	if _, body := v2Request(t, server, http.MethodGet, "/settings/search?q=delay", nil); !strings.Contains(body, "delay_torrent_minutes") {
		t.Fatalf("old wording must still find the setting: %s", body)
	}
}

func TestScoreMacroCardsKeepTheirSubgroups(t *testing.T) {
	fields := []uiSettingField{
		{Key: "score_res_2160p"}, {Key: "score_res_1080p"}, {Key: "score_res_360p"},
		{Key: "score_codec_h265"}, {Key: "score_source_webdl"}, {Key: "score_bonus_dv"},
		{Key: "score_audio_lossless"}, {Key: "score_audio_high"}, {Key: "score_channels_5.1"},
		{Key: "score_bonus_proper"}, {Key: "upgrade_min_score_diff"}, {Key: "cleanup_upgrades"},
	}
	groups := uiSettingsGroups("scores", fields)
	want := map[string][]string{
		"Video":                  {"4K", "Full HD", "SD", "Codec video", "Sorgente", "HDR / Dolby Vision"},
		"Audio":                  {"Lossless", "DTS / DD+", "Canali e oggetti"},
		"Upgrade e sostituzione": {"Correzioni", "Soglie e pulizia"},
	}
	for _, group := range groups {
		expected, ok := want[group.Title]
		if !ok {
			continue
		}
		if len(group.Subgroups) != len(expected) {
			t.Fatalf("%s subgroups = %d, want %d (%v)", group.Title, len(group.Subgroups), len(expected), group.Subgroups)
		}
		for i, title := range expected {
			if group.Subgroups[i].Title != title {
				t.Errorf("%s subgroup %d = %q, want %q", group.Title, i, group.Subgroups[i].Title, title)
			}
		}
	}
}
