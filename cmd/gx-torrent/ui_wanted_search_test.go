package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// wantedIndexerFeed is a Torznab answer with real episode titles, so the
// suggestion reducer (which needs an episode) can turn it into series names.
const wantedIndexerFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>The Mandalorian S01E01 1080p WEB-DL</title><enclosure url="http://t/1.torrent" length="10"/>
<torznab:attr name="seeders" value="42"/></item>
<item><title>The Mandalorian S01E02 1080p WEB-DL</title><enclosure url="http://t/2.torrent" length="10"/>
<torznab:attr name="seeders" value="30"/></item>
</channel></rss>`

// TestUIWantedSearchQueriesIndexers fixes that "Search in feeds" (the wanted
// title step) really asks the configured indexers, not just the RSS feeds: an
// indexer-only daemon must still return the series name.
func TestUIWantedSearchQueriesIndexers(t *testing.T) {
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(wantedIndexerFeed))
	}))
	defer indexer.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"`+indexer.URL+`","apikey":"k"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=mandalorian")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Indexers != 1 || result.IndexerHits == 0 {
		t.Fatalf("indexer not queried: %+v", result)
	}
	found := false
	for _, name := range result.Names {
		if name == "The Mandalorian" {
			found = true
		}
	}
	if !found {
		t.Fatalf("indexer results missing from %v", result.Names)
	}
}

// TestUIWantedSearchReportsWhenNoIndexerIsConfigured fixes the diagnostic that
// answers "seems to search only the feeds": with no indexer the page must be
// able to say so instead of silently returning feed names only.
func TestUIWantedSearchReportsWhenNoIndexerIsConfigured(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=mandalorian")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Indexers != 0 || result.IndexerHits != 0 {
		t.Fatalf("no indexer expected: %+v", result)
	}
}
