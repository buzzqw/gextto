package gextto

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func mdblistTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("apikey"); got != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/lists/user":
			_, _ = io.WriteString(w, `[{"id":"1","name":"Movies"}]`)
		case "/lists/1/items":
			_, _ = io.WriteString(w, `[{"title":"Example Movie","release_year":1999,"imdb_id":"tt0000001","tmdb_id":"42","type":"movie"},{"name":"Example Show","year":2020,"tvdb_id":"77","type":"show"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestMDbListItemsAndNormalization(t *testing.T) {
	server := mdblistTestServer(t)
	previous := mdblistAPIBaseURL
	mdblistAPIBaseURL = server.URL
	t.Cleanup(func() { mdblistAPIBaseURL = previous })

	client := (&MDbListClient{}).FromSettings(map[string]string{mdblistAPIKeyKey: "test-key", mdblistListIDKey: "1"})
	if !client.Configured() {
		t.Fatal("the client is not configured")
	}
	value, err := client.Items(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	normalized := normalizeMDbListItems(value)
	items := collectListImportItems("mdblist", normalized)
	if len(items) != 2 {
		t.Fatalf("items = %+v, want two", items)
	}
	var movie, series *ListImportItem
	for i := range items {
		if items[i].Kind == "movie" {
			movie = &items[i]
		} else {
			series = &items[i]
		}
		if !validListImportItem(items[i]) {
			t.Fatalf("a normalized MDbList item is invalid: %+v", items[i])
		}
	}
	if movie == nil || movie.ExternalIDs["imdb"] != "tt0000001" || movie.Year != "1999" {
		t.Fatalf("movie = %+v", movie)
	}
	if series == nil || series.ExternalIDs["tvdb"] != "77" {
		t.Fatalf("series = %+v", series)
	}
}

func TestMDbListPreviewAcceptsItsSource(t *testing.T) {
	server := mdblistTestServer(t)
	previous := mdblistAPIBaseURL
	mdblistAPIBaseURL = server.URL
	t.Cleanup(func() { mdblistAPIBaseURL = previous })

	state := newTestAppState(t)
	for key, value := range map[string]string{mdblistAPIKeyKey: "test-key", mdblistListIDKey: "1"} {
		if err := saveConfigSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/list-import/preview?source=mdblist", nil)
	ListImportPreview(recorder, request, state)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview -> %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Example Movie") {
		t.Fatalf("the preview has no MDbList item: %s", recorder.Body.String())
	}
}

func TestListImportRejectsUnknownProvider(t *testing.T) {
	state := newTestAppState(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/list-import/preview?source=trakt", nil)
	ListImportPreview(recorder, request, state)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("an unsupported provider -> %d, want 400", recorder.Code)
	}
}

func TestWatchedReleaseKey(t *testing.T) {
	series, season, episode, year := "Example Show", int64(2), int64(3), int64(1999)
	movie := &models.Release{Kind: "movie", Title: "Example Movie", Year: &year}
	ep := &models.Release{Kind: "series", Series: &series, Season: &season, Episode: &episode}
	if watchedReleaseKey(movie) != "movie\x00example movie\x001999" {
		t.Fatalf("movie key = %q", watchedReleaseKey(movie))
	}
	if watchedReleaseKey(ep) != "episode\x00example show\x002\x003" {
		t.Fatalf("episode key = %q", watchedReleaseKey(ep))
	}
}
