package gextto

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newCycleState returns the shared fixture used by the cycle tests. It is the
// same hermetic AppState as the web API tests: temporary databases, no feeds,
// no indexers, no series/movies/comics and DryRun enabled.
func newCycleState(t *testing.T) *AppState {
	t.Helper()
	return newTestAppState(t)
}

// TestCycleFullNoSourcesIsNoOp runs a full cycle with an empty configuration.
// With no feeds, indexers, series, movies or monitored comics every network
// phase is a no-op, so the cycle must complete cleanly and report zero work.
func TestCycleFullNoSourcesIsNoOp(t *testing.T) {
	state := newCycleState(t)

	stats, err := RunCycle(
		context.Background(),
		state.cfg,
		state.engine,
		state.db,
		state.archive,
		state.comics,
		state.notifier,
		state.activeEngine(),
	)
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if stats == nil {
		t.Fatal("RunCycle returned nil stats")
	}
	if stats.Scraped != 0 {
		t.Fatalf("Scraped = %d, want 0", stats.Scraped)
	}
	if stats.Candidates != 0 {
		t.Fatalf("Candidates = %d, want 0", stats.Candidates)
	}
	if stats.DownloadsStarted != 0 {
		t.Fatalf("DownloadsStarted = %d, want 0", stats.DownloadsStarted)
	}
}

func TestCycleSkipsDownloadsWhenConfiguredFreeSpaceCannotBeRead(t *testing.T) {
	state := newCycleState(t)
	cfg := *state.cfg
	cfg.LibtorrentDir = "\x00unreadable-download-volume"
	cfg.Settings = map[string]string{"min_free_space_gb": "1"}

	stats, err := RunCycle(context.Background(), &cfg, state.engine, state.db, state.archive, state.comics, state.notifier, state.activeEngine())
	if err != nil {
		t.Fatal(err)
	}
	if stats.ErrorDetails["min_free_space_unavailable"] != 1 {
		t.Fatalf("free-space error details = %+v", stats.ErrorDetails)
	}
}

// TestCycleCancelledContextAborts verifies that a cycle with an already
// cancelled context (daemon shutting down) returns cleanly without doing work,
// so the torrent session can be destroyed without a late engine call.
func TestCycleCancelledContextAborts(t *testing.T) {
	state := newCycleState(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stats, err := RunCycle(
		ctx,
		state.cfg,
		state.engine,
		state.db,
		state.archive,
		state.comics,
		state.notifier,
		state.activeEngine(),
	)
	if err != nil {
		t.Fatalf("RunCycle with cancelled context: %v", err)
	}
	if stats == nil {
		t.Fatal("RunCycle returned nil stats")
	}
	if stats.DownloadsStarted != 0 || stats.Scraped != 0 {
		t.Fatalf("cancelled cycle did work: scraped=%d started=%d", stats.Scraped, stats.DownloadsStarted)
	}
}

// TestBackgroundContextCancelledOnStop verifies that BackgroundContext is
// cancelled when the daemon shutdown is signalled, which is what makes the
// cycle abort before the session is destroyed.
func TestBackgroundContextCancelledOnStop(t *testing.T) {
	state := newTestAppState(t)
	ctx, cancel := state.BackgroundContext()
	defer cancel()

	stopBackgroundWorkers(state)

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("BackgroundContext was not cancelled on shutdown")
	}
}

// TestCycleDomainNoSourcesIsNoOp runs one cycle per domain with an empty
// configuration. "comics" reaches `RunComicsCycle`, which returns immediately
// because no comic is monitored (no HTTP client call); "series"/"movies" only
// scrape the empty config.
func TestCycleDomainNoSourcesIsNoOp(t *testing.T) {
	for _, domain := range []string{"series", "movies", "comics"} {
		t.Run(domain, func(t *testing.T) {
			state := newCycleState(t)
			current := domain

			stats, err := RunCycleDomain(
				context.Background(),
				state.cfg,
				state.engine,
				state.db,
				state.archive,
				state.comics,
				state.notifier,
				state.activeEngine(),
				&current,
			)
			if err != nil {
				t.Fatalf("RunCycleDomain(%s): %v", domain, err)
			}
			if stats == nil {
				t.Fatalf("RunCycleDomain(%s) returned nil stats", domain)
			}
			if stats.Scraped != 0 {
				t.Fatalf("RunCycleDomain(%s): Scraped = %d, want 0", domain, stats.Scraped)
			}
			if stats.Candidates != 0 {
				t.Fatalf("RunCycleDomain(%s): Candidates = %d, want 0", domain, stats.Candidates)
			}
			if stats.DownloadsStarted != 0 {
				t.Fatalf("RunCycleDomain(%s): DownloadsStarted = %d, want 0", domain, stats.DownloadsStarted)
			}
		})
	}
}

func TestScrapeAllRetainsFastIndexerResultsWhenSearchTimesOut(t *testing.T) {
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<rss><channel><item><title>Example.Show.S01E01.1080p.WEB-DL</title><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link></item></channel></rss>`)
	}))
	defer fast.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()

	oldTimeout := automaticSearchTimeout
	automaticSearchTimeout = 100 * time.Millisecond
	t.Cleanup(func() { automaticSearchTimeout = oldTimeout })
	oldIndexerTimeout := indexerRequestTimeout
	indexerRequestTimeout = time.Second
	t.Cleanup(func() { indexerRequestTimeout = oldIndexerTimeout })

	state := newTestAppState(t)
	cfg := DefaultConfig()
	cfg.DataDir = state.cfg.DataDir
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}
	cfg.FeedURLs = nil
	cfg.Indexers = []IndexerConfig{{Name: "fast", URL: fast.URL, Enabled: true}, {Name: "slow", URL: slow.URL, Enabled: true}}
	cfg.WebsearchEngines = nil

	releases, err := state.engine.ScrapeAll(context.Background(), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].Title != "Example.Show.S01E01.1080p.WEB-DL" {
		t.Fatalf("fast result lost after timeout: %+v", releases)
	}
}

func TestScrapeAllSearchesSeriesAliases(t *testing.T) {
	var mutex sync.Mutex
	queries := map[string]int{}
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		mutex.Lock()
		queries[query]++
		mutex.Unlock()
		_, _ = io.WriteString(w, `<rss><channel></channel></rss>`)
	}))
	defer indexer.Close()

	state := newTestAppState(t)
	cfg := DefaultConfig()
	cfg.DataDir = state.cfg.DataDir
	cfg.Series = []SeriesConfig{{Name: "Canonical Show", Aliases: []string{"Titolo Alternativo"}, Enabled: true}}
	cfg.FeedURLs = nil
	cfg.Indexers = []IndexerConfig{{Name: "test", URL: indexer.URL, Enabled: true}}
	cfg.WebsearchEngines = nil

	if _, err := state.engine.ScrapeAll(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if queries["Canonical Show"] != 1 || queries["Titolo Alternativo"] != 1 {
		t.Fatalf("alias queries = %+v", queries)
	}
}

func TestCycleCandidatesCountsOnlySurvivingReleases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<rss><channel>
<item><title>Monitored.Show.S01E01.1080p.WEB-DL</title><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link></item>
<item><title>Unmonitored.Show.S01E01.1080p.WEB-DL</title><link>magnet:?xt=urn:btih:1123456789012345678901234567890123456789</link></item>
</channel></rss>`)
	}))
	defer server.Close()

	state := newCycleState(t)
	cfg := *state.cfg
	cfg.Series = []SeriesConfig{{Name: "Monitored Show", Enabled: true}}
	cfg.FeedURLs = []string{server.URL}
	cfg.Indexers = nil
	cfg.WebsearchEngines = nil
	cfg.DryRun = true

	domain := "series"
	stats, err := RunCycleDomain(
		context.Background(),
		&cfg,
		state.engine,
		state.db,
		state.archive,
		state.comics,
		state.notifier,
		state.activeEngine(),
		&domain,
	)
	if err != nil {
		t.Fatalf("RunCycleDomain: %v", err)
	}
	if stats == nil {
		t.Fatal("RunCycleDomain returned nil stats")
	}
	if stats.Scraped < 2 {
		t.Fatalf("Scraped = %d, want at least 2", stats.Scraped)
	}
	if stats.Candidates != 1 {
		t.Fatalf("Candidates = %d, want 1", stats.Candidates)
	}
}

// TestDryRunCycleIsInnocuous locks in the guarantee that a dry-run cycle may
// collect and score candidates (and log what would happen) but must not write
// any placeholder, torrent or feed-seen row to the database.
func TestDryRunCycleIsInnocuous(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<rss><channel>
<item><title>Monitored.Show.S01E01.1080p.WEB-DL</title><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link></item>
</channel></rss>`)
	}))
	defer server.Close()

	state := newCycleState(t)
	cfg := *state.cfg
	cfg.Series = []SeriesConfig{{Name: "Monitored Show", Enabled: true}}
	cfg.FeedURLs = []string{server.URL}
	cfg.Indexers = nil
	cfg.WebsearchEngines = nil
	cfg.DryRun = true

	domain := "series"
	stats, err := RunCycleDomain(
		context.Background(),
		&cfg,
		state.engine,
		state.db,
		state.archive,
		state.comics,
		state.notifier,
		state.activeEngine(),
		&domain,
	)
	if err != nil {
		t.Fatalf("RunCycleDomain: %v", err)
	}
	if stats == nil || stats.Scraped < 1 {
		t.Fatalf("dry-run did not scrape the feed: %+v", stats)
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM episodes",
		"SELECT COUNT(*) FROM movies",
		"SELECT COUNT(*) FROM torrent_meta",
		"SELECT COUNT(*) FROM movie_feed_seen",
		"SELECT COUNT(*) FROM series_feed_seen",
	} {
		var count int64
		if err := state.db.db.QueryRow(query).Scan(&count); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if count != 0 {
			t.Fatalf("dry-run persisted state: %s = %d, want 0", query, count)
		}
	}
}

func TestScrapeAllSkipsTitleSearchWhenFeedsConfigured(t *testing.T) {
	var indexerCalls atomic.Int32
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indexerCalls.Add(1)
		_, _ = io.WriteString(w, `<rss><channel></channel></rss>`)
	}))
	defer indexer.Close()

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<rss><channel><item><title>Feed.Item.S01E01</title><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link></item></channel></rss>`)
	}))
	defer feed.Close()

	state := newTestAppState(t)
	cfg := DefaultConfig()
	cfg.DataDir = state.cfg.DataDir
	cfg.Series = []SeriesConfig{{Name: "Feed Item", Enabled: true}}
	cfg.FeedURLs = []string{feed.URL}
	cfg.Indexers = []IndexerConfig{{Name: "test-indexer", URL: indexer.URL, Enabled: true}}
	cfg.WebsearchEngines = nil

	releases, err := state.engine.ScrapeAll(context.Background(), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 feed release, got %d", len(releases))
	}
	if calls := indexerCalls.Load(); calls != 0 {
		t.Fatalf("expected 0 indexer calls when feeds present, got %d", calls)
	}
}

func TestScrapeAllRunsTitleSearchWhenForced(t *testing.T) {
	var indexerCalls atomic.Int32
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indexerCalls.Add(1)
		_, _ = io.WriteString(w, `<rss><channel><item><title>Indexer.Item.S01E01</title><link>magnet:?xt=urn:btih:1123456789012345678901234567890123456789</link></item></channel></rss>`)
	}))
	defer indexer.Close()

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<rss><channel><item><title>Feed.Item.S01E01</title><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link></item></channel></rss>`)
	}))
	defer feed.Close()

	state := newTestAppState(t)
	cfg := DefaultConfig()
	cfg.DataDir = state.cfg.DataDir
	cfg.Series = []SeriesConfig{{Name: "Target Item", Enabled: true}}
	cfg.FeedURLs = []string{feed.URL}
	cfg.Indexers = []IndexerConfig{{Name: "test-indexer", URL: indexer.URL, Enabled: true}}
	cfg.Settings = map[string]string{"cycle_title_search": "always"}
	cfg.WebsearchEngines = nil

	releases, err := state.engine.ScrapeAll(context.Background(), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if calls := indexerCalls.Load(); calls != 1 {
		t.Fatalf("expected 1 indexer call when cycle_title_search=always, got %d", calls)
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 releases (feed + indexer), got %d: %+v", len(releases), releases)
	}
}
