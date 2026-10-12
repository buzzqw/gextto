package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buzzqw/gextto/internal/torznab"
)

const searchFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>B</title><enclosure url="http://t/b.torrent" length="20"/>
<torznab:attr name="seeders" value="5"/></item>
<item><title>A</title><enclosure url="http://t/a.torrent" length="10"/>
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
	if items[0].Title != "A" || items[0].Seeders != 42 {
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

// TestUIRssSuggest checks the follow box suggestions: release titles are reduced
// to unique series names, most frequent first, title-cased.
func TestUIRssSuggest(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(suggestFeed))
	}))
	defer feed.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"`+feed.URL+`","apikey":"k"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/rss-suggest?q=the")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var names []string
	if err := json.NewDecoder(resp.Body).Decode(&names); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "The Last Of Us" || names[1] != "Breaking Bad" {
		t.Fatalf("suggestions = %v, want [The Last Of Us Breaking Bad]", names)
	}

	// A one-character query is ignored (no indexer hit, no suggestions).
	resp2, err := http.Get(srv.URL + "/ui/rss-suggest?q=t")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var short []string
	if err := json.NewDecoder(resp2.Body).Decode(&short); err != nil {
		t.Fatal(err)
	}
	if len(short) != 0 {
		t.Fatalf("a one-character query must return no suggestions, got %v", short)
	}
}
