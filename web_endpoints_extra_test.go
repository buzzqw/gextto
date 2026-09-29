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

func TestAPIRoutesArePublic(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	for _, path := range []string{"/api/status", "/api/series"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s -> %d, want 200", path, response.StatusCode)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/series", nil)
	public, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	public.Body.Close()
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
	if status, body := webPostJSON(t, server, "/api/i18n/language", `{"lang":"de"}`); status != http.StatusOK {
		t.Fatalf("set German language -> %d: %s", status, body)
	}
	if stored, err := state.i18n.Language(); err != nil || stored != "de" {
		t.Fatalf("stored German language = %q err=%v", stored, err)
	}
	if status, body := webPostJSON(t, server, "/api/i18n/language", `{"lang":"fr"}`); status != http.StatusOK {
		t.Fatalf("set French language -> %d: %s", status, body)
	}
	if stored, err := state.i18n.Language(); err != nil || stored != "fr" {
		t.Fatalf("stored French language = %q err=%v", stored, err)
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
