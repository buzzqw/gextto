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

// TestParseReportsIndexerError fixes that an error document returned with HTTP
// 200 (Jackett/Prowlarr answer "Invalid API Key" this way) is reported as an
// error and not mistaken for an empty feed, which used to hide the failure.
func TestParseReportsIndexerError(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?><error code="100" description="Invalid API Key" />`
	_, err := Parse(strings.NewReader(body), "jackett")
	if err == nil || !strings.Contains(err.Error(), "Invalid API Key") {
		t.Fatalf("error = %v, want the indexer description", err)
	}
}

// TestParseEmptyFeedIsNotAnError keeps the honest empty answer (a real feed with
// no items) successful: only the error document is a failure.
func TestParseEmptyFeedIsNotAnError(t *testing.T) {
	items, err := Parse(strings.NewReader(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`), "jackett")
	if err != nil || len(items) != 0 {
		t.Fatalf("empty feed: items=%d err=%v", len(items), err)
	}
}

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

// TestSearchUsesGetHook: when Get is set (the standalone daemon routing through
// FlareSolverr), Search goes through it instead of the default HTTP client.
func TestSearchUsesGetHook(t *testing.T) {
	var gotURL string
	idx := Indexer{Name: "x", URL: "https://indexer.invalid", APIKey: "k", Get: func(_ context.Context, rawURL string) ([]byte, error) {
		gotURL = rawURL
		return []byte(sampleFeed), nil
	}}
	items, err := idx.Search(context.Background(), "movie")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items via the hook = %d, want 2", len(items))
	}
	if !strings.Contains(gotURL, "q=movie") || !strings.Contains(gotURL, "apikey=k") {
		t.Fatalf("hook URL = %q", gotURL)
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

func TestJackettEndpointAndFallback(t *testing.T) {
	// 1. URL with :9117 automatically routes to Jackett endpoint
	idx9117 := Indexer{Name: "jackett", URL: "http://192.168.1.161:9117"}
	if got := idx9117.Endpoint(); got != "http://192.168.1.161:9117/api/v2.0/indexers/all/results/torznab/api" {
		t.Fatalf("endpoint = %q", got)
	}

	// 2. Fallback when server returns 404 on /api but 200 on Jackett path
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/v2.0/indexers/all/results/torznab/api" && r.URL.Query().Get("t") == "caps" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server version="1.0"/></caps>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	customIdx := Indexer{Name: "custom", URL: srv.URL}
	if err := customIdx.Ping(context.Background()); err != nil {
		t.Fatalf("ping should succeed via fallback: %v", err)
	}
}

// TestProwlarrAggregateSearch pins the Prowlarr shape: its aggregate search is
// the JSON API (`query`/`type`, not Torznab `t`/`q`), usenet results are
// skipped and a result with only an infohash still gets a magnet.
func TestProwlarrAggregateSearch(t *testing.T) {
	// Served under /prowlarr, like a reverse proxy: the URL names Prowlarr.
	srv := httptest.NewServer(http.StripPrefix("/prowlarr", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search":
			q := r.URL.Query()
			if q.Get("query") != "mandalorian" || q.Get("type") != "search" || q.Get("apikey") != "k" || q.Get("t") != "" {
				http.Error(w, "bad params: "+r.URL.RawQuery, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`[
 {"title":"The Mandalorian S02E07 1080p","size":2000000000,"seeders":40,"leechers":3,"magnetUrl":"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567","protocol":"torrent","publishDate":"2026-10-01T10:00:00Z"},
 {"title":"The Mandalorian S01 pack","size":9000000000,"seeders":12,"infoHash":"ABCDEF0123456789ABCDEF0123456789ABCDEF01","downloadUrl":"http://p/dl?link=x&file=y","protocol":"torrent"},
 {"title":"The Mandalorian nzb","size":1,"protocol":"usenet","downloadUrl":"http://p/nzb"}
]`))
		case "/api/v1/system/status":
			if r.URL.Query().Get("apikey") != "k" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"appName":"Prowlarr","version":"1.20.0"}`))
		default:
			http.NotFound(w, r)
		}
	})))
	defer srv.Close()
	base := srv.URL + "/prowlarr"
	idx := Indexer{Name: "prowlarr", URL: base, APIKey: "k"}
	if ep := idx.Endpoint(); ep != base+"/api/v1/search" {
		t.Fatalf("endpoint = %q", ep)
	}
	items, err := idx.Search(context.Background(), "mandalorian")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d (%+v), want 2 (usenet skipped)", len(items), items)
	}
	if items[0].Seeders != 40 || items[0].Leechers != 3 || items[0].Published.IsZero() || !strings.HasPrefix(items[0].Magnet, "magnet:") {
		t.Fatalf("first item = %+v", items[0])
	}
	if !strings.HasPrefix(items[1].Magnet, "magnet:?xt=urn:btih:abcdef0123456789") || items[1].Leechers != -1 {
		t.Fatalf("infohash item = %+v", items[1])
	}
	if err := idx.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := (Indexer{Name: "prowlarr", URL: base, APIKey: "bad"}).Ping(context.Background()); err == nil {
		t.Fatal("a wrong API key must fail the Prowlarr ping")
	}
}

// TestParseToleratesStrayAmpersand: a tracker title or URL with a raw "&" or an
// HTML entity must not make the whole feed unreadable.
func TestParseToleratesStrayAmpersand(t *testing.T) {
	body := `<?xml version="1.0"?><rss version="2.0"><channel>` +
		`<item><title>Tom &amp; Jerry &egrave; S01E01</title><link>http://t/dl?id=1&link=2</link></item>` +
		`<item><title>Other S01E02</title><link>http://t/dl?id=2</link></item>` +
		`</channel></rss>`
	items, err := Parse(strings.NewReader(body), "x")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 2 || items[0].Link != "http://t/dl?id=1&link=2" {
		t.Fatalf("items = %+v", items)
	}
}
