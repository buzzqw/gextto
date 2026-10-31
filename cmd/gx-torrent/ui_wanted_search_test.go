package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
)

// wantedIndexerFeed is a Torznab answer with real episode titles, so the
// suggestion reducer (which needs an episode) can turn it into series names.
const wantedIndexerFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>The Mandalorian S01E01 1080p WEB-DL</title><enclosure url="http://t/1.torrent" length="10"/>
<torznab:attr name="seeders" value="42"/></item>
<item><title>The Mandalorian S01E02 1080p WEB-DL</title><enclosure url="http://t/2.torrent" length="10"/>
<torznab:attr name="seeders" value="30"/></item>
</channel></rss>`

// TestUIWantedSearchQueriesIndexers fixes that "Search in feeds" (the wanted
// title step) really asks the configured indexers, not just the RSS feeds: an
// indexer-only daemon must still return the series name.
func TestUIWantedSearchQueriesIndexers(t *testing.T) {
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(wantedIndexerFeed))
	}))
	defer indexer.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"`+indexer.URL+`","apikey":"k"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=mandalorian&live=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Indexers != 1 || result.IndexerHits == 0 {
		t.Fatalf("indexer not queried: %+v", result)
	}
	found := false
	for _, name := range result.Names {
		if name == "The Mandalorian" {
			found = true
		}
	}
	if !found {
		t.Fatalf("indexer results missing from %v", result.Names)
	}
}

// wantedIndexerMovieFeed is a Torznab answer with no episode marker (a movie),
// so no series name can be parsed.
const wantedIndexerMovieFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>The Mandalorian 2019 1080p BluRay x264</title><enclosure url="http://t/m.torrent" length="10"/>
<torznab:attr name="seeders" value="42"/></item>
</channel></rss>`

// TestUIWantedSearchFallsBackToTitles: when no release carries an episode
// marker the search must still suggest the matching titles, instead of showing
// "No results" while the hit counters are full.
func TestUIWantedSearchFallsBackToTitles(t *testing.T) {
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(wantedIndexerMovieFeed))
	}))
	defer indexer.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"`+indexer.URL+`","apikey":"k"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=mandalorian&live=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.IndexerHits == 0 {
		t.Fatalf("indexer hits = 0: %+v", result)
	}
	if len(result.Names) != 1 || result.Names[0] != "The Mandalorian" {
		t.Fatalf("fallback names = %v, want the clean name, not the release title", result.Names)
	}
}

// TestReleaseBaseName pins how a release title becomes the name to track.
func TestReleaseBaseName(t *testing.T) {
	cases := []struct {
		title string
		name  string
		pack  bool
	}{
		{"The.Mandalorian.S01.2160p.UHD.BluRay", "the mandalorian", true},
		{"The Mandalorian - S03 (2023 ITA/ENG) [Pir8]", "the mandalorian", true},
		{"The Mandalorian Season 2 Complete 1080p", "the mandalorian", true},
		{"The Mandalorian and Grogu 2026 2160p web YTS", "the mandalorian and grogu", false},
		{"The.Mandalorian.and.Grogu.2026.1080p.HDTS.h264", "the mandalorian and grogu", false},
		{"Lego Star Wars The Mandalorian", "lego star wars the mandalorian", false},
		{"Star Wars - The Mandalorian and Grogu (2026) ITA ENG", "star wars the mandalorian and grogu", false},
		{"Star Wars: The Mandalorian &amp; Grogu IMAX 10bit", "star wars the mandalorian and grogu", false},
		{"Мандалорец и Грогу / The Mandalorian & Grogu (2026)", "the mandalorian and grogu", false},
		{"The Mandalorian S03D01 BD", "the mandalorian", true},
	}
	for _, tc := range cases {
		name, pack := releaseBaseName(tc.title, "mandalorian")
		if name != tc.name || pack != tc.pack {
			t.Fatalf("%q -> %q pack=%v, want %q pack=%v", tc.title, name, pack, tc.name, tc.pack)
		}
	}
}

// TestUIWantedSearchSeasonPacksAndPartialErrors: season packs give the series
// name next to the episodes, a failing indexer is reported even when another
// answered, and feed items unrelated to the query are not counted as hits.
func TestUIWantedSearchSeasonPacksAndPartialErrors(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel>` +
			`<item><title>Andor S01 1080p WEB</title><enclosure url="http://t/a.torrent" length="10"/></item>` +
			`<item><title>Andor.S02E03.1080p</title><enclosure url="http://t/b.torrent" length="10"/></item>` +
			`</channel></rss>`))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer bad.Close()
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel>` +
			`<item><title>Unrelated.Show.S01E01</title><link>http://t/u.torrent</link></item>` +
			`</channel></rss>`))
	}))
	defer feed.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"good","url":"`+good.URL+`","apikey":"k"},{"name":"bad","url":"`+bad.URL+`","apikey":"k"}]`)
	d.opts.Settings.Set(feedsSettingKey, `[{"name":"f","url":"`+feed.URL+`"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=andor&live=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Names) != 1 || result.Names[0] != "Andor" {
		t.Fatalf("names = %v, want [Andor] (episode and pack merged)", result.Names)
	}
	if result.IndexerHits != 2 || !strings.Contains(result.IndexerError, "bad") {
		t.Fatalf("hits=%d error=%q, want 2 hits and the failing indexer named", result.IndexerHits, result.IndexerError)
	}
	if result.FeedHits != 0 {
		t.Fatalf("feed hits = %d, want 0: the feed item does not match the query", result.FeedHits)
	}
}

// TestUIWantedSearchReadsTheArchiveByDefault: without live=1 the search must not
// query the indexers, even when they are configured. It answers from the archive
// (kept full by the feeds and the periodic indexer polls), which is the fast
// path; the indexer count is still reported so the page can offer the live query.
func TestUIWantedSearchReadsTheArchiveByDefault(t *testing.T) {
	indexerCalls := 0
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		indexerCalls++
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(wantedIndexerFeed))
	}))
	defer indexer.Close()

	d := feedDaemon(t, "")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"`+indexer.URL+`","apikey":"k"}]`)
	if _, err := d.archive().record("feedA", []rss.Item{{
		Title:  "The Mandalorian S01E01 1080p WEB-DL",
		Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
	}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=mandalorian")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if indexerCalls != 0 {
		t.Fatalf("the default search queried the indexer %d time(s)", indexerCalls)
	}
	if result.IndexerHits != 0 || result.IndexerError != "" {
		t.Fatalf("archive-only search reported indexer results: %+v", result)
	}
	if result.Indexers != 1 {
		t.Fatalf("the configured indexer count must be reported: %+v", result)
	}
	if result.ArchiveHits == 0 {
		t.Fatalf("no archive hit: %+v", result)
	}
	found := false
	for _, name := range result.Names {
		if name == "The Mandalorian" {
			found = true
		}
	}
	if !found {
		t.Fatalf("archive result missing from %v", result.Names)
	}
}

// TestUIWantedSearchReportsWhenNoIndexerIsConfigured fixes the diagnostic that
// answers "seems to search only the feeds": with no indexer the page must be
// able to say so instead of silently returning feed names only.
func TestUIWantedSearchReportsWhenNoIndexerIsConfigured(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=mandalorian&live=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Indexers != 0 || result.IndexerHits != 0 {
		t.Fatalf("no indexer expected: %+v", result)
	}
}

// TestUIWantedSearchKeepsOnlyQueryMatches: the RSS feeds hand over their whole
// current content, so their unrelated series must not appear among the
// suggestions of "mandalorian" (they used to, and looked like a broken search).
func TestUIWantedSearchKeepsOnlyQueryMatches(t *testing.T) {
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(wantedIndexerFeed))
	}))
	defer indexer.Close()
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>Arcane S01E01 1080p ITA WEB-DL</title><link>http://t/a.torrent</link></item>
<item><title>Silo S02E03 1080p ITA WEB-DL</title><link>http://t/s.torrent</link></item>
<item><title>The Mandalorian S01E02 1080p ITA WEB-DL</title><link>http://t/m.torrent</link></item>
</channel></rss>`))
	}))
	defer feed.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"`+indexer.URL+`","apikey":"k"}]`)
	d.opts.Settings.Set(feedsSettingKey, `[{"name":"f","url":"`+feed.URL+`"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=mandalorian&live=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Only the feed item that matches the query is a hit: the others are the
	// feed's unrelated content.
	if result.FeedHits != 1 {
		t.Fatalf("feed hits = %d, want 1: %+v", result.FeedHits, result)
	}
	for _, name := range result.Names {
		if name == "Arcane" || name == "Silo" {
			t.Fatalf("feed series unrelated to the query in %v", result.Names)
		}
	}
	if len(result.Names) != 1 || result.Names[0] != "The Mandalorian" {
		t.Fatalf("names = %v, want [The Mandalorian]", result.Names)
	}
}

// TestTitleLanguageTerms pins how a release name is read for the language
// field: the audio tags become the wanted terms the filter uses, and a tag
// right after "sub" is a subtitle, not the audio, so it stays out.
func TestTitleLanguageTerms(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"The Mandalorian S01E01 1080p ITA WEB-DL", "ita"},
		{"The.Mandalorian.S01E02.1080p.ITA-ENG", "ita, eng"},
		{"Show S01E01 2160p MULTI", "multi"},
		{"Show S01E01 1080p SUB ITA", ""},
		{"Show S01E01 1080p ITA SUB ITA", "ita"},
		{"Show S01E01 1080p English", "eng"},
		{"Show S01E01 1080p", ""},
	}
	for _, c := range cases {
		if got := strings.Join(titleLanguageTerms(c.title), ", "); got != c.want {
			t.Errorf("%q: language = %q, want %q", c.title, got, c.want)
		}
	}
}

// TestUIWantedSearchListsTheArchiveEpisodes: the search step must show, under
// each suggested title, the episodes the archive already holds, with the
// resolution and the language the add dialog copies from the release name.
func TestUIWantedSearchListsTheArchiveEpisodes(t *testing.T) {
	d := feedDaemon(t, "")
	items := []rss.Item{
		{Title: "The Mandalorian S01E01 720p ENG WEB-DL", Link: "http://t/m1a.torrent"},
		{Title: "The Mandalorian S01E01 1080p ITA WEB-DL", Link: "http://t/m1b.torrent"},
		{Title: "The Mandalorian S01E02 2160p MULTI WEB-DL", Link: "http://t/m2.torrent"},
		{Title: "The Mandalorian S01E03 720p ITA WEB-DL", Link: "http://t/m3a.torrent"},
		{Title: "The Mandalorian S01E03 2160p ENG WEB-DL", Link: "http://t/m3b.torrent"},
		{Title: "The Mandalorian S02 1080p ITA", Link: "http://t/mp2.torrent"},
		{Title: "Arcane S01E01 1080p ITA", Link: "http://t/a1.torrent"},
	}
	if _, err := d.archive().record("feedA", items, time.Now()); err != nil {
		t.Fatal(err)
	}
	// S01E03 was already taken at 720p: its parameters win over a better
	// release that was only found.
	d.archive().markAdded("feedA", items[3])
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=mandalorian")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Series) != 1 || result.Series[0].Name != "The Mandalorian" {
		t.Fatalf("series = %+v, want only The Mandalorian", result.Series)
	}
	series := result.Series[0]
	if series.Total != 4 || len(series.Episodes) != 4 {
		t.Fatalf("episodes = %+v (total %d), want 4: S01E01, S01E02, S01E03 and the S02 pack", series.Episodes, series.Total)
	}
	want := []wantedSearchEpisode{
		{Season: 1, Episode: 1, Quality: "1080p", Language: "ita"},
		{Season: 1, Episode: 2, Quality: "2160p", Language: "multi"},
		{Season: 1, Episode: 3, Quality: "720p", Language: "ita"},
		{Season: 2, Episode: 0, Pack: true, Quality: "1080p", Language: "ita"},
	}
	for i, w := range want {
		got := series.Episodes[i]
		if got.Season != w.Season || got.Episode != w.Episode || got.Pack != w.Pack ||
			got.Quality != w.Quality || got.Language != w.Language {
			t.Errorf("episode %d = %+v, want %+v", i, got, w)
		}
		if got.Title == "" {
			t.Errorf("episode %d carries no release title", i)
		}
	}
}

// TestUIWantedSearchCapsTheEpisodesButKeepsTheTotal: a long series must not
// flood the add dialog; the page says how many others are in the archive.
func TestUIWantedSearchCapsTheEpisodesButKeepsTheTotal(t *testing.T) {
	d := feedDaemon(t, "")
	var items []rss.Item
	for episode := 1; episode <= 20; episode++ {
		items = append(items, rss.Item{
			Title: fmt.Sprintf("The Mandalorian S01E%02d 1080p ITA WEB-DL", episode),
			Link:  fmt.Sprintf("http://t/m%d.torrent", episode),
		})
	}
	if _, err := d.archive().record("feedA", items, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/wanted-search?q=mandalorian")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result wantedSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Series) != 1 {
		t.Fatalf("series = %+v", result.Series)
	}
	if result.Series[0].Total != 20 || len(result.Series[0].Episodes) != wantedSearchEpisodesMax {
		t.Fatalf("total=%d shown=%d, want 20 and %d", result.Series[0].Total, len(result.Series[0].Episodes), wantedSearchEpisodesMax)
	}
}
