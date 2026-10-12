package main

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
	bolt "go.etcd.io/bbolt"
)

func openTestArchive(t *testing.T) *feedArchive {
	t.Helper()
	archive, err := openFeedArchive(filepath.Join(t.TempDir(), feedArchiveFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = archive.close() })
	return archive
}

func TestFeedArchiveRecordDeduplicatesAndSearches(t *testing.T) {
	archive := openTestArchive(t)
	now := time.Unix(1_700_000_000, 0)
	items := []rss.Item{
		{Title: "The.Office.S01E01.1080p.WEB", Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", Seeders: 5},
		{Title: "The.Office.S01E02.720p.CAM", Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{Title: "Other Movie 2160p", Link: "https://example.org/a.torrent"},
		{Title: "no download"},
	}
	added, err := archive.record("feedA", items, now)
	if err != nil || added != 3 {
		t.Fatalf("record = %d, %v; want 3 new", added, err)
	}
	// The same release from another feed (and an upper-case hash) is a duplicate.
	dup := []rss.Item{{Title: "The Office S01E01", Magnet: "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567", Seeders: 9}}
	if added, err := archive.record("feedB", dup, now.Add(time.Hour)); err != nil || added != 0 {
		t.Fatalf("duplicate record = %d, %v", added, err)
	}

	page, err := archive.search(parseArchiveQuery("the office -cam", "", false), "new", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Title != "The.Office.S01E01.1080p.WEB" || page.Items[0].Seeders != 9 {
		t.Fatalf("search = %+v", page)
	}
	if page, _ := archive.search(parseArchiveQuery(`"office s01e02"`, "", false), "", 1, 50); page.Total != 1 {
		t.Fatalf("phrase search total = %d", page.Total)
	}
	if page, _ := archive.search(parseArchiveQuery("", "feedA", false), "title", 1, 2); page.Total != 3 || page.Pages != 2 || len(page.Items) != 2 {
		t.Fatalf("feed filter paging = %+v", page)
	}

	archive.markAdded("feedA", items[0])
	if page, _ := archive.search(parseArchiveQuery("", "", true), "", 1, 50); page.Total != 1 || !page.Items[0].Added {
		t.Fatalf("only-added = %+v", page)
	}

	if _, err := archive.deleteMatching(parseArchiveQuery("", "", false)); err == nil {
		t.Fatal("an empty search must not wipe the archive")
	}
	if removed, err := archive.deleteMatching(parseArchiveQuery("office", "", false)); err != nil || removed != 2 {
		t.Fatalf("deleteMatching = %d, %v", removed, err)
	}
	left, _ := archive.search(archiveQuery{}, "", 1, 50)
	if removed, err := archive.deleteIDs([]string{left.Items[0].ID, "zz"}); err != nil || removed != 1 {
		t.Fatalf("deleteIDs = %d, %v", removed, err)
	}
	if stats := archive.stats(false); stats.Entries != 0 {
		t.Fatalf("entries left = %d", stats.Entries)
	}
}

func TestFeedArchivePruneByAgeAndCap(t *testing.T) {
	archive := openTestArchive(t)
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < 5; i++ {
		item := rss.Item{Title: "Item", Link: "https://example.org/" + string(rune('a'+i)) + ".torrent"}
		if _, err := archive.record("f", []rss.Item{item}, base.Add(time.Duration(i)*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	// Older than day 2 (entries of day 0 and 1) go.
	if removed, err := archive.prune(base.Add(2*24*time.Hour), archiveMaxEntries); err != nil || removed != 2 {
		t.Fatalf("prune by age = %d, %v", removed, err)
	}
	// The cap keeps the newest two.
	if removed, err := archive.prune(time.Time{}, 2); err != nil || removed != 1 {
		t.Fatalf("prune by cap = %d, %v", removed, err)
	}
	page, _ := archive.search(archiveQuery{}, "old", 1, 50)
	if page.Total != 2 || page.Items[0].FirstSeen != base.Add(3*24*time.Hour).Unix() {
		t.Fatalf("left = %+v", page)
	}
}

func TestPollFeedsArchivesItemsAndServesTheArchivePage(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(feedBody))
	}))
	defer feed.Close()
	d := feedDaemon(t, `[{"name":"f","url":"`+feed.URL+`","include":"1080p"}]`)
	d.pollFeeds()

	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/ui/archive/items?q=movie&sort=title")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items []archiveItemView `json:"items"`
		Total int               `json:"total"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	resp.Body.Close()
	if payload.Total != 2 {
		t.Fatalf("archived = %+v", payload)
	}
	// Only "Movie 1080p" passed the feed filter and was added.
	byTitle := map[string]archiveItemView{}
	for _, item := range payload.Items {
		byTitle[item.Title] = item
	}
	if !byTitle["Movie 1080p"].Added || byTitle["Movie 720p"].Added {
		t.Fatalf("added flags = %+v", byTitle)
	}
	if byTitle["Movie 1080p"].Quality != "1080p" || !byTitle["Movie 1080p"].InSession {
		t.Fatalf("derived fields = %+v", byTitle["Movie 1080p"])
	}

	// Add from the archive, then delete it.
	resp, err = http.PostForm(srv.URL+"/ui/archive/add", url.Values{"id": {byTitle["Movie 720p"].ID}})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("archive add: %v %v", err, resp.Status)
	}
	resp.Body.Close()
	resp, err = http.PostForm(srv.URL+"/ui/archive/delete", url.Values{"id": {byTitle["Movie 720p"].ID}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"removed":1`) {
		t.Fatalf("archive delete = %s", body)
	}

	resp, err = http.Get(srv.URL + "/ui/archive")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(page), `<option value="f">f</option>`) || strings.Contains(string(page), "{{") {
		t.Fatalf("archive page not rendered: %.300s", page)
	}
}

func TestUIFeedReloadEditToggle(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(feedBody))
	}))
	defer feed.Close()
	d := feedDaemon(t, `[{"name":"f","url":"`+feed.URL+`","include":"nothing-matches"}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/ui/feeds/poll", url.Values{"feed": {"f"}})
	if err != nil {
		t.Fatal(err)
	}
	var status feedStatus
	_ = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || status.Items != 2 || status.New != 2 || status.LastCheck == 0 {
		t.Fatalf("reload = %d %+v", resp.StatusCode, status)
	}

	resp, err = http.PostForm(srv.URL+"/ui/rss-feed", url.Values{"op": {"add"}, "index": {"0"}, "name": {"renamed"}, "url": {feed.URL}, "category": {"tv"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	feeds := d.feeds()
	if len(feeds) != 1 || feeds[0].Name != "renamed" || feeds[0].Category != "tv" {
		t.Fatalf("edit = %+v", feeds)
	}
	resp, err = http.PostForm(srv.URL+"/ui/rss-feed", url.Values{"op": {"toggle"}, "index": {"0"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !d.feeds()[0].Disabled {
		t.Fatal("toggle did not disable the feed")
	}
	resp, err = http.Get(srv.URL + "/ui/rss")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{`data-feed-reload="renamed"`, ">Enable<", "data-feed-edit"} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("RSS page misses %q", want)
		}
	}
	if strings.Contains(string(page), "{{") {
		t.Fatal("RSS page has an unreplaced placeholder")
	}
}

func TestUIRuleEditTogglePreview(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(feedBody))
	}))
	defer feed.Close()
	d := feedDaemon(t, `[{"name":"f","url":"`+feed.URL+`","disabled":true}]`)
	d.opts.Settings.Set(rulesSettingKey, `[{"name":"r","match":{"include":["720p"]}}]`)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	if !d.pollFeedsOnly("f") {
		t.Fatal("single poll refused")
	}

	resp, err := http.PostForm(srv.URL+"/ui/rss-rule-preview", url.Values{"name": {"p"}, "include": {"1080p"}})
	if err != nil {
		t.Fatal(err)
	}
	var preview struct {
		Matches []struct{ Title string } `json:"matches"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&preview)
	resp.Body.Close()
	if len(preview.Matches) != 1 || preview.Matches[0].Title != "Movie 1080p" {
		t.Fatalf("preview = %+v", preview)
	}

	resp, err = http.PostForm(srv.URL+"/ui/rss-rule", url.Values{"op": {"add"}, "index": {"0"}, "name": {"r2"}, "include": {"1080p"}, "enabled": {"1"}, "once": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	rules := d.rules()
	if len(rules) != 1 || rules[0].Name != "r2" || !rules[0].Action.Once {
		t.Fatalf("edit = %+v", rules)
	}
	resp, err = http.PostForm(srv.URL+"/ui/rss-rule", url.Values{"op": {"toggle"}, "index": {"0"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if rss.RuleEnabled(d.rules()[0]) {
		t.Fatal("toggle did not disable the rule")
	}
}

func TestIndexerPageURL(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:9117/api/v2.0/indexers/x/results/torznab?apikey=s": "http://127.0.0.1:9117/",
		"https://prowlarr.lan/1/api":                                         "https://prowlarr.lan/",
		"ftp://x":                                                            "",
		"":                                                                   "",
	}
	for in, want := range cases {
		if got := indexerPageURL(in); got != want {
			t.Errorf("indexerPageURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUIOptimizeQueueCacheBandwidth(t *testing.T) {
	d := feedDaemon(t, "")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(what string) string {
		resp, err := client.PostForm(srv.URL+"/ui/optimize", url.Values{"optimize": {what}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("Location")
	}
	if _, err := d.setConfig(map[string]json.RawMessage{"active_downloads": rawJSON(9), "cache_mb": rawJSON(64)}); err != nil {
		t.Fatal(err)
	}
	if loc := post("queue"); !strings.Contains(loc, "ok=1") {
		t.Fatalf("queue redirect = %s", loc)
	}
	if loc := post("cache"); !strings.Contains(loc, "ok=1") {
		t.Fatalf("cache redirect = %s", loc)
	}
	cfg, _, _ := d.config()
	if cfg.ActiveDownloads != 3 || cfg.ActiveLimit != 5 || !cfg.DynamicQueue || cfg.CacheMB != -1 {
		t.Fatalf("config after optimize = %+v", cfg)
	}
	// No upload measured yet: refused, nothing changed.
	if loc := post("bandwidth"); !strings.Contains(loc, "ok=0") {
		t.Fatalf("bandwidth without a measure = %s", loc)
	}
	d.mu.Lock()
	d.peakUp = 1000 * 1024
	d.mu.Unlock()
	if loc := post("bandwidth"); !strings.Contains(loc, "ok=1") {
		t.Fatalf("bandwidth redirect = %s", loc)
	}
	cfg, _, _ = d.config()
	if cfg.SpeedLimitUpload != 800 || cfg.SpeedLimitDownload != 0 {
		t.Fatalf("limits = %d/%d", cfg.SpeedLimitDownload, cfg.SpeedLimitUpload)
	}
	if d.opts.Settings.Get(autospeedEnabledKey, "") != "true" {
		t.Fatal("bandwidth optimization must enable the latency auto-speed")
	}
}

func TestIPFilterSourceURLIsDownloadedAndReported(t *testing.T) {
	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("10.0.0.0/8\n192.168.5.0/24\n"))
	}))
	defer list.Close()
	d := feedDaemon(t, "")
	d.opts.Settings.Set("ipfilter", list.URL)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(srv.URL+"/ui/ipfilter", url.Values{"return": {"settings"}, "ipfilter": {list.URL}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, uiSettingsPath) || !strings.Contains(loc, "ok=1") {
		t.Fatalf("redirect = %s", loc)
	}
	status := d.ipFilterStatusSnapshot()
	if status.Rules != 2 || status.LoadedAt.IsZero() || status.Error != "" {
		t.Fatalf("status = %+v", status)
	}
	if html := d.ipFilterStatusHTML(); !strings.Contains(html, "rules loaded") {
		t.Fatalf("status html = %s", html)
	}
}

func TestStandaloneCardsHideCacheAndShowPortDot(t *testing.T) {
	d := feedDaemon(t, "")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	text := string(page)
	if strings.Contains(text, "I/O ops r/w") || strings.Contains(text, "cache r/w") {
		t.Fatal("standalone page must not show the cache and I/O tiles")
	}
	if !strings.Contains(text, `class="dot dot-`) || !strings.Contains(text, `href="/ui/archive"`) {
		t.Fatal("peer-port dot or Archive link missing")
	}
}

func TestFeedArchiveRebuildsIndexAndClampsPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), feedArchiveFile)
	archive, err := openFeedArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	items := []rss.Item{{Title: "A.Show.S01E01", Link: "https://x/1.torrent"}, {Title: "B.Show.S01E02", Link: "https://x/2.torrent"}, {Title: "C.Film", Link: "https://x/3.torrent"}}
	if _, err := archive.record("f", items, time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	// Drop the index, as an archive written before it existed would be.
	if err := archive.db.Update(func(tx *bolt.Tx) error { return tx.DeleteBucket(archiveIdxBucket) }); err != nil {
		t.Fatal(err)
	}
	_ = archive.close()
	archive, err = openFeedArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.close()
	page, err := archive.search(parseArchiveQuery("show", "", false), "new", 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Pages != 2 || page.Page != 2 || len(page.Items) != 1 {
		t.Fatalf("rebuilt index / clamped page = %+v", page)
	}
}

// TestAddDoesNotHoldTheLockWhileResolving: a magnet whose xs= source answers
// slowly must not freeze the daemon (API, page, queue) for the whole wait.
func TestAddDoesNotHoldTheLockWhileResolving(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		http.NotFound(w, nil)
	}))
	defer slow.Close()
	d := newTestDaemonWith(t, NetworkOptions{PortBegin: 42000, PortEnd: 42100, Encryption: 1, PEX: true, WebSourcesAllowPrivate: true})
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&xs=" + url.QueryEscape(slow.URL+"/x.torrent")
	done := make(chan error, 1)
	go func() {
		_, _, err := d.add(addRequest{Magnet: magnet})
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	d.mu.Lock()
	waited := time.Since(start)
	adding := len(d.adding)
	d.mu.Unlock()
	if waited > 500*time.Millisecond {
		t.Fatalf("d.mu was held for %v while the magnet source was fetched", waited)
	}
	if adding != 1 {
		t.Fatalf("the torrent being added is not marked (adding = %d)", adding)
	}
	if err := <-done; err != nil {
		t.Fatalf("add: %v", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.adding) != 0 || len(d.state.Torrents) != 1 {
		t.Fatalf("after add: adding=%d torrents=%d", len(d.adding), len(d.state.Torrents))
	}
}

// TestMagnetSourceOnLANRefusedByDefault: an xs= source on a private address
// (here 127.0.0.1) is never contacted unless the operator allows it, so a
// magnet from an untrusted feed cannot make the client request LAN services.
func TestMagnetSourceOnLANRefusedByDefault(t *testing.T) {
	var hits atomic.Int32
	lan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.NotFound(w, nil)
	}))
	defer lan.Close()
	d := newTestDaemon(t)
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&xs=" + url.QueryEscape(lan.URL+"/x.torrent")
	if _, _, err := d.add(addRequest{Magnet: magnet}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("the LAN source was contacted %d time(s)", got)
	}
}

// TestArchiveIdxRecordClampsSeeders: seeders come from a feed and can be
// arbitrarily large, so the compact 4-byte index field clamps both ends
// instead of truncating the uint32 conversion.
func TestArchiveIdxRecordClampsSeeders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seeders int
		want    int
	}{
		{"negative", -3, 0},
		{"in range", 42, 42},
		{"above uint32", 1 << 40, math.MaxUint32},
	} {
		view, ok := parseArchiveIdx(archiveIdxRecord(archiveEntry{Title: "A", Seeders: tc.seeders}))
		if !ok {
			t.Fatalf("%s: index record not readable", tc.name)
		}
		if view.seeders != tc.want {
			t.Fatalf("%s: seeders = %d, want %d", tc.name, view.seeders, tc.want)
		}
	}
}
