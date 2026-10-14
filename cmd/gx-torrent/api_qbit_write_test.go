package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestQbitWriteEndpoints covers the qBittorrent write endpoints added on top of
// the base surface: addPeers, banPeers, editCategory, toggleFirstLastPiecePrio
// and the queue priorities. (app/shutdown is not exercised: it signals the
// process.)
func TestQbitWriteEndpoints(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	hashA, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "a.bin", 30_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	hashB, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "b.bin", 30_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hashA)
	waitListed(t, d, hashB)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	post := func(path string, form url.Values) int {
		t.Helper()
		resp, err := http.PostForm(srv.URL+path, form)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// addPeers accepts a peer for a known torrent.
	if code := post("/api/v2/torrents/addPeers", url.Values{"hashes": {hashA}, "peers": {"127.0.0.1:9"}}); code != http.StatusOK {
		t.Fatalf("addPeers: %d", code)
	}

	// banPeers bans the address in the session.
	if code := post("/api/v2/transfer/banPeers", url.Values{"peers": {"10.20.30.40:6881|10.20.30.41:1"}}); code != http.StatusOK {
		t.Fatalf("banPeers: %d", code)
	}
	if !d.session.IsBannedIP("10.20.30.40") || !d.session.IsBannedIP("10.20.30.41") {
		t.Fatal("banPeers did not ban the addresses")
	}

	// editCategory sets the save path of a category.
	if code := post("/api/v2/torrents/editCategory", url.Values{"category": {"movies"}, "savePath": {"/srv/movies"}}); code != http.StatusOK {
		t.Fatalf("editCategory: %d", code)
	}
	if got := d.categoriesSnapshot()["movies"]; got != "/srv/movies" {
		t.Fatalf("editCategory savePath = %q, want /srv/movies", got)
	}

	// toggleFirstLastPiecePrio flips the flag.
	before := metaFirstLast(t, d, hashA)
	if code := post("/api/v2/torrents/toggleFirstLastPiecePrio", url.Values{"hashes": {hashA}}); code != http.StatusOK {
		t.Fatalf("toggleFirstLastPiecePrio: %d", code)
	}
	if after := metaFirstLast(t, d, hashA); after == before {
		t.Fatal("first/last did not flip")
	}

	// topPrio moves a torrent to the head of the queue.
	if code := post("/api/v2/torrents/topPrio", url.Values{"hashes": {hashB}}); code != http.StatusOK {
		t.Fatalf("topPrio: %d", code)
	}
	if pos(t, d, hashB) >= pos(t, d, hashA) {
		t.Fatalf("topPrio did not move b to the head: a=%d b=%d", pos(t, d, hashA), pos(t, d, hashB))
	}
	// bottomPrio moves it back to the end.
	if code := post("/api/v2/torrents/bottomPrio", url.Values{"hashes": {hashB}}); code != http.StatusOK {
		t.Fatalf("bottomPrio: %d", code)
	}
	if pos(t, d, hashB) <= pos(t, d, hashA) {
		t.Fatalf("bottomPrio did not move b to the end: a=%d b=%d", pos(t, d, hashA), pos(t, d, hashB))
	}
}

func metaFirstLast(t *testing.T, d *Daemon, hash string) bool {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	_, meta := d.findLocked(hash)
	if meta == nil {
		t.Fatalf("meta %s not found", hash)
	}
	return meta.FirstLast
}

func pos(t *testing.T, d *Daemon, hash string) int64 {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	_, meta := d.findLocked(hash)
	if meta == nil {
		t.Fatalf("meta %s not found", hash)
	}
	return meta.Pos
}
