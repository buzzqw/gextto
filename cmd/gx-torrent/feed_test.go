package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
