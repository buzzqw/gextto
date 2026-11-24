package gextto

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSeriesAndMovieHandlersDoNotLoseConcurrentUpdates(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir,
		[]SeriesConfig{{Name: "Old Series"}},
		[]MovieConfig{{Name: "Old Movie"}},
	); err != nil {
		t.Fatal(err)
	}

	seriesPayload, err := json.Marshal([]SeriesConfig{{Name: "Series A", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	moviePayload, err := json.Marshal([]MovieConfig{{Name: "Movie A", Year: "2026", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		name   string
		status int
		body   string
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	go func() {
		<-start
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/config/series", bytes.NewReader(seriesPayload))
		SaveSeriesConfig(recorder, request, state)
		results <- result{name: "series", status: recorder.Code, body: recorder.Body.String()}
	}()
	go func() {
		<-start
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/config/movies", bytes.NewReader(moviePayload))
		SaveMoviesConfig(recorder, request, state)
		results <- result{name: "movies", status: recorder.Code, body: recorder.Body.String()}
	}()
	close(start)
	for range 2 {
		got := <-results
		if got.status != http.StatusOK {
			t.Fatalf("%s save returned %d: %s", got.name, got.status, got.body)
		}
	}

	cfg := DefaultConfig()
	cfg.DataDir = state.cfg.DataDir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Series) != 1 || cfg.Series[0].Name != "Series A" {
		t.Fatalf("series = %+v, want the concurrent series save", cfg.Series)
	}
	if len(cfg.Movies) != 1 || cfg.Movies[0].Name != "Movie A" {
		t.Fatalf("movies = %+v, want the concurrent movie save", cfg.Movies)
	}
}
