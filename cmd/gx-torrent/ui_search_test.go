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
