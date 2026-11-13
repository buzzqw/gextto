package gextto

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// TestGapsGroupedBySeriesAndSeason: the Missing tab shows one block per series
// (alphabetical) with its seasons in order and the episodes as codes, not one
// flat row per episode; the filter keeps the matching series and opens them.
func TestGapsGroupedBySeriesAndSeason(t *testing.T) {
	gaps := []libraryGap{
		{Series: "Walker", Season: 4, Episode: 3, AirDate: "2024-03-01"},
		{Series: "FBI", Season: 6, Episode: 2},
		{Series: "Walker", Season: 3, Episode: 9, AirDate: "2023-05-01"},
		{Series: "FBI", Season: 6, Episode: 1},
		{Series: "Walker", Season: 4, Episode: 1, AirDate: "2024-02-15"},
		{Series: "FBI: International", Season: 2, Episode: 5},
		{Series: "Dark Matter", Season: 1, Episode: 1},
		{Series: "New Tricks", Season: 1, Episode: 1},
	}
	view := v2GapsViewFrom(gaps, "")
	if view.Total != 8 || view.SeriesCount != 5 || view.Open {
		t.Fatalf("total=%d series=%d open=%v", view.Total, view.SeriesCount, view.Open)
	}
	if view.Series[0].Name != "Dark Matter" || view.Series[1].Name != "FBI" || view.Series[4].Name != "Walker" {
		t.Fatalf("order = %v", []string{view.Series[0].Name, view.Series[1].Name, view.Series[4].Name})
	}
	walker := view.Series[4]
	if walker.Count != 3 || walker.Latest != "2024-03-01" || len(walker.Seasons) != 2 {
		t.Fatalf("walker = %+v", walker)
	}
	if walker.Seasons[0].Season != 3 || walker.Seasons[1].Season != 4 ||
		walker.Seasons[1].Episodes[0].Code != "S04E01" || walker.Seasons[1].Episodes[1].Code != "S04E03" {
		t.Fatalf("walker seasons = %+v", walker.Seasons)
	}
	filtered := v2GapsViewFrom(gaps, "fbi")
	if filtered.SeriesCount != 2 || filtered.Total != 3 || !filtered.Open {
		t.Fatalf("filtered = %+v", filtered)
	}
}

// TestGapSearchRowsCarryTheEpisode: the bulk search used to report only a
// number, so the operator could not tell which episode a result belonged to.
// The row keeps the episode code next to the release.
func TestGapSearchRowsCarryTheEpisode(t *testing.T) {
	rows := v2SourceRowsFrom([]map[string]any{{
		"release": map[string]any{
			"title":  "FBI S06E03 1080p ITA",
			"magnet": "magnet:?xt=urn:btih:" + strings.Repeat("a", 40),
			"source": "indexer.one",
		},
		"origin":  "indexer",
		"season":  float64(6),
		"episode": float64(3),
	}})
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Episode != "S06E03" || rows[0].Title != "FBI S06E03 1080p ITA" || rows[0].Body == "" {
		t.Fatalf("row = %+v", rows[0])
	}
}

// TestGapSearchResultListsTheReleases: the panel after "Cerca mancanti" must
// show the releases with their Download button, not only a count with a hint
// to click an episode, and it must say that nothing was downloaded.
func TestGapSearchResultListsTheReleases(t *testing.T) {
	view := v2GapSearchResult{
		Results:  1,
		Episodes: 15,
		Total:    40,
		Sources: v2SourcesView{
			Redirect:    "/?view=series&tab=gaps",
			ShowEpisode: true,
			Results: []v2SourceRow{{
				Title:   "FBI S06E03 1080p ITA",
				Episode: "S06E03",
				Source:  "indexer.one",
				Score:   90,
				Body:    `{"items":[{"title":"FBI S06E03 1080p ITA","magnet":"magnet:?xt=urn:btih:` + strings.Repeat("a", 40) + `"}]}`,
			}},
		},
	}
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_gap_search_result", view); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buffer.String()
	for _, want := range []string{
		"Risultati:", "Puntate cercate:", "S06E03", "FBI S06E03 1080p ITA",
		"Scarica", "Nessun download automatico",
		`name="path" value="/api/archive/batch-download"`,
		`name="stay" value="1"`,
		`name="stay_title" value="Download"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "clicca una puntata") {
		t.Fatalf("old hint still in:\n%s", body)
	}
}

// TestGapDownloadKeepsTheResultsList: the Download button of a result must not
// navigate away. A whole-series search returns a long list and the operator
// queues several releases one after the other: a page reload emptied the list
// and hid the results after the first click. The answer is a toast instead.
func TestGapDownloadKeepsTheResultsList(t *testing.T) {
	state := newTestAppState(t)
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatalf("complete setup: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	release := `{"items":[{"title":"FBI S06E03 1080p ITA","magnet":"magnet:?xt=urn:btih:` + strings.Repeat("a", 40) + `"}]}`
	form := url.Values{
		"path":       {"/api/archive/batch-download"},
		"method":     {"POST"},
		"body":       {release},
		"stay":       {"1"},
		"stay_title": {"Download"},
		"redirect":   {"/?view=series&tab=gaps"},
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/section/action", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("section action: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stay action -> %d, want 200", response.StatusCode)
	}
	if redirect := response.Header.Get("HX-Redirect"); redirect != "" {
		t.Fatalf("stay action must not reload the page, got HX-Redirect %q", redirect)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="v2-toast-region" hx-swap-oob="innerHTML"`,
		`class="v2-toast"`,
		"Download",
		"Torrent aggiunto ai download con successo.",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

// TestGapSearchRunningShowsProgressAndCancel: the whole-series search runs as a
// background job, so the panel must say how far it is and offer a way to stop it
// instead of holding an HTTP request open.
func TestGapSearchRunningShowsProgressAndCancel(t *testing.T) {
	view := v2GapSearchResult{
		Running:  true,
		JobID:    "job-1",
		Filter:   "fbi",
		Slot:     "3",
		Progress: v2GapSearchProgress{Done: 3, Total: 52, Phase: "episodes"},
	}
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_gap_search_result", view); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buffer.String()
	for _, want := range []string{
		"Ricerca in corso:", "3/52", "puntate…", "Annulla",
		"/series/gaps/search/status?job=job-1", "/series/gaps/search/cancel",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

// TestGapSearchRowsCarryTheLastResortMark: the panel lists the outside-profile
// copy the cycle would take, so it must say what it is instead of showing a
// 480p download as a normal result.
func TestGapSearchRowsCarryTheLastResortMark(t *testing.T) {
	rows := v2SourceRowsFrom([]map[string]any{{
		"release": map[string]any{
			"title":       "FBI S06E03 480p ITA",
			"magnet":      "magnet:?xt=urn:btih:" + strings.Repeat("a", 40),
			"source":      "indexer.one",
			"last_resort": true,
		},
		"origin": "indexer",
	}})
	if len(rows) != 1 || !rows[0].LastResort {
		t.Fatalf("rows = %+v", rows)
	}
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_sources_rows", v2SourcesView{ShowEpisode: true, Results: rows}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buffer.String(), "ultima risorsa") {
		t.Fatalf("badge missing:\n%s", buffer.String())
	}
}

// TestSeriesDetailStartsTheBackgroundSearch: the detail page used to call the
// synchronous /api/series/{name}/search-missing, which has no episode cap any
// more: with a series full of gaps it held the request open for minutes. It
// must use the same background job as the Missing tab.
func TestSeriesDetailStartsTheBackgroundSearch(t *testing.T) {
	raw, err := os.ReadFile("uiweb/v2/templates/titles.html")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "/api/series/%s/search-missing") {
		t.Fatal("the series detail page still calls the synchronous missing search")
	}
	if !strings.Contains(body, `hx-post="/series/gaps/search"`) {
		t.Fatal("the series detail page does not start the background missing search")
	}
	if !strings.Contains(body, `id="v2-gap-result-series"`) {
		t.Fatal("the series detail page has no target for the search job")
	}
}

// gapTestRelease builds one search result of a missing episode.
func gapTestRelease(title, hash string, season, episode int64, score, seeders int64, lastResort bool) models.Release {
	s := season
	e := episode
	return models.Release{
		Title:      title,
		Magnet:     "magnet:?xt=urn:btih:" + hash,
		Season:     &s,
		Episode:    &e,
		Kind:       "series",
		Score:      score,
		Seeders:    seeders,
		LastResort: lastResort,
	}
}

// TestGapBestPicksChooseScoreThenSeeders: the bulk button queues one copy per
// missing episode, inside the profile first, then the highest score, then the
// most seeders.
func TestGapBestPicksChooseScoreThenSeeders(t *testing.T) {
	results := []gh2EpisodeResult{
		{Release: gapTestRelease("ep1 high seed", strings.Repeat("1", 40), 4, 1, 100, 90, false), Origin: episodeOriginIndexer, Season: 4, Episode: 1},
		{Release: gapTestRelease("ep1 high score", strings.Repeat("2", 40), 4, 1, 300, 2, false), Origin: episodeOriginIndexer, Season: 4, Episode: 1},
		{Release: gapTestRelease("ep1 last resort", strings.Repeat("3", 40), 4, 1, 900, 100, true), Origin: episodeOriginIndexer, Season: 4, Episode: 1},
		{Release: gapTestRelease("ep2 few seeds", strings.Repeat("4", 40), 4, 2, 150, 3, false), Origin: episodeOriginIndexer, Season: 4, Episode: 2},
		{Release: gapTestRelease("ep2 many seeds", strings.Repeat("5", 40), 4, 2, 150, 40, false), Origin: episodeOriginIndexer, Season: 4, Episode: 2},
	}
	picks := v2BestGapPicks(results)
	if len(picks) != 2 {
		t.Fatalf("picks = %d, want one per episode: %+v", len(picks), picks)
	}
	chosen := map[int64]string{}
	for _, pick := range picks {
		chosen[pick.Episode] = pick.Release.Title
	}
	if chosen[1] != "ep1 high score" {
		t.Fatalf("episode 1 pick = %q, want the highest score inside the profile", chosen[1])
	}
	if chosen[2] != "ep2 many seeds" {
		t.Fatalf("episode 2 pick = %q, want the most seeders at equal score", chosen[2])
	}
}

// TestGapBestPicksQueueAPackOnce: a release covering two episodes reaches the
// engine once, and a higher-scored single episode is still preferred for its
// own episode.
func TestGapBestPicksQueueAPackOnce(t *testing.T) {
	pack := gapTestRelease("pack 01-02", strings.Repeat("a", 40), 4, 1, 200, 10, false)
	pack.IsPack = true
	pack.EpisodeRange = []int64{1, 2}
	packB := pack
	packB.Episode = int64Ptr(2)
	single := gapTestRelease("ep2 single", strings.Repeat("b", 40), 4, 2, 500, 5, false)
	picks := v2BestGapPicks([]gh2EpisodeResult{
		{Release: pack, Origin: episodeOriginIndexer, Season: 4, Episode: 1},
		{Release: packB, Origin: episodeOriginIndexer, Season: 4, Episode: 2},
		{Release: single, Origin: episodeOriginIndexer, Season: 4, Episode: 2},
	})
	if len(picks) != 2 {
		t.Fatalf("picks = %d, want the single episode and the pack once: %+v", len(picks), picks)
	}
	titles := []string{picks[0].Release.Title, picks[1].Release.Title}
	if titles[0] != "ep2 single" || titles[1] != "pack 01-02" {
		t.Fatalf("pick order = %v, want the highest score first", titles)
	}
}

// TestGapDownloadBestRoute: a completed search exposes the bulk button, and
// its action queues the chosen releases as a background job.
func TestGapDownloadBestRoute(t *testing.T) {
	state := newCycleState(t)
	t.Cleanup(state.jobs.Close)
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatalf("complete setup: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	release := gapTestRelease("Show S01E01 1080p", strings.Repeat("c", 40), 1, 1, 500, 12, false)
	search, created := state.jobs.Create("missing-search", "missing-search:show")
	if !created {
		t.Fatal("search job not created")
	}
	if err := state.jobs.Start(search.ID, func(ctx context.Context, id string) {
		state.jobs.SetResult(id, v2GapSearchResult{
			Series: "Show", Episodes: 1, Total: 1, Results: 1,
			Sources: v2SourcesView{Results: []v2SourceRow{{Title: release.Title, Episode: "S01E01", Key: "k"}}},
			picks:   []gh2EpisodeResult{{Release: release, Origin: episodeOriginIndexer, Season: 1, Episode: 1}},
		})
	}); err != nil {
		t.Fatalf("start search: %v", err)
	}
	waitJobState(t, state.jobs, search.ID, JobSucceeded)

	code, body := v2Request(t, server, http.MethodGet, "/series/gaps/search/status?job="+url.QueryEscape(search.ID), nil)
	if code != http.StatusOK || !strings.Contains(body, "Aggiungi le migliori puntate mancanti") {
		t.Fatalf("result fragment has no bulk button -> %d: %s", code, body)
	}

	response, err := http.PostForm(server.URL+"/series/gaps/download-best", url.Values{"job": {search.ID}})
	if err != nil {
		t.Fatalf("post bulk action: %v", err)
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(answer), "1 download") {
		t.Fatalf("bulk action -> %d: %s", response.StatusCode, answer)
	}
	bulk := state.jobs.List("missing-download", "")
	if len(bulk) != 1 {
		t.Fatalf("bulk jobs = %d, want 1", len(bulk))
	}
	waitJobState(t, state.jobs, bulk[0].ID, JobSucceeded)
	meta, err := state.db.TorrentMeta(strings.Repeat("c", 40))
	if err != nil || meta == nil {
		t.Fatalf("chosen release not queued: %v %+v", err, meta)
	}
}
