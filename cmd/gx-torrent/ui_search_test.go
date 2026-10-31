package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

const suggestFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>The.Last.of.Us.S01E01.1080p</title><enclosure url="http://t/1.torrent" length="10"/></item>
<item><title>The.Last.of.Us.S01E02.1080p</title><enclosure url="http://t/2.torrent" length="10"/></item>
<item><title>Breaking.Bad.S05E14.720p</title><enclosure url="http://t/3.torrent" length="10"/></item>
<item><title>Some.Movie.2024.1080p</title><enclosure url="http://t/4.torrent" length="10"/></item>
</channel></rss>`

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
	feed := func(w http.ResponseWriter, _ *http.Request) {
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
