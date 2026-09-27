package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRouterRegistersEveryRoute builds the real mux. It fails (panic) on a
// duplicate/conflicting pattern and proves every handler name referenced by the
// router exists at compile time.
func TestRouterRegistersEveryRoute(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Router panicked: %v", r)
		}
	}()
	mux := Router(&AppState{})
	if mux == nil {
		t.Fatal("nil mux")
	}
}

func TestJSONHelpersShapes(t *testing.T) {
	recorder := httptest.NewRecorder()
	jsonResponse(recorder, map[string]any{"ok": true, "value": 3})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if ct := recorder.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["ok"] != true || decoded["value"].(float64) != 3 {
		t.Fatalf("body = %v", decoded)
	}

	recorder = httptest.NewRecorder()
	jsonError(recorder, http.StatusBadRequest, "boom")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("error status = %d", recorder.Code)
	}
	decoded = map[string]any{}
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	if decoded["ok"] != false || decoded["error"] != "boom" {
		t.Fatalf("error body = %v", decoded)
	}

	recorder = httptest.NewRecorder()
	jsonStatus(recorder, http.StatusConflict, map[string]any{"detail": "x"})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("jsonStatus = %d", recorder.Code)
	}
}

func TestPathAndQueryHelpers(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/series/My%20Show/episodes?page=2&limit=50", nil)
	request.SetPathValue("name", "My Show")
	if got := pathParam(request, "name"); got != "My Show" {
		t.Fatalf("pathParam = %q", got)
	}
	if got := queryParam(request, "page"); got != "2" {
		t.Fatalf("queryParam = %q", got)
	}
	if got := queryInt(request, "limit", 10); got != 50 {
		t.Fatalf("queryInt = %d", got)
	}
	if got := queryInt(request, "missing", 7); got != 7 {
		t.Fatalf("queryInt default = %d", got)
	}
}

func TestDecodeJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":5,"b":"x"}`))
	var decoded struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	if err := decodeJSON(request, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.A != 5 || decoded.B != "x" {
		t.Fatalf("decoded = %+v", decoded)
	}

	bad := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{not json"))
	if err := decodeJSON(bad, &decoded); err == nil {
		t.Fatal("malformed body should fail")
	}
}

func TestIndexAndEmbeddedUI(t *testing.T) {
	if !uiBundleAvailable() {
		t.Fatal("embedded UI bundle is not available")
	}
	recorder := httptest.NewRecorder()
	Index(recorder, httptest.NewRequest(http.MethodGet, "/", nil), &AppState{})
	if recorder.Code != http.StatusOK {
		t.Fatalf("index status = %d", recorder.Code)
	}
	if recorder.Body.Len() == 0 {
		t.Fatal("empty index")
	}
	if data, ok := uiAsset("pkg/ui.js"); !ok || len(data) == 0 {
		t.Fatal("embedded pkg/ui.js missing")
	}
}

func TestNewAppStateWiresCollaborators(t *testing.T) {
	state := NewAppState(&Config{}, "gextto.json", nil, nil, nil, nil, nil, nil, nil, nil)
	if state == nil {
		t.Fatal("nil state")
	}
	if state.cfg == nil || state.torrent_events == nil || state.last_cycle == nil ||
		state.cycle_lock == nil || state.rename_progress == nil || state.config_cache == nil {
		t.Fatal("state collaborators not initialised")
	}
}
