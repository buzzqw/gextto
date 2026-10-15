package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
)

func TestSeasonsToEpisodeFilter(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "1": "1x*", "1+": "1x01-", "2+": "2x01-", "1-3": "1x01-3x99",
	} {
		if got := seasonsToEpisodeFilter(in); got != want {
			t.Errorf("seasonsToEpisodeFilter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSeasonAllowed(t *testing.T) {
	for seasons, want := range map[string]map[int]bool{
		"":    {1: true, 9: true},
		"1":   {1: true, 2: false},
		"1+":  {1: true, 5: true, 0: false},
		"2-4": {1: false, 2: true, 4: true, 5: false},
	} {
		for season, ok := range want {
			if got := seasonAllowed(seasons, season); got != ok {
				t.Errorf("seasonAllowed(%q,%d)=%v want %v", seasons, season, got, ok)
			}
		}
	}
}

func TestUIWantedAddEditRemove(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	post := func(values url.Values) {
		t.Helper()
		resp, err := http.PostForm(srv.URL+"/ui/wanted", values)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	post(url.Values{"op": {"add"}, "name": {"Reacher"}, "kind": {"series"}, "seasons": {"1+"}, "quality": {"1080p"}})
	if got := d.wantedList(); len(got) != 1 || got[0].Name != "Reacher" {
		t.Fatalf("wanted = %+v", got)
	}

	// Edit by index renames it.
	post(url.Values{"op": {"add"}, "index": {"0"}, "name": {"Reacher Ray"}, "kind": {"series"}, "seasons": {"2+"}})
	if got := d.wantedList(); len(got) != 1 || got[0].Name != "Reacher Ray" || got[0].Seasons != "2+" {
		t.Fatalf("after edit = %+v", got)
	}

	// A second entry, then remove the first.
	post(url.Values{"op": {"add"}, "name": {"Big Movie"}, "kind": {"movie"}})
	if got := d.wantedList(); len(got) != 2 {
		t.Fatalf("wanted = %+v", got)
	}
	post(url.Values{"op": {"remove"}, "index": {"0"}})
	if got := d.wantedList(); len(got) != 1 || got[0].Name != "Big Movie" {
		t.Fatalf("after remove = %+v", got)
	}

	// An invalid episode filter is rejected.
	post(url.Values{"op": {"add"}, "name": {"Bad"}, "kind": {"series"}, "episode_filter": {"nope"}})
	for _, w := range d.wantedList() {
		if w.Name == "Bad" {
			t.Fatal("an invalid episode filter must be rejected")
		}
	}
}

func TestWantedSeenTakenMissing(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Reacher","kind":"series","seasons":"1+"}]`)
	w := d.wantedList()[0]
	now := time.Now()

	// No downloadable link: the episode is only recorded as seen.
	d.applyWanted(now, "f", []rss.Item{{Title: "Reacher S01E01 1080p", GUID: "g1"}})
	missing := d.wantedMissing(w)
	if len(missing) != 1 || missing[0].season != 1 || missing[0].episode != 1 {
		t.Fatalf("missing = %+v", missing)
	}

	// A season outside the spec never matches (the episode filter rejects
	// it): "1+" leaves out the specials (season 0) and takes every later one.
	d.applyWanted(now, "f", []rss.Item{{Title: "Reacher S00E01 1080p", GUID: "g0"}})
	if got := d.wantedMissing(w); len(got) != 1 {
		t.Fatalf("a season outside the spec must not appear: %+v", got)
	}
	d.applyWanted(now, "f", []rss.Item{{Title: "Reacher S09E01 1080p", GUID: "g9"}})
	if got := d.wantedMissing(w); len(got) != 2 {
		t.Fatalf("a later season is inside 1+: %+v", got)
	}
	d.recordWantedTaken(w, 9, 1)

	// Once taken it is no longer missing.
	d.recordWantedTaken(w, 1, 1)
	if got := d.wantedMissing(w); len(got) != 0 {
		t.Fatalf("missing after taken = %+v", got)
	}
}

// TestWantedRowListsHaveAndMissing checks the wanted row shows which episodes
// were taken (Have) and which were seen but not downloaded (Missing): a season
// with no gap collapses to its season, a gap lists the episodes.
func TestWantedRowListsHaveAndMissing(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Show","kind":"series","seasons":"1+"}]`)
	w := d.wantedList()[0]
	d.recordWantedSeen(w, 1, 1)
	d.recordWantedTaken(w, 1, 1)
	row := d.uiWantedRowsHTML()
	if strings.Contains(row, "Nothing missing") || strings.Contains(row, "<span>taken</span>") {
		t.Fatalf("the wanted row must list the episodes, not a count: %s", row)
	}
	// A season with no gap collapses to its season alone.
	if !strings.Contains(row, `>Have</span> S01`) || strings.Contains(row, ">Missing</span>") {
		t.Fatalf("a complete season must collapse and show no gap: %s", row)
	}
	// A seen-but-not-taken episode opens a gap: Have lists the episodes held,
	// Missing flags the episode seen but not downloaded.
	d.recordWantedSeen(w, 1, 2)
	row = d.uiWantedRowsHTML()
	if !strings.Contains(row, `>Have</span> S01E01`) || !strings.Contains(row, `>Missing</span> S01E02`) {
		t.Fatalf("the gap must be listed: %s", row)
	}
}

// TestFormatHaveEpisodes checks that complete seasons collapse and seasons with
// a gap list their episodes.
func TestFormatHaveEpisodes(t *testing.T) {
	if got := formatHaveEpisodes(
		[]wantedEpisode{{1, 1}, {1, 2}, {1, 3}}, nil, 24); got != "S01" {
		t.Fatalf("complete season = %q, want S01", got)
	}
	if got := formatHaveEpisodes(
		[]wantedEpisode{{1, 1}, {1, 2}, {1, 3}}, []wantedEpisode{{1, 5}}, 24); got != "S01E01–E03" {
		t.Fatalf("season with a gap = %q, want S01E01–E03", got)
	}
	if got := formatHaveEpisodes(
		[]wantedEpisode{{1, 1}, {1, 2}, {2, 1}, {2, 2}}, []wantedEpisode{{2, 3}}, 24); got != "S01, S02E01–E02" {
		t.Fatalf("mixed seasons = %q, want S01, S02E01–E02", got)
	}
}

// TestUIWantedEpisodesManager checks the episode manager of the wanted page: it
// lists the known episodes, marks one present/missing and adds one.
func TestUIWantedEpisodesManager(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Show","kind":"series","seasons":"1+"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	list := func() []wantedEpisodeView {
		t.Helper()
		resp, err := http.Get(srv.URL + "/ui/wanted-episodes?index=0")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /ui/wanted-episodes -> %d", resp.StatusCode)
		}
		var payload struct {
			Episodes []wantedEpisodeView `json:"episodes"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return payload.Episodes
	}
	post := func(values url.Values) []wantedEpisodeView {
		t.Helper()
		resp, err := http.PostForm(srv.URL+"/ui/wanted-episodes", values)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /ui/wanted-episodes -> %d", resp.StatusCode)
		}
		var payload struct {
			Episodes []wantedEpisodeView `json:"episodes"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return payload.Episodes
	}

	if got := list(); len(got) != 0 {
		t.Fatalf("no episodes expected yet: %+v", got)
	}
	// Mark S01E01 as downloaded.
	if eps := post(url.Values{"op": {"have"}, "index": {"0"}, "season": {"1"}, "episode": {"1"}}); len(eps) != 1 || !eps[0].Have {
		t.Fatalf("after have: %+v", eps)
	}
	// Add S01E02 as missing (never seen).
	eps := post(url.Values{"op": {"add"}, "index": {"0"}, "season": {"1"}, "episode": {"2"}})
	if len(eps) != 2 || eps[0].Episode != 1 || !eps[0].Have || eps[1].Have {
		t.Fatalf("after add: %+v", eps)
	}
	// Mark S01E01 missing again: it is searched once more.
	eps = post(url.Values{"op": {"missing"}, "index": {"0"}, "season": {"1"}, "episode": {"1"}})
	if len(eps) != 2 || eps[0].Have {
		t.Fatalf("after missing: %+v", eps)
	}
	// A movie has no episodes.
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Film","kind":"movie"}]`)
	resp, err := http.PostForm(srv.URL+"/ui/wanted-episodes", url.Values{"op": {"have"}, "index": {"0"}, "season": {"1"}, "episode": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a movie must have no episodes: %d", resp.StatusCode)
	}
}

// TestFormatEpisodeRanges checks the Have/Missing episode compaction.
func TestFormatEpisodeRanges(t *testing.T) {
	eps := []wantedEpisode{{1, 1}, {1, 2}, {1, 3}, {1, 5}, {2, 1}}
	if got := formatEpisodeRanges(eps, 24); got != "S01E01–E03, S01E05, S02E01" {
		t.Fatalf("ranges = %q", got)
	}
	// The cap keeps at most max episodes and marks the rest.
	many := make([]wantedEpisode, 0, 30)
	for i := 1; i <= 30; i++ {
		many = append(many, wantedEpisode{1, i})
	}
	if got := formatEpisodeRanges(many, 24); got != "S01E01–E24 +6" {
		t.Fatalf("capped ranges = %q", got)
	}
}

// TestPollFeedsDrivesWantedWithoutFeeds guards the wanted engine running even
// when no RSS feed is configured: the wanted list searches the indexers
// directly, so pollFeeds must not return early and skip pollWanted.
func TestPollFeedsDrivesWantedWithoutFeeds(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Reacher","kind":"series","seasons":"1+"}]`)
	if len(d.feeds()) != 0 {
		t.Fatal("this test needs zero feeds")
	}
	d.pollFeeds()
	if _, ok := d.state.WantedGapLast["Reacher"]; !ok {
		t.Fatalf("pollWanted did not run without feeds: WantedGapLast=%v", d.state.WantedGapLast)
	}
}

// TestPollWantedTakesFromTheArchive fixes the model: feeds only grow the
// archive, and a wanted title turns the archived material into downloads.
func TestPollWantedTakesFromTheArchive(t *testing.T) {
	d := feedDaemon(t, "")
	if _, err := d.archive().record("feedA", []rss.Item{{
		Title:  "The Mandalorian S01E01 1080p ITA WEB-DL",
		Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
	}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := d.saveWanted([]wantedEntry{{Name: "The Mandalorian", Kind: "series", Seasons: "1+", Language: "ita", Quality: "1080p"}}); err != nil {
		t.Fatal(err)
	}
	d.pollWanted(context.Background())
	waitFor(t, "the archived episode to be taken", func() bool {
		return len(d.snapshotViews()) == 1
	})
}

func TestWantedMatchesTitleAndFilters(t *testing.T) {
	w := wantedEntry{Name: "Mamma Mia", Kind: "series", Seasons: "1", Language: "ita", Quality: "1080p"}
	now := time.Now()
	if !w.matches("Mamma.Mia.S01E03.1080p.ITA", now) {
		t.Fatal("a matching title must match")
	}
	if w.matches("Mamma.Mia.S01E03.1080p.ENG", now) {
		t.Fatal("a title without the language must not match")
	}
	if w.matches("Mamma.Mia.S02E03.1080p.ITA", now) {
		t.Fatal("a season outside the spec must not match")
	}
	if w.matches("Altra.Serie.S01E03.1080p.ITA", now) {
		t.Fatal("a different title must not match")
	}
}

// TestWantedMatchesSubtitlesOnTheReleaseName: the Subtitles field is matched on
// the release name, like the language terms.
func TestWantedMatchesSubtitlesOnTheReleaseName(t *testing.T) {
	w := wantedEntry{Name: "Mamma Mia", Kind: "series", Seasons: "1", Subtitles: "sub ita"}
	now := time.Now()
	if !w.matches("Mamma.Mia.S01E03.SUB.ITA.1080p", now) {
		t.Fatal("a title carrying the subtitle term must match")
	}
	if w.matches("Mamma.Mia.S01E03.1080p", now) {
		t.Fatal("a title without the subtitle term must not match")
	}
}

func TestWantedQualityMatch(t *testing.T) {
	tests := []struct {
		req   string
		title string
		want  bool
	}{
		{"", "Movie.1080p", true},
		{"Any", "Movie.720p", true},
		{"1080p", "Movie.1080p.WEB-DL", true},
		{"1080p", "Movie.720p.WEB-DL", false},
		{"720p+", "Movie.720p.HDTV", true},
		{"720p+", "Movie.1080p.BluRay", true},
		{"720p+", "Movie.2160p.UHD", true},
		{"720p+", "Movie.480p.DVDRip", false},
		{"1080p+", "Movie.720p.HDTV", false},
		{"1080p+", "Movie.1080p.BluRay", true},
		{"1080p+", "Movie.4k.UHD", true},
		{"720p-1080p", "Movie.720p", true},
		{"720p-1080p", "Movie.1080p", true},
		{"720p-1080p", "Movie.2160p", false},
		{"720p-1080p", "Movie.480p", false},
		{"2160p 4K", "Movie.4K.HDR", true},
		{"2160p 4K", "Movie.2160p.UHD", true},
		{"2160p 4K", "Movie.1080p", false},
		{"2160p+ 4K+", "Movie.2160p.HDR", true},
		{"2160p+ 4K+", "Movie.1080p", false},
	}
	for _, tc := range tests {
		if got := wantedQualityMatch(tc.req, strings.ToLower(tc.title)); got != tc.want {
			t.Errorf("wantedQualityMatch(%q, %q) = %v, want %v", tc.req, tc.title, got, tc.want)
		}
	}
}

func TestUIWantedPageAndPollEndpoints(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Show","kind":"series","seasons":"1+"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	// GET /ui/wanted serves HTML page.
	resp, err := http.Get(srv.URL + "/ui/wanted")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/wanted status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(raw), "Wanted titles") {
		t.Fatalf("page missing Wanted titles: %s", string(raw))
	}

	// POST /ui/wanted toggle-pause
	post := func(path string, vals url.Values) *http.Response {
		r, err := http.PostForm(srv.URL+path, vals)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := post("/ui/wanted", url.Values{"op": {"toggle-pause"}, "index": {"0"}})
	r.Body.Close()
	if list := d.wantedList(); len(list) != 1 || !list[0].Paused {
		t.Fatalf("wanted[0] not paused: %+v", list)
	}

	// POST /ui/wanted-poll returns JSON.
	r = post("/ui/wanted-poll", url.Values{"name": {"Show"}})
	var pollRes wantedPollResult
	if err := json.NewDecoder(r.Body).Decode(&pollRes); err != nil {
		t.Fatalf("poll JSON error: %v", err)
	}
	r.Body.Close()
	if !pollRes.OK {
		t.Fatalf("pollRes not OK: %+v", pollRes)
	}
}

// TestPruneWantedTimers: the search timestamps of a removed title are dropped,
// those of a listed title and the episode ledgers are kept.
func TestPruneWantedTimers(t *testing.T) {
	d := newTestDaemon(t)
	d.mu.Lock()
	d.state.WantedSearchLast = map[string]int64{"Reacher": 1, "Gone": 2}
	d.state.WantedGapLast = map[string]int64{"Reacher": 1, "Gone": 2}
	d.state.WantedTaken = map[string]smartEpisodes{wantedKey("Gone", 1): {}}
	d.mu.Unlock()

	d.pruneWantedTimers([]wantedEntry{{Name: " Reacher "}})

	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.state.WantedSearchLast["Gone"]; ok {
		t.Fatal("removed title kept its indexer timestamp")
	}
	if _, ok := d.state.WantedGapLast["Gone"]; ok {
		t.Fatal("removed title kept its gap timestamp")
	}
	if d.state.WantedSearchLast["Reacher"] != 1 || d.state.WantedGapLast["Reacher"] != 1 {
		t.Fatal("a listed title lost its timestamps")
	}
	if len(d.state.WantedTaken) != 1 {
		t.Fatal("the taken ledger must survive (no re-download on re-add)")
	}
}
