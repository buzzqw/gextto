package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const torznabFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed" xmlns:jackett="https://github.com/Jackett/Jackett">
<channel>
<item>
  <title>Movie 2024 1080p</title>
  <guid>g1</guid>
  <jackettindexer>TrackerA</jackettindexer>
  <link>http://tracker.example/details/1</link>
  <pubDate>Tue, 01 Oct 2024 10:00:00 +0000</pubDate>
  <enclosure url="http://tracker.example/download/1.torrent" length="1500000000"/>
  <torznab:attr name="seeders" value="30"/>
  <torznab:attr name="peers" value="5"/>
</item>
<item>
  <title>Show S01E01 2160p</title>
  <guid>g2</guid>
  <description><![CDATA[<a href="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=x">dl</a>]]></description>
  <torznab:attr name="seeders" value="3"/>
</item>
</channel>
</rss>`

const atomFeed = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<entry>
  <title>Atom Movie</title>
  <id>a1</id>
  <updated>2024-10-02T09:00:00Z</updated>
  <link rel="enclosure" href="http://t.example/a.torrent" length="99"/>
</entry>
</feed>`

func TestParseTorznab(t *testing.T) {
	items, err := Parse(strings.NewReader(torznabFeed), "feedA")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	first := items[0]
	if first.Title != "Movie 2024 1080p" || first.Size != 1500000000 {
		t.Fatalf("first = %+v", first)
	}
	if first.Link != "http://tracker.example/download/1.torrent" || first.Magnet != "" {
		t.Fatalf("first link/magnet = %q/%q", first.Link, first.Magnet)
	}
	if first.Seeders != 30 || first.Leechers != 5 {
		t.Fatalf("seeders/leechers = %d/%d", first.Seeders, first.Leechers)
	}
	if first.Published.IsZero() || first.Source != "feedA" {
		t.Fatalf("published/source = %v/%q", first.Published, first.Source)
	}
	second := items[1]
	if !strings.HasPrefix(second.Magnet, "magnet:?xt=urn:btih:0123456789") {
		t.Fatalf("magnet from description not extracted: %q", second.Magnet)
	}
	if second.Download() != second.Magnet {
		t.Fatal("Download must prefer the magnet")
	}
}

func TestParseAtom(t *testing.T) {
	items, err := Parse(strings.NewReader(atomFeed), "atom")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Link != "http://t.example/a.torrent" {
		t.Fatalf("atom items = %+v", items)
	}
}

func TestRuleMatchAndFilter(t *testing.T) {
	rule := TitleFilter{Include: []string{"1080p", "2160p"}, Exclude: []string{"cam"}}
	cases := map[string]bool{
		"Movie 1080p":      true,
		"Movie 2160p":      true,
		"Movie 720p":       false,
		"Movie 1080p CAM":  false,
		"Movie 1080p x265": true,
	}
	for title, want := range cases {
		if got := rule.Match(title); got != want {
			t.Errorf("Match(%q) = %v, want %v", title, got, want)
		}
	}
	items := []Item{
		{Title: "A 1080p", Link: "http://t/a.torrent"},
		{Title: "B 720p", Link: "http://t/b.torrent"},
		{Title: "C 1080p"}, // no download target
		{Title: "D 1080p cam", Link: "http://t/d.torrent"},
	}
	kept := rule.Filter(items)
	if len(kept) != 1 || kept[0].Title != "A 1080p" {
		t.Fatalf("Filter = %+v", kept)
	}
}

func TestFetchRejectsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.Client(), srv.URL, "x"); err == nil {
		t.Fatal("a non-2xx answer must be an error")
	}
}
