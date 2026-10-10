package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func trackerURLs(t *testing.T, d *Daemon, hash string) []string {
	t.Helper()
	d.mu.Lock()
	tor, _ := d.findLocked(hash)
	d.mu.Unlock()
	if tor == nil {
		t.Fatalf("torrent %s not found", hash)
	}
	var out []string
	for _, tr := range tor.Trackers() {
		out = append(out, tr.URL)
	}
	sort.Strings(out)
	return out
}

func TestTrackerAddRemoveEdit(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 100_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.addTrackers(hash, []string{"http://a/announce", "http://b/announce"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "trackers added", func() bool { return len(trackerURLs(t, d, hash)) == 2 })
	if err := d.removeTrackers(hash, []string{"http://a/announce"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "tracker removed", func() bool {
		got := trackerURLs(t, d, hash)
		return len(got) == 1 && got[0] == "http://b/announce"
	})
	if err := d.editTracker(hash, "http://b/announce", "http://c/announce"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "tracker edited", func() bool {
		got := trackerURLs(t, d, hash)
		return len(got) == 1 && got[0] == "http://c/announce"
	})
}

func TestToggleSequentialAndSuperSeeding(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 50_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	metaOf := func() torrentMeta {
		d.mu.Lock()
		defer d.mu.Unlock()
		_, meta := d.findLocked(hash)
		if meta == nil {
			t.Fatal("meta not found")
		}
		return *meta
	}
	before := metaOf().Sequential
	if err := d.toggleSequential(hash); err != nil {
		t.Fatal(err)
	}
	if metaOf().Sequential == before {
		t.Fatal("sequential did not flip")
	}
	if err := d.setSuperSeeding(hash, true); err != nil {
		t.Fatal(err)
	}
	if !metaOf().SuperSeeding {
		t.Fatal("super-seeding not set")
	}
}

func TestQbitSyncTorrentPeersShape(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v2/sync/torrentPeers?hash=deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var payload struct {
		Peers map[string]any `json:"peers"`
		Rid   int            `json:"rid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Peers == nil {
		t.Fatal("peers must be an (empty) object, not null")
	}
}

func TestQbitExportTorrentAndFilePrio(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeMultiTorrent(t, src, "Season", []int{30_000, 20_000})
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	// export returns the .torrent bytes (a bencoded dictionary).
	resp, err := http.Get(srv.URL + "/api/v2/torrents/export?hash=" + hash)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 || body[0] != 'd' {
		t.Fatalf("export is not a bencoded torrent: %q", body)
	}

	// filePrio skips file 0 and keeps file 1 wanted: the daemon records [0, 4].
	form := url.Values{"hash": {hash}, "id": {"0"}, "priority": {"0"}}
	if _, err := http.PostForm(srv.URL+"/api/v2/torrents/filePrio", form); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "file priority applied", func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		_, meta := d.findLocked(hash)
		return meta != nil && len(meta.FilePriorities) == 2 && meta.FilePriorities[0] == 0 && meta.FilePriorities[1] == 4
	})
}

func TestQbitForceStartAndTorrentLimit(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 30_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	post := func(path string, form url.Values) {
		t.Helper()
		resp, err := http.PostForm(srv.URL+path, form)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
	}
	post("/api/v2/torrents/setForceStart", url.Values{"hashes": {hash}, "value": {"true"}})
	post("/api/v2/torrents/setDownloadLimit", url.Values{"hashes": {hash}, "limit": {strconv.Itoa(2 << 20)}})

	waitFor(t, "pin and limit applied", func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		_, meta := d.findLocked(hash)
		return meta != nil && meta.Pinned &&
			meta.DownloadLimitKib != nil && *meta.DownloadLimitKib == (2<<20)/1024
	})
}

func TestQbitSetPreferences(t *testing.T) {
	d := newTestDaemon(t)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	body := `{"dl_limit":2097152,"up_limit":1048576,"queueing_enabled":false,"disk_cache":512}`
	resp, err := http.PostForm(srv.URL+"/api/v2/app/setPreferences", url.Values{"json": {body}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	d.mu.Lock()
	dl, up := d.state.Config.SpeedLimitDownload, d.state.Config.SpeedLimitUpload
	d.mu.Unlock()
	if dl != 2048 || up != 1024 {
		t.Fatalf("global limits = %d/%d KiB, want 2048/1024", dl, up)
	}

	// A positive but tiny limit rounds up to 1 KiB, not 0 (which means unlimited).
	resp2, err := http.PostForm(srv.URL+"/api/v2/app/setPreferences", url.Values{"json": {`{"dl_limit":512}`}})
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	d.mu.Lock()
	small := d.state.Config.SpeedLimitDownload
	d.mu.Unlock()
	if small != 1 {
		t.Fatalf("small global limit = %d KiB, want 1", small)
	}
}

func TestQbitTorrentsInfoFilterAndAddTrackers(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "movie.bin", 100_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	// /torrents/info?hashes=<hash> returns only that torrent.
	resp, err := http.Get(srv.URL + "/api/v2/torrents/info?hashes=" + hash)
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("info filter returned %d items, want 1", len(list))
	}
	if got, _ := list[0]["hash"].(string); !strings.EqualFold(got, hash) {
		t.Fatalf("info filter returned %q, want %q", got, hash)
	}

	// addTrackers addresses the torrent with the singular "hash" field.
	r2, err := http.PostForm(srv.URL+"/api/v2/torrents/addTrackers", url.Values{"hash": {hash}, "urls": {"http://x/announce"}})
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	waitFor(t, "tracker added via the singular hash", func() bool {
		return len(trackerURLs(t, d, hash)) == 1
	})
}
