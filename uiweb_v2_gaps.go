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
	Sources  v2SourcesView
	Running  bool
	Queued   bool
	JobID    string
	Filter   string
	Slot     string
	Progress v2GapSearchProgress
}

func v2GapSearchRedirect(filter string) string {
	redirect := "/?view=series&tab=gaps"
	if filter = strings.TrimSpace(filter); filter != "" {
		redirect += "&f=" + url.QueryEscape(filter)
	}
	return redirect
}

func v2GapSearchResultFromOutcome(outcome gh2_missingSearchOutcome, redirect string) v2GapSearchResult {
	view := v2GapSearchResult{
		Episodes: outcome.Searched,
		Total:    outcome.TotalMissing,
		Sources:  v2SourcesView{Redirect: redirect, ShowEpisode: true},
	}
	for _, result := range outcome.Results {
		view.Sources.Results = append(view.Sources.Results,
			v2SourceRowFromRelease(&result.Release, result.Origin, result.Season, result.Episode))
	}
	view.Results = len(view.Sources.Results)
	return view
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
	} else if series := gh2_findSeries(cfg, name); series == nil {
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
				outcome := gh2_searchMissingEpisodes(ctx, s, cfg, series, func(done, total int, phase string) {
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
				s.jobs.SetResult(jobID, v2GapSearchResultFromOutcome(outcome, redirect))
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
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_gap_search_result", view, dict, eng)
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
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_gap_search_result", view, dict, eng)
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
