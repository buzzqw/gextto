package gextto

// engine.go implements the core module: the scrape/search engine that fans
// a cycle out to HTML/RSS feeds, Torznab indexers and web engines, applies the
// global filters and reports the per-source outcome.
//
// Cross-module calls rely on the sibling implements `rss.go` (`FetchFeed`,
// `FetchTorznabFlareSolverr`) and `websearch.go` (`SearchWithTimeout`,
// `TakeEngineFailures`, `EngineFailure`); see the note at the bottom of this
// file for the exact signatures.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buzzqw/gextto/internal/cache"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/rules"
	"github.com/buzzqw/gextto/internal/utils"
)

// Engine fans a scrape or a single title search out to every configured
// source. `db` is optional: `nil` in read-only tools that never touch the
// archive database.
type Engine struct {
	client *http.Client
	db     *Database
}

func seriesExternalIDs(series *SeriesConfig) [][2]string {
	if series == nil {
		return nil
	}
	ids := make([][2]string, 0, 2)
	if value := strings.TrimSpace(series.TvdbID); value != "" {
		ids = append(ids, [2]string{"tvdbid", value})
	}
	if value := strings.TrimSpace(series.TmdbID); value != "" {
		ids = append(ids, [2]string{"tmdbid", value})
	}
	return ids
}

// Concurrency and timeout budgets, copied verbatim .
const (
	queryConcurrency    = 6
	feedConcurrency     = 4
	indexerConcurrency  = 4
	feedFetchBudget     = 75 * time.Second
	manualSearchTimeout = 15 * time.Second
	// A full library can take several minutes: keep the operator informed while
	// bounded title searches are still in flight, without logging every title.
	searchProgressInterval = time.Minute
)

// indexerRequestTimeout bounds a single indexer/manager request. It is shorter
// than automaticSearchTimeout so that one slow source fails on its own while the
// healthy sources still return their results, instead of consuming the whole
// search budget.
var (
	automaticSearchTimeout = 25 * time.Second
	// 45s: enough for the MirCrew indexer, which may search its forum and thank
	// a topic on an on-demand query before answering.
	indexerRequestTimeout = 45 * time.Second
)

// NewEngine builds the default engine, mirroring `Engine::new`.
func NewEngine() *Engine {
	return &Engine{
		client: &http.Client{Timeout: 75 * time.Second},
	}
}

// WithDB binds the engine to the archive database so provider backoff survives
// restarts and is shared with the rest of the daemon.
func (e *Engine) WithDB(db *Database) *Engine {
	return &Engine{client: NewEngine().client, db: db}
}

// FetchTorrent downloads a `.torrent` file or resolves a magnet redirect from a
// feed that does not expose a magnet directly. Returns either the torrent file
// bytes or the target magnet link. The full URL is never logged: it can carry a
// passkey.
func (e *Engine) FetchTorrent(ctx context.Context, rawURL string) ([]byte, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "magnet:?") {
		return nil, rawURL, nil
	}
	var redirectedMagnet string
	client := &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if req.URL != nil && strings.EqualFold(req.URL.Scheme, "magnet") {
				redirectedMagnet = req.URL.String()
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "gextto/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if redirectedMagnet != "" {
		return nil, redirectedMagnet, nil
	}
	if loc := resp.Header.Get("Location"); loc != "" && strings.HasPrefix(strings.TrimSpace(loc), "magnet:?") {
		return nil, strings.TrimSpace(loc), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		host := "feed"
		if parsed, err := url.Parse(rawURL); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
		return nil, "", fmt.Errorf("HTTP %d fetching torrent from %s", resp.StatusCode, host)
	}
	payload, err := readLimitedBody(resp.Body, maxFeedResponseBytes)
	if err != nil {
		return nil, "", err
	}
	trimmedPayload := strings.TrimSpace(string(payload))
	if strings.HasPrefix(trimmedPayload, "magnet:?") {
		return nil, trimmedPayload, nil
	}
	return payload, "", nil
}

// ScrapeAll scans every configured feed and then searches every enabled series
// and movie on the Torznab indexers and web engines, returning the unique
// releases that survive the global filters.
func (e *Engine) ScrapeAll(ctx context.Context, cfg *Config) ([]models.Release, error) {
	var all []models.Release
	maxPages := cfg.FeedMaxPages()
	maxAgeDays := cfg.MaxReleaseAgeDays
	oldRatio := cfg.StopOnOldPageRatio()
	// Each cycle reports only its own sources: drop anything accumulated since
	// the previous drain (e.g. manual searches from the UI).
	_ = logging.TakeSourceStats()
	logging.Info(fmt.Sprintf("🔎 Reading %s…", countLabel(int64(len(cfg.FeedURLs)), "feed", "feeds")))

	// Feed fan-out, bounded by `feedConcurrency`, scheduled in config order.
	feedResults := make([][]models.Release, len(cfg.FeedURLs))
	var feedWG sync.WaitGroup
	feedSem := make(chan struct{}, feedConcurrency)
	for i, feedURL := range cfg.FeedURLs {
		feedWG.Add(1)
		feedSem <- struct{}{}
		go func(index int, rawURL string) {
			defer recoverGoroutine("feed scrape")
			defer feedWG.Done()
			defer func() { <-feedSem }()
			feedResults[index] = e.scrapeFeed(ctx, cfg, rawURL, maxPages, maxAgeDays, oldRatio)
		}(i, feedURL)
	}
	feedWG.Wait()
	for _, items := range feedResults {
		all = append(all, items...)
	}

	// One readable summary instead of one line per feed.
	feedsReleases := len(all)
	feedStats := logging.TakeSourceStats()
	breakdown := sourceBreakdown(feedStats)
	if breakdown == "" {
		breakdown = "none"
	}
	logging.Info(fmt.Sprintf("🌐 Feeds read: %s found (%s)",
		countLabel(int64(feedsReleases), "release", "releases"), breakdown))
	// Persist the detail-page cache right after the feed phase: the indexer
	// searches below can take minutes, and a restart would otherwise throw away
	// every magnet resolved in this cycle.
	cache.Save()

	if !cfg.ShouldSearchTitlesInCycle() {
		logging.Debug(fmt.Sprintf(
			"online title search skipped (%d feeds provide releases for the local archive)",
			len(cfg.FeedURLs),
		))
	} else {
		// Build the per-target search list.
		type searchTarget struct {
			Query string
			IDs   [][2]string
			Type  string
		}
		var targets []searchTarget
		seenTargets := map[string]struct{}{}
		for _, series := range cfg.Series {
			if !series.Enabled {
				continue
			}
			// Pass the real external ids to Torznab: `tvdbid` with the TVDB id and
			// `tmdbid` with the TMDB id.
			ids := seriesExternalIDs(&series)
			queries := append([]string{series.Name}, series.Aliases...)
			for _, query := range queries {
				query = strings.TrimSpace(query)
				if query == "" {
					continue
				}
				key := strings.ToLower(query)
				if _, exists := seenTargets[key]; exists {
					continue
				}
				seenTargets[key] = struct{}{}
				targets = append(targets, searchTarget{Query: query, IDs: ids, Type: searchTypeTV})
			}
		}
		for _, movie := range cfg.Movies {
			if !movie.Enabled {
				continue
			}
			query := fmt.Sprintf("%s %s", movie.Name, movie.Year)
			key := strings.ToLower(query)
			if _, exists := seenTargets[key]; !exists {
				seenTargets[key] = struct{}{}
				ids := [][2]string{}
				if tmdbID := strings.TrimSpace(movie.TmdbID); tmdbID != "" {
					ids = append(ids, [2]string{"tmdbid", tmdbID})
				}
				targets = append(targets, searchTarget{Query: query, IDs: ids, Type: searchTypeMovie})
			}
		}
		targetsTotal := len(targets)
		logging.Info(fmt.Sprintf("🔎 Searching the indexers for %s…", countLabel(int64(targetsTotal), "title", "titles")))

		// Title search fan-out, bounded by `queryConcurrency`.
		type searchResult struct {
			Query string
			Items []models.Release
		}
		results := make([]searchResult, len(targets))
		timeoutQueries := make(chan string, len(targets))
		var searchWG sync.WaitGroup
		var searchesCompleted atomic.Int32
		searchStarted := time.Now()
		progressDone := make(chan struct{})
		if targetsTotal > 0 {
			go func() {
				defer recoverGoroutine("title search progress")
				ticker := time.NewTicker(searchProgressInterval)
				defer ticker.Stop()
				for {
					select {
					case <-progressDone:
						return
					case <-ticker.C:
						completed := int(searchesCompleted.Load())
						remaining := targetsTotal - completed
						if remaining < 0 {
							remaining = 0
						}
						logging.Info("title search progress",
							"completed", completed,
							"total", targetsTotal,
							"remaining", remaining,
							"elapsed", logging.HumanDuration(int64(time.Since(searchStarted).Seconds())))
					}
				}
			}()
		}
		searchSem := make(chan struct{}, queryConcurrency)
		for i, target := range targets {
			searchWG.Add(1)
			searchSem <- struct{}{}
			go func(index int, query string, ids [][2]string, searchType string) {
				defer recoverGoroutine("title search")
				defer searchWG.Done()
				defer func() { <-searchSem }()
				defer searchesCompleted.Add(1)
				started := time.Now()
				searchCtx, cancel := context.WithTimeout(ctx, automaticSearchTimeout)
				items := searchOneWithDBType(searchCtx, cfg, query, ids, nil, e.db, false, searchType, false)
				timedOut := errors.Is(searchCtx.Err(), context.DeadlineExceeded)
				cancel()
				if timedOut {
					logging.Debug("scheduled title search timed out",
						"query", query,
						"timeout_secs", int(automaticSearchTimeout.Seconds()))
					timeoutQueries <- query
				}
				logging.Debug("scheduled title search completed",
					"query", query,
					"elapsed_ms", time.Since(started).Milliseconds(),
					"results", len(items))
				results[index] = searchResult{Query: query, Items: items}
			}(i, target.Query, target.IDs, target.Type)
		}
		searchWG.Wait()
		close(progressDone)
		close(timeoutQueries)
		var timedOutQueries []string
		for query := range timeoutQueries {
			timedOutQueries = append(timedOutQueries, query)
		}
		if len(timedOutQueries) > 0 {
			indexerNames := make([]string, 0, len(cfg.Indexers))
			for _, indexer := range cfg.Indexers {
				if indexer.Enabled {
					indexerNames = append(indexerNames, indexer.Name)
				}
			}
			providers := strings.Join(indexerNames, ", ")
			if providers == "" {
				providers = "no indexers configured"
			}
			logging.Warn(fmt.Sprintf(
				"scheduled title searches timed out: %d target(s) · indexers: %s · queries: %s",
				len(timedOutQueries), providers, strings.Join(timedOutQueries, ", ")),
				"timeout_secs", int(automaticSearchTimeout.Seconds()))
		}

		// "Compatible" count: a release is only useful when it passes the global
		// filters and those of the searched series/movie (language, quality,
		// exclude, subtitles). This keeps the log from announcing releases that
		// will never be downloaded.
		usable := func(query string, items []models.Release) int {
			for i := range cfg.Series {
				series := &cfg.Series[i]
				matchesQuery := strings.EqualFold(series.Name, query)
				if !matchesQuery {
					for _, alias := range series.Aliases {
						if strings.EqualFold(strings.TrimSpace(alias), query) {
							matchesQuery = true
							break
						}
					}
				}
				if !matchesQuery {
					continue
				}
				count := 0
				for j := range items {
					if cfg.ReleaseAllowed(&items[j]) &&
						cfg.SeriesReleaseAllowed(series, &items[j].Quality, items[j].Title) {
						count++
					}
				}
				return count
			}
			for i := range cfg.Movies {
				movie := &cfg.Movies[i]
				if fmt.Sprintf("%s %s", movie.Name, movie.Year) != query {
					continue
				}
				count := 0
				for j := range items {
					if cfg.ReleaseAllowed(&items[j]) && cfg.MovieReleaseAllowedForTitle(movie, &items[j].Quality, items[j].Title) {
						count++
					}
				}
				return count
			}
			count := 0
			for j := range items {
				if cfg.ReleaseAllowed(&items[j]) {
					count++
				}
			}
			return count
		}

		targetsDone := 0
		targetsWithHits := 0
		step2Usable := 0
		for _, result := range results {
			targetsDone++
			compatible := usable(result.Query, result.Items)
			if compatible > 0 {
				targetsWithHits++
				step2Usable += compatible
				// Per-target detail is diagnostic only. The cycle already reports
				// the total in "Step 2/2 complete".
				logging.Debug("🔎 compatible releases", "query", result.Query, "compatible", compatible)
			} else {
				logging.Debug("🔎 search: no matching releases", "query", result.Query, "found", len(result.Items))
			}
			all = append(all, result.Items...)
		}
		logging.Info(fmt.Sprintf("🔎 Indexer search done: suitable releases for %d of %s (%s in total)",
			targetsWithHits, countLabel(int64(targetsDone), "title", "titles"),
			countLabel(int64(step2Usable), "release", "releases")))
		engineFailures := TakeEngineFailures()
		if len(engineFailures) > 0 {
			detail := make([]string, 0, len(engineFailures))
			for _, failure := range engineFailures {
				detail = append(detail, fmt.Sprintf("%s (%d)", failure.Engine, failure.Count))
			}
			logging.Warn("⚠️ Web engines unreachable in this cycle: " + strings.Join(detail, ", "))
		}
	}

	seen := map[string]struct{}{}
	kept := make([]models.Release, 0, len(all))
	for _, release := range all {
		if reason := cfg.AllReleaseDeniedReason(&release); reason != "" {
			if cfg.ReleaseIsMonitored(&release) {
				rules.LogRejection(&release, reason)
			}
			continue
		}
		// A feed with only a `.torrent` link (e.g. TorrentLeech) has no magnet:
		// do not drop it here, the infohash is resolved in the cycle. In that
		// case deduplicate by URL.
		dedupKey, ok := releaseDedupKey(&release)
		if !ok {
			logging.Debug("filter skipped",
				"title", release.Title,
				"source", release.Source,
				"reason", "missing magnet hash")
			continue
		}
		if _, duplicate := seen[dedupKey]; duplicate {
			logging.Debug("filter skipped duplicate",
				"title", release.Title,
				"source", release.Source,
				"reason", "duplicate infohash")
			continue
		}
		seen[dedupKey] = struct{}{}
		kept = append(kept, release)
	}
	all = kept
	logging.Debug(fmt.Sprintf("scraping: %d unique releases after filters", len(all)))
	sourceStats := append([]logging.SourceStatEntry{}, feedStats...)
	sourceStats = append(sourceStats, logging.TakeSourceStats()...)
	providerFailures := map[string]int{}
	failedSources := map[string]int{}
	for _, entry := range sourceStats {
		if entry.Stats.Fail == 0 {
			continue
		}
		providerFailures[entry.Kind] += entry.Stats.Fail
		failedSources[entry.Kind]++
	}
	if len(providerFailures) > 0 {
		var failed []string
		for _, kind := range []struct{ key, singular, pluralForm string }{
			{"feed", "feed", "feeds"}, {"indexer", "indexer", "indexers"}, {"web", "search engine", "search engines"},
		} {
			if count := failedSources[kind.key]; count > 0 {
				failed = append(failed, countLabel(count, kind.singular, kind.pluralForm))
			}
		}
		logging.Warn("⚠️ Some sources did not answer in this search: " + strings.Join(failed, ", "))
		logging.Debug("provider failures in cycle",
			"feed_failures", providerFailures["feed"],
			"feed_sources", failedSources["feed"],
			"indexer_failures", providerFailures["indexer"],
			"indexer_sources", failedSources["indexer"],
			"web_failures", providerFailures["web"],
			"web_sources", failedSources["web"],
		)
	}
	printSourceReport(sourceStats)
	cache.Save()
	return all, nil
}

// scrapeFeed runs one feed inside its backoff window and total time budget,
// updating the provider backoff and the per-source stats.
func (e *Engine) scrapeFeed(ctx context.Context, cfg *Config, rawURL string, maxPages int, maxAgeDays int64, oldRatio float64) []models.Release {
	if cfg != nil {
		ConfigureCloudflareState(cfg.DataDir)
	}
	feed := feedLabel(rawURL)
	source := feedSourceName(rawURL)
	// Skip sources inside their backoff window instead of hammering them once
	// more every cycle.
	if e.db != nil {
		if blocked, err := e.db.ProviderBlocked("feed", source); err == nil && blocked {
			logging.Debug("feed skipped (backoff)", "feed", feed)
			return nil
		}
	}
	fetchCtx, cancel := context.WithTimeout(ctx, feedFetchBudget)
	defer cancel()
	items, err := FetchFeed(fetchCtx, rawURL, cfg.FlaresolverrURL, maxPages, maxAgeDays, oldRatio)
	if err == nil {
		logging.SourceOK("feed", source, len(items))
		logging.Debug("RSS feed analyzed", "feed", feed, "items", len(items))
		if e.db != nil {
			_ = e.db.ProviderSuccess("feed", source)
		}
		return items
	}
	message := utils.RedactURLSecrets(err.Error())
	if errors.Is(err, context.DeadlineExceeded) {
		message = fmt.Sprintf("feed exceeded the %ds total time budget", int(feedFetchBudget.Seconds()))
	} else if strings.Contains(message, "decoding response body") {
		// Cloudflare sometimes closes the stream halfway: reqwest reports it as
		// a decoding error. Say it plainly.
		message = "connection interrupted before the feed was complete"
	}
	logging.SourceFail("feed", source, message)
	logging.Warn(fmt.Sprintf("⚠️ RSS feed unavailable — %s: %s", feed, message))
	if e.db != nil {
		_ = e.db.ProviderFailure("feed", source, message)
	}
	return nil
}

// SearchQuery is the gap-fill title search: the whole fan-out is bounded by the
// automatic timeout.
func (e *Engine) SearchQuery(ctx context.Context, cfg *Config, query string) []models.Release {
	searchCtx, cancel := context.WithTimeout(ctx, automaticSearchTimeout)
	defer cancel()
	items := e.SearchQueryIDs(searchCtx, cfg, query, nil)
	if errors.Is(searchCtx.Err(), context.DeadlineExceeded) {
		logging.Warn("gap-fill title search timed out",
			"query", query,
			"timeout_secs", int(automaticSearchTimeout.Seconds()))
	}
	return items
}

// SearchQueryManual is the interactive search: Torznab indexers stay available
// for their whole search, while the web engines get a short overall budget.
func (e *Engine) SearchQueryManual(ctx context.Context, cfg *Config, query string) []models.Release {
	timeout := manualSearchTimeout
	return searchOneWithDB(ctx, cfg, query, nil, &timeout, e.db, false)
}

// SearchQueryManualAll keeps globally rejected releases visible so the user can
// inspect or manually queue them. Automatic acquisition still uses the filtered
// methods above.
func (e *Engine) SearchQueryManualAll(ctx context.Context, cfg *Config, query string) []models.Release {
	timeout := manualSearchTimeout
	return searchOneWithDB(ctx, cfg, query, nil, &timeout, e.db, true)
}

// SearchQueryIDs searches with explicit external ids (`tvdbid`/`tmdbid`).
func (e *Engine) SearchQueryIDs(ctx context.Context, cfg *Config, query string, externalIDs [][2]string) []models.Release {
	return searchOneWithDB(ctx, cfg, query, externalIDs, nil, e.db, false)
}

// SearchSeriesEpisode searches one known episode using its structured TV
// identity. It is used by gap filling and episode-level manual searches: a
// broad title search can miss older episodes even when the indexer supports an
// exact season/episode request.
func (e *Engine) SearchSeriesEpisode(ctx context.Context, cfg *Config, series *SeriesConfig, season, episode int64, manual bool) []models.Release {
	if series == nil || season < 1 || episode < 1 {
		return nil
	}
	query := fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode)
	var webTimeout *time.Duration
	if manual {
		timeout := manualSearchTimeout
		webTimeout = &timeout
	}
	return searchOneWithDBType(ctx, cfg, query, seriesExternalIDs(series), webTimeout, e.db, false, searchTypeTV, true)
}

// SearchSeriesSeason is the broader fallback for a known episode gap. It can
// discover partial and complete season packs that an episode-specific indexer
// query does not return. Callers must still verify that each result covers the
// target episode before accepting it.
func (e *Engine) SearchSeriesSeason(ctx context.Context, cfg *Config, series *SeriesConfig, season int64, manual bool) []models.Release {
	if series == nil || season < 1 {
		return nil
	}
	query := fmt.Sprintf("%s S%02d", series.Name, season)
	var webTimeout *time.Duration
	if manual {
		timeout := manualSearchTimeout
		webTimeout = &timeout
	}
	return searchOneWithDBType(ctx, cfg, query, seriesExternalIDs(series), webTimeout, e.db, false, searchTypeTV, true)
}

// SearchMovie searches one configured movie using its movie mode and TMDB id
// where available. `includeRejected` is reserved for the manual movie view,
// which applies its own per-movie filters before displaying results.
func (e *Engine) SearchMovie(ctx context.Context, cfg *Config, movie *MovieConfig, manual, includeRejected bool) []models.Release {
	if movie == nil || !movie.Enabled {
		return nil
	}
	query := strings.TrimSpace(movie.Name + " " + movie.Year)
	ids := make([][2]string, 0, 1)
	if tmdbID := strings.TrimSpace(movie.TmdbID); tmdbID != "" {
		ids = append(ids, [2]string{"tmdbid", tmdbID})
	}
	var webTimeout *time.Duration
	if manual {
		timeout := manualSearchTimeout
		webTimeout = &timeout
	}
	return searchOneWithDBType(ctx, cfg, query, ids, webTimeout, e.db, includeRejected, searchTypeMovie, true)
}

// searchOneWithDB runs the indexer and web fan-outs concurrently, then applies
// the global filters (unless `includeRejected`) and deduplicates by infohash.
func searchOneWithDB(
	ctx context.Context,
	cfg *Config,
	query string,
	externalIDs [][2]string,
	webTimeout *time.Duration,
	providerDB *Database,
	includeRejected bool,
) []models.Release {
	return searchOneWithDBType(ctx, cfg, query, externalIDs, webTimeout, providerDB, includeRejected, searchTypeAuto, true)
}

func searchOneWithDBType(
	ctx context.Context,
	cfg *Config,
	query string,
	externalIDs [][2]string,
	webTimeout *time.Duration,
	providerDB *Database,
	includeRejected bool,
	searchType string,
	includeWeb bool,
) []models.Release {
	if cfg != nil {
		ConfigureCloudflareState(cfg.DataDir)
	}
	var all []models.Release
	// Load the disabled set once, then exclude those providers from the fan-out.
	blockedProviders := map[[2]string]struct{}{}
	if providerDB != nil {
		if value, err := providerDB.BlockedProviders(); err == nil {
			blockedProviders = value
		}
	}
	var indexers []IndexerConfig
	for _, indexer := range cfg.Indexers {
		if !indexer.Enabled {
			continue
		}
		if _, blocked := blockedProviders[[2]string{"indexer", indexer.Name}]; blocked {
			continue
		}
		indexers = append(indexers, indexer)
	}

	type indexerResult struct {
		Name  string
		Query string
		Items []models.Release
		Err   error
	}
	indexerResults := make([]indexerResult, len(indexers))
	// An installation can have many indexers. Keep the fan-out bounded: title
	// searches already run concurrently, so one unbounded goroutine per indexer
	// would otherwise multiply the number of simultaneous HTTP requests.
	jobs := make(chan int)
	workers := indexerConcurrency
	if workers > len(indexers) {
		workers = len(indexers)
	}
	var indexerWG sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		indexerWG.Add(1)
		go func() {
			defer recoverGoroutine("indexer search")
			defer indexerWG.Done()
			for index := range jobs {
				indexer := indexers[index]
				if ctx.Err() != nil {
					indexerResults[index] = indexerResult{Name: indexer.Name, Query: query, Err: ctx.Err()}
					continue
				}
				logging.Debug("indexer search started", "indexer", indexer.Name, "query", query)
				requestCtx, cancel := context.WithTimeout(ctx, indexerRequestTimeout)
				items, err := fetchTorznabFlareSolverr(requestCtx, indexer, query, externalIDs, cfg.FlaresolverrURL, searchType)
				cancel()
				indexerResults[index] = indexerResult{Name: indexer.Name, Query: query, Items: items, Err: err}
			}
		}()
	}
	for index := range indexers {
		jobs <- index
	}
	close(jobs)

	var webResults []models.Release
	var webWG sync.WaitGroup
	if includeWeb && len(cfg.WebsearchEngines) > 0 {
		webWG.Add(1)
		go func() {
			defer recoverGoroutine("web search")
			defer webWG.Done()
			webResults = SearchWithTimeout(ctx, cfg, query, webTimeout)
			logging.Debug("web search completed", "query", query, "results", len(webResults))
		}()
	}

	indexerWG.Wait()
	for _, result := range indexerResults {
		if result.Err == nil {
			logging.SourceOK("indexer", result.Name, len(result.Items))
			if providerDB != nil {
				_ = providerDB.ProviderSuccess("indexer", result.Name)
			}
			logging.Debug("indexer search completed",
				"indexer", result.Name,
				"results", len(result.Items),
				"query", result.Query)
			all = append(all, result.Items...)
		} else {
			// Cancellation belongs to the search/request lifecycle, not to the
			// indexer. Do not mark the provider as failed or emit a warning when
			// the caller has already stopped waiting for this search. A request
			// timeout that fires while the search is still running (the parent
			// context is alive) is a real per-source failure and is handled below.
			if (errors.Is(result.Err, context.Canceled) || errors.Is(result.Err, context.DeadlineExceeded)) && ctx.Err() != nil {
				logging.Debug("indexer search stopped with the search context",
					"indexer", result.Name,
					"query", result.Query,
					"reason", result.Err)
				continue
			}
			message := utils.RedactURLSecrets(result.Err.Error())
			logging.SourceFail("indexer", result.Name, message)
			if providerDB != nil {
				_ = providerDB.ProviderFailure("indexer", result.Name, message)
			}
			// SourceStats and the cycle-level provider summary retain the failure
			// details. Logging every failed title/indexer pair at WARN makes a
			// single provider outage look like dozens of independent incidents.
			logging.Debug("indexer search failed",
				"indexer", result.Name,
				"query", result.Query,
				"error", message)
		}
	}
	if includeWeb && len(cfg.WebsearchEngines) > 0 {
		webWG.Wait()
		all = append(all, webResults...)
	}

	seen := map[string]struct{}{}
	kept := make([]models.Release, 0, len(all))
	for _, release := range all {
		if !includeRejected {
			if reason := cfg.AllReleaseDeniedReason(&release); reason != "" {
				if cfg.ReleaseIsMonitored(&release) {
					rules.LogRejection(&release, reason)
				}
				continue
			}
		}
		dedupKey, ok := releaseDedupKey(&release)
		if !ok {
			continue
		}
		if _, duplicate := seen[dedupKey]; !duplicate {
			seen[dedupKey] = struct{}{}
			kept = append(kept, release)
		}
	}
	return kept
}

// printSourceReport is one readable line per source (feed, indexer, web engine)
// telling the user whether it worked and how many releases it produced.
// Aggregated per cycle: attempts repeat for every query, so a raw per-attempt
// log would flood.
func printSourceReport(stats []logging.SourceStatEntry) {
	if len(stats) == 0 {
		return
	}
	// Detailed per-source reporting is useful for diagnostics, but can produce
	// dozens of lines per cycle. The summary ("Sources: ..." and unreachable
	// engine warnings) remains at INFO/WARN level.
	logging.Debug("📡 SOURCE REPORT — outcomes of every source in this cycle")
	for _, entry := range stats {
		stat := entry.Stats
		lastError := stat.LastError
		if lastError == "" {
			lastError = "unknown error"
		}
		switch {
		case stat.Fail == 0:
			logging.Debug(fmt.Sprintf("   ✅ [%s] %s: %d run(s), %d releases", entry.Kind, entry.Name, stat.OK, stat.Results))
		case stat.OK == 0:
			logging.Debug(fmt.Sprintf("   ❌ [%s] %s: %d failure(s) — %s", entry.Kind, entry.Name, stat.Fail, lastError))
		default:
			logging.Debug(fmt.Sprintf("   ⚠️ [%s] %s: %d ok / %d failed, %d releases — %s", entry.Kind, entry.Name, stat.OK, stat.Fail, stat.Results, lastError))
		}
	}
}

// sourceBreakdown renders the per-feed outcome for the "Sources:" summary.
func sourceBreakdown(stats []logging.SourceStatEntry) string {
	var parts []string
	for _, entry := range stats {
		if entry.Kind != "feed" {
			continue
		}
		stat := entry.Stats
		switch {
		case stat.Fail > 0 && stat.OK == 0:
			parts = append(parts, entry.Name+" unavailable")
		case stat.Fail > 0:
			parts = append(parts, fmt.Sprintf("%s %s (%s failed)", entry.Name, logCount(int64(stat.Results)),
				countLabel(int64(stat.Fail), "feed", "feeds")))
		default:
			parts = append(parts, entry.Name+" "+logCount(int64(stat.Results)))
		}
	}
	return strings.Join(parts, " · ")
}

// feedSourceName maps a feed URL to the stable provider name used by the
// backoff table and the source report.
func feedSourceName(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, "ext.to") || strings.Contains(lower, "extto"):
		return "ExtTo"
	case strings.Contains(lower, "torrentgalaxy") || strings.Contains(lower, "tgx"):
		return "TGx"
	case strings.Contains(lower, "torrentleech"):
		return "TorrentLeech"
	case strings.Contains(lower, "knaben"):
		return "Knaben"
	case strings.Contains(lower, "eztv"):
		return "EZTV"
	}
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return feedLabel(rawURL)
}

// feedLabel is the short human label (host + path) shown in the feed log lines.
func feedLabel(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		if index := strings.IndexByte(rawURL, '?'); index >= 0 {
			return rawURL[:index]
		}
		return rawURL
	}
	host := parsed.Hostname()
	if host == "" {
		host = "feed"
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		return host
	}
	return host + path
}

// Cross-module calls reconciled with the shared ports:
//
//	rss.FetchFeed(ctx, rawURL string, flaresolverr *string, maxPages int,
// maxAgeDays int64, oldRatio float64) ([]models.Release, error)
//	rss.FetchTorznabFlareSolverr(ctx, indexer IndexerConfig, query string,
// externalIDs [][2]string, flaresolverr *string)
// ([]models.Release, error)
//	websearch.SearchWithTimeout(ctx, cfg *Config, query string,
// timeout *time.Duration) []models.Release
//	websearch.TakeEngineFailures() []EngineFailure
// with EngineFailure{Engine string; Count int}
