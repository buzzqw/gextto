package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// TestDaemonRenameFile renames a single-file torrent's file: the on-disk move
// happens with the session closed and the reload reopens the new path.
func TestDaemonRenameFile(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 40_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)

	if err := d.renameFile(hash, "movie.bin", "renamed.mkv"); err != nil {
		t.Fatalf("renameFile: %v", err)
	}
	waitFor(t, "the renamed file on disk", func() bool {
		_, err := os.Stat(filepath.Join(src, "renamed.mkv"))
		return err == nil
	})
	if _, err := os.Stat(filepath.Join(src, "movie.bin")); !os.IsNotExist(err) {
		t.Fatalf("the old file is still there (err=%v)", err)
	}

	d.mu.Lock()
	tor, _ := d.findLocked(hash)
	d.mu.Unlock()
	if tor == nil {
		t.Fatal("torrent not found after the reload")
	}
	files, err := tor.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path() != "renamed.mkv" {
		t.Fatalf("files = %+v", files)
	}
}

// TestDaemonRenameTorrent renames a torrent: the content root and the display
// name change.
func TestDaemonRenameTorrent(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 30_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)

	if err := d.renameTorrent(hash, "New.Name"); err != nil {
		t.Fatalf("renameTorrent: %v", err)
	}
	waitFor(t, "the renamed content on disk", func() bool {
		_, err := os.Stat(filepath.Join(src, "New.Name"))
		return err == nil
	})

	d.mu.Lock()
	tor, _ := d.findLocked(hash)
	d.mu.Unlock()
	if tor == nil {
		t.Fatal("torrent not found after the reload")
	}
	if tor.Name() != "New.Name" {
		t.Fatalf("name = %q, want New.Name", tor.Name())
	}
	files, err := tor.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path() != "New.Name" {
		t.Fatalf("files = %+v", files)
	}
}

// TestDaemonRenameValidation checks that a path escaping the torrent is refused
// before anything is queued.
func TestDaemonRenameValidation(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 10_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	if err := d.renameFile(hash, "../escape", "ok"); err == nil {
		t.Fatal("a path escaping the torrent must be refused")
	}
}

// TestQbitRenameEndpoints covers the qBittorrent rename routes.
func TestQbitRenameEndpoints(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 20_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
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
	if code := post("/api/v2/torrents/renameFile", url.Values{"hash": {hash}, "oldPath": {"movie.bin"}, "newPath": {"api.mkv"}}); code != http.StatusOK {
		t.Fatalf("renameFile: %d", code)
	}
	waitFor(t, "the renamed file via the API", func() bool {
		_, err := os.Stat(filepath.Join(src, "api.mkv"))
		return err == nil
	})
	// A path escaping the torrent is a 409.
	if code := post("/api/v2/torrents/renameFile", url.Values{"hash": {hash}, "oldPath": {"api.mkv"}, "newPath": {"../evil"}}); code != http.StatusConflict {
		t.Fatalf("renameFile escaping path: %d, want 409", code)
	}
	// A missing parameter is a 400.
	if code := post("/api/v2/torrents/rename", url.Values{"hash": {hash}}); code != http.StatusBadRequest {
		t.Fatalf("rename without name: %d, want 400", code)
	}
	if code := post("/api/v2/torrents/rename", url.Values{"hash": {hash}, "name": {"NewName"}}); code != http.StatusOK {
		t.Fatalf("rename: %d", code)
	}
}
