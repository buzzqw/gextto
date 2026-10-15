package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Every field the settings page shows must be savable: the page and the write
// allowlist used to be two hand-kept lists, and eight indexed options rendered
// as editable fields whose save always answered «chiave non modificabile».
func TestSettingsIndexIsWritable(t *testing.T) {
	for _, def := range uiSettingsIndex {
		if !gh7_setting_key_allowed(def.Key) {
			t.Errorf("%s is shown in the settings page but is not writable", def.Key)
		}
	}
	for _, def := range uiScoreSettingDefs {
		if !gh7_setting_key_allowed(def.Key) {
			t.Errorf("score weight %s is shown but is not writable", def.Key)
		}
	}
	for key := range settingNeverWritable {
		if uiSettingIndexed[key] {
			t.Errorf("%s is both shown in the settings page and never writable", key)
		}
		if gh7_setting_key_allowed(key) {
			t.Errorf("%s must stay unwritable", key)
		}
	}
}

// The API-only list holds exactly the keys that nothing else makes writable:
// an entry already covered by the index or a prefix is dead weight that hides
// where a key really comes from.
func TestSettingWritableAPIOnlyKeysAreNotRedundant(t *testing.T) {
	for key := range settingWritableAPIOnly {
		if uiSettingIndexed[key] {
			t.Errorf("%s is in the settings index: drop it from settingWritableAPIOnly", key)
		}
		for _, prefix := range settingWritablePrefixes {
			if strings.HasPrefix(key, prefix) {
				t.Errorf("%s is covered by the %q prefix: drop it from settingWritableAPIOnly", key, prefix)
			}
		}
	}
	for _, key := range []string{"", "unknown_key", "constants", "data_dir"} {
		if gh7_setting_key_allowed(key) {
			t.Errorf("%q must not be writable", key)
		}
	}
}

// The options that were shown but unsavable are saved through the real page
// endpoint and read back from the configuration.
func TestSettingsPreviouslyUnsavableOptionsSave(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	for key, value := range map[string]string{
		"cycle_title_search":           "no",
		"stall_alternative_after_min":  "120",
		"stall_alternative_score_drop": "50",
		"movies_flat_files":            "no",
		"temp_orphan_cleanup_enabled":  "false",
		"temp_orphan_min_age_days":     "14",
		"update_check":                 "false",
		"update_check_hours":           "12",
	} {
		code, body := v2Request(t, server, http.MethodPost, "/settings/save", url.Values{"key": {key}, "value": {value}})
		if code != http.StatusOK || strings.Contains(body, "chiave non modificabile") {
			t.Errorf("save %s -> %d: %s", key, code, body)
			continue
		}
		if got := latestConfig(state).Settings[key]; got != value {
			t.Errorf("%s stored as %q, want %q", key, got, value)
		}
	}
}

// The settings page applies the same value checks as the JSON API: a trash
// folder at the filesystem root (whose cleanup would empty the whole disk) or a
// relative path is refused with a message tied to the field, not stored.
func TestSettingsPageRejectsUnsafePaths(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	for value, message := range map[string]string{
		"/":        "non può essere la radice del filesystem",
		"relative": "deve essere un percorso assoluto",
	} {
		code, body := v2Request(t, server, http.MethodPost, "/settings/save", url.Values{"key": {"trash_path"}, "value": {value}})
		if code != http.StatusOK || !strings.Contains(body, message) || !strings.Contains(body, `aria-invalid="true"`) {
			t.Errorf("trash_path=%q -> %d, want %q: %s", value, code, message, body)
		}
		if got := latestConfig(state).Settings["trash_path"]; got == value {
			t.Errorf("trash_path=%q was stored", value)
		}
	}
}
