package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/rss"
)

// TestUIBrowseDirListsDirectories checks the folder picker endpoint: it lists
// the sub-directories of a path and the parent to go back to.
func TestUIBrowseDirListsDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "media"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "incoming"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/browse-dir?path=" + url.QueryEscape(root))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var payload struct {
		Path   string   `json:"path"`
		Parent string   `json:"parent"`
		Dirs   []string `json:"dirs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Path != want {
		t.Fatalf("path = %q, want %q", payload.Path, want)
	}
	if payload.Parent != filepath.Dir(want) {
		t.Fatalf("parent = %q, want %q", payload.Parent, filepath.Dir(want))
	}
	if len(payload.Dirs) != 2 || !strings.HasSuffix(payload.Dirs[0], "incoming") || !strings.HasSuffix(payload.Dirs[1], "media") {
		t.Fatalf("dirs = %v", payload.Dirs)
	}
}

// TestUIBrowseDirMissingPathFallsBackToAncestor checks that a not-yet-existing
// path keeps the picker navigable instead of failing.
func TestUIBrowseDirMissingPathFallsBackToAncestor(t *testing.T) {
	root := t.TempDir()
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/browse-dir?path=" + url.QueryEscape(filepath.Join(root, "not", "yet")))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Path != want {
		t.Fatalf("path = %q, want the nearest existing ancestor %q", payload.Path, want)
	}
}

// TestSetupPageOffersFolderPickerAndIndexerPresets checks the wizard markup.
func TestSetupPageOffersFolderPickerAndIndexerPresets(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/setup", nil)
	d.handleUISetup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`class="browse"`, `data-target="download-dir"`, `data-target="temp-dir"`,
		`id="browse-overlay"`, `/ui/browse-dir`,
		`id="indexer-name"`, `name="indexer-name"`,
		`data-url="http://127.0.0.1:9696"`, `data-url="http://127.0.0.1:9117"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the wizard is missing %q", want)
		}
	}
}

// TestSetupFillsIndexerURLFromPreset checks that choosing a known indexer and
// leaving the URL empty still stores the preset's usual address and port.
func TestSetupFillsIndexerURLFromPreset(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := setupPost(t, d, url.Values{"lang": {"en"}, "indexer-name": {"Prowlarr"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup: %d", rec.Code)
	}
	got := d.opts.Settings.Get(indexersSettingKey, "")
	if !strings.Contains(got, `"url":"http://127.0.0.1:9696"`) {
		t.Fatalf("indexers = %q, want the Prowlarr preset URL", got)
	}
}

// TestUIRssAddAndRemoveIndexer exercises the friendly indexer editor.
func TestUIRssAddAndRemoveIndexer(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/ui/rss-indexer", url.Values{"op": {"add"}, "type": {"Jackett"}, "apikey": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := d.opts.Settings.Get(indexersSettingKey, ""); !strings.Contains(got, `"url":"http://127.0.0.1:9117"`) || !strings.Contains(got, "secret") {
		t.Fatalf("indexers = %q", got)
	}

	// The RSS page lists it.
	page, err := http.Get(srv.URL + "/ui/rss")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(body), "http://127.0.0.1:9117") {
		t.Fatalf("the RSS page does not list the indexer: %s", body)
	}

	// Remove by its row index.
	resp, err = http.PostForm(srv.URL+"/ui/rss-indexer", url.Values{"op": {"remove"}, "index": {"0"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := d.indexers(); len(got) != 0 {
		t.Fatalf("indexer not removed: %v", got)
	}
}

// TestUIRssAddAndRemoveFeed exercises the friendly feed editor.
func TestUIRssAddAndRemoveFeed(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/ui/rss-feed", url.Values{
		"op": {"add"}, "name": {"serie"}, "url": {"http://example/feed"}, "include": {"1080p"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	feeds := d.feeds()
	if len(feeds) != 1 || feeds[0].Name != "serie" || feeds[0].Include != "1080p" {
		t.Fatalf("feeds = %+v", feeds)
	}

	// A feed without a URL or a search query is refused.
	resp, err = http.PostForm(srv.URL+"/ui/rss-feed", url.Values{"op": {"add"}, "name": {"empty"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(d.feeds()) != 1 {
		t.Fatal("a feed without a source must not be saved")
	}

	resp, err = http.PostForm(srv.URL+"/ui/rss-feed", url.Values{"op": {"remove"}, "index": {"0"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(d.feeds()) != 0 {
		t.Fatalf("feed not removed: %+v", d.feeds())
	}
}

// TestUIRssSearchFeedCanBeAdded checks that a Torznab search feed works without
// a URL.
func TestUIRssSearchFeedCanBeAdded(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/ui/rss-feed", url.Values{"op": {"add"}, "name": {"cerca"}, "search": {"Serie S01"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	feeds := d.feeds()
	if len(feeds) != 1 || feeds[0].Search != "Serie S01" {
		t.Fatalf("feeds = %+v", feeds)
	}
}

// TestStandaloneNavHasRssLink checks the standalone navigation exposes the RSS
// and indexer management; managed mode stays without it.
func TestStandaloneNavHasRssLink(t *testing.T) {
	d := &Daemon{}
	if !strings.Contains(renderUITemplate(t, d, true), "/ui/rss") {
		t.Fatal("the standalone page must link to the RSS/indexer management")
	}
	if strings.Contains(renderUITemplate(t, d, false), "/ui/rss") {
		t.Fatal("managed mode must not show the standalone RSS link")
	}
}

// TestStandalonePagesAreLocalized checks the new strings are wired into the
// translation pass, not just present in English.
func TestStandalonePagesAreLocalized(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/setup?lang=it", nil)
	d.handleUISetup(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "Sfoglia") {
		t.Fatalf("the Italian wizard does not translate the browse button")
	}

	d.opts.Settings.Set(setupCompleteKey, "true")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/rss?lang=it", nil)
	d.handleUIRss(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "Aggiungi indexer") || !strings.Contains(body, "RSS e indexer") {
		t.Fatalf("the Italian RSS page is not fully translated: %s", body)
	}
}

// TestSetupWithoutIndexerStoresNone checks the dropdown's default (empty) does
// not create a phantom indexer when the operator skips that field.
func TestSetupWithoutIndexerStoresNone(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := setupPost(t, d, url.Values{"lang": {"en"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup: %d", rec.Code)
	}
	if got := d.opts.Settings.Get(indexersSettingKey, ""); got != "" {
		t.Fatalf("a wizard without an indexer must not store one: %q", got)
	}
}

// TestSetupCustomIndexerUsesDefaultName checks the "other" choice stores the
// typed URL under the generic name.
func TestSetupCustomIndexerUsesDefaultName(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := setupPost(t, d, url.Values{"lang": {"en"}, "indexer-name": {"other"}, "indexer-url": {"http://127.0.0.1:1234/api"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup: %d", rec.Code)
	}
	got := d.opts.Settings.Get(indexersSettingKey, "")
	if !strings.Contains(got, `"name":"indexer"`) || !strings.Contains(got, "http://127.0.0.1:1234/api") {
		t.Fatalf("indexers = %q", got)
	}
}

// TestUIRssRuleEditor exercises the friendly ordered-rules editor.
func TestUIRssRuleEditor(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	// A SKIP rule (no-cam) first, then an ADD rule for shows.
	if _, err := http.PostForm(srv.URL+"/ui/rss-rule", url.Values{"op": {"add"}, "name": {"no-cam"}, "fail": {"1"}, "regex": {"(?i)cam"}}); err != nil {
		t.Fatal(err)
	}
	resp, err := http.PostForm(srv.URL+"/ui/rss-rule", url.Values{
		"op": {"add"}, "name": {"shows"}, "feeds": {"serie, film"}, "enabled": {"1"},
		"min_size": {"1500"}, "min_seeders": {"3"}, "require_episode": {"1"}, "smart_episode": {"1"},
		"episode_filter": {"1x01-05"}, "ignore_days": {"7"}, "savepath": {"/srv/tv"}, "category": {"tv"}, "tags": {"hd, ita"}, "sequential": {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	rules := d.rules()
	if len(rules) != 2 {
		t.Fatalf("rules = %+v", rules)
	}
	if !rules[0].Fail || rules[0].Name != "no-cam" || rules[0].Match.Regex != "(?i)cam" {
		t.Fatalf("first rule = %+v", rules[0])
	}
	shows := rules[1]
	if shows.Match.MinSize != 1500*1024*1024 {
		t.Fatalf("min_size = %d, want 1500 MiB", shows.Match.MinSize)
	}
	if len(shows.Feeds) != 2 || shows.Feeds[0] != "serie" || shows.Feeds[1] != "film" {
		t.Fatalf("feeds = %v", shows.Feeds)
	}
	if !shows.Match.RequireEpisode || !shows.Match.SmartEpisode || shows.Match.MinSeeders != 3 {
		t.Fatalf("match = %+v", shows.Match)
	}
	if shows.Match.EpisodeFilter != "1x01-05" {
		t.Fatalf("episode_filter = %q", shows.Match.EpisodeFilter)
	}
	if shows.IgnoreDays != 7 {
		t.Fatalf("ignore_days = %d", shows.IgnoreDays)
	}
	if !rss.RuleEnabled(shows) {
		t.Fatal("the rule must be enabled when the checkbox is on")
	}
	if shows.Action.SavePath != "/srv/tv" || shows.Action.Category != "tv" || !shows.Action.Sequential || len(shows.Action.Tags) != 2 {
		t.Fatalf("action = %+v", shows.Action)
	}

	// Reorder: move the second rule up and it becomes the first.
	if _, err := http.PostForm(srv.URL+"/ui/rss-rule", url.Values{"op": {"up"}, "index": {"1"}}); err != nil {
		t.Fatal(err)
	}
	if got := d.rules()[0].Name; got != "shows" {
		t.Fatalf("after up the first rule = %q, want shows", got)
	}

	// The page lists both rules.
	page, err := http.Get(srv.URL + "/ui/rss")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(body), "no-cam") || !strings.Contains(string(body), "shows") {
		t.Fatalf("the RSS page does not list the rules")
	}

	// An invalid regex is refused and stored nowhere.
	resp, err = http.PostForm(srv.URL+"/ui/rss-rule", url.Values{"op": {"add"}, "name": {"bad"}, "regex": {"("}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(d.rules()) != 2 {
		t.Fatalf("a rule with an invalid regex must not be saved: %+v", d.rules())
	}

	// An invalid episode filter is refused too.
	resp, err = http.PostForm(srv.URL+"/ui/rss-rule", url.Values{"op": {"add"}, "name": {"bad-episode"}, "episode_filter": {"nope"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(d.rules()) != 2 {
		t.Fatalf("a rule with an invalid episode filter must not be saved: %+v", d.rules())
	}

	// Remove by index.
	if _, err := http.PostForm(srv.URL+"/ui/rss-rule", url.Values{"op": {"remove"}, "index": {"0"}}); err != nil {
		t.Fatal(err)
	}
	if got := d.rules(); len(got) != 1 || got[0].Name != "no-cam" {
		t.Fatalf("after remove: %+v", got)
	}
}

func renderUITemplate(t *testing.T, d *Daemon, standalone bool) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	d.renderUI(rec, req, "", uiPageData{Standalone: standalone})
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d", rec.Code)
	}
	return rec.Body.String()
}
