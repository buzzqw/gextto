package gextto

// web_search.go holds the manual search handlers: the dashboard and classic
// search, the archive search, adding a result, explaining a decision, probing an
// indexer, and the whole-series search of the missing episodes with its sources.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

func ManualArchiveSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	if !SetupComplete(s.cfg) {
		jsonError(w, http.StatusConflict, uiText(s, "completa prima la configurazione iniziale"))
		return
	}
	var input SearchInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, uiText(s, "la ricerca deve contenere da 1 a 256 caratteri"))
		return
	}
	cfg := latestConfig(s)
	results := archiveReleasesForQuery(s, cfg, query)
	seen := map[string]struct{}{}
	kept := results[:0]
	for _, release := range results {
		hash, ok := utils.MagnetHash(release.Magnet)
		if !ok {
			continue
		}
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		kept = append(kept, release)
	}
	for index := range kept {
		kept[index].Score = cfg.ReleaseScore(&kept[index])
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return kept[i].Score > kept[j].Score
	})
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "query": query, "results": kept})
}

// ManualSearch is the handler of POST /api/search, the classic manual search.
func ManualSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	manualSearch(w, r, s, false)
}

// ManualSearchDashboard is the v2 manual search. Unlike the classic endpoint,
// it keeps globally rejected releases visible because the user is explicitly
// inspecting arbitrary RSS/indexer/web results and may choose to queue one.
func ManualSearchDashboard(w http.ResponseWriter, r *http.Request, s *AppState) {
	manualSearch(w, r, s, true)
}

// AddSearchResult is the handler of POST /api/search/add: it queues a result
// chosen in the interface.
func AddSearchResult(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SearchAddInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, value := AddRelease(s, input.Release)
	jsonStatus(w, status, value)
}

// IndexerTest is the handler of POST /api/indexer/test: probe a single indexer
// as typed in the editor (which may not be saved yet) and report reachability
// plus any Torznab application error. It is the per-row "Testa" action of the
// Indexer editor.
func IndexerTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input IndexerConfig
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" || len(rawURL) > 2048 {
		jsonError(w, http.StatusBadRequest, "a valid indexer url is required")
		return
	}
	if len(input.APIKey) > 512 {
		jsonError(w, http.StatusBadRequest, "api key is too long")
		return
	}
	status, reachable, body := servicesProbe(HealthProbeURL(input), 10*time.Second)
	torznabError := ""
	if reachable {
		torznabError = TorznabError(body)
	}
	ok := reachable && status >= 200 && status < 300 && torznabError == ""
	message := ""
	switch {
	case !reachable:
		message = "irraggiungibile"
	case torznabError != "":
		message = torznabError
	case !ok:
		message = fmt.Sprintf("HTTP %d", status)
	}
	// A direct Torznab service (e.g. mircrew-indexer) is neither a manager nor
	// a known product: it is shown as "torznab" instead of guessing Jackett.
	kind := "torznab"
	if isManagerSource(input) {
		kind = managerKind(input)
	}
	text := indexerTestText(kind, input.Name, domainOf(rawURL), reachable, ok, status, torznabError)
	if ok {
		logging.Info(text)
	} else {
		logging.Warn(text)
	}
	if torznabError != "" {
		logging.Debug("indexer test detail", "name", strings.TrimSpace(input.Name), "error", torznabError)
	}
	jsonResponse(w, map[string]any{
		"ok":        ok,
		"reachable": reachable,
		"status":    status,
		"error":     message,
	})
}

// ExplainRelease is the handler of POST /api/search/explain: it returns the
// trace of the decision taken on a release.
func ExplainRelease(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input ExplainReleaseInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Release.Title) == "" {
		jsonError(w, http.StatusBadRequest, "release senza titolo")
		return
	}
	cfg := latestConfig(s)
	scanRelease := input.Release
	disk := ArchiveQuality(cfg, &scanRelease)
	trace, err := ExplainWithArchive(cfg, s.db, &input.Release, disk)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "trace": trace})
}

// requestCycle starts the search cycle of one domain, or joins the request
// already waiting. It is shared by the manual request from the interface and by
// the file integrity check, which asks for a search as soon as it finds a
// damaged file. origin says who asked, for the log line. It reports whether the
// cycle was already running (so the request was queued) and whether the request
// joined one already pending.
func requestCycle(s *AppState, domain *string, origin string) (running, queued bool, err error) {
	cfg, err := LoadConfig(s.config_path)
	if err != nil {
		return false, false, err
	}
	if s.dryRun() {
		cfg.DryRun = true
	}
	if !SetupComplete(&cfg) {
		return false, false, errSetupIncomplete
	}
	if !cfg.Active {
		return false, false, errDaemonInactive
	}
	running = true
	if s.cycle_lock.TryLock() {
		s.cycle_lock.Unlock()
		running = false
	}
	if !s.manualCyclePending.CompareAndSwap(false, true) {
		logging.Info(fmt.Sprintf("A %s cycle is already waiting to start: this request joins it", domainLabel(domain)))
		return running, true, nil
	}
	logging.Info(fmt.Sprintf("▶️ %s (%s)", origin, cycleModeLabel(domainLabel(domain))))
	taskDomain := domain
	// Cancelled on shutdown so a manual cycle releases the torrent engine
	// before the native session is destroyed.
	cycleCtx, cancelCycle := s.BackgroundContext()
	done := s.trackOperation()
	go func() {
		defer recoverGoroutine("g1 background")
		defer done()
		defer cancelCycle()
		defer s.manualCyclePending.Store(false)
		s.cycle_lock.Lock()
		defer s.cycle_lock.Unlock()
		logging.Debug("search cycle started", "domain", domainLabel(taskDomain))
		now := time.Now().UTC()
		s.last_cycle.Set(models.CycleStats{LastStartedAt: &now, ErrorDetails: map[string]int{}})
		notifier := FromConfig(&cfg).Async()
		stats, runErr := RunCycleDomain(
			cycleCtx,
			&cfg,
			s.engine,
			s.db,
			s.archive,
			s.comics,
			notifier,
			s.activeEngine(),
			taskDomain,
		)
		if runErr != nil {
			logging.Error("search cycle failed", "domain", domainLabel(taskDomain), "error", runErr.Error())
			return
		}
		// The cycle has already reported its outcome in the log with the
		// "Search finished" line; repeating the same numbers here would only
		// duplicate it in a less readable form.
		if stats != nil {
			s.last_cycle.Set(*stats)
		}
		// The same peak the scheduled cycle trims: a manual run leaves the
		// scrape and evaluation heap resident until the next burst otherwise.
		TrimMemory()
	}()
	return running, false, nil
}

// domainLabel names the cycle's domain in the logs, "full" when none was given.
func domainLabel(domain *string) string {
	if domain == nil || *domain == "" {
		return "full"
	}
	return *domain
}

func manualSearch(w http.ResponseWriter, r *http.Request, s *AppState, includeRejected bool) {
	if !SetupComplete(s.cfg) {
		jsonError(w, http.StatusConflict, uiText(s, "completa prima la configurazione iniziale"))
		return
	}
	var input SearchInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		jsonError(w, http.StatusBadRequest, uiText(s, "la ricerca deve contenere da 1 a 256 caratteri"))
		return
	}
	startedAt := time.Now()
	logging.Info(fmt.Sprintf("🔎 Searching for «%s» by hand", query))
	cfg := latestConfig(s)
	manualSearchBudget := 90 * time.Second
	if includeRejected {
		// The Dashboard search must not wait behind the automatic cycle. The
		// cycle can legitimately hold cycle_lock for several minutes while
		// scraping feeds, leaving the browser with a fetch that appears to have
		// failed at the network layer. Search providers still receive this
		// bounded request context and return completed sources.
		manualSearchBudget = 20 * time.Second
	} else {
		// Preserve the classic endpoint's existing serialization with cycles.
		s.cycle_lock.Lock()
		defer s.cycle_lock.Unlock()
	}
	ctx, cancel := context.WithTimeout(r.Context(), manualSearchBudget)
	defer cancel()
	var results []models.Release
	if includeRejected {
		results = s.engine.SearchQueryManualAll(ctx, cfg, query)
	} else {
		results = s.engine.SearchQueryManual(ctx, cfg, query)
	}
	// eD2k/Kad is a search provider, not a torrent engine: when it is enabled
	// and this search explicitly asked for it, its results sit next to torrent
	// ones, labelled by Network. The UI only offers them for inspection while
	// gx-ed2k reports download=false.
	if input.Kad {
		// The checkbox is an explicit per-search opt-in: start the managed
		// daemon if the global provider is off, without persisting the switch.
		results = append(results, SearchED2KManual(ctx, cfg, query)...)
	}
	if ctx.Err() == context.DeadlineExceeded {
		logging.Warn(fmt.Sprintf("⏱️ The search for «%s» ran out of its %s: keeping the %s found so far",
			query, logDuration(manualSearchBudget), countLabel(len(results), "result", "results")))
		// searchOneWithDB deliberately preserves releases received before the
		// deadline. The v2 dashboard must show those partial RSS/indexer/web
		// results instead of throwing them away and falling back to the archive.
		if !includeRejected {
			// Keep the classic endpoint's historical behaviour unchanged.
			results = nil
		}
	}
	results = append(results, archiveReleasesForQuery(s, cfg, query)...)
	seen := map[string]struct{}{}
	deduped := make([]models.Release, 0, len(results))
	for _, release := range results {
		key, ok := releaseDedupKey(&release)
		if !ok {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, release)
	}
	results = deduped
	for index := range results {
		results[index].Score = cfg.ReleaseScore(&results[index])
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	logging.Info(fmt.Sprintf("🔎 The search for «%s» found %s in %s",
		query, countLabel(len(results), "result", "results"), logDuration(time.Since(startedAt))))
	jsonResponse(w, map[string]any{"ok": true, "query": query, "results": results})
}

// trashRoot returns the configured trash directory, or "" when none is set.
func trashRoot(cfg *Config) string {
	if cfg == nil || cfg.TrashPath == nil {
		return ""
	}
	return strings.TrimSpace(*cfg.TrashPath)
}

// missingSearchOutcome is the result of a whole-series missing search.
// Searched is how many gaps were actually looked up: a synchronous caller stops
// at its deadline and gets the partial answer, while the background job goes on
// to the end.
type missingSearchOutcome struct {
	Results       []gh2EpisodeResult
	Gaps          [][2]int64
	TotalMissing  int
	Searched      int
	Complete      bool
	MetadataReady bool
}

// searchMissingEpisodes searches every missing episode of one series: the
// local archive and feeds first, then each episode on the indexers, then eD2k
// per season when the operator enabled the fallback. There is no cap: a series
// with a hundred gaps is searched in full, and the caller can report progress.
//
// The context is the whole budget: a synchronous caller gives it a deadline and
// receives the episodes looked up so far, while the background job gives it the
// cancel button. Only the gaps whose search actually ran are marked as
// searched, so an interrupted pass does not push them behind the deep interval.
func searchMissingEpisodes(ctx context.Context, s *AppState, cfg *Config, series *SeriesConfig, progress missingSearchProgress) missingSearchOutcome {
	outcome := missingSearchOutcome{}
	// A newly added series may not have season counts yet. Populate them before
	// calculating gaps so the first manual search does not misleadingly report
	// zero episodes just because the background refresh has not run.
	if metadataConfigured(cfg) {
		stale, err := s.db.SeriesMetadataStale(series.Name, 24)
		if err != nil || stale {
			if err := refreshSeriesMetadataForSearch(ctx, cfg, s.db, series); err != nil {
				logging.Debug("manual missing search: metadata refresh unavailable", "series", series.Name, "error", err)
			}
		}
	}
	allGaps, err := s.db.UnarchivedEpisodesForSeries(series.Name, series.IgnoredSeasons)
	if err != nil {
		allGaps = nil
	}
	gaps := make([][2]int64, 0, len(allGaps))
	for _, gap := range allGaps {
		if containsInt64(series.IgnoredSeasons, gap[0]) {
			continue
		}
		if !SeasonAllowedForScan(series.Seasons, gap[0]) {
			continue
		}
		gaps = append(gaps, gap)
	}
	outcome.Gaps = gaps
	outcome.TotalMissing = len(gaps)
	if progress != nil {
		progress(0, len(gaps), "episodes")
	}
	liveReleases := make([][]models.Release, len(gaps))
	searchedGaps := make([]bool, len(gaps))
	if s.engine == nil {
		// Nothing to ask online (dry run or a deliberately engine-less daemon):
		// the gaps were covered by the archive and the stored results.
		for index := range searchedGaps {
			searchedGaps[index] = true
		}
	}
	if s.engine != nil && len(gaps) > 0 {
		// A generic title query can return only recent episodes and miss an old
		// gap. Search every gap with the structured TV identity, a few at a
		// time so the indexers are not flooded.
		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		var done atomic.Int64
	search:
		for index, gap := range gaps {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				break search
			}
			wg.Add(1)
			go func(index int, season, episode int64) {
				defer recoverGoroutine("search missing episode")
				defer wg.Done()
				defer func() { <-sem }()
				liveReleases[index] = s.engine.SearchSeriesEpisode(ctx, cfg, series, season, episode, true)
				if ctx.Err() == nil {
					searchedGaps[index] = true
				}
				if progress != nil {
					progress(int(done.Add(1)), len(gaps), "episodes")
				}
			}(index, gap[0], gap[1])
		}
		wg.Wait()
	}
	cancelled := ctx.Err() != nil
	results := []gh2EpisodeResult{}
	perGap := make([][]episodeSource, len(gaps))
	for index, gap := range gaps {
		season, episode := gap[0], gap[1]
		episodeResults := gh2StoredSeriesEpisodeSources(s, series, season, episode)
		for releaseIndex := range liveReleases[index] {
			if gh2ReleaseMatchesSeriesEpisode(&liveReleases[index][releaseIndex], series, season, episode) {
				episodeResults = append(episodeResults, episodeSource{Release: liveReleases[index][releaseIndex], Origin: episodeOriginIndexer})
			}
		}
		perGap[index] = episodeResults
	}
	accepted := make([][]episodeSource, len(gaps))
	for index := range gaps {
		accepted[index] = finalizeEpisodeSearchResults(perGap[index], cfg, series)
	}
	// Nothing usable from the torrent plane for some gaps: ask eD2k/Kad too, on
	// the same explicit switch as the automatic fallback. The check uses the
	// filtered results, so a stored release rejected by quality or language
	// does not hide the gap. One query per season (not per episode) keeps the
	// wait bounded: aMule serialises its searches.
	//
	// A caller that has already given up does not start it: aMule answers one
	// season at a time and the wait is minutes.
	if ed2kFallbackEligible(cfg) && !cancelled {
		searched := map[int64]bool{}
		for _, gap := range gaps {
			if ctx.Err() != nil {
				break
			}
			season := gap[0]
			if searched[season] {
				continue
			}
			needsFallback := false
			for index := range gaps {
				if gaps[index][0] == season && len(accepted[index]) == 0 {
					needsFallback = true
					break
				}
			}
			if !needsFallback {
				continue
			}
			searched[season] = true
			if progress != nil {
				progress(len(gaps), len(gaps), "ed2k")
			}
			candidates := ed2kManualSeasonFallback(ctx, s.engine, cfg, series, season)
			if len(candidates) == 0 {
				continue
			}
			for index, candidateGap := range gaps {
				if candidateGap[0] != season || len(accepted[index]) != 0 {
					continue
				}
				for _, release := range candidates {
					if gh2ReleaseMatchesSeriesEpisode(&release, series, season, candidateGap[1]) {
						perGap[index] = append(perGap[index], episodeSource{Release: release, Origin: episodeOriginED2K})
					}
				}
				accepted[index] = finalizeEpisodeSearchResults(perGap[index], cfg, series)
			}
		}
	}
	for index, gap := range gaps {
		season, episode := gap[0], gap[1]
		listed := accepted[index]
		if len(listed) == 0 {
			// Nothing within the profile: show the copies in the right language
			// that miss only the resolution, marked, exactly as the cycle would
			// take them for this episode.
			listed = lastResortEpisodeSources(perGap[index], cfg, series)
		}
		for _, result := range listed {
			results = append(results, gh2EpisodeResult{
				Release: result.Release,
				Origin:  result.Origin,
				Season:  season,
				Episode: episode,
			})
		}
	}
	seen := map[string]struct{}{}
	deduped := make([]gh2EpisodeResult, 0, len(results))
	for _, result := range results {
		key, ok := releaseDedupKey(&result.Release)
		if !ok {
			continue
		}
		key += ":" + result.Origin
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, result)
	}
	results = deduped
	for index := range results {
		results[index].Release.Score = cfg.ReleaseScore(&results[index].Release)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Release.Score > results[j].Release.Score
	})
	// Persist what the search found, exactly like a single-episode search: the
	// archive can then answer the next click locally instead of throwing the
	// result away with this response.
	if len(results) > 0 {
		releases := make([]models.Release, 0, len(results))
		for _, result := range results {
			releases = append(releases, result.Release)
		}
		if s.archive != nil {
			if err := s.archive.SaveBatch(releases, cfg); err != nil {
				logging.Warn("failed to save missing-episode search results to archive",
					"series", series.Name, "error", err)
			}
		}
		if s.db != nil {
			if err := s.db.RecordSeenBatch(releases, cfg); err != nil {
				logging.Warn("missing-episode search seen recording failed", "error", err)
			}
		}
	}
	for index, gap := range gaps {
		if !searchedGaps[index] {
			continue
		}
		_ = s.db.MarkGapSearched(series.Name, gap[0], gap[1])
	}
	// Searched is what the caller may rely on: a synchronous call that hit its
	// deadline returns a partial set, and says so with Complete.
	for _, ok := range searchedGaps {
		if ok {
			outcome.Searched++
		}
	}
	outcome.Complete = !cancelled && outcome.Searched == len(gaps)
	if cancelled {
		logging.Info(fmt.Sprintf("🔎 Search for the missing episodes of %s stopped early: %s for %d of %d episodes",
			series.Name, countLabel(len(results), "result", "results"), outcome.Searched, len(gaps)))
	} else {
		logging.Info(fmt.Sprintf("🔎 Search for the missing episodes of %s: %s for %s",
			series.Name, countLabel(len(results), "result", "results"), countLabel(len(gaps), "episode", "episodes")))
	}
	seasonMetadata, _ := s.db.SeriesSeasonCounts(series.Name)
	outcome.Results = results
	outcome.MetadataReady = len(seasonMetadata) > 0 || len(allGaps) > 0
	return outcome
}

// DecodeOptionalJSON decodes a request body that may be absent: an empty body
// leaves the target at its zero value, a malformed one answers 400.
func DecodeOptionalJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Body == nil {
		return true
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	if err := json.Unmarshal(body, target); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func gh6DecodeOptionalJSON(r *http.Request, target any) error {
	if r.ContentLength == 0 {
		return nil
	}
	err := decodeJSON(r, target)
	if err == io.EOF {
		return nil
	}
	return err
}

// withSearchStages attaches an observer of the stages of a manual episode
// search: "sources" before the indexer fan-out and "ed2k" before the fallback.
// The inline episode panel uses it to say what the search is doing; the cycle
// and the other callers carry no observer and pay nothing for it.
func withSearchStages(ctx context.Context, report func(string)) context.Context {
	if report == nil {
		return ctx
	}
	return context.WithValue(ctx, searchStageContextKey{}, report)
}
