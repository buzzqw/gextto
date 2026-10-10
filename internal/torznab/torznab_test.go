package torznab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel>
<item>
  <title>Movie 2024 1080p</title>
  <guid>abc</guid>
  <link>http://tracker.example/download/1</link>
  <pubDate>Tue, 01 Oct 2024 10:00:00 +0000</pubDate>
  <enclosure url="http://tracker.example/download/1.torrent" length="1234567890" type="application/x-bittorrent"/>
  <torznab:attr name="seeders" value="42"/>
  <torznab:attr name="leechers" value="7"/>
  <torznab:attr name="size" value="1234567890"/>
  <torznab:attr name="magneturl" value="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"/>
</item>
<item>
  <title>Show S01</title>
  <enclosure url="http://tracker.example/download/2.torrent" length="500"/>
</item>
</channel>
</rss>`

func TestParse(t *testing.T) {
	items, err := Parse(strings.NewReader(sampleFeed), "jackett")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	first := items[0]
	if first.Title != "Movie 2024 1080p" || first.Size != 1234567890 {
		t.Fatalf("first item = %+v", first)
	}
	if first.Seeders != 42 || first.Leechers != 7 {
		t.Fatalf("seeders/leechers = %d/%d", first.Seeders, first.Leechers)
	}
	if !strings.HasPrefix(first.Magnet, "magnet:") {
		t.Fatalf("magnet = %q", first.Magnet)
	}
	if first.Download() != first.Magnet {
		t.Fatalf("Download must prefer the magnet")
	}
	if first.Indexer != "jackett" {
		t.Fatalf("indexer = %q", first.Indexer)
	}
	if first.Published.IsZero() {
		t.Fatal("pubDate not parsed")
	}
	second := items[1]
	if second.Download() != "http://tracker.example/download/2.torrent" {
		t.Fatalf("second Download = %q", second.Download())
	}
	if second.Leechers != -1 {
		t.Fatalf("missing leechers = %d, want -1", second.Leechers)
	}
}

func TestSearchAgainstFakeServer(t *testing.T) {
	var gotPath, gotKey, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.URL.Query().Get("apikey")
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()

	idx := Indexer{Name: "prowlarr", URL: srv.URL, APIKey: "secret"}
	items, err := idx.Search(context.Background(), "movie 2024")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	if gotPath != "/api" || gotKey != "secret" || gotQuery != "movie 2024" {
		t.Fatalf("request path=%q key=%q q=%q", gotPath, gotKey, gotQuery)
	}
}

func TestSearchRejectsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := (Indexer{URL: srv.URL}).Search(context.Background(), "x"); err == nil {
		t.Fatal("a non-2xx answer must be an error")
	}
}
