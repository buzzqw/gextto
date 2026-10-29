package gextto

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// limitsRecordingEngine remembers the last SetLimits call, so the tests can
// check which fields the handler forwarded.
type limitsRecordingEngine struct {
	nullEngine

	download *int64
	upload   *int64
	ratio    *float64
	days     *int64
	calls    int
}

func (e *limitsRecordingEngine) Name() string { return "test" }

func (e *limitsRecordingEngine) SetLimits(_ string, download, upload *int64, ratio *float64, days *int64) (bool, error) {
	e.download, e.upload, e.ratio, e.days = download, upload, ratio, days
	e.calls++
	return true, nil
}

// postLimits sends a limits request with the given JSON body.
func postLimits(t *testing.T, state *AppState, body string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/torrents/aa/limits", bytes.NewReader([]byte(body)))
	request.SetPathValue("hash", "aa")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	SetTorrentLimits(recorder, request, state)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s answered %d (%s)", body, recorder.Code, recorder.Body.String())
	}
}

// TestTorrentLimitsOnlyChangeTheFieldsTheyName is the regression test of the
// seed limits crossing: the interface and the TUI send one field at a time, and
// an omitted field must keep its value instead of falling back to the global
// one (ratio to -1, days to -1) or to "unlimited" for the rates.
func TestTorrentLimitsOnlyChangeTheFieldsTheyName(t *testing.T) {
	state := newTestAppState(t)
	engine := &limitsRecordingEngine{}
	state.setActiveEngine(engine)

	postLimits(t, state, `{"seed_days":0}`)
	if engine.ratio != nil {
		t.Fatalf("setting the seed days also changed the ratio: %v", *engine.ratio)
	}
	if engine.days == nil || *engine.days != 0 {
		t.Fatalf("seed days not forwarded: %v", engine.days)
	}

	postLimits(t, state, `{"seed_ratio":2.5}`)
	if engine.days != nil {
		t.Fatalf("setting the ratio also changed the seed days: %v", *engine.days)
	}
	if engine.ratio == nil || *engine.ratio != 2.5 {
		t.Fatalf("ratio not forwarded: %v", engine.ratio)
	}

	// A rate limit alone must not reset the seed limits nor the other rate.
	postLimits(t, state, `{"download_limit":1048576}`)
	if engine.ratio != nil || engine.days != nil {
		t.Fatal("a rate limit touched the seed limits")
	}
	if engine.download == nil || *engine.download != 1048576 {
		t.Fatalf("download limit not forwarded: %v", engine.download)
	}
	if engine.upload != nil {
		t.Fatalf("download limit also changed the upload limit: %v", *engine.upload)
	}

	// The complete payload (what the detail drawer sends) still sets everything.
	postLimits(t, state, `{"download_limit":0,"upload_limit":-1,"seed_ratio":0,"seed_days":-1,"max_connections":-1,"max_uploads":-1}`)
	if engine.download == nil || *engine.download != 0 || engine.upload == nil || *engine.upload != -1 {
		t.Fatalf("rates not forwarded: %v %v", engine.download, engine.upload)
	}
	if engine.ratio == nil || *engine.ratio != 0 || engine.days == nil || *engine.days != -1 {
		t.Fatalf("seed limits not forwarded: %v %v", engine.ratio, engine.days)
	}
}

// TestTorrentLimitsLegacyKeepsUnsentSeedLimits checks the historical payload:
// the rates are always part of it, the seed limits only when present.
func TestTorrentLimitsLegacyKeepsUnsentSeedLimits(t *testing.T) {
	state := newTestAppState(t)
	engine := &limitsRecordingEngine{}
	state.setActiveEngine(engine)

	body, err := json.Marshal(map[string]any{"hash": "aa", "dl_kbps": 10, "ul_kbps": 0})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/torrents/set_limits", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	SetTorrentLimitsLegacy(recorder, request, state)
	if recorder.Code != http.StatusOK {
		t.Fatalf("legacy limits answered %d (%s)", recorder.Code, recorder.Body.String())
	}
	if engine.ratio != nil || engine.days != nil {
		t.Fatal("the legacy payload reset the seed limits it did not carry")
	}
	if engine.download == nil || *engine.download != 10*1024 {
		t.Fatalf("download limit = %v", engine.download)
	}
	if engine.upload == nil || *engine.upload != 0 {
		t.Fatalf("upload limit = %v", engine.upload)
	}
}
