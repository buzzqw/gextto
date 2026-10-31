package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/tvmaze"
)

func TestWantedMetadataEpisodesAndMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/shows":
			_, _ = w.Write([]byte(`[{"score":1,"show":{"id":7,"name":"The Bear"}}]`))
		case "/shows/7/episodes":
			_, _ = w.Write([]byte(`[
				{"id":1,"season":1,"number":1,"airdate":"2022-06-23"},
				{"id":2,"season":1,"number":2,"airdate":"2099-01-01"},
				{"id":3,"season":2,"number":1,"airdate":"2023-06-22"}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(metadataEpisodesKey, "true")
	d.metadata = tvmaze.New()
	d.metadata.BaseURL = srv.URL
	w := wantedEntry{Name: "The Bear", Kind: "series", Seasons: "1"}
	if !d.refreshWantedMetadata(context.Background(), w, true) {
		t.Fatal("the refresh must have run")
	}
	meta := d.wantedMetadataFor(w.Name)
	if meta.ID != 7 || meta.Name != "The Bear" || len(meta.Episodes) != 3 {
		t.Fatalf("metadata = %+v", meta)
	}
	// Only the aired episode inside season 1 counts.
	eps := d.wantedMetadataEpisodes(w, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC))
	if len(eps) != 1 || eps[0].season != 1 || eps[0].episode != 1 {
		t.Fatalf("episodes = %+v", eps)
	}
	missing := d.wantedMissing(w)
	if len(missing) != 1 || missing[0].episode != 1 {
		t.Fatalf("missing = %+v", missing)
	}
	stats := d.wantedStatsFor(w)
	if stats.Missing != 1 || stats.Downloaded != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if !strings.Contains(d.wantedMetadataChip(w), "TVmaze 3") {
		t.Fatalf("chip = %q", d.wantedMetadataChip(w))
	}
	// Taken: the real episode is no longer missing.
	d.recordWantedTaken(w, 1, 1)
	if got := d.wantedMissing(w); len(got) != 0 {
		t.Fatalf("missing after taking it = %+v", got)
	}
	if got := d.wantedStatsFor(w); got.Downloaded != 1 || got.Missing != 0 {
		t.Fatalf("stats after taking it = %+v", got)
	}
}

func TestWantedMetadataIsOffByDefault(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	if d.metadataEpisodesEnabled() {
		t.Fatal("the real episode list must be opt-in")
	}
	if d.refreshWantedMetadata(context.Background(), wantedEntry{Name: "x", Kind: "series"}, true) {
		t.Fatal("a disabled metadata list must not call the network")
	}
	d.opts.Settings.Set(metadataEpisodesKey, "true")
	if !d.metadataEpisodesEnabled() {
		t.Fatal("the explicit setting must enable it")
	}
}

func TestWantedMetadataNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(metadataEpisodesKey, "true")
	d.metadata = tvmaze.New()
	d.metadata.BaseURL = srv.URL
	if !d.refreshWantedMetadata(context.Background(), wantedEntry{Name: "Unknown Show", Kind: "series"}, true) {
		t.Fatal("the refresh must have run")
	}
	meta := d.wantedMetadataFor("Unknown Show")
	if meta.ID != 0 || !meta.NoMatch {
		t.Fatalf("metadata = %+v", meta)
	}
	if !strings.Contains(d.wantedMetadataChip(wantedEntry{Name: "Unknown Show", Kind: "series"}), "not matched") {
		t.Fatal("the chip must say the match failed")
	}
}
