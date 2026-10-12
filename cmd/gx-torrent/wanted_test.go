package main

import (
	"context"
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

	// A season outside the spec never matches (the episode filter rejects it).
	d.applyWanted(now, "f", []rss.Item{{Title: "Reacher S09E01 1080p", GUID: "g9"}})
	if got := d.wantedMissing(w); len(got) != 1 {
		t.Fatalf("a season outside the spec must not appear: %+v", got)
	}

	// Once taken it is no longer missing.
	d.recordWantedTaken(w, 1, 1)
	if got := d.wantedMissing(w); len(got) != 0 {
		t.Fatalf("missing after taken = %+v", got)
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
