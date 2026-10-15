package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/torznab"
)

func archiveItems(t *testing.T, d *Daemon, feed string, titles ...string) {
	t.Helper()
	items := make([]rss.Item, 0, len(titles))
	for i, title := range titles {
		hash := strings.Repeat("0", 39) + string(rune('a'+i))
		items = append(items, rss.Item{Title: title, GUID: hash, Magnet: "magnet:?xt=urn:btih:" + hash})
	}
	if _, err := d.archive().record(feed, items, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// TestWantedSkipsEpisodesAlreadyInTheSession: an episode a rule (or anything
// else) already brought into the client counts as had, so the wanted title
// takes only what is really missing instead of a second release of it.
func TestWantedSkipsEpisodesAlreadyInTheSession(t *testing.T) {
	d := feedDaemon(t, "")
	src := filepath.Join(t.TempDir(), "src")
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "Show.Name.S01E01.720p.mkv", 40_000), Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	archiveItems(t, d, "feedA", "Show Name S01E01 1080p WEB", "Show Name S01E02 1080p WEB")
	w := wantedEntry{Name: "Show Name", Kind: "series", Seasons: "1+", Quality: "1080p"}
	if err := d.saveWanted([]wantedEntry{w}); err != nil {
		t.Fatal(err)
	}
	if got := d.wantedSessionEpisodes(w); !got[wantedEpisode{season: 1, episode: 1}] {
		t.Fatalf("the session episode is not seen: %v", got)
	}
	d.pollWanted(context.Background())
	// The page snapshot is published by the run loop: wait for S01E02.
	waitFor(t, "S01E02 to be taken", func() bool { return len(d.snapshotViews()) >= 2 })
	d.pollWanted(context.Background())
	time.Sleep(200 * time.Millisecond)
	if n := len(d.snapshotViews()); n != 2 {
		t.Fatalf("session has %d torrents, want the existing one plus S01E02 only", n)
	}
	have := d.wantedTakenEpisodes(w)
	if len(have) != 2 {
		t.Fatalf("have = %v, want S01E01 (in the session) and S01E02 (taken)", have)
	}
	if missing := d.wantedMissing(w); len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
}

// TestWantedPreviewGivesEveryReleaseAVerdict covers /ui/wanted-preview: what
// would be taken, and the first filter that turns each other release away.
func TestWantedPreviewGivesEveryReleaseAVerdict(t *testing.T) {
	d := feedDaemon(t, "")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	archiveItems(t, d, "feedA",
		"Show Name S01E01 1080p ITA WEB",
		"Show Name S01E02 720p ITA WEB",
		"Show Name S02E01 1080p ITA WEB",
		"Show Name S01E03 1080p ENG WEB",
		"Show Name S01E04 1080p ITA CAM",
		"Other Show S01E01 1080p ITA",
	)
	q := url.Values{"name": {"Show Name"}, "kind": {"series"}, "seasons": {"1"}, "language": {"ita"}, "quality": {"1080p"}, "exclude": {"cam"}}
	resp, err := http.Get(srv.URL + "/ui/wanted-preview?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Total    int `json:"total"`
		Take     int `json:"take"`
		Releases []struct {
			Title   string `json:"title"`
			Verdict string `json:"verdict"`
		} `json:"releases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Show Name S01E01 1080p ITA WEB": verdictTake,
		"Show Name S01E02 720p ITA WEB":  verdictQuality,
		"Show Name S02E01 1080p ITA WEB": verdictSeason,
		"Show Name S01E03 1080p ENG WEB": verdictLanguage,
		"Show Name S01E04 1080p ITA CAM": verdictExcluded,
	}
	if out.Total != len(want) || out.Take != 1 {
		t.Fatalf("total=%d take=%d, want %d and 1 (another show is left out)", out.Total, out.Take, len(want))
	}
	for _, r := range out.Releases {
		if want[r.Title] != r.Verdict {
			t.Errorf("%s: verdict %q, want %q", r.Title, r.Verdict, want[r.Title])
		}
	}
}

// TestWantedPreviewLiveSearchesTheIndexers: live=1 asks the indexers for the
// title and lists what they return with the same verdicts, so a release that is
// not in the archive yet can be seen and taken by hand (the interactive search).
func TestWantedPreviewLiveSearchesTheIndexers(t *testing.T) {
	d := feedDaemon(t, "")
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(searchFeedXML)) // "Movie 2024 1080p" with a magnet
	}))
	defer indexer.Close()
	if err := d.saveIndexers([]torznab.Indexer{{Name: "jackett", URL: indexer.URL}}); err != nil {
		t.Fatal(err)
	}
	w := wantedEntry{Name: "Movie 2024", Kind: "movie", Quality: "1080p", Category: "film", SavePath: "/srv/film"}
	if err := d.saveWanted([]wantedEntry{w}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	type previewOut struct {
		Total    int    `json:"total"`
		Take     int    `json:"take"`
		Live     bool   `json:"live"`
		Category string `json:"category"`
		SavePath string `json:"savepath"`
		Releases []struct {
			Title    string `json:"title"`
			Verdict  string `json:"verdict"`
			Download string `json:"download"`
		} `json:"releases"`
	}
	load := func(query string) previewOut {
		t.Helper()
		resp, err := http.Get(srv.URL + "/ui/wanted-preview?" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out previewOut
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// The archive holds nothing for this title: without live there is nothing to
	// take, and the indexers are not asked at all.
	if out := load("index=0"); out.Total != 0 || out.Live {
		t.Fatalf("without live: total=%d live=%v, want an empty archive preview", out.Total, out.Live)
	}
	out := load("index=0&live=1")
	if !out.Live || out.Total != 1 || out.Take != 1 {
		t.Fatalf("live preview: live=%v total=%d take=%d, want the indexer release ready to take", out.Live, out.Total, out.Take)
	}
	rel := out.Releases[0]
	if rel.Title != "Movie 2024 1080p" || rel.Verdict != verdictTake || !strings.HasPrefix(rel.Download, "magnet:") {
		t.Fatalf("release = %+v, want the indexer release with a magnet and a take verdict", rel)
	}
	// The page adds it with the title's own category and path.
	if out.Category != "film" || out.SavePath != "/srv/film" {
		t.Fatalf("category/path = %q/%q, want the wanted entry's own", out.Category, out.SavePath)
	}
}

// TestWantedPreviewScopesToOneEpisode: the per-episode interactive search asks
// for "Title SxxEyy" and lists only that episode, so the operator can pick a
// release for the episode that is missing without wading through the series.
func TestWantedPreviewScopesToOneEpisode(t *testing.T) {
	d := feedDaemon(t, "")
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(searchFeedXML)) // a movie title, i.e. no episode
	}))
	defer indexer.Close()
	if err := d.saveIndexers([]torznab.Indexer{{Name: "jackett", URL: indexer.URL}}); err != nil {
		t.Fatal(err)
	}
	archiveItems(t, d, "feedA",
		"Show Name S01E01 1080p ITA",
		"Show Name S01E02 1080p ITA WEB",
		"Show Name S01E02 720p ITA WEB",
		"Show Name S01E03 1080p ITA",
	)
	if err := d.saveWanted([]wantedEntry{{Name: "Show Name", Kind: "series", Seasons: "1", Quality: "1080p"}}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	var out struct {
		Total    int `json:"total"`
		Season   int `json:"season"`
		Episode  int `json:"episode"`
		Releases []struct {
			Title   string `json:"title"`
			Episode string `json:"episode"`
			Verdict string `json:"verdict"`
		} `json:"releases"`
	}
	resp, err := http.Get(srv.URL + "/ui/wanted-preview?index=0&season=1&episode=2&live=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	// Only the asked episode is listed: its two releases with their own verdicts.
	// S01E01, S01E03 and the movie the indexer returns are left out.
	if out.Total != 2 || out.Season != 1 || out.Episode != 2 {
		t.Fatalf("total=%d season=%d episode=%d, want the two S01E02 releases", out.Total, out.Season, out.Episode)
	}
	byTitle := map[string]string{}
	for _, rel := range out.Releases {
		if rel.Episode != "S01E02" {
			t.Fatalf("release %q is not S01E02: %+v", rel.Title, rel)
		}
		byTitle[rel.Title] = rel.Verdict
	}
	if byTitle["Show Name S01E02 1080p ITA WEB"] != verdictTake {
		t.Fatalf("the 1080p release must be ready to take: %v", byTitle)
	}
	if byTitle["Show Name S01E02 720p ITA WEB"] != verdictQuality {
		t.Fatalf("the 720p release must be turned away by quality: %v", byTitle)
	}
}

// TestRulePreviewWarnsAboutSeveralTitles: the rule preview groups what a rule
// takes by title, so the editor can say "1080p" alone takes three series.
// TestWantedVerdictRequiresSubtitles: a release without the subtitle term is
// reported with its own reason, so the preview explains it.
func TestWantedVerdictRequiresSubtitles(t *testing.T) {
	w := wantedEntry{Name: "Show", Kind: "series", Seasons: "1", Subtitles: "sub ita"}
	now := time.Now()
	taken := map[wantedEpisode]bool{}
	inSession := map[wantedEpisode]bool{}
	if v, _ := wantedVerdict(w, nil, "Show.S01E02.SUB.ITA.1080p", now, taken, inSession); v != verdictTake {
		t.Fatalf("with the subtitle term the verdict is %q, want %q", v, verdictTake)
	}
	if v, _ := wantedVerdict(w, nil, "Show.S01E02.1080p", now, taken, inSession); v != verdictSubtitle {
		t.Fatalf("without the subtitle term the verdict is %q, want %q", v, verdictSubtitle)
	}
}

func TestRulePreviewWarnsAboutSeveralTitles(t *testing.T) {
	d := feedDaemon(t, "")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	archiveItems(t, d, "feedA",
		"The.Office.US.S03E01.1080p", "The.Office.US.S03E02.1080p",
		"Ted.Lasso.S01E01.1080p", "Severance.S02E01.1080p", "Ted.Lasso.S01E01.720p")
	preview := func(form url.Values) (total int, titles []string) {
		t.Helper()
		resp, err := http.PostForm(srv.URL+"/ui/rss-rule-preview", form)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Total  int `json:"total"`
			Titles []struct {
				Name string `json:"name"`
			} `json:"titles"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		for _, g := range out.Titles {
			titles = append(titles, g.Name)
		}
		return out.Total, titles
	}
	if total, titles := preview(url.Values{"name": {"r"}, "enabled": {"1"}, "include": {"the office, 1080p"}}); total != 4 || len(titles) != 3 {
		t.Fatalf("one of: total=%d titles=%v, want 4 releases of 3 titles", total, titles)
	}
	if total, titles := preview(url.Values{"name": {"r"}, "enabled": {"1"}, "require": {"the office, 1080p"}}); total != 2 || len(titles) != 1 {
		t.Fatalf("all of: total=%d titles=%v, want the two Office episodes", total, titles)
	}
}
