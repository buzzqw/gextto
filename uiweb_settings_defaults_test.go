package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The settings page shows a starting value for every option it exposes
// (uiSettingDefaults). The backend has its own defaults in two other places:
// the typed DefaultTransferSettings struct — which loadConfigDB's fallbacks
// must reproduce — and the scalar getters on *Config. The three used to drift
// unseen: the page could show a value the backend would never use when nothing
// has been saved (that is how `torrent_preallocate` came to display «No» while
// the engine preallocated by default). These tests pin them together.

// uiSettingDefaultNotABackendDefault lists the few options whose page default
// is deliberately not the backend fallback; the value explains why. Everything
// else must agree: saving the page default must not move the transfer settings
// away from DefaultTransferSettings().
var uiSettingDefaultNotABackendDefault = map[string]string{
	"torrent_listen_interfaces": "the page pre-fills the interfaces the engine binds by default; leaving the key unset lets the engine choose",
}

// uiSettingBackendDefaults maps an option the backend reads through a typed
// getter (not through TransferSettings) to that getter read on a config with no
// saved settings. The map grows as options gain a getter; every entry ties the
// page default to the value the backend actually uses.
var uiSettingBackendDefaults = map[string]func(*Config) string{
	"torrent_preallocate":          func(c *Config) string { return strconv.FormatBool(c.TorrentPreallocate()) },
	"stall_alternative_after_min":  func(c *Config) string { return strconv.FormatInt(c.StallAlternativeAfterMin, 10) },
	"stall_alternative_score_drop": func(c *Config) string { return strconv.FormatInt(c.StallAlternativeScoreDrop, 10) },
	"cleanup_action":               func(c *Config) string { return c.CleanupAction },
	"default_language":             func(c *Config) string { return c.DefaultLanguage() },
	"tmdb_language":                func(c *Config) string { return c.TmdbLanguage() },
	"tvdb_language":                func(c *Config) string { return c.TvdbLanguage() },
	"delay_torrent_minutes":        func(c *Config) string { return strconv.FormatInt(c.DelayMinutes("series"), 10) },
	"delay_movies_minutes":         func(c *Config) string { return strconv.FormatInt(c.DelayMinutes("movie"), 10) },
	"delay_bypass_score":           func(c *Config) string { return strconv.FormatInt(c.DelayBypassScore(), 10) },
	"debug_enabled":                func(c *Config) string { return strconv.FormatBool(c.DebugEnabled()) },
	"max_release_age_days":         func(c *Config) string { return strconv.FormatInt(c.MaxReleaseAgeDays, 10) },
}

// TestDefaultTransferSettingsMatchLoader proves the fallbacks loadConfigDB
// applies to an absent setting reproduce DefaultTransferSettings, through the
// real loader (an empty settings table, no key saved).
func TestDefaultTransferSettingsMatchLoader(t *testing.T) {
	dir := t.TempDir()
	conn, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlExec(conn, "CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);"); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	conn.Close()

	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if cfg.Transfer != DefaultTransferSettings() {
		t.Errorf("loadConfigDB with no saved settings gave\n %+v\nwant DefaultTransferSettings()\n %+v", cfg.Transfer, DefaultTransferSettings())
	}
}

// TestSettingDefaultsMatchTheBackend keeps uiSettingDefaults aligned with the
// value the backend uses when nothing is saved: the page and the engine must
// not disagree about a fresh installation.
func TestSettingDefaultsMatchTheBackend(t *testing.T) {
	// Every option of the settings page: applying only that option's page
	// default must leave the transfer settings at their backend default.
	for key, value := range uiSettingDefaults {
		if reason := uiSettingDefaultNotABackendDefault[key]; reason != "" {
			continue
		}
		cfg := DefaultConfig()
		cfg.Settings = map[string]string{key: value}
		cfg.applyTransferSettings()
		if cfg.Transfer != DefaultTransferSettings() {
			t.Errorf("%s default %q changes the transfer settings away from the backend default\n got  %+v\n want %+v",
				key, value, cfg.Transfer, DefaultTransferSettings())
		}
	}

	// Options read through a typed getter instead of TransferSettings.
	for key, backend := range uiSettingBackendDefaults {
		cfg := DefaultConfig()
		cfg.applyTransferSettings()
		want, ok := uiSettingDefaults[key]
		if !ok {
			t.Errorf("%s is checked against the backend but has no page default", key)
			continue
		}
		if got := backend(&cfg); got != want {
			t.Errorf("%s: the page shows %q but the backend default is %q", key, want, got)
		}
	}
}

// The exception list above must reference options that exist, so a rename does
// not leave a stale exemption that silently disables the check.
func TestUISettingDefaultExceptionsReferenceRealOptions(t *testing.T) {
	for key := range uiSettingDefaultNotABackendDefault {
		if _, ok := uiSettingDefaults[key]; !ok {
			t.Errorf("%s is exempted but has no page default", key)
		}
	}
}

// TestConfigViewDefaultsMatchThePage pins the third copy of the defaults: the
// `/api/config` view (ConfigView) repeats a page default for the flat options
// it reports, and the two could drift (qbittorrent_url and qbittorrent_username
// reported empty while the page, and the managed runtime's fallback, said
// otherwise). Only the scalar, top-level keys are read here: a key the view
// nests (the transfer settings, the paths) is compared through the config
// itself, and list keys are a different representation, not a default.
func TestConfigViewDefaultsMatchThePage(t *testing.T) {
	state := newTestAppState(t)
	rec := httptest.NewRecorder()
	ConfigView(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil), state)
	var view map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode /api/config: %v", err)
	}
	for key, want := range uiSettingDefaults {
		// A scaled key (hours on the page, seconds in the config) is compared in
		// the stored unit elsewhere; the view reports the stored unit.
		if uiSettingMetaByKey[key].Scale > 1 {
			continue
		}
		raw, ok := view[key]
		if !ok || raw == nil {
			continue // not a flat key of the view
		}
		got, scalar := viewScalarString(raw)
		if !scalar {
			continue // a list or a nested object: not a scalar default
		}
		if !sameSettingValue(want, got) {
			t.Errorf("%s: the page shows %q but /api/config reports %q", key, want, got)
		}
	}
}

// viewScalarString renders a JSON-decoded scalar the way the page spells its
// default, and reports false for the values that are not scalars.
func viewScalarString(raw any) (string, bool) {
	switch value := raw.(type) {
	case string:
		return value, true
	case bool:
		return strconv.FormatBool(value), true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	default:
		return "", false
	}
}

// sameSettingValue compares a page default with a backend value tolerating the
// two spellings the codebase uses for the same boolean (yes/no and true/false).
func sameSettingValue(page, backend string) bool {
	page = strings.TrimSpace(page)
	backend = strings.TrimSpace(backend)
	if page == backend {
		return true
	}
	truth := func(value string) (bool, bool) {
		switch strings.ToLower(value) {
		case "yes", "true":
			return true, true
		case "no", "false":
			return false, true
		default:
			return false, false
		}
	}
	left, leftOK := truth(page)
	right, rightOK := truth(backend)
	return leftOK && rightOK && left == right
}

// The automatic cycle runs by default: a fresh installation searches (in
// dry-run) without the user having to flip a switch first. The page default and
// the backend default must agree, and the legacy "1" spelling must read as the
// same on/off so the row is not flagged as modified.
func TestAutomaticCycleIsOnByDefault(t *testing.T) {
	if !DefaultConfig().Active {
		t.Fatal("the automatic cycle must be on by default")
	}
	if got := uiSettingDefaults["active"]; got != "true" {
		t.Fatalf("page default for active = %q, want true", got)
	}
	field := uiSettingFieldFor("active", "active", "1")
	if !uiSettingSameValue(field, "1", "true") {
		t.Fatal("the stored \"1\" must read as the same on/off as the default \"true\"")
	}
}
