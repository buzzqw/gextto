package gextto

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func settingPageKeys(page uiSettingsPage) map[string]bool {
	keys := map[string]bool{}
	for _, group := range page.Groups {
		for _, field := range group.Fields {
			keys[field.Key] = true
		}
	}
	return keys
}

func TestSettingsModeDefaultIsExpert(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/?view=settings", nil)
	if got := v2SettingsMode(request); got != v2SettingsModeExpert {
		t.Fatalf("default mode = %q, want expert (an existing install must not lose rows)", got)
	}
}

func TestSettingsModeCookieRemembersBrief(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/?view=settings", nil)
	request.AddCookie(&http.Cookie{Name: v2SettingsModeCookie, Value: v2SettingsModeBrief})
	if got := v2SettingsMode(request); got != v2SettingsModeBrief {
		t.Fatalf("remembered mode = %q, want brief", got)
	}
	// The query parameter wins over the cookie.
	request = httptest.NewRequest(http.MethodGet, "/?view=settings&mode=expert", nil)
	request.AddCookie(&http.Cookie{Name: v2SettingsModeCookie, Value: v2SettingsModeBrief})
	if got := v2SettingsMode(request); got != v2SettingsModeExpert {
		t.Fatalf("parameter mode = %q, want expert", got)
	}
}

func TestSettingsModeBriefHidesInternals(t *testing.T) {
	state := newTestAppState(t)
	brief := settingPageKeys(uiSettingsPageFrom(state, "performance", v2SettingsModeBrief))
	for _, hidden := range []string{"torrent_cache_size", "torrent_active_downloads", "torrent_dynamic_queue", "torrent_max_connections_per_torrent"} {
		if brief[hidden] {
			t.Fatalf("Facilitata must hide the internal %q", hidden)
		}
	}
	expert := settingPageKeys(uiSettingsPageFrom(state, "performance", v2SettingsModeExpert))
	if !expert["torrent_cache_size"] {
		t.Fatal("Esperto must show the internals")
	}
	// The user limits live on other tabs and stay in Facilitata.
	paths := settingPageKeys(uiSettingsPageFrom(state, "paths", v2SettingsModeBrief))
	if !paths["min_free_space_gb"] || !paths["archive_root"] {
		t.Fatal("Facilitata must show the space and folder limits")
	}
}

// Every key the setup wizard writes that is part of the settings index must be
// available in Facilitata: the user must find after setup what the wizard asked.
func TestEssentialCoversWizardSteps(t *testing.T) {
	indexed := map[string]bool{}
	for _, def := range uiSettingsIndex {
		indexed[def.Key] = true
	}
	wizard := []string{
		"auth_enabled", "auth_username", "auth_local_bypass", "auth_password",
		"archive_root", "torrent_dir", "torrent_temp_dir", "trash_path",
		"tmdb_api_key", "tvdb_api_key", "tvdb_pin", "active",
	}
	for _, key := range wizard {
		if !indexed[key] {
			t.Fatalf("wizard key %q is not in the settings index", key)
		}
		if !uiEssentialKeys[key] {
			t.Fatalf("wizard key %q must be Essential (Facilitata)", key)
		}
	}
}

func TestSettingsToggleRendersAndSwitches(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// Default (no cookie): Esperto, the full page.
	code, body := v2Request(t, server, http.MethodGet, "/?view=settings&tab=performance", nil)
	if code != http.StatusOK || !strings.Contains(body, "settings-mode") {
		t.Fatalf("settings page is missing the mode switch -> %d", code)
	}
	if !strings.Contains(body, `id="v2-setting-torrent_cache_size"`) {
		t.Fatal("the default level must be Esperto (the internals are visible)")
	}

	// Facilitata: the internals disappear, the limits stay.
	code, body = v2Request(t, server, http.MethodGet, "/?view=settings&tab=performance&mode=brief", nil)
	if code != http.StatusOK {
		t.Fatalf("brief settings page -> %d", code)
	}
	if strings.Contains(body, `id="v2-setting-torrent_cache_size"`) {
		t.Fatal("Facilitata must hide the cache row")
	}
	// The performance tab stays, but only for the guided card.
	if !strings.Contains(body, "v2-tuning-panel") {
		t.Fatal("Facilitata must keep the guided panel reachable in the performance tab")
	}
	// The user limits live on other tabs and stay.
	code, body = v2Request(t, server, http.MethodGet, "/?view=settings&tab=paths&mode=brief", nil)
	if code != http.StatusOK || !strings.Contains(body, `id="v2-setting-min_free_space_gb"`) {
		t.Fatalf("Facilitata must keep the free-space limit -> %d", code)
	}
}

// The search result must never link to a row hidden by the level.
func TestSettingsSearchRespectsBriefMode(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2RequestWithMode(t, server, "/settings/search?q=cache", v2SettingsModeBrief)
	if code != http.StatusOK {
		t.Fatalf("brief search -> %d", code)
	}
	if strings.Contains(body, "torrent_cache_size") {
		t.Fatalf("Facilitata search offered a hidden setting: %s", body)
	}
	code, body = v2RequestWithMode(t, server, "/settings/search?q=cache", v2SettingsModeExpert)
	if code != http.StatusOK || !strings.Contains(body, "torrent_cache_size") {
		t.Fatalf("Esperto search must find the cache setting -> %d: %s", code, body)
	}
}

// v2RequestWithMode issues a GET carrying the settings-mode cookie.
func v2RequestWithMode(t *testing.T, server *httptest.Server, path, mode string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: v2SettingsModeCookie, Value: mode})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return response.StatusCode, string(raw)
}
