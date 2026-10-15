package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFetchReadsMagnetListing(t *testing.T) {
	page := `<!DOCTYPE html><html><head><meta charset="utf-8"><title>x</title></head><body>
<a class="torrent-title-link" href="/t/1">Show S01E01 1080p</a>
<a href="magnet:?xt=urn:btih:EF82F7DE7589CD261C28CD940F976EB4E4159C4B&amp;dn=Show.S01E01.1080p.mkv&amp;tr=udp%3A%2F%2Fx"></a>
<a href="magnet:?xt=urn:btih:ef82f7de7589cd261c28cd940f976eb4e4159c4b&amp;dn=dup"></a>
<a href="magnet:?xt=urn:btih:CCEF206758E0CEA51190A721B4EEE686BD4EDCF5&amp;dn=Other">Other.Release.720p</a>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()
	items, err := Fetch(context.Background(), srv.Client(), srv.URL, "ext")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (duplicate hash dropped): %+v", len(items), items)
	}
	if items[0].Title != "Show.S01E01.1080p.mkv" || !strings.Contains(items[0].Magnet, "&dn=") {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[0].GUID != "ef82f7de7589cd261c28cd940f976eb4e4159c4b" {
		t.Fatalf("guid = %q", items[0].GUID)
	}
	if items[1].Title != "Other.Release.720p" {
		t.Fatalf("second title = %q", items[1].Title)
	}
}

func TestFetchResolvesDetailPagesOnce(t *testing.T) {
	var detailHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/get-posts/user:X/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div class="tgxtablerow"><a class="txlight" href="/post-detail/1/a/"><b>Movie (2026) 2160p</b></a></div>
<div class="tgxtablerow"><a class="txlight" href="/post-detail/2/b/">No Magnet</a></div></body></html>`))
	})
	mux.HandleFunc("/post-detail/1/a/", func(w http.ResponseWriter, r *http.Request) {
		detailHits.Add(1)
		_, _ = w.Write([]byte(`<html><a href="magnet:?xt=urn:btih:A1686C635C2B9FE0955A7192D37F2014451561BF&amp;dn=Movie">m</a></html>`))
	})
	mux.HandleFunc("/post-detail/2/b/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>nothing</html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for round := 0; round < 2; round++ {
		items, err := Fetch(context.Background(), srv.Client(), srv.URL+"/get-posts/user:X/", "tgx")
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Title != "Movie (2026) 2160p" || !strings.HasPrefix(items[0].Magnet, "magnet:?xt=urn:btih:A1686C") {
			t.Fatalf("round %d items = %+v", round, items)
		}
		if items[0].GUID != srv.URL+"/post-detail/1/a/" {
			t.Fatalf("guid = %q", items[0].GUID)
		}
	}
	if got := detailHits.Load(); got != 1 {
		t.Fatalf("detail page fetched %d times, want 1 (cached)", got)
	}
}

func TestFetchFollowsAdvertisedFeed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head><link rel="alternate" type="application/rss+xml" href="/feed.xml"></head><body>hi</body></html>`))
	})
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss><channel><item><title>Feed Item</title><link>magnet:?xt=urn:btih:CCEF206758E0CEA51190A721B4EEE686BD4EDCF5</link></item></channel></rss>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	items, err := Fetch(context.Background(), srv.Client(), srv.URL+"/page", "site")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Feed Item" {
		t.Fatalf("items = %+v", items)
	}
}

// TestFetchHTMLWithoutTorrentsExplainsTheError: an HTML page that lists nothing
// is reported as such — it may be an empty listing, not necessarily an address
// that is not a feed.
func TestFetchHTMLWithoutTorrentsExplainsTheError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><meta charset="utf-8"></head><body>login</body></html>`))
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.Client(), srv.URL, "site")
	if err == nil || !strings.Contains(err.Error(), "no torrent link") {
		t.Fatalf("err = %v", err)
	}
}
