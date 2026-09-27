package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWatchedFoldersEndpointRoundtrip(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	folder := t.TempDir()
	payload, _ := json.Marshal([]map[string]any{{
		"path": folder, "enabled": true, "recursive": false, "delete_after": false,
	}})
	status, body := webPostJSON(t, server, "/api/watched-folders", string(payload))
	if status != http.StatusOK {
		t.Fatalf("save watched folders -> %d: %s", status, body)
	}
	code, _, listing := webGet(t, server, "/api/watched-folders")
	if code != http.StatusOK || !strings.Contains(string(listing), folder) {
		t.Fatalf("watched folders not persisted: %d %s", code, listing)
	}
}

func TestEventHooksEndpointValidatesAndReloads(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	valid, _ := json.Marshal([]map[string]any{{
		"name": "notify", "enabled": true, "events": []string{"torrent_completed"},
		"program": "/bin/true", "args": "--flag", "timeout_secs": 30,
	}})
	if status, body := webPostJSON(t, server, "/api/event-hooks", string(valid)); status != http.StatusOK {
		t.Fatalf("valid hooks rejected -> %d: %s", status, body)
	}
	invalid, _ := json.Marshal([]map[string]any{{"name": "broken", "enabled": true}})
	if status, _ := webPostJSON(t, server, "/api/event-hooks", string(invalid)); status == http.StatusOK {
		t.Fatal("hook without a program was accepted")
	}
	code, _, listing := webGet(t, server, "/api/event-hooks")
	if code != http.StatusOK || !strings.Contains(string(listing), "notify") {
		t.Fatalf("hooks not persisted: %d %s", code, listing)
	}
}

func TestAPIAuthEnforced(t *testing.T) {
	state := newTestAppState(t)
	token := "secret-token"
	// The token is read from the live configuration; persist it instead of
	// mutating the startup snapshot.
	if err := SaveSetting(state.cfg.DataDir, "api_token", token); err != nil {
		t.Fatalf("save token: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// Public endpoints stay reachable.
	if code, _, _ := webGet(t, server, "/api/status"); code != http.StatusOK {
		t.Fatalf("status should not require the token: %d", code)
	}
	// Protected endpoint without a token.
	response, err := http.Get(server.URL + "/api/series")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token -> %d, want 401", response.StatusCode)
	}
	// Protected endpoint with the token.
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/series", nil)
	request.Header.Set("x-gextto-token", token)
	authorized, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	authorized.Body.Close()
	if authorized.StatusCode != http.StatusOK {
		t.Fatalf("with token -> %d, want 200", authorized.StatusCode)
	}
}

func TestLanguageEndpointSwitchesActive(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	if status, body := webPostJSON(t, server, "/api/i18n/language", `{"lang":"en"}`); status != http.StatusOK {
		t.Fatalf("set language -> %d: %s", status, body)
	}
	code, _, active := webGet(t, server, "/api/i18n/active")
	if code != http.StatusOK {
		t.Fatalf("active -> %d", code)
	}
	if !strings.Contains(string(active), "en") {
		t.Fatalf("active language not english: %s", active)
	}
	if stored, err := state.i18n.Language(); err != nil || stored != "en" {
		t.Fatalf("stored language = %q err=%v", stored, err)
	}
}

func TestScorePreviewNoRenameAndLanguageEndpoints(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, body := webPostJSON(t, server, "/api/score/preview", `{"title":"Show.S01E01.1080p.WEB-DL.ITA.ENG.H264-TBK"}`)
	if status != http.StatusOK {
		t.Fatalf("score preview -> %d: %s", status, body)
	}
	var preview struct {
		Quality struct {
			Resolution string `json:"resolution"`
		} `json:"quality"`
		Score int64 `json:"score"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Quality.Resolution != "1080p" || preview.Score <= 0 {
		t.Fatalf("preview = %+v", preview)
	}

	code, _, listing := webGet(t, server, "/api/torrent-no-rename")
	if code != http.StatusOK {
		t.Fatalf("no-rename list -> %d", code)
	}
	if !strings.Contains(string(listing), "torrents") {
		t.Fatalf("no-rename listing shape: %s", listing)
	}
}

func TestNormalizeReleaseSeriesMapsToConfiguredName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "CIA", Enabled: true}}
	release := ParseRelease("CIA.2026.S01E10.Rare.Earth.1080p.WEB-DL.ITA.ENG.H264-TBK",
		"magnet:?xt=urn:btih:"+repeatTest("a", 40), "test")
	if release == nil || release.Series == nil {
		t.Fatal("release not parsed")
	}
	normalizeReleaseSeries(&cfg, release)
	if *release.Series != "CIA" {
		t.Fatalf("series = %q, want CIA", *release.Series)
	}
}
