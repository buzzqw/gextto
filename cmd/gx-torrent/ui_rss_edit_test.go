package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
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
		`data-url="http://127.0.0.1:9696"`, `data-url="http://127.0.0.1:9117"`, `data-url="http://127.0.0.1:8788"`,
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

	d = standaloneTestDaemon(t, "", true)
	rec = setupPost(t, d, url.Values{"lang": {"en"}, "indexer-name": {"indexarr"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup: %d", rec.Code)
	}
	got = d.opts.Settings.Get(indexersSettingKey, "")
	if !strings.Contains(got, `"url":"http://127.0.0.1:8788"`) {
		t.Fatalf("indexers = %q, want the indexarr preset URL", got)
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
		"op": {"add"}, "name": {"serie"}, "url": {"http://example/feed"}, "include": {"1080p"}, "folder": {"shows"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	feeds := d.feeds()
	if len(feeds) != 1 || feeds[0].Name != "serie" || feeds[0].Include != "1080p" || feeds[0].Folder != "shows" {
		t.Fatalf("feeds = %+v", feeds)
	}

	// The page groups the feed and shows its folder.
	page, err := http.Get(srv.URL + "/ui/rss")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(body), "shows") {
		t.Fatal("the RSS page does not show the feed folder")
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

// TestUIRssRuleTakesSeveralFeedsAndThePicker: the "apply this rule to these
// feeds" picker (qBittorrent's RSS Downloader) posts one value per checked box
// and the page renders one checkbox per configured feed.
func TestUIRssRuleTakesSeveralFeedsAndThePicker(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	if err := d.saveFeeds([]feedConfig{{Name: "serie", URL: "http://example/serie"}, {Name: "film", URL: "http://example/film"}}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	page, err := http.Get(srv.URL + "/ui/rss")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	for _, name := range []string{"serie", "film"} {
		if !strings.Contains(string(body), `name="feeds" value="`+name+`"`) {
			t.Fatalf("the rule editor does not offer the feed %q as a checkbox", name)
		}
	}

	resp, err := http.PostForm(srv.URL+"/ui/rss-rule", url.Values{
		"op": {"add"}, "name": {"le serie"}, "enabled": {"1"}, "feeds": {"serie", "film"}, "include": {"1080p"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	rules := d.rules()
	if len(rules) != 1 || len(rules[0].Feeds) != 2 || rules[0].Feeds[0] != "serie" || rules[0].Feeds[1] != "film" {
		t.Fatalf("rules = %+v, want the two checked feeds", rules)
	}
	// A hand-written list ("a, b") still works, and a repeated value is not
	// duplicated in the rule.
	resp, err = http.PostForm(srv.URL+"/ui/rss-rule", url.Values{
		"op": {"add"}, "name": {"a mano"}, "enabled": {"1"}, "feeds": {"serie, film", "serie"}, "include": {"1080p"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	rules = d.rules()
	if len(rules) != 2 || len(rules[1].Feeds) != 2 {
		t.Fatalf("rules = %+v, want the second rule with the two feeds once each", rules)
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

// TestAddFormOffersFolderPickerInStandalone checks the add-torrent dialog's
// "Save to" field carries a Browse button and the shared picker dialog in
// standalone, matching the /ui/browse-dir endpoint that managed mode refuses.
func TestAddFormOffersFolderPickerInStandalone(t *testing.T) {
	standalone := renderUITemplate(t, &Daemon{}, true)
	for _, want := range []string{`data-target="add-destination"`, `id="browse-overlay"`, `class="browse"`} {
		if !strings.Contains(standalone, want) {
			t.Fatalf("the standalone add form is missing %q", want)
		}
	}
	if managed := renderUITemplate(t, &Daemon{}, false); strings.Contains(managed, `data-target="add-destination"`) {
		t.Fatal("managed mode must not offer the folder picker in the add form")
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

// TestStandaloneNavHasMoreMenu checks the navigation menu beside Settings:
// Log, Tools and Search indexers on the main page (actions) and as /?show=
// links on the secondary pages.
func TestStandaloneNavHasMoreMenu(t *testing.T) {
	page := renderUITemplate(t, &Daemon{}, true)
	for _, want := range []string{`id="nav-menu"`, `data-navshow="log"`, `data-navshow="tools"`, `data-navshow="search"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("the standalone main page must offer the More menu (%q)", want)
		}
	}
	if strings.Contains(renderUITemplate(t, &Daemon{}, false), `id="nav-menu"`) {
		t.Fatal("managed mode must not show the standalone More menu")
	}
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	for _, path := range []string{"/ui/wanted", "/ui/rss", "/ui/archive", "/ui/stats", "/ui/settings"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s -> %d", path, resp.StatusCode)
		}
		for _, want := range []string{`id="nav-menu"`, `href="/?show=log"`, `href="/?show=tools"`, `href="/?show=search"`} {
			if !strings.Contains(string(body), want) {
				t.Fatalf("GET %s must offer the More menu (%q)", path, want)
			}
		}
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

// TestUIRssEditIndexerByIndex edits an existing indexer by its row index; a
// rename must replace the entry in place and an empty API key must be kept.
func TestUIRssEditIndexerByIndex(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	post := func(values url.Values) {
		t.Helper()
		resp, err := http.PostForm(srv.URL+"/ui/rss-indexer", values)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	post(url.Values{"op": {"add"}, "name": {"a"}, "url": {"http://127.0.0.1:9001"}, "apikey": {"k1"}})
	post(url.Values{"op": {"add"}, "name": {"b"}, "url": {"http://127.0.0.1:9002"}, "apikey": {"k2"}})

	// Edit the second one by index: rename it and change the URL; the blank API
	// key keeps the stored one.
	post(url.Values{"op": {"add"}, "index": {"1"}, "name": {"bee"}, "url": {"http://127.0.0.1:9003"}, "apikey": {""}})
	list := d.indexers()
	if len(list) != 2 {
		t.Fatalf("indexers = %v", list)
	}
	if list[0].Name != "a" {
		t.Fatalf("the first indexer changed: %+v", list[0])
	}
	if list[1].Name != "bee" || list[1].URL != "http://127.0.0.1:9003" || list[1].APIKey != "k2" {
		t.Fatalf("edit by index = %+v", list[1])
	}
}

// TestUIRssIndexerTestEndpoint probes a configured indexer with a Torznab caps
// request and reports success or failure.
func TestUIRssIndexerTestEndpoint(t *testing.T) {
	var capsHits int32
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&capsHits, 1)
		if r.URL.Query().Get("t") != "caps" {
			http.Error(w, "want t=caps", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server version="1.0"/></caps>`))
	}))
	defer indexer.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"`+indexer.URL+`","apikey":"k"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/ui/rss-indexer-test", url.Values{"index": {"0"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("test result = %s", body)
	}
	if atomic.LoadInt32(&capsHits) == 0 {
		t.Fatal("the indexer was not probed")
	}

	// A failing endpoint reports ok:false.
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer down.Close()
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"down","url":"`+down.URL+`","apikey":""}]`)
	resp, err = http.PostForm(srv.URL+"/ui/rss-indexer-test", url.Values{"index": {"0"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"ok":false`) {
		t.Fatalf("down indexer test = %s", body)
	}
}

// TestTitleMatchRegex checks the regex a wanted title generates: the title's
// words in order, case-insensitive and tolerant of the usual separators, so
// fuzzy indexer results do not slip into the match.
func TestTitleMatchRegex(t *testing.T) {
	cases := map[string]string{
		"Mamma Mia":                     `(?i)mamma[\s._\-]+mia`,
		"FROM":                          `(?i)from`,
		"Star Trek: Strange New Worlds": `(?i)star[\s._\-]+trek[\s._\-]+strange[\s._\-]+new[\s._\-]+worlds`,
		"...":                           "",
	}
	for title, want := range cases {
		if got := titleMatchRegex(title); got != want {
			t.Errorf("titleMatchRegex(%q) = %q, want %q", title, got, want)
		}
	}
	re := regexp.MustCompile(titleMatchRegex("Mamma Mia"))
	if !re.MatchString("Mamma.Mia.2024.1080p") || re.MatchString("The.Mamma.Man") {
		t.Fatalf("mamma mia regex must require both words: %q", titleMatchRegex("Mamma Mia"))
	}
}

// TestFeedErrorHint checks the actionable hints of the feed rows: a wrong key,
// a Cloudflare challenge, a listing without links and a timeout each get their
// own hint, while a plain error stays bare.
func TestFeedErrorHint(t *testing.T) {
	cases := map[string]string{
		"HTTP 401":             "Check the indexer's API key",
		"invalid api key":      "Check the indexer's API key",
		"Cloudflare challenge": "Cloudflare challenge: set FlareSolverr in Options",
		"the page has no torrent link (an empty listing)": "The page returned no items: it may be an empty listing, or an address that is not a listing",
		"rss: ext.to returned HTTP 429":                   "Too many requests: the feeds of this site are spaced and the rest is left for the next check",
		"request timeout":                                 "The server took too long: retry, or check its address",
		"connection refused":                              "",
	}
	for errText, want := range cases {
		if got := feedErrorHint(errText); got != want {
			t.Errorf("feedErrorHint(%q) = %q, want %q", errText, got, want)
		}
	}
	row := uiFeedStatusHTML(feedStatus{Name: "f", LastCheck: 1700000000, LastError: "HTTP 401"}, 3)
	if !strings.Contains(row, "HTTP 401") || !strings.Contains(row, "Check the indexer") {
		t.Errorf("error row misses the error or its hint: %s", row)
	}
}
