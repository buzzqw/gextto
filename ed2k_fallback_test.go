package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/amule/amuletest"
	"github.com/buzzqw/gextto/internal/models"
)

// ed2kFallbackState builds a test application whose configuration uses the fake
// aMule core, with the eD2k integration and the fallback switch on.
func ed2kFallbackState(t *testing.T, coreCfg *Config) *AppState {
	t.Helper()
	state := newTestAppState(t)
	cfg := *state.cfg
	cfg.Settings = map[string]string{}
	for key, value := range state.cfg.Settings {
		cfg.Settings[key] = value
	}
	for key, value := range coreCfg.Settings {
		cfg.Settings[key] = value
	}
	state.config_cache = &ConfigCache{
		generation: ConfigGeneration(),
		cfg:        &cfg,
		valid:      true,
	}
	return state
}

// TestManualED2KFallbackForEpisode covers the shared helper: only the episode
// asked for, the canonical series name, the ed2k link kept for the download.
func TestManualED2KFallbackForEpisode(t *testing.T) {
	core, cfg := ed2kTestCore(t, map[string]string{"ed2k_fallback": "true"})
	core.SetResults([]amuletest.Result{
		{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Scrubs S02E01 720p ITA.mkv", Size: 900000000, Sources: 4},
		{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "Scrubs S02E02 720p ITA.mkv", Size: 900000000, Sources: 4},
		// Another show with the same episode number: it must not pass as Scrubs.
		{Hash: "dddddddddddddddddddddddddddddddd", Name: "Scrubs Interns S02E01 720p ITA.mkv", Size: 300000000, Sources: 9},
	}, "")
	series := &SeriesConfig{Name: "Scrubs"}
	got := ed2kManualEpisodeFallback(context.Background(), &Engine{}, cfg, series, 2, 1)
	if len(got) != 1 {
		t.Fatalf("fallback returned %d releases, want 1: %+v", len(got), got)
	}
	if got[0].Series == nil || *got[0].Series != "Scrubs" {
		t.Fatalf("series = %v, want Scrubs", got[0].Series)
	}
	if !strings.HasPrefix(got[0].Ed2kLink, "ed2k://|file|Scrubs") {
		t.Fatalf("ed2k link = %q", got[0].Ed2kLink)
	}

	// The fallback is an explicit switch: with it off no eD2k search runs.
	before := len(core.Searches())
	cfg.Settings["ed2k_fallback"] = "false"
	if got := ed2kManualEpisodeFallback(context.Background(), &Engine{}, cfg, series, 2, 1); len(got) != 0 {
		t.Fatalf("fallback ran with the switch off: %+v", got)
	}
	if searches := core.Searches(); len(searches) != before {
		t.Fatalf("searches with the switch off = %v", searches)
	}
}

// TestEpisodeSearchFallsBackToED2K is the operator path: the online search for
// one episode finds nothing on torrents and then asks eD2k, keeping the origin
// and the link the interface needs to queue it.
func TestEpisodeSearchFallsBackToED2K(t *testing.T) {
	core, coreCfg := ed2kTestCore(t, map[string]string{"ed2k_fallback": "true"})
	core.SetResults([]amuletest.Result{{
		Hash: "cccccccccccccccccccccccccccccccc", Name: "Scrubs S02E01 720p ITA.mkv", Size: 900000000, Sources: 6,
	}}, "")
	state := ed2kFallbackState(t, coreCfg)
	cfg := state.config_cache.cfg
	cfg.Series = []SeriesConfig{{Name: "Scrubs", Seasons: "2", Enabled: true}}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, body := webPostJSON(t, server, "/api/episodes/Scrubs/2/1/search", `{}`)
	if status != http.StatusOK {
		t.Fatalf("episode search -> %d: %s", status, body)
	}
	var payload struct {
		Results []struct {
			Origin  string         `json:"origin"`
			Release map[string]any `json:"release"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response: %v (%s)", err, body)
	}
	if len(payload.Results) == 0 {
		t.Fatalf("no results: %s", body)
	}
	found := false
	for _, result := range payload.Results {
		if result.Origin != episodeOriginED2K {
			continue
		}
		found = true
		if link := v2String(result.Release["ed2k_link"]); !strings.HasPrefix(link, "ed2k://|file|Scrubs") {
			t.Fatalf("ed2k link = %q", link)
		}
	}
	if !found {
		t.Fatalf("no eD2k result in the episode search: %s", body)
	}
}

// TestED2KFallbackRunsWhenStoredRowsAreRejected guards the subtle case that
// made the manual search return nothing: a stored release that matches the
// episode but is rejected by the series quality must not hide the eD2k
// fallback.
func TestED2KFallbackRunsWhenStoredRowsAreRejected(t *testing.T) {
	core, coreCfg := ed2kTestCore(t, map[string]string{"ed2k_fallback": "true"})
	core.SetResults([]amuletest.Result{{
		Hash: "88888888888888888888888888888888", Name: "Scrubs S02E01 1080p ITA.mkv", Size: 900000000, Sources: 6,
	}}, "")
	state := ed2kFallbackState(t, coreCfg)
	cfg := state.config_cache.cfg
	cfg.Series = []SeriesConfig{{Name: "Scrubs", Seasons: "2", Quality: "1080p", Enabled: true}}
	stored := ParseRelease("Scrubs S02E01 480p.mkv", "magnet:?xt=urn:btih:"+strings.Repeat("1", 40), "test")
	if stored == nil {
		t.Fatal("could not build the stored release")
	}
	if err := state.archive.SaveBatch([]models.Release{*stored}, cfg); err != nil {
		t.Fatalf("save stored release: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, body := webPostJSON(t, server, "/api/episodes/Scrubs/2/1/search", `{}`)
	if status != http.StatusOK {
		t.Fatalf("episode search -> %d: %s", status, body)
	}
	if !strings.Contains(string(body), `"origin":"eD2k"`) || !strings.Contains(string(body), "ed2k://|file|Scrubs") {
		t.Fatalf("eD2k fallback did not run after a rejected stored row: %s", body)
	}
}

// TestSeriesSearchMissingFallsBackToED2K is the exact action the operator used:
// "Cerca mancanti" on a series. The torrent plane returns nothing, so every
// gap must be looked up on eD2k too.
func TestSeriesSearchMissingFallsBackToED2K(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/1399" {
			t.Fatalf("TMDB path = %q, want /tv/1399", r.URL.Path)
		}
		tmdbWriteJSON(t, w, `{"seasons":[{"season_number":2,"episode_count":3}]}`)
	})
	core, coreCfg := ed2kTestCore(t, map[string]string{"ed2k_fallback": "true"})
	core.SetResults([]amuletest.Result{
		{Hash: "dddddddddddddddddddddddddddddddd", Name: "Scrubs S02E01 720p ITA.mkv", Size: 900000000, Sources: 5},
		{Hash: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Name: "Scrubs S02E02 720p ITA.mkv", Size: 900000000, Sources: 5},
		{Hash: "ffffffffffffffffffffffffffffffff", Name: "Scrubs S02E03 720p ITA.mkv", Size: 900000000, Sources: 5},
		{Hash: "abababababababababababababababab", Name: "Scrubs Interns S02E02 720p ITA.mkv", Size: 300000000, Sources: 9},
	}, "")
	state := ed2kFallbackState(t, coreCfg)
	key := "test-key"
	cfg := state.config_cache.cfg
	cfg.TmdbAPIKey = &key
	cfg.Series = []SeriesConfig{{Name: "Scrubs", Seasons: "2", TmdbID: "1399", Enabled: true}}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, body := webPostJSON(t, server, "/api/series/Scrubs/search-missing", `{}`)
	if status != http.StatusOK {
		t.Fatalf("search missing -> %d: %s", status, body)
	}
	var payload struct {
		Results []struct {
			Origin  string         `json:"origin"`
			Season  int64          `json:"season"`
			Episode int64          `json:"episode"`
			Release map[string]any `json:"release"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response: %v (%s)", err, body)
	}
	episodes := map[int64]bool{}
	for _, result := range payload.Results {
		if result.Origin != episodeOriginED2K {
			continue
		}
		link := v2String(result.Release["ed2k_link"])
		if !strings.HasPrefix(link, "ed2k://|file|Scrubs%20S02") {
			t.Fatalf("ed2k link = %q, want only Scrubs releases", link)
		}
		episodes[result.Episode] = true
	}
	for episode := int64(1); episode <= 3; episode++ {
		if !episodes[episode] {
			t.Fatalf("episode %d has no eD2k result: %s", episode, body)
		}
	}
	// One query for the whole season: aMule serialises searches, so one per
	// episode would make the manual action several times slower.
	if searches := core.Searches(); len(searches) != 1 || searches[0] != "Scrubs S02" {
		t.Fatalf("eD2k searches = %v, want one season query", searches)
	}
}

// TestEpisodeSearchModalShowsED2KDownload checks the interface side: the eD2k
// row carries its source, its link and a Scarica button that queues it in aMule.
func TestEpisodeSearchModalShowsED2KDownload(t *testing.T) {
	core, coreCfg := ed2kTestCore(t, map[string]string{"ed2k_fallback": "true"})
	core.SetResults([]amuletest.Result{{
		Hash: "99999999999999999999999999999999", Name: "Scrubs S02E01 720p ITA.mkv", Size: 900000000, Sources: 6,
	}}, "")
	state := ed2kFallbackState(t, coreCfg)
	state.config_cache.cfg.Series = []SeriesConfig{{Name: "Scrubs", Seasons: "2", Enabled: true}}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/series/episode-search-online?series=Scrubs&season=2&episode=1", nil)
	if code != http.StatusOK {
		t.Fatalf("episode search modal -> %d: %s", code, body)
	}
	for _, want := range []string{"eD2k/Kad", "ed2k://|file|Scrubs", ">Scarica<"} {
		if !strings.Contains(body, want) {
			t.Fatalf("episode search modal is missing %q: %s", want, body)
		}
	}
}

// TestBatchDownloadQueuesED2KLink checks the download button of an eD2k row:
// the batch endpoint must hand the ed2k:// link to aMule instead of looking for
// a magnet.
func TestBatchDownloadQueuesED2KLink(t *testing.T) {
	core, coreCfg := ed2kTestCore(t, nil)
	state := ed2kFallbackState(t, coreCfg)
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatalf("complete setup: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	payload := `{"items":[{"title":"Scrubs S02E01 720p ITA.mkv","source":"ed2k:amule","ed2k_link":"ed2k://|file|Scrubs%20S02E01%20720p%20ITA.mkv|900000000|aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|/"}]}`
	status, body := webPostJSON(t, server, "/api/archive/batch-download", payload)
	if status != http.StatusOK || !strings.Contains(string(body), `"accepted":1`) {
		t.Fatalf("batch download -> %d: %s", status, body)
	}
	downloads := core.Downloads()
	if len(downloads) != 1 || downloads[0].Hash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("aMule downloads = %+v", downloads)
	}
}
