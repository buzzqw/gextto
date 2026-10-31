package tvmaze

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearchAndEpisodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/shows":
			if r.URL.Query().Get("q") != "the bear" {
				t.Errorf("query = %q", r.URL.Query().Get("q"))
			}
			_, _ = w.Write([]byte(`[{"score":0.9,"show":{"id":1,"name":"The Bear","premiered":"2022-06-23"}}]`))
		case "/shows/1/episodes":
			_, _ = w.Write([]byte(`[{"id":10,"name":"System","season":1,"number":1,"airdate":"2022-06-23","airstamp":"2022-06-23T12:00:00+00:00"},{"id":11,"name":"Hands","season":1,"number":2,"airdate":"2099-01-01"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New()
	c.BaseURL = srv.URL
	shows, err := c.Search(context.Background(), "the bear")
	if err != nil || len(shows) != 1 || shows[0].Name != "The Bear" {
		t.Fatalf("shows = %+v err=%v", shows, err)
	}
	episodes, err := c.Episodes(context.Background(), shows[0].ID)
	if err != nil || len(episodes) != 2 {
		t.Fatalf("episodes = %+v err=%v", episodes, err)
	}
	now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	if !episodes[0].Aired(now) || episodes[1].Aired(now) {
		t.Fatalf("air dates: %+v", episodes)
	}
}

func TestClientRejectsBadResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New()
	c.BaseURL = srv.URL
	if _, err := c.Search(context.Background(), "x"); err == nil {
		t.Fatal("a 502 must be an error")
	}
	if _, err := c.Episodes(context.Background(), 0); err == nil {
		t.Fatal("an invalid id must be an error")
	}
}
