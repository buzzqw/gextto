package gextto

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Jackett's "all" aggregate caps, as served by the live instance.
const jackettAllCaps = `<?xml version="1.0" encoding="UTF-8"?><caps><searching>
<search available="yes" supportedParams="q" searchEngine="raw" />
<tv-search available="yes" supportedParams="q,season,ep" searchEngine="raw" />
<movie-search available="yes" supportedParams="q,imdbid" searchEngine="raw" />
</searching></caps>`

const oneTorznabItem = `<rss><channel><item><title>Example.Show.S02E03.1080p.WEB-DL.ITA</title><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link></item></channel></rss>`

// jackettLike answers t=caps (when caps is set) and rejects a tmdbid in a
// tv/movie search exactly like Jackett does (HTTP 400, Torznab error 203).
func jackettLike(t *testing.T, caps string) (*httptest.Server, *[]url.Values) {
	t.Helper()
	var mu sync.Mutex
	requests := []url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		if values.Get("t") == "caps" {
			if caps == "" {
				http.Error(w, "no caps", http.StatusInternalServerError)
				return
			}
			_, _ = io.WriteString(w, caps)
			return
		}
		mu.Lock()
		requests = append(requests, values)
		mu.Unlock()
		if values.Get("tmdbid") != "" && values.Get("t") != "search" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><error code="203" description="Function Not Available: tmdbid is not supported for TV search by this indexer" />`)
			return
		}
		_, _ = io.WriteString(w, oneTorznabItem)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func TestJackettSearchSendsOnlyDeclaredParams(t *testing.T) {
	server, requests := jackettLike(t, jackettAllCaps)
	indexer := IndexerConfig{Name: "jackett", URL: server.URL + "/api", APIKey: "k", Enabled: true, Manager: ManagerJackett}
	ids := [][2]string{{"tvdbid", "123"}, {"tmdbid", "456"}}
	releases, err := fetchTorznabFlareSolverr(context.Background(), indexer, "Example Show S02E03", ids, nil, searchTypeTV)
	if err != nil || len(releases) != 1 {
		t.Fatalf("releases = %d, err = %v", len(releases), err)
	}
	if len(*requests) != 1 {
		t.Fatalf("requests = %d, want 1 (no rejected first try)", len(*requests))
	}
	got := (*requests)[0]
	if got.Get("tmdbid") != "" || got.Get("tvdbid") != "" || got.Get("season") != "2" || got.Get("ep") != "3" || got.Get("t") != "tvsearch" {
		t.Fatalf("request = %v, want season/ep and no undeclared ids", got)
	}
	// imdbid is declared for movies and stays; tmdbid does not.
	full := torznabRequestURLForType(indexer, "Example Film 2026", [][2]string{{"imdbid", "tt1"}, {"tmdbid", "9"}}, searchTypeMovie)
	if !strings.Contains(full, "imdbid=tt1") || strings.Contains(full, "tmdbid") {
		t.Fatalf("movie request = %s", full)
	}
}

func TestJackettRejectedIDIsRetriedWithoutIt(t *testing.T) {
	// Caps unreadable: the request goes out as before, is rejected with
	// Torznab 203 and repeated with the title only.
	server, requests := jackettLike(t, "")
	indexer := IndexerConfig{Name: "jackett", URL: server.URL + "/api", APIKey: "k", Enabled: true, Manager: ManagerJackett}
	releases, err := fetchTorznabFlareSolverr(context.Background(), indexer, "Example Show S02E03", [][2]string{{"tmdbid", "456"}}, nil, searchTypeTV)
	if err != nil || len(releases) != 1 {
		t.Fatalf("releases = %d, err = %v", len(releases), err)
	}
	if len(*requests) != 2 || (*requests)[0].Get("tmdbid") != "456" || (*requests)[1].Get("tmdbid") != "" {
		t.Fatalf("requests = %v, want the id try then the title-only retry", *requests)
	}
}

func TestTorznabHTTPErrorKeepsIndexerExplanation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, jackettAllCaps)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `<error code="100" description="Invalid API Key" />`)
	}))
	defer server.Close()
	indexer := IndexerConfig{Name: "jackett", URL: server.URL + "/api", APIKey: "bad", Enabled: true, Manager: ManagerJackett}
	_, err := fetchTorznabFlareSolverr(context.Background(), indexer, "Example", nil, nil, searchTypeGeneric)
	if err == nil || err.Error() != "HTTP 400: Torznab 100: Invalid API Key" {
		t.Fatalf("error = %v", err)
	}
}

// prowlarrLike serves the indexer list of the live instance (no enabled
// indexer can search by tvdbid/tmdbid) and answers an id search with nothing.
func prowlarrLike(t *testing.T, indexerList string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	queries := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/indexer" {
			_, _ = io.WriteString(w, indexerList)
			return
		}
		query := r.URL.Query().Get("query")
		mu.Lock()
		queries = append(queries, query)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(query, "id:") {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		_, _ = io.WriteString(w, `[{"title":"Example.Show.S02E03.1080p.WEB-DL.ITA","magnetUrl":"magnet:?xt=urn:btih:0123456789012345678901234567890123456789","indexer":"LimeTorrents","protocol":"torrent"}]`)
	}))
	t.Cleanup(server.Close)
	return server, &queries
}

const liveProwlarrIndexers = `[
 {"name":"Knaben","enable":false,"capabilities":{"searchParams":["q"],"tvSearchParams":["q","season","ep","imdbId","tvdbId"],"movieSearchParams":["q","imdbId","tmdbId"]}},
 {"name":"LimeTorrents","enable":true,"capabilities":{"searchParams":["q"],"tvSearchParams":["q","season","ep"],"movieSearchParams":["q"]}},
 {"name":"TorrentLeech","enable":true,"capabilities":{"searchParams":["q"],"tvSearchParams":["q","season","ep"],"movieSearchParams":["q","imdbId"]}}
]`

func TestProwlarrSearchDropsIDsItsIndexersCannotUse(t *testing.T) {
	server, queries := prowlarrLike(t, liveProwlarrIndexers)
	indexer := IndexerConfig{Name: "prowlarr", URL: server.URL, APIKey: "k", Enabled: true, Manager: ManagerProwlarr}
	ids := [][2]string{{"tvdbid", "123"}, {"tmdbid", "456"}}
	releases, err := fetchTorznabFlareSolverr(context.Background(), indexer, "Example Show S02E03", ids, nil, searchTypeTV)
	if err != nil || len(releases) != 1 {
		t.Fatalf("releases = %d, err = %v", len(releases), err)
	}
	if strings.Join(*queries, "|") != "Example Show S02E03{season:2}{episode:3}" {
		t.Fatalf("queries = %q, want one title search with season/episode", *queries)
	}
}

func TestProwlarrKeepsAnIDEveryIndexerSupports(t *testing.T) {
	caps := parseProwlarrCaps(`[
	 {"enable":true,"capabilities":{"movieSearchParams":["q","imdbId"]}},
	 {"enable":true,"capabilities":{"movieSearchParams":["q","imdbId","tmdbId"]}},
	 {"enable":false,"capabilities":{"movieSearchParams":["q"]}}
	]`)
	if !caps.supports(searchTypeMovie, "imdbid") || caps.supports(searchTypeMovie, "tmdbid") {
		t.Fatalf("caps = %+v, want imdbid only", caps)
	}
	if parseProwlarrCaps(`[{"enable":false}]`) != nil || parseProwlarrCaps(`not json`) != nil {
		t.Fatal("unknown caps must be nil (keep every parameter)")
	}
}

func TestParseTorznabCaps(t *testing.T) {
	caps := parseTorznabCaps(jackettAllCaps)
	if caps == nil || !caps.supports(searchTypeTV, "season") || caps.supports(searchTypeTV, "tmdbid") || !caps.supports(searchTypeMovie, "imdbid") {
		t.Fatalf("caps = %+v", caps)
	}
	noTV := parseTorznabCaps(`<caps><searching><search available="yes" supportedParams="q"/><tv-search available="no" supportedParams="q"/></searching></caps>`)
	if noTV.functionAvailable(searchTypeTV) {
		t.Fatal("tv-search available=no must be honoured")
	}
	if parseTorznabCaps(`<rss/>`) != nil {
		t.Fatal("a reply without <searching> is unknown caps")
	}
	var unknown *searchCaps
	if !unknown.supports(searchTypeTV, "tmdbid") || !unknown.functionAvailable(searchTypeMovie) {
		t.Fatal("unknown caps must keep every parameter")
	}
	if !torznabUnsupportedParam("Torznab 203: Function Not Available") || torznabUnsupportedParam("Torznab 100: Invalid API Key") {
		t.Fatal("only 201/203 mean an unsupported parameter")
	}
}
