package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/torznab"
)

const searchFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>Movie B 1080p</title><enclosure url="http://t/b.torrent" length="20"/>
<torznab:attr name="seeders" value="5"/></item>
<item><title>Movie A 1080p</title><enclosure url="http://t/a.torrent" length="10"/>
<torznab:attr name="seeders" value="42"/></item>
</channel></rss>`

func TestUISearch(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(searchFeed))
	}))
	defer feed.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"`+feed.URL+`","apikey":"k"}]`)
	d.opts.DataDir = t.TempDir() // so the live results can be archived

	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/search?q=movie")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var items []torznab.Item
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Title != "Movie A 1080p" || items[0].Seeders != 42 {
		t.Fatalf("results not sorted by seeders: %+v", items)
	}
	if items[0].Indexer != "fake" {
		t.Fatalf("indexer = %q", items[0].Indexer)
	}
	// The archive page reads this header to tell "no indexer configured" from
	// "configured but no result".
	if got := resp.Header.Get("X-Gx-Indexers"); got != "1" {
		t.Fatalf("X-Gx-Indexers = %q, want 1", got)
	}

	// The live results are archived, so a later archive search finds them
	// locally without querying the indexers again (issue: they used to be
	// shown once and forgotten).
	found, err := d.archive().search(parseArchiveQuery("movie", "", false), "new", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Items) != 2 {
		t.Fatalf("live results not archived: %d entries", len(found.Items))
	}
}

// TestUISearchDropsUnrelatedResults checks the live indexer results are filtered
// to the titles carrying every search term, like the local archive section: an
// indexer that answers loosely must not flood the page with unrelated items.
func TestUISearchDropsUnrelatedResults(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(searchFeed))
	}))
	defer feed.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"`+feed.URL+`","apikey":"k"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/search?q=movie%20a")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var items []torznab.Item
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 || items[0].Title != "Movie A 1080p" {
		t.Fatalf("filter kept the wrong items: %+v", items)
	}
}

// TestUISearchIndexerCountHeader checks the count header is present even when
// no indexer is configured, so the archive page can report it accurately.
func TestUISearchIndexerCountHeader(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/search?q=movie")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Gx-Indexers"); got != "0" {
		t.Fatalf("X-Gx-Indexers = %q, want 0", got)
	}
}

func TestUISearchDisabledInManaged(t *testing.T) {
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/search?q=x", nil)
	d.handleUISearch(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("managed must not expose the search: %d", rec.Code)
	}
}

// TestUIRssSuggest checks the wanted-title typeahead: it reads the archive (fast)
// and reduces release titles to unique series names, most frequent first,
// title-cased. The indexers are not contacted while typing.
func TestUIRssSuggest(t *testing.T) {
	d := feedDaemon(t, "")
	if _, err := d.archive().record("feedA", []rss.Item{
		{Title: "The.Last.of.Us.S01E01.1080p", Magnet: "magnet:?xt=urn:btih:1111111111111111111111111111111111111111"},
		{Title: "The.Last.of.Us.S01E02.1080p", Magnet: "magnet:?xt=urn:btih:2222222222222222222222222222222222222222"},
		{Title: "Breaking.Bad.S05E14.720p", Magnet: "magnet:?xt=urn:btih:3333333333333333333333333333333333333333"},
		{Title: "Some.Movie.2024.1080p", Magnet: "magnet:?xt=urn:btih:4444444444444444444444444444444444444444"},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	suggest := func(q string) []string {
		t.Helper()
		resp, err := http.Get(srv.URL + "/ui/rss-suggest?q=" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var names []string
		if err := json.NewDecoder(resp.Body).Decode(&names); err != nil {
			t.Fatal(err)
		}
		return names
	}

	names := suggest("the")
	if len(names) != 1 || names[0] != "The Last Of Us" {
		t.Fatalf("suggestions = %v, want [The Last Of Us]", names)
	}
	if names := suggest("breaking"); len(names) != 1 || names[0] != "Breaking Bad" {
		t.Fatalf("suggestions = %v, want [Breaking Bad]", names)
	}
	// A one-character query is ignored (no suggestions).
	if names := suggest("t"); len(names) != 0 {
		t.Fatalf("a one-character query must return no suggestions, got %v", names)
	}
}

// TestUISearchQueriesIndexersInParallel checks that several indexers are
// queried together: with a sequential round the page waits for the sum of
// their times, and a slow indexer makes the search look stuck.
func TestUISearchQueriesIndexersInParallel(t *testing.T) {
	feed := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			// Capabilities are metadata, read once when the indexer is added;
			// the 300 ms delay below belongs to the actual search.
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><limits max="100"/><searching><search available="yes" supportedParams="q"/></searching></caps>`))
			return
		}
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(searchFeed))
	}
	a := httptest.NewServer(http.HandlerFunc(feed))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(feed))
	defer b.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"a","url":"`+a.URL+`","apikey":"k"},{"name":"b","url":"`+b.URL+`","apikey":"k"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	start := time.Now()
	resp, err := http.Get(srv.URL + "/ui/search?q=movie")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var items []torznab.Item
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("items = %d, want 4", len(items))
	}
	if elapsed > 550*time.Millisecond {
		t.Fatalf("search took %v: the indexers were queried sequentially", elapsed)
	}
}

func TestUITorrentRowMatchesQueryAcrossSearchFields(t *testing.T) {
	row := uiTorrentRow{
		Name:     "Example release",
		Hash:     "ABCDEF0123456789",
		Tracker:  "tracker.example",
		Category: "series",
		Tags:     "keep, archive",
	}
	for _, query := range []string{"example", "abcdef", "tracker.example", "series", "archive"} {
		if !uiTorrentRowMatchesQuery(row, query) {
			t.Fatalf("row did not match %q", query)
		}
	}
	for _, query := range []string{"missing", "movie", "other.example"} {
		if uiTorrentRowMatchesQuery(row, query) {
			t.Fatalf("row unexpectedly matched %q", query)
		}
	}
}

// TestUIRowsServerSideQuery checks the mobile path: the top-bar filter sends
// `rows=all&q=…` to /ui/rows, so a match past the desktop's 300-row window is
// still found and the count describes the filtered result, not the whole set.
func TestUIRowsServerSideQuery(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "src")
	for _, name := range []string{"Alpha.Release.bin", "Beta.Release.bin"} {
		if _, _, err := d.add(addRequest{
			TorrentData: makeTorrent(t, src, name, 80_000),
			Destination: src, SeedRatio: -1, SeedDays: -1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	d.list() // publish the snapshot now, so the assertion is not a ticker race.

	fetch := func(query string) struct {
		Rows       []struct{ Name string }
		RowsTotal  int
	} {
		t.Helper()
		resp, err := http.Get(server.URL + "/ui/rows?rows=all" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /ui/rows%s -> %d", query, resp.StatusCode)
		}
		var page struct {
			Rows      []struct{ Name string }
			RowsTotal int
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page
	}

	if page := fetch(""); len(page.Rows) != 2 || page.RowsTotal != 2 {
		t.Fatalf("without a query: rows=%d total=%d, want 2 and 2", len(page.Rows), page.RowsTotal)
	}
	if page := fetch("&q=alpha"); len(page.Rows) != 1 || page.RowsTotal != 1 || page.Rows[0].Name != "Alpha.Release.bin" {
		t.Fatalf("q=alpha: rows=%+v total=%d", page.Rows, page.RowsTotal)
	}
	if page := fetch("&q=ALPHA"); len(page.Rows) != 1 || page.RowsTotal != 1 {
		t.Fatalf("the query must be case-insensitive: rows=%+v", page.Rows)
	}
	if page := fetch("&q=missing"); len(page.Rows) != 0 || page.RowsTotal != 0 {
		t.Fatalf("q=missing: rows=%+v total=%d", page.Rows, page.RowsTotal)
	}
}
