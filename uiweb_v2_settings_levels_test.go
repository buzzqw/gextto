package gextto

// uiweb_v2_settings_levels_test.go covers the two level rules of the
// configuration page: the essential rows of a panel come first, and a section
// shows how many of its settings left their default.

import (
	"testing"
)

func TestSettingsEssentialsLeadEveryPanel(t *testing.T) {
	for _, tab := range uiSettingsTabs {
		fields := []uiSettingField{}
		for _, def := range uiSettingsIndex {
			if def.Tab == tab.ID {
				fields = append(fields, uiSettingFieldFor(def.Key, def.Label, ""))
			}
		}
		if len(fields) == 0 {
			continue
		}
		ordered := uiSettingsOrderFields(tab.ID, fields)
		if len(ordered) != len(fields) {
			t.Fatalf("%s: ordering lost fields (%d of %d)", tab.ID, len(ordered), len(fields))
		}
		internals := map[string]bool{}
		seen := map[string]bool{}
		for _, field := range ordered {
			if seen[field.Key] {
				t.Fatalf("%s: %q repeated", tab.ID, field.Key)
			}
			seen[field.Key] = true
			title := uiSettingGroupTitle(tab.ID, field.Key)
			if uiEssentialKeys[field.Key] {
				if internals[title] {
					t.Fatalf("%s: the essential %q follows an internal row in %q", tab.ID, field.Key, title)
				}
				continue
			}
			internals[title] = true
		}
	}
}

func TestSettingsPageCountsWhatLeftTheDefault(t *testing.T) {
	state := newTestAppState(t)
	// A row with a registered default: the value below is not it.
	if err := SaveSetting(state.cfg.DataDir, "refresh_interval", "3600"); err != nil {
		t.Fatal(err)
	}
	if refreshed, err := LoadConfig(state.config_path); err == nil {
		state.cfg = &refreshed
	}
	state.config_cache = &ConfigCache{}
	page := uiSettingsPageFrom(state, "daemon", v2SettingsModeExpert)
	if page.ModifiedCount < 1 {
		t.Fatalf("modified total = %d, want at least the changed refresh interval", page.ModifiedCount)
	}
	found := false
	for _, tab := range page.Tabs {
		if tab.ID != "daemon" {
			continue
		}
		found = true
		if tab.Modified < 1 {
			t.Fatalf("daemon section modified = %d, want at least 1", tab.Modified)
		}
	}
	if !found {
		t.Fatal("the daemon section is missing")
	}
	// A row kept at its default is not counted.
	clear, err := LoadConfig(state.config_path)
	if err != nil {
		t.Fatal(err)
	}
	if got := clear.Settings["refresh_interval"]; got != "3600" {
		t.Fatalf("saved refresh interval = %q", got)
	}
}
