package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
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
