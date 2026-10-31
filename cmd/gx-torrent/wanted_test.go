package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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

// TestWantedStatsForSeries checks the per-season picture the page shows: what
// is downloaded, what is still downloading and what was seen but not taken.
func TestWantedStatsForSeries(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Show","kind":"series","seasons":"1+"}]`)
	w := d.wantedList()[0]
	// Seen and taken E01..E03: three downloaded, nothing missing.
	for ep := 1; ep <= 3; ep++ {
		d.recordWantedSeen(w, 1, ep)
		d.recordWantedTaken(w, 1, ep)
	}
	stats := d.wantedStatsFor(w)
	if stats.Downloaded != 3 || stats.Missing != 0 || stats.Downloading != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(stats.Seasons) != 1 || stats.Seasons[0].Season != 1 || stats.Seasons[0].Downloaded != 3 {
		t.Fatalf("seasons = %+v", stats.Seasons)
	}
	// E05 seen but not taken: one known missing episode.
	d.recordWantedSeen(w, 1, 5)
	stats = d.wantedStatsFor(w)
	if stats.Downloaded != 3 || stats.Missing != 1 {
		t.Fatalf("stats after a gap = %+v", stats)
	}
	if stats.Seasons[0].Missing != 1 {
		t.Fatalf("the season must carry the gap: %+v", stats.Seasons)
	}
}

// TestWantedRowShowsCounts checks the wanted row counts what is downloaded and
// never presents a partly-seen season as complete (the old "Have S01" made a
// season of three seen episodes read as the whole season).
func TestWantedRowShowsCounts(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Show","kind":"series","seasons":"1+"}]`)
	w := d.wantedList()[0]
	for ep := 1; ep <= 3; ep++ {
		d.recordWantedSeen(w, 1, ep)
		d.recordWantedTaken(w, 1, ep)
	}
	row := d.uiWantedRowsHTML()
	if strings.Contains(row, `>Have</span>`) || strings.Contains(row, ">Missing</span>") {
		t.Fatalf("the row must use the new counts, not Have/Missing: %s", row)
	}
	if !strings.Contains(row, `>3</b> <span>Downloaded</span>`) {
		t.Fatalf("the row must count the downloaded episodes: %s", row)
	}
	if !strings.Contains(row, "seasonchip") {
		t.Fatalf("the row must carry a per-season chip: %s", row)
	}
	if !strings.Contains(row, `data-wanted-state="uptodate"`) || !strings.Contains(row, "wprogress") {
		t.Fatalf("the row must carry its state and a progress bar: %s", row)
	}
	if !strings.Contains(row, "Why not?") || !strings.Contains(row, `class="row-more"`) {
		t.Fatalf("the row must keep the secondary actions in the More menu: %s", row)
	}
	// A seen-but-not-taken episode shows as such.
	d.recordWantedSeen(w, 1, 5)
	row = d.uiWantedRowsHTML()
	if !strings.Contains(row, "Found, not downloaded") {
		t.Fatalf("a known missing episode must be flagged: %s", row)
	}
	if !strings.Contains(row, `data-wanted-state="missing"`) || !strings.Contains(row, "Search missing") {
		t.Fatalf("a title with a gap must offer Search missing: %s", row)
	}
}

// TestWantedTitleState fixes the badge of a wanted title: paused, downloading,
// to-complete, up to date or still waiting.
func TestWantedTitleState(t *testing.T) {
	if got := wantedTitleState(wantedEntry{}, wantedStats{}); got != "waiting" {
		t.Fatalf("no episodes = %q, want waiting", got)
	}
	if got := wantedTitleState(wantedEntry{}, wantedStats{Downloaded: 2}); got != "uptodate" {
		t.Fatalf("downloaded only = %q, want uptodate", got)
	}
	if got := wantedTitleState(wantedEntry{}, wantedStats{Downloaded: 2, Missing: 1}); got != "missing" {
		t.Fatalf("with a gap = %q, want missing", got)
	}
	if got := wantedTitleState(wantedEntry{}, wantedStats{Downloaded: 2, Downloading: 1}); got != "downloading" {
		t.Fatalf("with a download running = %q, want downloading", got)
	}
	if got := wantedTitleState(wantedEntry{Paused: true}, wantedStats{Missing: 3}); got != "paused" {
		t.Fatalf("paused = %q, want paused", got)
	}
}

// TestWantedEpisodeViewsStates checks the episode grid states: an episode still
// downloading in the session reads "downloading" and carries the torrent hash
// to link to it, a downloaded one reads "downloaded".
func TestWantedEpisodeViewsStates(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Show","kind":"series","seasons":"1+"}]`)
	w := d.wantedList()[0]
	d.recordWantedSeen(w, 1, 1)
	d.recordWantedTaken(w, 1, 1)
	d.recordWantedSeen(w, 1, 2)
	d.recordWantedSeen(w, 1, 3) // seen only: missing
	d.snapMu.Lock()
	d.snapshot = []torrentInfo{{Hash: "abc", Name: "Show S01E02 1080p", State: "downloading", Progress: 42}}
	d.snapMu.Unlock()

	views := d.wantedEpisodeViews(w)
	if len(views) != 3 {
		t.Fatalf("views = %+v", views)
	}
	if views[0].Episode != 1 || views[0].State != wantedEpisodeDownloaded || !views[0].Have {
		t.Fatalf("E01 = %+v", views[0])
	}
	if views[1].Episode != 2 || views[1].State != wantedEpisodeDownloading || views[1].Hash != "abc" {
		t.Fatalf("E02 = %+v", views[1])
	}
	if views[2].Episode != 3 || views[2].State != wantedEpisodeMissing || views[2].Have {
		t.Fatalf("E03 = %+v", views[2])
	}
	if got := d.wantedStatsFor(w); got.Downloading != 1 || got.Downloaded != 1 || got.Missing != 1 {
		t.Fatalf("stats = %+v", got)
	}
}

// TestWantedMovieStatusLinksToTorrent checks a wanted movie in the session
// reads as downloaded and links to its torrent, so the operator can see it.
func TestWantedMovieStatusLinksToTorrent(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Big Movie","kind":"movie"}]`)
	d.snapMu.Lock()
	d.snapshot = []torrentInfo{{Hash: "deadbeef", Name: "Big Movie 2024 1080p", State: "seeding", Progress: 100}}
	d.snapMu.Unlock()

	row := d.uiWantedRowsHTML()
	if !strings.Contains(row, `/?open=deadbeef`) {
		t.Fatalf("the movie row must link to its torrent: %s", row)
	}
	if !strings.Contains(row, "Downloaded") {
		t.Fatalf("the movie must read as downloaded: %s", row)
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
	if !strings.Contains(string(raw), "Series &amp; movies list") {
		t.Fatalf("page missing the series & movies list: %s", string(raw))
	}
	for _, want := range []string{`data-wanted-state="missing"`, `id="wanted-sort"`, "With missing episodes", "Not monitored"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("page missing %q: %s", want, string(raw))
		}
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

// TestWantedSeasonPacksMatch: a season pack (season marker without an episode)
// belongs to the wanted series and is downloaded as the whole season.
func TestWantedSeasonPacksMatch(t *testing.T) {
	now := time.Now()
	w := wantedEntry{Name: "Reacher", Kind: "series", Seasons: "4"}
	packs := []string{
		"Reacher - S04 (2026) [COMPLETE] SD AMZN Rip XviD ita eng Mp3 Sub ita eng[Pir8]",
		"Reacher (2022) S04 1080p WEBRip 10bit EAC3 5 1 x265-iVy",
	}
	for _, title := range packs {
		if season, ok := wantedPackSeason(title); !ok || season != 4 {
			t.Fatalf("wantedPackSeason(%q) = %d,%v want 4,true", title, season, ok)
		}
		if !w.matches(title, now) {
			t.Fatalf("wanted Reacher S04 must match %q", title)
		}
		if season, episode := wantedItemEpisode(w, title); season != 4 || episode != 0 {
			t.Fatalf("wantedItemEpisode(%q) = %d,%d want 4,0", title, season, episode)
		}
	}
	// Another season is not wanted.
	other := "Reacher S03 COMPLETE 1080p"
	if w.matches(other, now) {
		t.Fatalf("wanted Reacher S04 must not match %q", other)
	}
	// The preview verdict takes the pack instead of reporting "No episode number".
	re, _ := regexp.Compile(titleMatchRegex(w.Name))
	for _, title := range packs {
		verdict, ep := wantedVerdict(w, re, title, now, map[wantedEpisode]bool{}, map[wantedEpisode]bool{})
		if verdict != verdictTake || ep.season != 4 || ep.episode != 0 {
			t.Fatalf("verdict(%q) = %q %+v, want %q S04 pack", title, verdict, ep, verdictTake)
		}
	}
}
