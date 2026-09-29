package gextto

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProwlarrURLRequestsAFullPage(t *testing.T) {
	indexer := IndexerConfig{Name: "Prowlarr", URL: "http://host:9696", APIKey: "secret", Enabled: true}
	full := torznabRequestURL(indexer, "Show S01E02", nil)
	for _, fragment := range []string{"/api/v1/search?", "query=Show+S01E02", "type=search", "limit=100", "apikey=secret"} {
		if !strings.Contains(full, fragment) {
			t.Fatalf("Prowlarr URL %q missing %q", full, fragment)
		}
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
