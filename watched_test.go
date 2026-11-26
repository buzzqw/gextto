package gextto

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadWatchedKeysFromPlex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/library/sections":
			_, _ = io.WriteString(w, `{"MediaContainer":{"Directory":[{"key":"1","Location":[{"path":"/movies"}]}]}}`)
		case "/library/sections/1/all":
			if r.URL.Query().Get("type") != "1" {
				_, _ = io.WriteString(w, `{"MediaContainer":{"Metadata":[]}}`)
				return
			}
			_, _ = io.WriteString(w, `{"MediaContainer":{"Metadata":[{"type":"movie","title":"Example Movie","year":1999,"viewCount":2},{"type":"movie","title":"Unwatched Movie","year":2020,"viewCount":0}]}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	state := newTestAppState(t)
	for key, value := range map[string]string{"plex_url": server.URL, "plex_token": "test-token"} {
		if err := saveConfigSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := loadWatchedKeys(context.Background(), latestConfig(state))
	if err != nil {
		t.Fatal(err)
	}
	if !keys["movie\x00example movie\x001999"] {
		t.Fatalf("the played movie is missing: %+v", keys)
	}
	if keys["movie\x00unwatched movie\x002020"] {
		t.Fatalf("a movie with no view count was marked as watched: %+v", keys)
	}
}

func TestWatchedSuppressionIsOffByDefault(t *testing.T) {
	cfg := DefaultConfig()
	if watchedSuppressionEnabled(&cfg) {
		t.Fatal("watched suppression must be opt-in")
	}
}
