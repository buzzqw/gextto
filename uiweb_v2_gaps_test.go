package gextto

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
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
