package torznab

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const capsDocument = `<?xml version="1.0"?><caps>
 <server version="1.0"/>
 <limits max="50" default="25"/>
 <searching>
  <search available="yes" supportedParams="q,season"/>
  <tv-search available="yes" supportedParams="q,season,ep,tvdbid"/>
  <movie-search available="no" supportedParams="q"/>
 </searching>
</caps>`

// TestCapsAwareSearchBuildsOnlyDeclaredParameters pins the reason for the caps
// negotiation: a Jackett aggregate rejects tmdbid with error 203 and Prowlarr
// returns nothing for an id its indexers do not support.
func TestCapsAwareSearchBuildsOnlyDeclaredParameters(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") == "caps" {
			_, _ = w.Write([]byte(capsDocument))
			return
		}
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()

	idx := Indexer{Name: "jackett", URL: srv.URL, APIKey: "k"}
	items, err := idx.SearchWith(context.Background(), SearchQuery{
		Text: "Show S02E04 1080p",
		IDs:  [][2]string{{"tvdbid", "81189"}, {"tmdbid", "123"}},
	})
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	if len(queries) != 1 {
		t.Fatalf("queries = %d, want one search after the caps read", len(queries))
	}
	got := queries[0]
	for _, want := range []string{"t=tvsearch", "season=2", "ep=4", "tvdbid=81189", "limit=50", "extended=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("query %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "tmdbid=") {
		t.Fatalf("undeclared tmdbid was sent: %q", got)
	}
	if strings.Contains(got, "live_wait=") {
		t.Fatalf("a non-indexarr search must not carry live_wait: %q", got)
	}
}

// TestCapsUnavailableFunctionFallsBackToGenericSearch: a caps document marking
// movie-search unavailable must not make the client send t=movie to that indexer.
func TestCapsUnavailableFunctionFallsBackToGenericSearch(t *testing.T) {
	var searchType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = w.Write([]byte(capsDocument))
			return
		}
		searchType = r.URL.Query().Get("t")
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()
	if _, err := (Indexer{Name: "x", URL: srv.URL}).SearchWith(context.Background(), SearchQuery{Text: "Movie 2024", Type: "movie"}); err != nil {
		t.Fatal(err)
	}
	if searchType != "search" {
		t.Fatalf("search type = %q, want the generic fallback", searchType)
	}
}

// TestIndexarrGetsLiveWaitAndCappedPage: indexarr merges slower sources, so the
// first answer needs a longer window, and its caps max bounds the page size.
func TestIndexarrGetsLiveWaitAndCappedPage(t *testing.T) {
	var searchQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") == "caps" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><limits max="30" default="25"/><searching><search available="yes" supportedParams="q"/></searching></caps>`))
			return
		}
		searchQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()
	if _, err := (Indexer{Name: "indexarr", URL: srv.URL}).Search(context.Background(), "title"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"live_wait=45", "limit=30"} {
		if !strings.Contains(searchQuery, want) {
			t.Fatalf("query %q does not contain %q", searchQuery, want)
		}
	}
}

// TestUnsupportedParameterRetriesWithoutOptionalFields: Torznab error 203 is a
// request rejection, not an empty result. The client must repeat once without
// extended/id parameters instead of reporting a failed search.
func TestUnsupportedParameterRetriesWithoutOptionalFields(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") == "caps" {
			_, _ = w.Write([]byte(capsDocument))
			return
		}
		requests = append(requests, r.URL.RawQuery)
		if len(requests) == 1 {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><error code="203" description="Function not available"/>`))
			return
		}
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()
	items, err := (Indexer{Name: "x", URL: srv.URL}).SearchWith(context.Background(), SearchQuery{Text: "title", IDs: [][2]string{{"tvdbid", "81189"}}})
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want a retry", len(requests))
	}
	if strings.Contains(requests[1], "extended=") || strings.Contains(requests[1], "tvdbid=") {
		t.Fatalf("retry still carries optional fields: %q", requests[1])
	}
}

// TestProwlarrCapsIntersectOnlyCommonIDs: Prowlarr's aggregate has no Torznab
// caps document, so the client reads /api/v1/indexer and keeps only the params
// shared by every enabled indexer. An unsupported id must never be sent.
func TestProwlarrCapsIntersectOnlyCommonIDs(t *testing.T) {
	var query, searchType string
	srv := httptest.NewServer(http.StripPrefix("/prowlarr", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/indexer":
			_, _ = w.Write([]byte(`[
 {"enable":true,"capabilities":{"searchParams":["q"],"tvSearchParams":["q","season","tvdbid"],"movieSearchParams":["q"]}},
 {"enable":true,"capabilities":{"searchParams":["q"],"tvSearchParams":["q","season","imdbid"],"movieSearchParams":["q"]}}
]`))
		case "/api/v1/search":
			query = r.URL.Query().Get("query")
			searchType = r.URL.Query().Get("type")
			_, _ = w.Write([]byte(fmt.Sprintf(`[{"title":"A release","size":1,"protocol":"torrent","magnetUrl":"magnet:?xt=urn:btih:%040d"}]`, 1)))
		default:
			http.NotFound(w, r)
		}
	})))
	defer srv.Close()

	idx := Indexer{Name: "prowlarr", URL: srv.URL + "/prowlarr", APIKey: "k"}
	items, err := idx.SearchWith(context.Background(), SearchQuery{
		Text: "Show S01E01",
		IDs:  [][2]string{{"tvdbid", "81189"}, {"imdbid", "tt123"}},
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	if searchType != "tvsearch" {
		t.Fatalf("type = %q, want tvsearch", searchType)
	}
	if !strings.Contains(query, "Show S01E01") || strings.Contains(query, "{tvdbid:") || strings.Contains(query, "{imdbid:") {
		t.Fatalf("Prowlarr query = %q, want the bare title (no shared id)", query)
	}
}
