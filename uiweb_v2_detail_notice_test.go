package gextto

// uiweb_v2_detail_notice_test.go locks the confirmation the torrent detail
// modal shows after saving a tab: a save that succeeds silently looks like a
// click that did nothing, and a failure must not wipe the form.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// fakeLimitsEngine records the per-torrent limits the detail form sends.
type fakeLimitsEngine struct {
	TorrentEngine
	hash  string
	err   error
	saved *int64
}

func (fakeLimitsEngine) Name() string { return BackendGxTorrent }

func (fakeLimitsEngine) Trackers(string) ([]models.TrackerView, bool, error) {
	return nil, false, nil
}

func (f *fakeLimitsEngine) List() []models.TorrentView {
	return []models.TorrentView{{Hash: f.hash, Name: "movie", State: "downloading"}}
}

func (f *fakeLimitsEngine) SetLimits(_ string, download, _ *int64, _ *float64, _ *int64) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if download != nil {
		value := *download
		f.saved = &value
	}
	return true, nil
}

// postDetailLimits drives the router with the same form the limits tab submits.
func postDetailLimits(t *testing.T, router http.Handler, hash string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{
		"hash": {hash}, "tab": {"limits"}, "op": {"limits"},
		"download_limit": {"500"}, "upload_limit": {"0"},
		"seed_ratio": {"-1"}, "seed_days": {"-1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/downloads/detail/action", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// TestV2DetailSaveShowsConfirmation: saving the limits returns the refreshed
// tab plus an out-of-band toast saying the limits were saved.
func TestV2DetailSaveShowsConfirmation(t *testing.T) {
	state := newTestAppState(t)
	hash := strings.Repeat("d", 40)
	fake := &fakeLimitsEngine{hash: hash}
	state.setActiveEngine(fake)

	recorder := postDetailLimits(t, Router(state), hash)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail action -> %d: %s", recorder.Code, body)
	}
	if fake.saved == nil || *fake.saved != 500*1024 {
		t.Fatalf("download limit sent = %v, want 500 KiB/s", fake.saved)
	}
	if !strings.Contains(body, `hx-swap-oob="innerHTML"`) || !strings.Contains(body, "Limiti del torrent salvati.") {
		t.Fatalf("save confirmation toast missing: %s", body)
	}
	if !strings.Contains(body, `name="download_limit"`) {
		t.Fatalf("the limits form was not re-rendered: %s", body)
	}
}

// TestV2DetailSaveFailureKeepsTheForm: an error is reported in the toast and
// the tab is still rendered, so the values just typed are not lost.
func TestV2DetailSaveFailureKeepsTheForm(t *testing.T) {
	state := newTestAppState(t)
	hash := strings.Repeat("e", 40)
	fake := &fakeLimitsEngine{hash: hash, err: errors.New("limite non valido")}
	state.setActiveEngine(fake)

	recorder := postDetailLimits(t, Router(state), hash)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail action -> %d: %s", recorder.Code, body)
	}
	if !strings.Contains(body, `class="v2-toast error"`) || !strings.Contains(body, "limite non valido") {
		t.Fatalf("error toast missing: %s", body)
	}
	if !strings.Contains(body, `name="download_limit"`) {
		t.Fatalf("the failure wiped the form: %s", body)
	}
}
