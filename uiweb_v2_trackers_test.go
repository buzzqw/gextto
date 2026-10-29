package gextto

// uiweb_v2_trackers_test.go locks the tracker editor to add and remove one row
// at a time: the form never sends a list that wipes the trackers the user did
// not touch.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// fakeTrackerEngine keeps a tracker list in memory and records what the editor
// sends to SetTrackers.
type fakeTrackerEngine struct {
	TorrentEngine
	hash     string
	trackers []models.TrackerView
	last     []TrackerEntry
}

func (fakeTrackerEngine) Name() string { return BackendGxTorrent }

func (f *fakeTrackerEngine) List() []models.TorrentView {
	return []models.TorrentView{{Hash: f.hash, Name: "movie", State: "downloading"}}
}

func (f *fakeTrackerEngine) Trackers(string) ([]models.TrackerView, bool, error) {
	return append([]models.TrackerView(nil), f.trackers...), true, nil
}

func (f *fakeTrackerEngine) SetTrackers(_ string, trackers []TrackerEntry) (bool, error) {
	f.last = append([]TrackerEntry(nil), trackers...)
	f.trackers = nil
	for _, tracker := range trackers {
		f.trackers = append(f.trackers, models.TrackerView{URL: tracker.URL, Tier: int(tracker.Tier)})
	}
	return true, nil
}

func (f *fakeTrackerEngine) urls() []string {
	out := make([]string, 0, len(f.trackers))
	for _, tracker := range f.trackers {
		out = append(out, tracker.URL)
	}
	return out
}

// TestV2TrackerEditorAddsAndRemovesOneRow drives the real router: the "add"
// form appends the typed trackers to the existing ones and the per-row remove
// form drops only that URL.
func TestV2TrackerEditorAddsAndRemovesOneRow(t *testing.T) {
	state := newTestAppState(t)
	hash := strings.Repeat("c", 40)
	const first, second, added = "https://one.example/announce", "https://two.example/announce", "https://three.example/announce"
	fake := &fakeTrackerEngine{hash: hash, trackers: []models.TrackerView{
		{URL: first, Tier: 0},
		{URL: second, Tier: 1},
	}}
	state.setActiveEngine(fake)
	router := Router(state)

	post := func(form url.Values) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/downloads/detail/action", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusSeeOther && recorder.Code != http.StatusOK {
			t.Fatalf("detail action -> %d: %s", recorder.Code, recorder.Body.String())
		}
	}

	post(url.Values{
		"hash": {hash}, "tab": {"trackers"}, "op": {"trackers"},
		"trackers": {"0|" + added + "\n"},
	})
	if got := fake.urls(); len(got) != 3 || got[0] != first || got[1] != second || got[2] != added {
		t.Fatalf("adding one tracker sent %v, want the two existing ones and the new one", got)
	}
	if fake.last[1].Tier != 1 {
		t.Fatalf("the existing tier was lost: %+v", fake.last[1])
	}

	post(url.Values{
		"hash": {hash}, "tab": {"trackers"}, "op": {"trackers-remove"}, "url": {first},
	})
	if got := fake.urls(); len(got) != 2 || got[0] != second || got[1] != added {
		t.Fatalf("removing one tracker sent %v, want only the other two", got)
	}

	// A duplicate typed again is not sent twice.
	post(url.Values{
		"hash": {hash}, "tab": {"trackers"}, "op": {"trackers"},
		"trackers": {"0|" + second + "\n"},
	})
	if got := fake.urls(); len(got) != 2 {
		t.Fatalf("a repeated tracker was added again: %v", got)
	}
}

// TestV2TrackerTabHidesQbittorrentMetaRows checks the "** [DHT] **"-style
// pseudo trackers qBittorrent reports are not shown (and so cannot be sent
// back to the engine as real trackers).
func TestV2TrackerTabHidesQbittorrentMetaRows(t *testing.T) {
	trackers := v2RealTrackers([]models.TrackerView{
		{URL: "** [DHT] **"},
		{URL: "https://one.example/announce"},
		{URL: "** [PeX] **"},
	})
	if len(trackers) != 1 || trackers[0].URL != "https://one.example/announce" {
		t.Fatalf("meta rows survived the filter: %+v", trackers)
	}
	inputs := v2TrackerInputs(trackers)
	if len(inputs) != 1 || inputs[0].Url != "https://one.example/announce" {
		t.Fatalf("payload = %+v", inputs)
	}
}
