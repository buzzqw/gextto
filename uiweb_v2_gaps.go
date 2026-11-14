package gextto

// uiweb_v2_gaps.go is the Missing tab of the Series page: the missing episodes
// grouped by series and season instead of one flat row per episode. A click on
// an episode opens the same search window as the series page (results from the
// local archive, then online, each with Download); "Cerca mancanti" searches a
// whole series and reports the outcome next to it.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// v2SeriesPage is /?view=series: the monitored list or the Missing tab.
type v2SeriesPage struct {
	Tab       string
	Monitored v2PanelsView
	Gaps      v2GapsView
	GapCount  int
}

// v2GapsView is the Missing tab.
type v2GapsView struct {
	Total       int
	SeriesCount int
	Filter      string
	// Open unfolds every series: few of them, or a filter typed on purpose.
	Open   bool
	Series []v2GapSeries
}

type v2GapSeries struct {
	Name     string
	PathName string
	Count    int
	Latest   string
	Seasons  []v2GapSeason
}

type v2GapSeason struct {
	Season   int64
	Episodes []v2GapEpisode
}

type v2GapEpisode struct {
	Season  int64
	Episode int64
	Code    string
	AirDate string
}

func v2SeriesPageFrom(s *AppState, r *http.Request) v2SeriesPage {
	page := v2SeriesPage{Tab: "monitored"}
	gaps, _ := libraryGaps(s)
	page.GapCount = len(gaps)
	if r.FormValue("tab") == "gaps" {
		page.Tab = "gaps"
		page.Gaps = v2GapsViewFrom(gaps, r.FormValue("f"))
		return page
	}
	page.Monitored = v2PanelsViewFrom(s, r, "series")
	return page
}

// v2GapsViewFrom groups the missing episodes by series (alphabetical) and
// season; filter keeps the series whose name contains it.
func v2GapsViewFrom(gaps []libraryGap, filter string) v2GapsView {
	view := v2GapsView{Filter: strings.TrimSpace(filter)}
	needle := strings.ToLower(view.Filter)
	bySeries := map[string]*v2GapSeries{}
	for _, gap := range gaps {
		if needle != "" && !strings.Contains(strings.ToLower(gap.Series), needle) {
			continue
		}
		series := bySeries[gap.Series]
		if series == nil {
			series = &v2GapSeries{Name: gap.Series, PathName: url.PathEscape(gap.Series)}
			bySeries[gap.Series] = series
		}
		series.Count++
		view.Total++
		if gap.AirDate > series.Latest {
			series.Latest = gap.AirDate
		}
		episode := v2GapEpisode{Season: gap.Season, Episode: gap.Episode, AirDate: gap.AirDate,
			Code: fmt.Sprintf("S%02dE%02d", gap.Season, gap.Episode)}
		placed := false
		for i := range series.Seasons {
			if series.Seasons[i].Season == gap.Season {
				series.Seasons[i].Episodes = append(series.Seasons[i].Episodes, episode)
				placed = true
				break
			}
		}
		if !placed {
			series.Seasons = append(series.Seasons, v2GapSeason{Season: gap.Season, Episodes: []v2GapEpisode{episode}})
		}
	}
	for _, series := range bySeries {
		sort.Slice(series.Seasons, func(i, j int) bool { return series.Seasons[i].Season < series.Seasons[j].Season })
		for _, season := range series.Seasons {
			sort.Slice(season.Episodes, func(i, j int) bool { return season.Episodes[i].Episode < season.Episodes[j].Episode })
		}
		view.Series = append(view.Series, *series)
	}
	sort.Slice(view.Series, func(i, j int) bool {
		return strings.ToLower(view.Series[i].Name) < strings.ToLower(view.Series[j].Name)
	})
	view.SeriesCount = len(view.Series)
	view.Open = view.Filter != "" || view.SeriesCount <= 3
	return view
}

// v2GapSearchProgress is what the status fragment shows while the job runs.
type v2GapSearchProgress struct {
	Done  int
	Total int
	Phase string
}

// v2GapSearchResult is the outcome shown next to a series after "Cerca
// mancanti". It carries the releases themselves, not just their count.
type v2GapSearchResult struct {
	Error    string
	Results  int
	Episodes int
	Total    int
	Series   string
	Sources  v2SourcesView
	Running  bool
	Queued   bool
	JobID    string
	Filter   string
	Slot     string
	Progress v2GapSearchProgress
	// picks are the releases «Aggiungi le migliori puntate mancanti» queues:
	// the best copy for each episode of the results. They are not rendered.
	picks []v2GapPick
}

// v2GapPick is one release the bulk action queues and the missing episodes it
// fills: a pack fills several at once, and the log has to name them all.
type v2GapPick struct {
	Result   gh2EpisodeResult
	Season   int64
	Episodes []int64
}

func v2GapSearchRedirect(filter string) string {
	redirect := "/?view=series&tab=gaps"
	if filter = strings.TrimSpace(filter); filter != "" {
		redirect += "&f=" + url.QueryEscape(filter)
	}
	return redirect
}

func v2GapSearchResultFromOutcome(outcome missingSearchOutcome, redirect, series string) v2GapSearchResult {
	view := v2GapSearchResult{
		Episodes: outcome.Searched,
		Total:    outcome.TotalMissing,
		Series:   series,
		Sources:  v2SourcesView{Redirect: redirect, ShowEpisode: true},
	}
	for _, result := range outcome.Results {
		view.Sources.Results = append(view.Sources.Results,
			v2SourceRowFromRelease(&result.Release, result.Origin, result.Season, result.Episode))
	}
	view.Results = len(view.Sources.Results)
	view.picks = v2BestGapPicks(outcome.Results)
	return view
}

// v2BestGapPicks chooses, for every missing episode the search found, the copy
// to queue: within the profile before a last-resort copy, then the highest
// score, then the most seeders. A release that covers several episodes (a
// multi-episode file or a pack) is queued once: the episodes it covers are not
// downloaded again on their own.
func v2BestGapPicks(results []gh2EpisodeResult) []v2GapPick {
	type episodeKey struct{ season, episode int64 }
	best := map[episodeKey]gh2EpisodeResult{}
	for _, result := range results {
		if result.Season < 1 || result.Episode < 1 || !v2GapReleaseDownloadable(&result.Release) {
			continue
		}
		key := episodeKey{season: result.Season, episode: result.Episode}
		if current, ok := best[key]; !ok || v2GapPickPreferred(result, current) {
			best[key] = result
		}
	}
	// One entry per release: the same pack can be the best copy of several
	// episodes, and it has to reach the engine once.
	candidates := map[string]gh2EpisodeResult{}
	for _, result := range best {
		key := v2GapReleaseIdentity(&result.Release)
		if current, ok := candidates[key]; !ok || v2GapPickPreferred(result, current) {
			candidates[key] = result
		}
	}
	ordered := make([]gh2EpisodeResult, 0, len(candidates))
	for _, result := range candidates {
		ordered = append(ordered, result)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return v2GapPickPreferred(ordered[i], ordered[j])
	})
	covered := map[episodeKey]bool{}
	picks := make([]v2GapPick, 0, len(ordered))
	for _, candidate := range ordered {
		season := int64(0)
		if candidate.Release.Season != nil {
			season = *candidate.Release.Season
		}
		episodes := make([]int64, 0, 1)
		for key := range best {
			if covered[key] || !v2GapReleaseCovers(&candidate.Release, key.season, key.episode) {
				continue
			}
			episodes = append(episodes, key.episode)
		}
		if len(episodes) == 0 {
			continue
		}
		sort.Slice(episodes, func(i, j int) bool { return episodes[i] < episodes[j] })
		picks = append(picks, v2GapPick{Result: candidate, Season: season, Episodes: episodes})
		for key := range best {
			if v2GapReleaseCovers(&candidate.Release, key.season, key.episode) {
				covered[key] = true
			}
		}
	}
	return picks
}

// v2GapAddedEpisodesText names the episodes the bulk action queued, season by
// season and in the compact form of the log: "S01E01–S01E03, S02E04". A pack
// counts once for every episode it fills.
func v2GapAddedEpisodesText(picks []v2GapPick) string {
	bySeason := map[int64][]int64{}
	seasons := make([]int64, 0, 1)
	for _, pick := range picks {
		if _, seen := bySeason[pick.Season]; !seen {
			seasons = append(seasons, pick.Season)
		}
		bySeason[pick.Season] = append(bySeason[pick.Season], pick.Episodes...)
	}
	sort.Slice(seasons, func(i, j int) bool { return seasons[i] < seasons[j] })
	parts := make([]string, 0, len(seasons))
	for _, season := range seasons {
		if label := episodeRangeLabel(season, bySeason[season]); label != "" {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, ", ")
}

// v2GapPickPreferred reports whether candidate is the copy to queue over
// current: the profile beats a last resort, then the score, then the seeders,
// then the origin (a live indexer result over a stored one).
func v2GapPickPreferred(candidate, current gh2EpisodeResult) bool {
	if candidate.Release.LastResort != current.Release.LastResort {
		return !candidate.Release.LastResort
	}
	if candidate.Release.Score != current.Release.Score {
		return candidate.Release.Score > current.Release.Score
	}
	if candidate.Release.Seeders != current.Release.Seeders {
		return candidate.Release.Seeders > current.Release.Seeders
	}
	if rank := v2GapOriginRank(candidate.Origin); rank != v2GapOriginRank(current.Origin) {
		return rank > v2GapOriginRank(current.Origin)
	}
	// Same score, seeders and origin: pick the same one on every run.
	return strings.ToLower(candidate.Release.Title) < strings.ToLower(current.Release.Title)
}

func v2GapOriginRank(origin string) int {
	switch {
	case strings.EqualFold(origin, episodeOriginIndexer):
		return 3
	case strings.EqualFold(origin, episodeOriginArchive):
		return 2
	case strings.EqualFold(origin, episodeOriginFeed):
		return 1
	default:
		return 0
	}
}

// v2GapReleaseDownloadable reports whether a result carries a usable transfer.
func v2GapReleaseDownloadable(release *models.Release) bool {
	if release == nil {
		return false
	}
	if strings.TrimSpace(release.Magnet) != "" || strings.TrimSpace(release.Ed2kLink) != "" {
		return true
	}
	return release.TorrentURL != nil && strings.TrimSpace(*release.TorrentURL) != ""
}

// v2GapReleaseIdentity names a release for de-duplication: its infohash, the
// infohash the indexer declared, its link or, as a last resort, its title.
func v2GapReleaseIdentity(release *models.Release) string {
	if hash, err := releaseIdentityHash(release); err == nil && hash != "" {
		return "hash:" + strings.ToLower(hash)
	}
	if hash := strings.ToLower(strings.TrimSpace(release.InfoHash)); hash != "" {
		return "hash:" + hash
	}
	if strings.TrimSpace(release.Magnet) != "" {
		return "link:" + strings.TrimSpace(release.Magnet)
	}
	if release.TorrentURL != nil && strings.TrimSpace(*release.TorrentURL) != "" {
		return "url:" + strings.TrimSpace(*release.TorrentURL)
	}
	return "title:" + strings.ToLower(strings.TrimSpace(release.Title))
}

// v2GapReleaseCovers reports whether the release holds that episode: its own
// number, its declared range, or the whole season of a complete pack.
func v2GapReleaseCovers(release *models.Release, season, episode int64) bool {
	if release == nil || release.Season == nil || *release.Season != season {
		return false
	}
	if hasCompleteRange(release) {
		return true
	}
	if release.Episode != nil && *release.Episode == episode {
		return true
	}
	for _, value := range release.EpisodeRange {
		if value == episode {
			return true
		}
	}
	return false
}

// V2SeriesGapsListPartial renders only the list of missing episodes, filtered
// by `f`. The search box above it asks for this while the person types, so the
// list narrows without a full page load and the focus stays in the field.
func V2SeriesGapsListPartial(w http.ResponseWriter, r *http.Request, s *AppState) {
	gaps, _ := libraryGaps(s)
	v2Render(w, http.StatusOK, "v2_gaps_list_wrap", v2GapsViewFrom(gaps, r.FormValue("f")), s)
}

// V2SeriesGapsSearch is POST /series/gaps/search: it starts the whole-series
// search as a background job and answers with a fragment that polls it. The
// search used to stop at 15 episodes and hold the request open for minutes;
// the job covers every gap and can be followed (or cancelled) while it runs.
func V2SeriesGapsSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := strings.TrimSpace(r.FormValue("series"))
	filter := strings.TrimSpace(r.FormValue("filter"))
	slot := strings.TrimSpace(r.FormValue("slot"))
	redirect := v2GapSearchRedirect(filter)
	view := v2GapSearchResult{
		Filter:  filter,
		Slot:    slot,
		Sources: v2SourcesView{Redirect: redirect, ShowEpisode: true},
	}
	if name == "" {
		view.Error = "Serie non indicata."
	} else if cfg := latestConfig(s); cfg == nil {
		view.Error = "Ricerca non avviata."
	} else if series := findSeries(cfg, name); series == nil {
		view.Error = "Serie non trovata."
	} else {
		if slot == "series" {
			// Started from the series page: the Download buttons of the results
			// must come back there, not to the Missing tab.
			redirect = "/?view=series&series=" + url.QueryEscape(series.Name)
			view.Sources.Redirect = redirect
		}
		job, created := s.jobs.Create("missing-search", "missing-search:"+strings.ToLower(series.Name))
		if job == nil {
			// The manager is closed (the daemon is shutting down): there is no
			// job to show, and a nil dereference would take the page with it.
			view.Error = "Ricerca non avviata."
		} else if created {
			if err := s.jobs.Start(job.ID, func(ctx context.Context, jobID string) {
				outcome := searchMissingEpisodes(ctx, s, cfg, series, func(done, total int, phase string) {
					progress := 0.0
					if phase == "ed2k" {
						progress = 0.99
					} else if total > 0 {
						progress = float64(done) / float64(total)
					}
					s.jobs.SetProgress(jobID, progress, phase)
					s.jobs.SetResult(jobID, v2GapSearchProgress{Done: done, Total: total, Phase: phase})
				})
				if ctx.Err() != nil {
					return
				}
				s.jobs.SetResult(jobID, v2GapSearchResultFromOutcome(outcome, redirect, series.Name))
				s.jobs.SetProgress(jobID, 1, "done")
			}); err != nil {
				s.jobs.Fail(job.ID, err)
				view.Error = "Ricerca non avviata."
			}
		}
		if view.Error == "" {
			current := *job
			if snapshot, ok := s.jobs.Get(job.ID); ok {
				current = snapshot
			}
			view.Running = true
			view.JobID = job.ID
			view.Progress = v2GapSearchProgress{}
			view.Queued = current.State == JobQueued
			if current.State == JobRunning {
				if progress, ok := current.Result.(v2GapSearchProgress); ok {
					view.Progress = progress
				}
			}
		}
	}
	v2Render(w, http.StatusOK, "v2_gap_search_result", view, s)
}

// V2SeriesGapsSearchStatus is GET /series/gaps/search/status: it renders the
// running progress or, once the job is done, the same result table.
func V2SeriesGapsSearchStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	filter := strings.TrimSpace(r.FormValue("filter"))
	slot := strings.TrimSpace(r.FormValue("slot"))
	view := v2GapSearchResult{
		Filter:  filter,
		Slot:    slot,
		Sources: v2SourcesView{Redirect: v2GapSearchRedirect(filter), ShowEpisode: true},
	}
	id := strings.TrimSpace(r.FormValue("job"))
	job, ok := s.jobs.Get(id)
	if !ok {
		view.Error = "Ricerca non trovata o scaduta."
	} else {
		switch job.State {
		case JobQueued, JobRunning:
			view.Running = true
			view.Queued = job.State == JobQueued
			view.JobID = job.ID
			if progress, ok := job.Result.(v2GapSearchProgress); ok {
				view.Progress = progress
			}
		case JobSucceeded:
			if result, ok := job.Result.(v2GapSearchResult); ok {
				view = result
				view.Filter = filter
				view.Slot = slot
				// The bulk button needs the job to read the chosen releases.
				view.JobID = job.ID
				// The redirect was decided when the job started: the Missing
				// tab comes back to the tab, the series page to the series.
				view.Running = false
			} else {
				view.Error = "Ricerca non riuscita."
			}
		case JobFailed:
			view.Error = job.Error
			if view.Error == "" {
				view.Error = "Ricerca non riuscita."
			}
		default:
			view.Error = "Ricerca annullata."
		}
	}
	v2Render(w, http.StatusOK, "v2_gap_search_result", view, s)
}

// V2SeriesGapsSearchCancel is POST /series/gaps/search/cancel: it asks the job
// to stop and renders the status again, which shows the cancellation when the
// worker has observed it.
func V2SeriesGapsSearchCancel(w http.ResponseWriter, r *http.Request, s *AppState) {
	if id := strings.TrimSpace(r.FormValue("job")); id != "" {
		s.jobs.Cancel(id)
	}
	V2SeriesGapsSearchStatus(w, r, s)
}

// v2GapDownloadBestResult is the answer of the bulk action: a toast, because
// the search results must stay on the page.
type v2GapDownloadBestResult struct {
	Added   int
	Error   bool
	Message string
}

// V2SeriesGapsDownloadBest is POST /series/gaps/download-best: it queues, for
// every missing episode the search found, the best copy (the score, then the
// seeders). It runs as a background job because a `.torrent` link has to be
// opened first: a series with a hundred gaps would otherwise hold the request
// for minutes.
func V2SeriesGapsDownloadBest(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2GapDownloadBestResult{}
	searchID := strings.TrimSpace(r.FormValue("job"))
	job, found := s.jobs.Get(searchID)
	result, resultOK := v2GapSearchResult{}, false
	if found {
		result, resultOK = job.Result.(v2GapSearchResult)
	}
	switch {
	case !found:
		view.Error = true
		view.Message = "Ricerca non trovata o scaduta: rilancia «Cerca mancanti»."
	case job.State != JobSucceeded || !resultOK:
		view.Error = true
		view.Message = "La ricerca non è ancora finita: aspetta i risultati."
	case len(result.picks) == 0:
		view.Error = true
		view.Message = "Nessuna puntata da aggiungere: la ricerca non ha risultati scaricabili."
	default:
		bulk, created := s.jobs.Create("missing-download", "missing-download:"+searchID)
		switch {
		case bulk == nil:
			view.Error = true
			view.Message = "Aggiunta non avviata."
		case !created:
			view.Error = true
			view.Message = "Aggiunta già in corso."
		default:
			picks := result.picks
			series := result.Series
			if err := s.jobs.Start(bulk.ID, func(ctx context.Context, id string) {
				added, rejected := 0, 0
				queued := make([]v2GapPick, 0, len(picks))
				for index, pick := range picks {
					if ctx.Err() != nil {
						return
					}
					status, _ := AddRelease(s, pick.Result.Release)
					if (status >= 200 && status < 300) || status == http.StatusAccepted {
						added++
						queued = append(queued, pick)
					} else {
						rejected++
						logging.Debug("best missing episode not queued",
							"series", series, "title", pick.Result.Release.Title, "status", status)
					}
					s.jobs.SetProgress(id, float64(index+1)/float64(len(picks)), "")
				}
				message := fmt.Sprintf("📥 Added %s of «%s» to the downloads",
					countLabel(int64(added), "best missing episode", "best missing episodes"), series)
				if episodes := v2GapAddedEpisodesText(queued); episodes != "" {
					message += ": " + episodes
				}
				if rejected > 0 {
					message += fmt.Sprintf("; %d could not be started", rejected)
				}
				logging.Info(message)
			}); err != nil {
				s.jobs.Fail(bulk.ID, err)
				view.Error = true
				view.Message = "Aggiunta non avviata."
			} else {
				view.Added = len(picks)
			}
		}
	}
	v2Render(w, http.StatusOK, "v2_gap_download_best_result", view, s)
}
