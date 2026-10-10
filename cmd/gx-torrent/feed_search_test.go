package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buzzqw/gextto/internal/torznab"
)

const searchFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel>
<item>
  <title>Movie 2024 1080p</title>
  <enclosure url="http://tracker.example/1.torrent" length="1234567890" type="application/x-bittorrent"/>
  <torznab:attr name="seeders" value="42"/>
  <torznab:attr name="size" value="1234567890"/>
  <torznab:attr name="magneturl" value="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"/>
</item>
</channel>
</rss>`

func TestSearchIndexersMapsResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" || r.URL.Query().Get("q") != "movie 2024" {
			t.Errorf("unexpected request %s q=%q", r.URL.Path, r.URL.Query().Get("q"))
		}
		_, _ = w.Write([]byte(searchFeedXML))
	}))
	defer srv.Close()

	items, errs := searchIndexers(context.Background(), []torznab.Indexer{{Name: "jackett", URL: srv.URL}}, "movie 2024")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	it := items[0]
	if it.Title != "Movie 2024 1080p" || it.Size != 1234567890 || it.Seeders != 42 {
		t.Fatalf("item = %+v", it)
	}
	if it.GUID != it.Magnet || it.Magnet == "" {
		t.Fatalf("GUID must be the magnet, got %q", it.GUID)
	}
	if it.Source != "jackett" {
		t.Fatalf("source = %q", it.Source)
	}
}

func TestSearchIndexersCollectsErrors(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(searchFeedXML))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer bad.Close()

	items, errs := searchIndexers(context.Background(), []torznab.Indexer{
		{Name: "good", URL: good.URL},
		{Name: "bad", URL: bad.URL},
	}, "x")
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want 1", errs)
	}
}

func TestRSSItemFromTorznabPrefersMagnetAsGUID(t *testing.T) {
	it := rssItemFromTorznab(torznab.Item{Title: "t", Link: "http://x/a.torrent"})
	if it.GUID != "http://x/a.torrent" {
		t.Fatalf("GUID = %q, want the link when there is no magnet", it.GUID)
	}
}
