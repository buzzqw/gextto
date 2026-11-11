package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/tvmaze"
)

func TestSeriesEndVerdictWeighsTheSignals(t *testing.T) {
	today := time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		override string
		record   SeriesEndRecord
		state    string
		source   string
		conflict bool
	}{
		{"manual ended beats a running provider", "ended", SeriesEndRecord{Status: "Returning Series", NextAirDate: "2026-12-01"}, seriesEndEnded, "manual", false},
		{"manual running beats an ended provider", "continuing", SeriesEndRecord{Status: "Ended", TvmazeStatus: "Ended"}, seriesEndContinuing, "manual", false},
		{"an announced episode keeps it running", "", SeriesEndRecord{Status: "Ended", NextAirDate: "2026-11-11"}, seriesEndContinuing, "schedule", false},
		{"a past next date is no promise", "", SeriesEndRecord{Status: "Canceled", NextAirDate: "2026-11-10"}, seriesEndEnded, "tmdb", false},
		{"provider ended", "", SeriesEndRecord{Status: "Canceled", LastAirDate: "2025-01-01"}, seriesEndEnded, "tmdb", false},
		{"provider ended, TVmaze still running", "", SeriesEndRecord{Status: "Ended", TvmazeStatus: "Running"}, seriesEndEnded, "tmdb", true},
		{"TVmaze ended while TMDB is late", "", SeriesEndRecord{Status: "Returning Series", LastAirDate: "2024-01-01", TvmazeStatus: "Ended"}, seriesEndEnded, "tvmaze", true},
		{"waiting for a renewal", "", SeriesEndRecord{Status: "Returning Series", LastAirDate: "2025-10-01", TvmazeStatus: "To Be Determined"}, seriesEndUncertain, "tvmaze", false},
		{"never aired", "", SeriesEndRecord{Status: "In Production"}, seriesEndUpcoming, "tmdb", false},
		{"running", "", SeriesEndRecord{Status: "Returning Series", LastAirDate: "2026-11-01", TvmazeStatus: "Running"}, seriesEndContinuing, "tmdb", false},
		{"TVmaze only", "", SeriesEndRecord{TvmazeStatus: "Running"}, seriesEndContinuing, "tvmaze", false},
		{"nothing known", "", SeriesEndRecord{}, seriesEndUnknown, "", false},
	}
	for _, tc := range cases {
		got := seriesEndVerdict(tc.override, tc.record, "tmdb", today)
		if got.State != tc.state || got.Source != tc.source || got.Disagreement != tc.conflict {
			t.Errorf("%s: verdict = %+v, want state %q source %q conflict %v", tc.name, got, tc.state, tc.source, tc.conflict)
		}
	}
}

func TestNormalizeSeriesStatusOverride(t *testing.T) {
	for input, want := range map[string]string{"": "", "auto": "", " Ended ": "ended", "continuing": "continuing"} {
		if got, ok := normalizeSeriesStatusOverride(input); !ok || got != want {
			t.Errorf("%q -> %q, %v; want %q", input, got, ok, want)
		}
	}
	if _, ok := normalizeSeriesStatusOverride("maybe"); ok {
		t.Error("an unknown value must be refused")
	}
}

// fakeTvmaze answers the id lookups and counts the calls.
func fakeTvmaze(t *testing.T, answer func(query url.Values) (int, string)) *int32 {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/lookup/shows" {
			http.NotFound(w, r)
			return
		}
		status, body := answer(r.URL.Query())
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	previous := newSeriesTvmazeClient
	newSeriesTvmazeClient = func() *tvmaze.Client {
		client := tvmaze.New()
		client.BaseURL = server.URL
		return client
	}
	t.Cleanup(func() { newSeriesTvmazeClient = previous })
	return &calls
}

func TestRefreshSeriesEndRecordAsksTvmazeByID(t *testing.T) {
	calls := fakeTvmaze(t, func(query url.Values) (int, string) {
		if query.Get("thetvdb") == "4242" {
			return http.StatusOK, `{"id":1,"name":"Series A","status":"Ended","ended":"2025-03-01"}`
		}
		return http.StatusNotFound, ``
	})
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Settings: map[string]string{}}
	series := &SeriesConfig{Name: "Series A", Enabled: true}
	info := map[string]any{}
	if err := json.Unmarshal([]byte(`{"status":"Returning Series","last_air_date":"2025-03-01","next_episode_to_air":null,"external_ids":{"tvdb_id":4242,"imdb_id":"tt0000001"}}`), &info); err != nil {
		t.Fatal(err)
	}
	record, err := refreshSeriesEndRecord(context.Background(), cfg, db, series, info, SeriesEndRecord{})
	if err != nil || record.TvmazeStatus != "Ended" || record.Status != "Returning Series" {
		t.Fatalf("record = %+v, %v", record, err)
	}
	stored, _ := db.SeriesEndRecords()
	if stored["Series A"] != record {
		t.Fatalf("stored = %+v, want %+v", stored["Series A"], record)
	}
	if verdict := seriesEndVerdictFor(cfg, series, stored["Series A"]); verdict.State != seriesEndEnded || verdict.Source != "tvmaze" {
		t.Fatalf("verdict = %+v", verdict)
	}

	// Switched off: TVmaze is not asked and its answer is not kept.
	cfg.Settings[settingTvmazeSeriesStatus] = "false"
	before := atomic.LoadInt32(calls)
	record, err = refreshSeriesEndRecord(context.Background(), cfg, db, series, info, stored["Series A"])
	if err != nil || record.TvmazeStatus != "" || atomic.LoadInt32(calls) != before {
		t.Fatalf("switched off: record = %+v, calls %d -> %d", record, before, atomic.LoadInt32(calls))
	}
}

func TestTvmazeOutageKeepsThePreviousAnswer(t *testing.T) {
	fakeTvmaze(t, func(url.Values) (int, string) { return http.StatusBadGateway, "" })
	series := &SeriesConfig{Name: "Series B", TvdbID: "77"}
	if got := tvmazeSeriesStatus(context.Background(), map[string]any{}, series, "To Be Determined"); got != "To Be Determined" {
		t.Fatalf("an outage must keep the previous status, got %q", got)
	}
	// Without any id TVmaze is not asked at all (a search by name could pick a
	// different series with the same title).
	if got := tvmazeSeriesStatus(context.Background(), map[string]any{}, &SeriesConfig{Name: "Series C"}, "Running"); got != "" {
		t.Fatalf("no id: got %q", got)
	}
}

func TestSeriesStatusOverrideIsSavedAndShown(t *testing.T) {
	state := newTestAppState(t)
	series := SeriesConfig{Name: "Test Show", Seasons: "1+", Quality: "1080p", Language: "ita", Enabled: true}
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{series}, nil); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SaveSeriesEndRecord("Test Show", SeriesEndRecord{Status: "Returning Series", LastAirDate: "2024-01-01", TvmazeStatus: "Ended"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	_, body := v2Request(t, server, http.MethodGet, "/?view=series&series=Test+Show", nil)
	for _, want := range []string{`name="status_override"`, "🏁 terminata", "Secondo TVmaze non usciranno altre puntate", "⚠ fonti in disaccordo"} {
		if !strings.Contains(body, want) {
			t.Errorf("series page misses %q", want)
		}
	}
	if code, _ := v2Request(t, server, http.MethodPost, "/series/save", url.Values{"original_name": {"Test Show"}, "name": {"Test Show"}, "seasons": {"1+"}, "status_override": {"continuing"}}); code >= 400 {
		t.Fatalf("save answered %d", code)
	}
	if got := latestConfig(state).Series[0].StatusOverride; got != "continuing" {
		t.Fatalf("override = %q, want continuing", got)
	}
	_, body = v2Request(t, server, http.MethodGet, "/?view=series&series=Test+Show", nil)
	if !strings.Contains(body, "▶ in corso") || strings.Contains(body, "⚠ fonti in disaccordo") {
		t.Error("a manual choice must win and hide the disagreement")
	}
	// The library view carries the verdict for the series list.
	raw, code := v2InternalJSON(state, http.MethodGet, "/api/config/library", nil, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"series_end_state":"continuing"`) || !strings.Contains(string(raw), `"status_override":"continuing"`) {
		t.Fatalf("library view: %d %s", code, raw)
	}
	// An unknown value is refused by the API.
	if _, code := v2InternalJSON(state, http.MethodPost, "/api/series/Test%20Show", nil, []byte(`{"status_override":"forse"}`)); code != http.StatusBadRequest {
		t.Fatalf("bad override answered %d, want 400", code)
	}
}
