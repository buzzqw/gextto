package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	// The list (what the page and the APIs show) follows the rename too.
	listed := ""
	for _, info := range d.list() {
		if info.Hash == hash {
			listed = info.Name
		}
	}
	if listed != "New.Name" {
		t.Fatalf("listed name = %q, want New.Name", listed)
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

// TestExecuteRenameMovesNeverOverwrites: a target that already exists outside
// the torrent refuses the whole rename and nothing moves.
func TestExecuteRenameMovesNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	c, d := filepath.Join(dir, "c"), filepath.Join(dir, "d")
	for _, p := range []string{a, c, d} {
		if err := os.WriteFile(p, []byte(p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// a→b is fine, c→d would overwrite an unrelated file.
	if err := executeRenameMoves([]absRename{{old: a, new: b}, {old: c, new: d}}); err == nil {
		t.Fatal("a rename onto an existing file must be refused")
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatalf("the first move must not have happened: %v", err)
	}
	if got, _ := os.ReadFile(d); string(got) != d {
		t.Fatalf("the existing target was touched: %q", got)
	}
}

// TestPayloadGoneFollowsRenamedRoot: after a rename the payload check looks
// for the renamed content root, not the original name.
func TestPayloadGoneFollowsRenamedRoot(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 30_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	if err := d.renameFile(hash, "movie.bin", "other.bin"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the renamed file on disk", func() bool {
		_, err := os.Stat(filepath.Join(src, "other.bin"))
		return err == nil
	})
	waitFor(t, "the reloaded torrent", func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		tor, _ := d.findLocked(hash)
		return tor != nil && contentRoot(tor) == "other.bin"
	})
	d.mu.Lock()
	tor, meta := d.findLocked(hash)
	d.mu.Unlock()
	done := *meta
	done.CompletedAt = time.Now()
	done.SavePath = src
	if err := errPayloadGone(tor, &done); err != nil {
		t.Fatalf("payload reported gone after a rename: %v", err)
	}
}

// TestDaemonRenameRefusesExistingTarget: a clash with a file already on disk is
// reported to the caller at once, and nothing is queued.
func TestDaemonRenameRefusesExistingTarget(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 10_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	if err := os.WriteFile(filepath.Join(src, "taken.bin"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.renameFile(hash, "movie.bin", "taken.bin"); err == nil {
		t.Fatal("a rename onto an unrelated existing file must be refused")
	}
	d.mu.Lock()
	pending := d.pendingRename
	d.mu.Unlock()
	if pending != nil {
		t.Fatalf("a refused rename was queued: %+v", pending)
	}
}
