package gextto

// Removing a title from the followed lists is a deliberate action: the log
// must say what was removed and remind that the files in the library stay.
// These tests drive the real router, so the API, the web table and the TUI
// (which all call the same endpoints) are covered together.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func deleteViaRouter(t *testing.T, server *httptest.Server, path string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodDelete, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func TestRemovingASeriesFromTheListIsLogged(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir,
		[]SeriesConfig{{Name: "Example Show", Enabled: true}, {Name: "Other Show", Enabled: true}},
		[]MovieConfig{{ID: 7, Name: "Example Movie", Year: "2024", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	captured := captureLog(t)

	if code := deleteViaRouter(t, server, "/api/series/Example%20Show"); code != http.StatusOK {
		t.Fatalf("DELETE series -> %d", code)
	}
	log := captured.String()
	if !strings.Contains(log, "You removed «Example Show» from the series list") {
		t.Fatalf("the series removal is not in the log: %s", log)
	}
	if !strings.Contains(log, "files already in the library are left untouched") {
		t.Fatalf("the log does not say what happens to the files: %s", log)
	}
	if strings.Contains(log, "from the movie list") {
		t.Fatalf("a movie removal was logged too: %s", log)
	}
}

func TestRemovingAMovieFromTheListIsLogged(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir,
		[]SeriesConfig{{Name: "Example Show", Enabled: true}},
		[]MovieConfig{{ID: 7, Name: "Example Movie", Year: "2024", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	captured := captureLog(t)

	if code := deleteViaRouter(t, server, "/api/movies/7"); code != http.StatusOK {
		t.Fatalf("DELETE movie -> %d", code)
	}
	log := captured.String()
	if !strings.Contains(log, "You removed «Example Movie (2024)» from the movie list") {
		t.Fatalf("the movie removal is not in the log: %s", log)
	}
	if strings.Contains(log, "from the series list") {
		t.Fatalf("a series removal was logged too: %s", log)
	}
}

func TestSavingTheWholeLibraryLogsTheRemovedTitles(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir,
		[]SeriesConfig{{Name: "Example Show", Enabled: true}, {Name: "Other Show", Enabled: true}},
		[]MovieConfig{{ID: 7, Name: "Example Movie", Year: "2024", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	captured := captureLog(t)

	series, err := json.Marshal([]SeriesConfig{{Name: "Other Show", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := webPostJSON(t, server, "/api/config/series", string(series)); code != http.StatusOK {
		t.Fatalf("save series list -> %d: %s", code, body)
	}
	log := captured.String()
	if !strings.Contains(log, "You removed «Example Show» from the series list") {
		t.Fatalf("a removal made by a whole-list save was not logged: %s", log)
	}

	library, err := json.Marshal(LibraryInput{
		Series: []SeriesConfig{{Name: "Other Show", Enabled: true}},
		Movies: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := webPostJSON(t, server, "/api/config/library", string(library)); code != http.StatusOK {
		t.Fatalf("save library -> %d: %s", code, body)
	}
	log = captured.String()
	if !strings.Contains(log, "You removed «Example Movie (2024)» from the movie list") {
		t.Fatalf("a movie removed by a whole-library save was not logged: %s", log)
	}
}
