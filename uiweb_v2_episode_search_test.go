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
	"time"
)

// episodeSearchTestState builds a state whose series page lists the three
// episodes of season 1 of "Show": one row per episode, none in the archive.
func episodeSearchTestState(t *testing.T) *AppState {
	t.Helper()
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1", Enabled: true}}, nil); err != nil {
		t.Fatalf("save library: %v", err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(id,name,ignored_seasons) VALUES (1,'Show','[]')"); err != nil {
		t.Fatalf("insert series: %v", err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series_metadata(series_name,season,episode_count,updated_at) VALUES ('Show',1,3,datetime('now'))"); err != nil {
		t.Fatalf("insert metadata: %v", err)
	}
	return state
}

// TestEpisodeSearchOpensUnderTheEpisode locks the point of the change: the 🔍
// fills a frame under the episode row instead of a modal that covers the page.
func TestEpisodeSearchOpensUnderTheEpisode(t *testing.T) {
	state := episodeSearchTestState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=series&series=Show", nil)
	if code != http.StatusOK {
		t.Fatalf("series page -> %d: %s", code, body)
	}
	for _, want := range []string{
		`hx-post="/series/episode-search?series=Show&amp;season=1&amp;episode=2"`,
		`hx-target="#v2-ep-search-1-2"`,
		`data-v2-ep-search-target="v2-ep-search-1-2"`,
		`aria-controls="v2-ep-search-1-2"`,
		`aria-expanded="false"`,
		`<div id="v2-ep-search-1-2" data-v2-ep-search-slot></div>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the episode search frame is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `hx-post="/series/episode-search?series=Show&amp;season=1&amp;episode=2" hx-target="#v2-modal"`) {
		t.Fatal("the episode search still opens in the modal")
	}

	// The Missing tab opens the same frame under the series block.
	code, body = v2Request(t, server, http.MethodGet, "/?view=series&tab=gaps", nil)
	if code != http.StatusOK {
		t.Fatalf("missing tab -> %d: %s", code, body)
	}
	for _, want := range []string{
		`hx-target="#v2-gap-ep-0-1"`,
		`<div id="v2-gap-ep-0-1" data-v2-ep-search-slot></div>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the missing-episodes frame is missing %q:\n%s", want, body)
		}
	}
}

// TestEpisodeSearchPanelFollowsTheSearch checks the fragment the panel polls: it
// says which stage the search is in and how many sources have answered, then
// stops polling and shows the outcome.
func TestEpisodeSearchPanelFollowsTheSearch(t *testing.T) {
	state := episodeSearchTestState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// A search that stays open until the test releases it, so the panel has a
	// running state to follow.
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseSearch := func() { releaseOnce.Do(func() { close(release) }) }
	registry := state.episodeSearches()
	running := registry.begin(context.Background(), v2EpisodeSearch{
		Key: "show|1|2", Series: "Show", Season: 1, Episode: 2,
	}, func(ctx context.Context, id string) {
		<-release
		registry.finish(id, nil, "")
	})
	t.Cleanup(releaseSearch)
	// The search has started asking the sources: the panel must show that,
	// not a fresh "starting" line.
	registry.update(running.ID, func(search *v2EpisodeSearch) { search.Phase = "sources" })

	form := url.Values{"series": {"Show"}, "season": {"1"}, "episode": {"2"}}
	code, body := v2Request(t, server, http.MethodPost, "/series/episode-search", form)
	if code != http.StatusOK {
		t.Fatalf("panel -> %d: %s", code, body)
	}
	// The running search is joined, not started twice, and the panel follows it.
	if !strings.Contains(body, "v2-ep-online-"+running.ID) {
		t.Fatalf("the panel does not follow the running search %q:\n%s", running.ID, body)
	}
	if !strings.Contains(body, "Interrogo indexer e motori di ricerca…") {
		t.Fatalf("the panel does not show the stage of the running search:\n%s", body)
	}

	registry.update(running.ID, func(search *v2EpisodeSearch) {
		search.Phase = "sources"
		search.Done, search.Total = 3, 12
	})
	code, body = v2Request(t, server, http.MethodGet, "/series/episode-search-status?search="+url.QueryEscape(running.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("status -> %d: %s", code, body)
	}
	for _, want := range []string{`hx-trigger="every 1s"`, "3/12", "sorgenti hanno risposto…"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the running search is missing %q:\n%s", want, body)
		}
	}

	registry.update(running.ID, func(search *v2EpisodeSearch) { search.Phase = "ed2k" })
	if _, body = v2Request(t, server, http.MethodGet, "/series/episode-search-status?search="+url.QueryEscape(running.ID), nil); !strings.Contains(body, "Nessun risultato dai torrent: controllo eD2k/Kad…") {
		t.Fatalf("the eD2k stage is not announced:\n%s", body)
	}

	releaseSearch()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, body = v2Request(t, server, http.MethodGet, "/series/episode-search-status?search="+url.QueryEscape(running.ID), nil)
		if !strings.Contains(body, `hx-trigger="every 1s"`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(body, `hx-trigger="every 1s"`) {
		t.Fatalf("the panel keeps polling a finished search:\n%s", body)
	}
	if !strings.Contains(body, "Nessun risultato online per questa puntata.") {
		t.Fatalf("the finished search does not say it found nothing:\n%s", body)
	}
}

// TestEpisodeSearchPanelRejectsAnInvalidEpisode checks that a stale or
// hand-written link answers in the frame instead of breaking the page.
func TestEpisodeSearchPanelRejectsAnInvalidEpisode(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	form := url.Values{"series": {"Show"}, "season": {"x"}, "episode": {"1"}}
	code, body := v2Request(t, server, http.MethodPost, "/series/episode-search", form)
	if code != http.StatusOK || !strings.Contains(body, "Stagione o episodio non validi.") {
		t.Fatalf("invalid episode -> %d: %s", code, body)
	}
}

// TestEpisodeSearchReportsWhatItDoes checks the two observers the panel uses:
// the stage of the search and how many of its sources have answered.
func TestEpisodeSearchReportsWhatItDoes(t *testing.T) {
	// A source that answers immediately with nothing to offer.
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<rss><channel></channel></rss>")
	}))
	t.Cleanup(indexer.Close)

	cfg := DefaultConfig()
	cfg.WebsearchEngines = nil
	cfg.Indexers = []IndexerConfig{
		{Name: "First", URL: indexer.URL, Enabled: true},
		{Name: "Second", URL: indexer.URL, Enabled: true},
	}
	// The progress callback is called from the indexer fan-out goroutines: the
	// slice and the counter below it are shared, so they need a lock. Without
	// it `go test -race` flags the test and a lost append makes the assertion
	// fail on a slow machine.
	var progressMu sync.Mutex
	var counts []searchProgress
	ctx := withSearchProgress(context.Background(), func(progress searchProgress) {
		progressMu.Lock()
		counts = append(counts, progress)
		progressMu.Unlock()
	})
	NewEngine().SearchSeriesEpisode(ctx, &cfg, &SeriesConfig{Name: "Example Show", Enabled: true}, 1, 1, true)
	progressMu.Lock()
	got := len(counts)
	var last searchProgress
	if got > 0 {
		last = counts[got-1]
	}
	progressMu.Unlock()
	if got != 2 {
		t.Fatalf("progress reports = %+v, want one per source", counts)
	}
	if last.Total != 2 || last.Done != 2 {
		t.Fatalf("last progress = %+v, want 2 of 2 sources", last)
	}

	// The stages: the indexer fan-out first, then the eD2k fallback when the
	// torrent side left nothing.
	state := newTestAppState(t)
	var stagesMu sync.Mutex
	var stages []string
	stageCtx := withSearchStages(context.Background(), func(name string) {
		stagesMu.Lock()
		stages = append(stages, name)
		stagesMu.Unlock()
	})
	gh7_search_series_episode_sources(stageCtx, state, latestConfig(state), SeriesConfig{Name: "Show", Enabled: true}, 1, 1)
	stagesMu.Lock()
	joined := strings.Join(stages, ",")
	stagesMu.Unlock()
	if joined != "sources,ed2k" {
		t.Fatalf("stages = %v, want sources then ed2k", stages)
	}
}
