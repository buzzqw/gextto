package gextto

// uiweb_v2_gaps.go is the Missing tab of the Series page: the missing episodes
// grouped by series and season instead of one flat row per episode. A click on
// an episode opens the same search window as the series page (results from the
// local archive, then online, each with Download); "Cerca mancanti" searches a
// whole series and reports the outcome next to it.

import (
	"encoding/json"
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

// v2GapSearchResult is the outcome shown next to a series after "Cerca
// mancanti".
type v2GapSearchResult struct {
	Error    string
	Results  int
	Episodes int
}

// V2SeriesGapsSearch is POST /series/gaps/search: it searches every missing
// episode of one series and answers with the outcome, shown in place.
func V2SeriesGapsSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2GapSearchResult{}
	name := strings.TrimSpace(r.FormValue("series"))
	if name == "" {
		view.Error = "Serie non indicata."
	} else {
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/series/"+url.PathEscape(name)+"/search-missing", nil, []byte("{}"))
		var payload struct {
			Results  []any `json:"results"`
			Episodes []any `json:"episodes"`
		}
		if status >= http.StatusBadRequest {
			view.Error = v2JSONError(raw)
			if view.Error == "" {
				view.Error = "Ricerca non riuscita."
			}
		} else if json.Unmarshal(raw, &payload) == nil {
			view.Results, view.Episodes = len(payload.Results), len(payload.Episodes)
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_gap_search_result", view, dict, eng)
}
