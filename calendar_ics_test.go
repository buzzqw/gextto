package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRenderICSFoldsAndEscapes(t *testing.T) {
	events := []icsEvent{{
		UID:         "gextto-tv-1-S01E02@gextto",
		Date:        time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC),
		Summary:     "Show; with, commas S01E02 · " + strings.Repeat("è", 60),
		Description: "line1\nline2",
	}}
	out := renderICS(events, "Gextto", time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n", "VERSION:2.0\r\n", "DTSTART;VALUE=DATE:20261009\r\n",
		"DTEND;VALUE=DATE:20261010\r\n", `Show\; with\, commas`, `line1\nline2`, "END:VCALENDAR\r\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\r\n") {
		if len(line) > 75 {
			t.Errorf("line longer than 75 octets: %d", len(line))
		}
	}
	// Unfolding restores the summary without breaking UTF-8.
	unfolded := strings.ReplaceAll(out, "\r\n ", "")
	if !strings.Contains(unfolded, strings.Repeat("è", 60)) {
		t.Error("folding broke a multi-byte character")
	}
}

func TestCalendarICSListsSeasonEpisodesInWindow(t *testing.T) {
	today := time.Now().UTC()
	day := func(offset int) string { return today.AddDate(0, 0, offset).Format("2006-01-02") }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tv/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"next_episode_to_air": map[string]any{"season_number": 2, "episode_number": 3, "air_date": day(3)},
			})
		case "/tv/42/season/2":
			_ = json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
				{"season_number": 2, "episode_number": 1, "air_date": day(-30), "name": "Too old"},
				{"season_number": 2, "episode_number": 2, "air_date": day(-2), "name": "Last week"},
				{"season_number": 2, "episode_number": 3, "air_date": day(3), "name": "Soon"},
				{"season_number": 2, "episode_number": 4, "air_date": day(200), "name": "Too far"},
			}})
		case "/movie/7/release_dates":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"iso_3166_1": "US", "release_dates": []map[string]any{{"type": 4, "release_date": day(-100) + "T00:00:00.000Z"}}},
				{"iso_3166_1": "IT", "release_dates": []map[string]any{
					{"type": 3, "release_date": day(5) + "T00:00:00.000Z"},
					{"type": 4, "release_date": day(20) + "T00:00:00.000Z"},
				}},
			}})
		case "/movie/8/release_dates":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		case "/movie/8":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 8, "title": "Other", "release_date": day(12)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	previous := tmdbAPIBaseURL
	tmdbAPIBaseURL = server.URL
	defer func() { tmdbAPIBaseURL = previous }()

	state := newTestAppState(t)
	cfg := latestConfig(state)
	key := "test-key"
	cfg.TmdbAPIKey = &key
	cfg.Series = []SeriesConfig{{Name: "Show", TmdbID: "42", Enabled: true}}
	cfg.Movies = []MovieConfig{{Name: "Film", TmdbID: "7", Enabled: true}, {Name: "Other", TmdbID: "8", Enabled: true}}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Old Show')"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,downloaded_at) SELECT id,1,4,?1 FROM series WHERE name='Old Show'", today.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	events := icsCollectEvents(t.Context(), cfg, state.db, func(text string) string { return text }, time.Now())
	summaries := []string{}
	for _, event := range events {
		summaries = append(summaries, event.Summary)
	}
	joined := strings.Join(summaries, "|")
	for _, want := range []string{"📺 Show S02E02 · Last week", "📺 Show S02E03 · Soon", "🎬 Film", "🎬 Other (uscita originale)", "📥 Old Show S01E04"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	for _, event := range events {
		if event.Summary == "🎬 Film" && event.Date.Format("2006-01-02") != day(20) {
			t.Errorf("movie must use the Italian digital release, got %s", event.Date.Format("2006-01-02"))
		}
	}
	for _, unwanted := range []string{"Too old", "Too far"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("%q is outside the window: %q", unwanted, joined)
		}
	}
}
