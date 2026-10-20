package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/amule/amuletest"
)

// ed2kTestRouter is the real router with the aMule settings pointing at a
// fake core.
func ed2kTestRouter(t *testing.T) (*amuletest.Core, http.Handler) {
	t.Helper()
	core, cfg := ed2kTestCore(t, nil)
	state := newTestAppState(t)
	for key, value := range cfg.Settings {
		if err := SaveSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	return core, Router(state)
}

func ed2kRequest(t *testing.T, handler http.Handler, method, path, body string) map[string]any {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("%s %s: status %d, body %q", method, path, recorder.Code, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s: status %d, body %v", method, path, recorder.Code, payload)
	}
	return payload
}

func TestEd2kAPI(t *testing.T) {
	core, handler := ed2kTestRouter(t)
	const hash = "ffffffffffffffffffffffffffffffff"
	core.SetDownloads([]amuletest.File{{Hash: hash, Name: "Rare Show S01E01.mkv", Size: 200, Done: 50, Speed: 1024, Sources: 4}})
	ed2kForgetQueue()

	list := ed2kRequest(t, handler, http.MethodGet, "/api/ed2k/downloads", "")
	downloads, _ := list["downloads"].([]any)
	if list["enabled"] != true || len(downloads) != 1 {
		t.Fatalf("downloads = %v", list)
	}
	first := downloads[0].(map[string]any)
	if first["state"] != "downloading" || first["progress"].(float64) != 25 || !strings.HasPrefix(first["link"].(string), "ed2k://|file|") {
		t.Fatalf("download = %v", first)
	}

	ed2kRequest(t, handler, http.MethodPost, "/api/ed2k/downloads/"+hash+"/priority", `{"priority":"high"}`)
	if got := core.Downloads()[0].Priority; got != 2 {
		t.Fatalf("priority = %d", got)
	}
	ed2kRequest(t, handler, http.MethodPost, "/api/ed2k/downloads/"+hash+"/pause", "")
	if got := core.Downloads()[0].Status; got != 7 {
		t.Fatalf("status after pause = %d", got)
	}

	limits := ed2kRequest(t, handler, http.MethodPost, "/api/ed2k/limits", `{"download_kib":800,"upload_kib":90}`)
	if limits["download_kib"].(float64) != 800 || limits["upload_kib"].(float64) != 90 {
		t.Fatalf("limits = %v", limits)
	}
	log := ed2kRequest(t, handler, http.MethodGet, "/api/ed2k/log", "")
	if lines, _ := log["lines"].([]any); len(lines) != 2 {
		t.Fatalf("log = %v", log)
	}

	ed2kRequest(t, handler, http.MethodPost, "/api/ed2k/downloads/"+hash+"/remove", "")
	if len(core.Downloads()) != 0 {
		t.Fatalf("queue after remove = %+v", core.Downloads())
	}
}

func TestEd2kFollowsTheTorrentSpeedLimits(t *testing.T) {
	_, cfg := ed2kTestCore(t, nil)
	ed2kSpeedState.Lock()
	ed2kSpeedState.key = ""
	ed2kSpeedState.Unlock()
	now := time.Now()
	applyEd2kSpeedLimits(cfg, 3000, 400, now)
	prefs, err := Ed2kLimits(cfg)
	if err != nil || prefs.MaxDownload != 3000 || prefs.MaxUpload != 400 {
		t.Fatalf("limits = %+v, %v", prefs, err)
	}
	ed2kSpeedState.Lock()
	sent := ed2kSpeedState.sent
	ed2kSpeedState.Unlock()
	applyEd2kSpeedLimits(cfg, 3000, 400, now.Add(time.Minute))
	ed2kSpeedState.Lock()
	again := ed2kSpeedState.sent
	ed2kSpeedState.Unlock()
	if !again.Equal(sent) {
		t.Fatal("unchanged limits were sent again within the resend interval")
	}
	applyEd2kSpeedLimits(cfg, -1, 0, now.Add(2*time.Minute))
	if prefs, _ := Ed2kLimits(cfg); prefs.MaxDownload != 0 || prefs.MaxUpload != 0 {
		t.Fatalf("unlimited = %+v", prefs)
	}
}

func TestV2Ed2kDetailAndPriority(t *testing.T) {
	core, handler := ed2kTestRouter(t)
	const hash = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	core.SetDownloads([]amuletest.File{{Hash: hash, Name: "Rare Show S01E02.mkv", Size: 100, Done: 10, LastRecv: time.Now().Unix()}})
	ed2kForgetQueue()
	request := httptest.NewRequest(http.MethodGet, "/downloads/ed2k-detail?hash="+hash, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "Rare Show S01E02.mkv") || !strings.Contains(body, `data-v2-copy="ed2k://|file|`) {
		t.Fatalf("detail %d: %.400s", recorder.Code, body)
	}
	request = httptest.NewRequest(http.MethodPost, "/downloads/ed2k-priority", strings.NewReader("hash="+hash+"&priority=low"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || core.Downloads()[0].Priority != 0 {
		t.Fatalf("priority form %d, queue %+v", recorder.Code, core.Downloads())
	}
	request = httptest.NewRequest(http.MethodGet, "/ed2k/log", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if !strings.Contains(recorder.Body.String(), "Connected to Kad") {
		t.Fatalf("log panel: %.300s", recorder.Body.String())
	}
}
