package gextto

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProwlarrURLRequestsAFullPage(t *testing.T) {
	indexer := IndexerConfig{Name: "Prowlarr", URL: "http://host:9696", APIKey: "secret", Enabled: true}
	full := torznabRequestURL(indexer, "Show S01E02", nil)
	for _, fragment := range []string{"/api/v1/search?", "query=Show+S01E02%7Bseason%3A1%7D%7Bepisode%3A2%7D", "type=tvsearch", "limit=100", "apikey=secret"} {
		if !strings.Contains(full, fragment) {
			t.Fatalf("Prowlarr URL %q missing %q", full, fragment)
		}
	}
}

func TestManagerRequestsUseStructuredTVAndMovieSearches(t *testing.T) {
	prowlarr := IndexerConfig{Name: "Prowlarr", URL: "http://host:9696", APIKey: "secret", Enabled: true}
	full := torznabRequestURLForType(prowlarr, "Example Show", [][2]string{{"tvdbid", "123"}, {"tmdbid", "456"}}, searchTypeTV)
	parsed, err := url.Parse(full)
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Query()
	if values.Get("type") != "tvsearch" || values.Get("query") != "Example Show{tvdbid:123}{tmdbid:456}" {
		t.Fatalf("Prowlarr TV request = %q", full)
	}

	jackett := IndexerConfig{Name: "Jackett", URL: "http://host:9117", APIKey: "secret", Enabled: true}
	full = torznabRequestURLForType(jackett, "Example Film 2026", [][2]string{{"tmdbid", "789"}}, searchTypeMovie)
	parsed, err = url.Parse(full)
	if err != nil {
		t.Fatal(err)
	}
	values = parsed.Query()
	if values.Get("t") != "movie" || values.Get("tmdbid") != "789" {
		t.Fatalf("Jackett movie request = %q", full)
	}
}

func TestJackettURLUsesTvsearchForEpisodes(t *testing.T) {
	indexer := IndexerConfig{Name: "Jackett", URL: "http://host:9117", APIKey: "secret", Enabled: true}
	full := torznabRequestURL(indexer, "Show S01E02", nil)
	for _, fragment := range []string{"t=tvsearch", "season=1", "ep=2", "limit=100", "extended=1"} {
		if !strings.Contains(full, fragment) {
			t.Fatalf("Jackett URL %q missing %q", full, fragment)
		}
	}
	// A free-text query stays a plain search.
	free := torznabRequestURL(indexer, "The Veil 2024", nil)
	if !strings.Contains(free, "t=search") || strings.Contains(free, "season=") {
		t.Fatalf("free-text URL = %q", free)
	}
	// External ids are forwarded for tvsearch.
	withIDs := torznabRequestURL(indexer, "Show S01E02", [][2]string{{"imdbid", "tt1234"}})
	if !strings.Contains(withIDs, "imdbid=tt1234") {
		t.Fatalf("external id not forwarded: %q", withIDs)
	}
}

func TestManagerSeasonFallbackUsesStructuredSeasonSearch(t *testing.T) {
	ids := [][2]string{{"tvdbid", "123"}, {"tmdbid", "456"}}
	prowlarr := IndexerConfig{Name: "Prowlarr", URL: "http://host:9696", APIKey: "secret", Enabled: true}
	parsed, err := url.Parse(torznabRequestURLForType(prowlarr, "Example Show S02", ids, searchTypeTV))
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Query()
	if values.Get("type") != searchTypeTV || values.Get("query") != "Example Show S02{tvdbid:123}{tmdbid:456}{season:2}" {
		t.Fatalf("Prowlarr season request = %q", parsed.String())
	}

	jackett := IndexerConfig{Name: "Jackett", URL: "http://host:9117", APIKey: "secret", Enabled: true}
	parsed, err = url.Parse(torznabRequestURLForType(jackett, "Example Show S02", ids, searchTypeTV))
	if err != nil {
		t.Fatal(err)
	}
	values = parsed.Query()
	if values.Get("t") != searchTypeTV || values.Get("season") != "2" || values.Get("ep") != "" || values.Get("tvdbid") != "123" {
		t.Fatalf("Jackett season request = %q", parsed.String())
	}
}

func TestParseProwlarrJSONRebuildsMagnetFromInfoHash(t *testing.T) {
	body := `[
	  {"title":"Show S01E02 1080p WEB-DL","infoHash":"0123456789abcdef0123456789abcdef01234567","size":1500000000,"seeders":12,"leechers":3,"protocol":"torrent","indexer":"LimeTorrents"},
	  {"title":"Usenet Item","magnetUrl":"magnet:?xt=urn:btih:` + strings.Repeat("a", 40) + `","size":1500000000,"protocol":"usenet"},
	  {"title":"Sample Only","magnetUrl":"magnet:?xt=urn:btih:` + strings.Repeat("b", 40) + `","size":10000000,"protocol":"torrent"}
	]`
	releases, err := parse_prowlarr_json(body, "Prowlarr")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("got %d releases, want 1 (usenet and sample excluded)", len(releases))
	}
	release := releases[0]
	if !strings.Contains(release.Magnet, "0123456789abcdef0123456789abcdef01234567") {
		t.Fatalf("magnet not rebuilt from infohash: %q", release.Magnet)
	}
	if release.Source != "prowlarr:LimeTorrents" {
		t.Fatalf("source = %q", release.Source)
	}
	if release.Seeders != 12 || release.Peers != 3 || release.SizeBytes != 1500000000 {
		t.Fatalf("metadata = %+v", release)
	}
}

func TestProwlarrPreservesPublishDateAndV2InfoHash(t *testing.T) {
	hash := strings.Repeat("a", 64)
	body := `[{"title":"Show S01E02 1080p WEB-DL","infoHash":"` + hash + `","size":1500000000,"protocol":"torrent","publishDate":"2026-09-01T12:00:00Z"}]`
	releases, err := parse_prowlarr_json(body, "Prowlarr")
	if err != nil || len(releases) != 1 {
		t.Fatalf("parse = %+v, %v", releases, err)
	}
	if !strings.Contains(releases[0].Magnet, "urn:btmh:1220"+hash) {
		t.Fatalf("v2 magnet = %q", releases[0].Magnet)
	}
	if got := releases[0].DiscoveredAt.Format(time.RFC3339); got != "2026-09-01T12:00:00Z" {
		t.Fatalf("publish date = %q", got)
	}
}

func TestProwlarrPreservesTMDBID(t *testing.T) {
	body := `[{"title":"Example Film 1080p WEB-DL","infoHash":"` + strings.Repeat("e", 40) + `","tmdbId":987,"size":1500000000,"protocol":"torrent"}]`
	releases, err := parse_prowlarr_json(body, "Prowlarr")
	if err != nil || len(releases) != 1 {
		t.Fatalf("parse = %+v, %v", releases, err)
	}
	if got := releases[0].TmdbID; got != "987" {
		t.Fatalf("tmdb id = %q", got)
	}
}

func TestHealthProbeURLValidatesProwlarrKey(t *testing.T) {
	prowlarr := IndexerConfig{Name: "Prowlarr", URL: "http://host:9696", APIKey: "secret", Enabled: true}
	probe := HealthProbeURL(prowlarr)
	if !strings.Contains(probe, "/api/v1/system/status?apikey=secret") {
		t.Fatalf("prowlarr probe = %q", probe)
	}
	jackett := IndexerConfig{Name: "Jackett", URL: "http://host:9117", APIKey: "secret", Enabled: true}
	if got := HealthProbeURL(jackett); !strings.Contains(got, "t=caps&apikey=secret") {
		t.Fatalf("jackett probe = %q", got)
	}
}

func TestCanceledTorznabRequestDoesNotInvokeFlareSolverr(t *testing.T) {
	var fallbackCalls atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer fallback.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	indexer := IndexerConfig{Name: "Jackett", URL: fallback.URL, APIKey: "secret", Enabled: true}
	_, err := FetchTorznabFlareSolverr(ctx, indexer, "test", nil, &fallback.URL)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if calls := fallbackCalls.Load(); calls != 0 {
		t.Fatalf("FlareSolverr fallback called %d times after cancellation", calls)
	}
}

func TestResolveProwlarrMagnetsFollowsDownloadRedirect(t *testing.T) {
	const hash = "cccccccccccccccccccccccccccccccccccccccc"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/4/download" {
			w.Header().Set("Location", "magnet:?xt=urn:btih:"+hash)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	items := []map[string]any{{
		"title":       "LimeTorrents Show S01E01 720p",
		"protocol":    "torrent",
		"downloadUrl": server.URL + "/4/download",
		"size":        float64(1_500_000_000),
		"seeders":     float64(3),
	}}
	resolveProwlarrMagnets(context.Background(), items)
	if !strings.HasPrefix(jsonStringValue(items[0]["magnetUrl"]), "magnet:") {
		t.Fatalf("download redirect was not resolved: %v", items[0]["magnetUrl"])
	}
	releases := parseProwlarrItems(items, "prowlarr")
	if len(releases) != 1 || !strings.Contains(releases[0].Magnet, hash) {
		t.Fatalf("release not produced from the resolved magnet: %+v", releases)
	}
}

func TestProwlarrProxyMagnetAndTorrentURLAreRetained(t *testing.T) {
	const hash = "dddddddddddddddddddddddddddddddddddddddd"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("link") == "magnet" {
			w.Header().Set("Location", "magnet:?xt=urn:btih:"+hash)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write([]byte("torrent payload"))
	}))
	defer server.Close()

	magnetBody := `[{"title":"Show S01E01 1080p","protocol":"torrent","magnetUrl":"` + server.URL + `/12/download?link=magnet"}]`
	releases, err := parseProwlarrBody(context.Background(), magnetBody, "Prowlarr")
	if err != nil || len(releases) != 1 || !strings.Contains(releases[0].Magnet, hash) {
		t.Fatalf("proxy magnet releases = %+v, %v", releases, err)
	}

	torrentBody := `[{"title":"Show S01E02 1080p","protocol":"torrent","downloadUrl":"` + server.URL + `/12/download?link=torrent"}]`
	releases, err = parseProwlarrBody(context.Background(), torrentBody, "Prowlarr")
	if err != nil || len(releases) != 1 || releases[0].TorrentURL == nil {
		t.Fatalf("proxy torrent releases = %+v, %v", releases, err)
	}
}

// TestLiveProwlarrSearch exercises the real Prowlarr endpoint. It is opt-in via
// GEXTTO_TEST_PROWLARR_URL / GEXTTO_TEST_PROWLARR_KEY so no credentials are
// committed.
func TestLiveProwlarrSearch(t *testing.T) {
	url := os.Getenv("GEXTTO_TEST_PROWLARR_URL")
	key := os.Getenv("GEXTTO_TEST_PROWLARR_KEY")
	if url == "" || key == "" {
		t.Skip("set GEXTTO_TEST_PROWLARR_URL and GEXTTO_TEST_PROWLARR_KEY to run")
	}
	indexer := IndexerConfig{Name: "prowlarr", URL: url, APIKey: key, Enabled: true}
	releases, err := FetchTorznabFlareSolverr(context.Background(), indexer, "Howard Stern S01E01", nil, nil)
	if err != nil {
		t.Fatalf("prowlarr search: %v", err)
	}
	t.Logf("prowlarr releases: %d", len(releases))
	for index, release := range releases {
		if index >= 3 {
			break
		}
		t.Logf("  %s | %s", release.Source, release.Title)
	}
	if len(releases) == 0 {
		t.Fatal("prowlarr returned no usable releases")
	}
}
