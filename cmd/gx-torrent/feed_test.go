package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/settings"
)

const feedBody = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>Movie 1080p</title><guid>g1</guid>
<description><![CDATA[<a href="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567">x</a>]]></description></item>
<item><title>Movie 720p</title><guid>g2</guid>
<description><![CDATA[<a href="magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa">y</a>]]></description></item>
</channel></rss>`

func feedDaemon(t *testing.T, feedsJSON string) *Daemon {
	t.Helper()
	d := newTestDaemon(t)
	d.opts.Mode = ModeStandalone
	store, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.Set("setup-complete", "true")
	store.Set(feedsSettingKey, feedsJSON)
	d.opts.Settings = store
	return d
}

func TestPollFeedsAddsAndDedupes(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(feedBody))
	}))
	defer feed.Close()

	d := feedDaemon(t, `[{"name":"f","url":"`+feed.URL+`","include":"1080p"}]`)
	d.pollFeeds()

	waitFor(t, "the matching torrent to be added", func() bool {
		return len(d.snapshotViews()) == 1
	})
	if got := d.snapshotViews()[0].Hash; got != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("added hash %q, want the 1080p magnet", got)
	}
	if statuses := d.feedStatusesSnapshot(); len(statuses) != 1 || statuses[0].Items != 2 || statuses[0].Added != 1 {
		t.Fatalf("feed status after first poll = %+v", statuses)
	}

	// A second poll must not add it again (feed_seen).
	d.pollFeeds()
	if len(d.snapshotViews()) != 1 {
		t.Fatalf("dedup failed: %d torrents", len(d.snapshotViews()))
	}
	if statuses := d.feedStatusesSnapshot(); len(statuses) != 1 || statuses[0].Added != 0 {
		t.Fatalf("second poll re-added items: %+v", statuses)
	}
}

const episodesFeed = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>Show S01E05 1080p</title><guid>e5</guid>
<description><![CDATA[<a href="magnet:?xt=urn:btih:1111111111111111111111111111111111111111">x</a>]]></description></item>
<item><title>Show S01E04 1080p</title><guid>e4</guid>
<description><![CDATA[<a href="magnet:?xt=urn:btih:2222222222222222222222222222222222222222">x</a>]]></description></item>
<item><title>Show S01E06 1080p CAM</title><guid>e6</guid>
<description><![CDATA[<a href="magnet:?xt=urn:btih:3333333333333333333333333333333333333333">x</a>]]></description></item>
</channel></rss>`

func TestPollFeedsOrderedRulesAndSmartEpisode(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(episodesFeed))
	}))
	defer feed.Close()

	d := feedDaemon(t, `[{"name":"f","url":"`+feed.URL+`"}]`)
	d.opts.Settings.Set(rulesSettingKey,
		`[{"name":"no-cam","fail":true,"match":{"regex":"(?i)cam"}},`+
			`{"name":"shows","match":{"require_episode":true,"smart_episode":true},"action":{"category":"tv"}}]`)

	d.pollFeeds()
	waitFor(t, "the newest episode", func() bool { return len(d.snapshotViews()) == 1 })
	view := d.snapshotViews()[0]
	if view.Hash != "1111111111111111111111111111111111111111" {
		t.Fatalf("added %q, want S01E05", view.Hash)
	}
	if view.Category != "tv" {
		t.Fatalf("category = %q, want tv", view.Category)
	}

	// A second poll adds nothing: E05 is seen and E04/E06 are filtered.
	d.pollFeeds()
	if len(d.snapshotViews()) != 1 {
		t.Fatalf("re-poll added torrents: %d", len(d.snapshotViews()))
	}
}

func TestFeedEndpoints(t *testing.T) {
	// Standalone, open: the status is a JSON array and the poll is accepted.
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/feeds")
	if err != nil {
		t.Fatal(err)
	}
	var list []feedStatus
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("feeds status is not JSON: %v", err)
	}
	resp.Body.Close()
	if len(list) != 0 {
		t.Fatalf("expected no feeds, got %d", len(list))
	}

	resp, err = http.Post(srv.URL+"/ui/feeds/poll", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("poll status = %d", resp.StatusCode)
	}
}

func TestFeedEndpointsRefusedInManaged(t *testing.T) {
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	for _, handler := range []func(http.ResponseWriter, *http.Request){d.handleUIFeeds, d.handleUIFeedPoll} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/feeds", nil)
		handler(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("managed must refuse the feed endpoints, got %d", rec.Code)
		}
	}
}

func TestFeedIntervalDefaults(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	if got := d.feedInterval(); got != defaultFeedInterval {
		t.Fatalf("default interval = %v", got)
	}
	d.opts.Settings.Set(feedIntervalKey, "300")
	if got := d.feedInterval(); got.Minutes() != 5 {
		t.Fatalf("interval = %v, want 5m", got)
	}
	d.opts.Settings.Set(feedIntervalKey, "5") // below the 60s floor
	if got := d.feedInterval(); got != defaultFeedInterval {
		t.Fatalf("too-small interval must fall back to the default, got %v", got)
	}
}

func TestUIRssPageAndConfig(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/rss")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `name="feeds"`) || !strings.Contains(string(body), `name="rules"`) {
		t.Fatalf("RSS page: status=%d", resp.StatusCode)
	}

	feedsJSON := `[{"name":"f","url":"http://example/feed"}]`
	rulesJSON := `[{"name":"r","match":{}}]`
	resp, err = http.PostForm(srv.URL+"/ui/rss-config", url.Values{"feeds": {feedsJSON}, "rules": {rulesJSON}, "indexers": {`[]`}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := d.opts.Settings.Get(feedsSettingKey, ""); got != feedsJSON {
		t.Fatalf("feeds not saved: %q", got)
	}
	if got := d.opts.Settings.Get(rulesSettingKey, ""); got != rulesJSON {
		t.Fatalf("rules not saved: %q", got)
	}

	resp, err = http.PostForm(srv.URL+"/ui/rss-config", url.Values{"feeds": {`{bad`}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Invalid feeds JSON") {
		t.Fatalf("invalid JSON must be reported: status=%d", resp.StatusCode)
	}
}

func TestUIFeedItems(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(feedBody))
	}))
	defer feed.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(feedsSettingKey, `[{"name":"f","url":"`+feed.URL+`","include":"1080p"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/feed-items?feed=f")
	if err != nil {
		t.Fatal(err)
	}
	var items []feedItemView
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("feed-items is not JSON: %v", err)
	}
	resp.Body.Close()
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	byTitle := map[string]feedItemView{}
	for _, item := range items {
		byTitle[item.Title] = item
	}
	if !byTitle["Movie 1080p"].Pass || byTitle["Movie 720p"].Pass {
		t.Fatalf("include filter not reflected: %+v", items)
	}
}
