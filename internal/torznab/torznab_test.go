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

func TestIndexerPing(t *testing.T) {
	caps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "caps" {
			http.Error(w, "want t=caps", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server version="1.0"/></caps>`))
	}))
	defer caps.Close()
	if err := (Indexer{Name: "caps", URL: caps.URL}).Ping(context.Background()); err != nil {
		t.Fatalf("a caps document must pass the ping: %v", err)
	}

	// A reachable but non-Torznab page (login HTML) must not pass.
	htmlPage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body>login</body></html>`))
	}))
	defer htmlPage.Close()
	if err := (Indexer{Name: "html", URL: htmlPage.URL}).Ping(context.Background()); err == nil {
		t.Fatal("a page without a caps document must fail the ping")
	}

	// An HTTP error must fail.
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer down.Close()
	if err := (Indexer{Name: "down", URL: down.URL}).Ping(context.Background()); err == nil {
		t.Fatal("an HTTP 500 must fail the ping")
	}
}

// TestMagnetLinkBecomesMagnet covers the MirCrew shape: the magnet is in
// link/enclosure with no magneturl attribute, so Item.Magnet must be populated
// (otherwise the caller tries to fetch a magnet: URL over HTTP).
func TestMagnetLinkBecomesMagnet(t *testing.T) {
	body := `<?xml version="1.0"?><rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>` +
		`<item><title>FBI.Most.Wanted-1x02-DLMux-1080p.mkv</title><pubDate>Mon, 05 Oct 2026 10:17:44 +0000</pubDate>` +
		`<link>magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=FBI</link>` +
		`<enclosure url="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=FBI" length="0" type="application/x-bittorrent"/>` +
		`<torznab:attr name="seeders" value="0"/></item></channel></rss>`
	items, err := Parse(strings.NewReader(body), "mircrew")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	if !strings.HasPrefix(items[0].Magnet, "magnet:") {
		t.Fatalf("a magnet link must populate Magnet, got %+v", items[0])
	}
	if got := items[0].Download(); !strings.HasPrefix(got, "magnet:") {
		t.Fatalf("Download() = %q, want a magnet", got)
	}
}
