package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestQbitStateFor(t *testing.T) {
	done := torrentInfo{State: "paused", HasMetadata: true, TotalSize: 10, TotalDone: 10}
	cases := []struct {
		in   torrentInfo
		want string
	}{
		{torrentInfo{State: "downloading"}, "downloading"},
		{torrentInfo{State: "downloading", Pinned: true}, "forcedDL"},
		{torrentInfo{State: "downloading_metadata"}, "metaDL"},
		{torrentInfo{State: "downloading_metadata", Pinned: true}, "forcedMetaDL"},
		{torrentInfo{State: "seeding"}, "uploading"},
		{torrentInfo{State: "seeding", Pinned: true}, "forcedUP"},
		{torrentInfo{State: "paused"}, "pausedDL"},
		{done, "pausedUP"},
		{torrentInfo{State: "stalled"}, "stalledDL"},
		{torrentInfo{State: "stalled", HasMetadata: true, TotalSize: 10, TotalDone: 10}, "stalledUP"},
		{torrentInfo{State: "checking_files"}, "checkingDL"},
		{torrentInfo{State: "checking_files", HasMetadata: true, TotalSize: 10, TotalDone: 10}, "checkingUP"},
		{torrentInfo{State: "moving"}, "moving"},
		{torrentInfo{State: "error"}, "error"},
	}
	for _, c := range cases {
		if got := qbitStateFor(c.in); got != c.want {
			t.Errorf("qbitStateFor(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQbitMatchFilter(t *testing.T) {
	cases := []struct {
		filter string
		v      torrentInfo
		want   bool
	}{
		{"all", torrentInfo{State: "downloading"}, true},
		{"downloading", torrentInfo{State: "downloading"}, true},
		{"downloading", torrentInfo{State: "checking_files"}, true},
		{"downloading", torrentInfo{State: "seeding"}, false},
		{"seeding", torrentInfo{State: "seeding"}, true},
		{"completed", torrentInfo{State: "seeding", HasMetadata: true, TotalSize: 4, TotalDone: 4}, true},
		{"completed", torrentInfo{State: "seeding", HasMetadata: true, TotalSize: 4, TotalDone: 1}, false},
		{"paused", torrentInfo{State: "paused"}, true},
		{"active", torrentInfo{State: "downloading", DownloadRate: 10}, true},
		{"inactive", torrentInfo{State: "downloading"}, true},
		{"stalled_downloading", torrentInfo{State: "stalled"}, true},
		{"stalled_uploading", torrentInfo{State: "stalled"}, false},
		{"errored", torrentInfo{State: "error"}, true},
	}
	for _, c := range cases {
		if got := qbitMatchFilter(c.filter, c.v); got != c.want {
			t.Errorf("qbitMatchFilter(%q, %+v) = %v, want %v", c.filter, c.v, got, c.want)
		}
	}
}

func TestQbitSortAndPage(t *testing.T) {
	list := []qbitTorrent{{Name: "b", TotalSize: 2}, {Name: "a", TotalSize: 3}, {Name: "c", TotalSize: 1}}
	qbitSortTorrents(list, "name", false)
	if list[0].Name != "a" || list[2].Name != "c" {
		t.Fatalf("sort by name: %+v", list)
	}
	qbitSortTorrents(list, "size", true)
	if list[0].TotalSize != 3 || list[2].TotalSize != 1 {
		t.Fatalf("reverse sort by size: %+v", list)
	}
	paged := qbitPage(append([]qbitTorrent(nil), list...), "1", "1")
	if len(paged) != 1 || paged[0].TotalSize != 2 {
		t.Fatalf("limit/offset: %+v", paged)
	}
}

func TestQbitPieceStatesAndHashes(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 100_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	var states []int
	getJSONInto(t, srv.URL+"/api/v2/torrents/pieceStates?hash="+hash, &states)
	var hashes []string
	getJSONInto(t, srv.URL+"/api/v2/torrents/pieceHashes?hash="+hash, &hashes)
	if len(states) == 0 || len(states) != len(hashes) {
		t.Fatalf("pieceStates=%d pieceHashes=%d", len(states), len(hashes))
	}
	for _, s := range states {
		if s < 0 || s > 2 {
			t.Fatalf("piece state out of range: %d", s)
		}
	}
	// An unknown hash yields empty arrays, not an error.
	if err := json.NewDecoder(mustGet(t, srv.URL+"/api/v2/torrents/pieceStates?hash=deadbeef").Body).Decode(&states); err != nil {
		t.Fatalf("pieceStates unknown hash: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("pieceStates unknown hash = %v, want empty", states)
	}
}

func TestQbitWebseedsAndPerTorrentLimits(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 50_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	// No web seeds: an empty array.
	var seeds []qbitWebseed
	getJSONInto(t, srv.URL+"/api/v2/torrents/webseeds?hash="+hash, &seeds)
	if len(seeds) != 0 {
		t.Fatalf("webseeds = %+v, want empty", seeds)
	}

	// Set a per-torrent download limit, then read it back (bytes/s). The limit
	// reaches the published snapshot on a cycle, so poll briefly.
	if _, err := http.PostForm(srv.URL+"/api/v2/torrents/setDownloadLimit", url.Values{"hashes": {hash}, "limit": {strconv.Itoa(2 << 20)}}); err != nil {
		t.Fatal(err)
	}
	var limits map[string]int64
	deadline := time.Now().Add(3 * time.Second)
	for {
		limits = map[string]int64{}
		getJSONInto(t, srv.URL+"/api/v2/torrents/downloadLimit?hashes="+hash, &limits)
		if limits[hash] == int64(2<<20) || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if limits[hash] != int64(2<<20) {
		t.Fatalf("downloadLimit = %v, want %d", limits, 2<<20)
	}
	// No upload limit: zero.
	getJSONInto(t, srv.URL+"/api/v2/torrents/uploadLimit?hashes="+hash, &limits)
	if limits[hash] != 0 {
		t.Fatalf("uploadLimit = %v, want 0", limits)
	}
}

func TestQbitLogAndNetworkEndpoints(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	var entries []qbitLogEntry
	getJSONInto(t, srv.URL+"/api/v2/log/main", &entries)
	for _, e := range entries {
		if e.ID == 0 || e.Message == "" {
			t.Fatalf("bad log entry: %+v", e)
		}
	}

	var ifaces []string
	getJSONInto(t, srv.URL+"/api/v2/app/networkInterfaceList", &ifaces)
	if ifaces == nil {
		t.Fatal("networkInterfaceList must be an array, not null")
	}

	body := mustGet(t, srv.URL+"/api/v2/transfer/speedLimitsMode")
	raw, _ := io.ReadAll(body.Body)
	body.Body.Close()
	if got := strings.TrimSpace(string(raw)); got != "0" && got != "1" {
		t.Fatalf("speedLimitsMode = %q, want 0 or 1", got)
	}
}

// getJSONInto GETs a URL and decodes the JSON body into out.
func getJSONInto(t *testing.T, url string, out any) {
	t.Helper()
	resp := mustGet(t, url)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestQbitPreferencesSurface checks that the preferences document carries the
// keys qBittorrent clients read.
func TestQbitPreferencesSurface(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	var prefs map[string]any
	getJSONInto(t, srv.URL+"/api/v2/app/preferences", &prefs)
	for _, key := range []string{
		"save_path", "temp_path", "temp_path_enabled", "listen_port", "dht", "pex",
		"lsd", "upnp", "encryption", "bittorrent_protocol", "queueing_enabled",
		"max_active_downloads", "max_active_uploads", "max_active_torrents",
		"dont_count_slow_torrents", "slow_torrent_dl_rate_threshold",
		"dl_limit", "up_limit", "max_connec", "scheduler_enabled",
		"ip_filter_enabled", "ip_filter_trackers", "rss_refresh_interval",
		"save_resume_data_interval", "scan_dirs",
	} {
		if _, ok := prefs[key]; !ok {
			t.Errorf("preferences missing key %q", key)
		}
	}
}

// TestV1ExtraReadEndpoints covers the additive v1 read surface: the webseeds
// resource, the single-torrent resource and the list paging.
func TestV1ExtraReadEndpoints(t *testing.T) {
	d := newTestDaemon(t)
	d.opts.Token = "test-token"
	src := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 20_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	get := func(path string, out any) int {
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Gx-Token", "test-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil && resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
		}
		return resp.StatusCode
	}

	var seeds []webseedInfo
	if code := get("/api/v1/torrents/"+hash+"/webseeds", &seeds); code != http.StatusOK {
		t.Fatalf("webseeds: status %d", code)
	}
	if len(seeds) != 0 {
		t.Fatalf("webseeds = %+v, want empty", seeds)
	}

	var one torrentInfo
	if code := get("/api/v1/torrents/"+hash, &one); code != http.StatusOK {
		t.Fatalf("single torrent: status %d", code)
	}
	if !strings.EqualFold(one.Hash, hash) {
		t.Fatalf("single torrent hash = %q, want %q", one.Hash, hash)
	}
	if code := get("/api/v1/torrents/deadbeef", nil); code != http.StatusNotFound {
		t.Fatalf("unknown single torrent: status %d, want 404", code)
	}

	var paged []torrentInfo
	if code := get("/api/v1/torrents?limit=1", &paged); code != http.StatusOK {
		t.Fatalf("limit=1: status %d", code)
	}
	if len(paged) != 1 {
		t.Fatalf("limit=1 returned %d, want 1", len(paged))
	}
	var none []torrentInfo
	if code := get("/api/v1/torrents?offset=100", &none); code != http.StatusOK {
		t.Fatalf("offset=100: status %d", code)
	}
	if len(none) != 0 {
		t.Fatalf("offset=100 returned %d, want 0", len(none))
	}
}

type qbitSyncPayload struct {
	RID             int64                  `json:"rid"`
	FullUpdate      bool                   `json:"full_update"`
	Torrents        map[string]qbitTorrent `json:"torrents"`
	TorrentsRemoved []string               `json:"torrents_removed"`
}

func qbitSyncCall(t *testing.T, base, rid string) qbitSyncPayload {
	t.Helper()
	url := base + "/api/v2/sync/maindata"
	if rid != "" {
		url += "?rid=" + rid
	}
	var payload qbitSyncPayload
	getJSONInto(t, url, &payload)
	return payload
}

// TestQbitSyncIncremental checks that sync/maindata answers a full snapshot on a
// first/stale rid and only the diff when the client echoes the last rid.
func TestQbitSyncIncremental(t *testing.T) {
	d := newTestDaemon(t)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	first := qbitSyncCall(t, srv.URL, "")
	if !first.FullUpdate || first.RID == 0 {
		t.Fatalf("first sync: %+v", first)
	}
	src := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 40_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	second := qbitSyncCall(t, srv.URL, strconv.FormatInt(first.RID, 10))
	if second.FullUpdate {
		t.Fatalf("second poll must be incremental")
	}
	if _, ok := second.Torrents[strings.ToLower(hash)]; !ok {
		t.Fatalf("incremental diff misses the new torrent: %+v", second.Torrents)
	}
	// A stale rid gets a full update again.
	if third := qbitSyncCall(t, srv.URL, "0"); !third.FullUpdate {
		t.Fatalf("stale rid must be a full update")
	}
}

// TestQbitTorrentsInfoCategoryTagAndState exercises the filters end to end:
// category, tag, state filter and the empty-category/empty-tag meaning.
func TestQbitTorrentsInfoCategoryTagAndState(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 80_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	d.createCategory("movies", "/srv/movies")
	if err := d.setCategory(hash, "movies"); err != nil {
		t.Fatal(err)
	}
	if err := d.addTags(hash, []string{"sonarr"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	count := func(query string) int {
		var list []qbitTorrent
		getJSONInto(t, srv.URL+"/api/v2/torrents/info?"+query, &list)
		return len(list)
	}
	// The category/tag live in the published snapshot: wait until they show up.
	deadline := time.Now().Add(3 * time.Second)
	for count("category=movies") != 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := count("category=movies"); n != 1 {
		t.Fatalf("category=movies returned %d, want 1", n)
	}
	if n := count("category=other"); n != 0 {
		t.Fatalf("category=other returned %d, want 0", n)
	}
	if n := count("category="); n != 0 {
		t.Fatalf("empty category returned %d, want 0", n)
	}
	if n := count("tag=sonarr"); n != 1 {
		t.Fatalf("tag=sonarr returned %d, want 1", n)
	}
	if n := count("tag=nope"); n != 0 {
		t.Fatalf("tag=nope returned %d, want 0", n)
	}
	if n := count("filter=downloading"); n != 1 {
		t.Fatalf("filter=downloading returned %d, want 1", n)
	}
	if n := count("filter=seeding"); n != 0 {
		t.Fatalf("filter=seeding returned %d, want 0", n)
	}
}
