package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestWatchFoldersParsing(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	if got := d.watchFolders(); got != nil {
		t.Fatalf("default watch folders = %v, want nil", got)
	}
	d.opts.Settings.Set(watchFoldersSettingKey, "/srv/watch\n/srv/incoming=/srv/media\n\n  /srv/x  ")
	got := d.watchFolders()
	want := [][2]string{{"/srv/watch", ""}, {"/srv/incoming", "/srv/media"}, {"/srv/x", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("watch folders = %v, want %v", got, want)
	}
}

func TestWatchFoldersScan(t *testing.T) {
	d := newTestDaemon(t)
	watchDir := t.TempDir()
	dest := t.TempDir()
	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 10_000)
	file := filepath.Join(watchDir, "movie.torrent")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-.torrent file must be left alone.
	txt := filepath.Join(watchDir, "readme.txt")
	if err := os.WriteFile(txt, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A file not yet settled (it may still be being copied) waits.
	d.scanWatchFolders([][2]string{{watchDir, dest}})
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("a freshly written .torrent must wait for the next scan: %v", err)
	}
	// A corrupt .torrent is set aside, not retried at every scan.
	bad := filepath.Join(watchDir, "broken.torrent")
	if err := os.WriteFile(bad, []byte("not bencode"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	for _, p := range []string{file, bad} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	d.scanWatchFolders([][2]string{{watchDir, dest}})
	if _, err := os.Stat(bad + watchInvalidSuffix); err != nil {
		t.Fatalf("the corrupt .torrent was not renamed aside: %v", err)
	}

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("the .torrent was not removed after adding (err=%v)", err)
	}
	if _, err := os.Stat(txt); err != nil {
		t.Fatalf("a non-torrent file was removed: %v", err)
	}
	waitFor(t, "torrent added from the watch folder", func() bool {
		for _, v := range d.list() {
			if v.SavePath == dest {
				return true
			}
		}
		return false
	})
}

func TestQbitScanDirsPreferences(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	var prefs map[string]any
	getJSONInto(t, srv.URL+"/api/v2/app/preferences", &prefs)
	if m, ok := prefs["scan_dirs"].(map[string]any); !ok || len(m) != 0 {
		t.Fatalf("scan_dirs = %v, want empty object", prefs["scan_dirs"])
	}

	body := `{"scan_dirs":{"/srv/watch":1,"/srv/in":0,"/srv/other":"/srv/dest"}}`
	if code := postFormJSON(t, srv.URL+"/api/v2/app/setPreferences", url.Values{"json": {body}}, nil); code != http.StatusOK {
		t.Fatalf("setPreferences: %d", code)
	}
	got := d.watchFolders()
	lines := make([]string, 0, len(got))
	for _, pair := range got {
		if pair[1] == "" {
			lines = append(lines, pair[0])
		} else {
			lines = append(lines, pair[0]+"="+pair[1])
		}
	}
	sort.Strings(lines)
	want := []string{"/srv/in=/srv/in", "/srv/other=/srv/dest", "/srv/watch"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("watch folders after setPreferences = %v, want %v", lines, want)
	}

	// Reading the preferences back renders the qBittorrent scan_dirs shape.
	prefs = nil
	getJSONInto(t, srv.URL+"/api/v2/app/preferences", &prefs)
	dirs, ok := prefs["scan_dirs"].(map[string]any)
	if !ok || len(dirs) != 3 {
		t.Fatalf("scan_dirs = %v", prefs["scan_dirs"])
	}
	if dirs["/srv/watch"] != float64(1) || dirs["/srv/in"] != float64(0) || dirs["/srv/other"] != "/srv/dest" {
		t.Fatalf("scan_dirs = %v", dirs)
	}
}

// TestWatchFoldersSkipUnsafe locks the drop-folder rules: a link is not a
// torrent to read, a world-writable folder is refused, and a folder inside the
// daemon's own data is refused.
func TestWatchFoldersSkipUnsafe(t *testing.T) {
	d := newTestDaemon(t)
	watchDir := t.TempDir()
	dest := t.TempDir()
	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 10_000)
	realTorrent := filepath.Join(t.TempDir(), "real.torrent")
	if err := os.WriteFile(realTorrent, data, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(watchDir, "link.torrent")
	if err := os.Symlink(realTorrent, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(realTorrent, old, old); err != nil {
		t.Fatal(err)
	}
	d.scanWatchFolders([][2]string{{watchDir, dest}})
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("a symlinked .torrent was consumed: %v", err)
	}

	open := t.TempDir()
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(open, 0o700) })
	file := filepath.Join(open, "movie.torrent")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	d.scanWatchFolders([][2]string{{open, dest}})
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("a world-writable folder was scanned: %v", err)
	}

	inside := filepath.Join(d.opts.DataDir, "drop")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(inside, "movie.torrent")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	d.scanWatchFolders([][2]string{{inside, dest}})
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("a folder inside the daemon's data was scanned: %v", err)
	}
}

// TestWatchFoldersCap bounds one scan: a huge drop is handled a batch at a
// time instead of holding the scan goroutine.
func TestWatchFoldersCap(t *testing.T) {
	d := newTestDaemon(t)
	watchDir := t.TempDir()
	dest := t.TempDir()
	old := time.Now().Add(-time.Minute)
	total := watchMaxPerScan + 4
	for i := 0; i < total; i++ {
		file := filepath.Join(watchDir, fmt.Sprintf("%03d.torrent", i))
		if err := os.WriteFile(file, []byte("not bencode"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, old, old); err != nil {
			t.Fatal(err)
		}
	}
	d.scanWatchFolders([][2]string{{watchDir, dest}})
	aside := 0
	left := 0
	entries, err := os.ReadDir(watchDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		switch {
		case strings.HasSuffix(entry.Name(), watchInvalidSuffix):
			aside++
		case strings.HasSuffix(entry.Name(), ".torrent"):
			left++
		}
	}
	if aside != watchMaxPerScan || left != total-watchMaxPerScan {
		t.Fatalf("after one scan: %d set aside, %d left; want %d and %d", aside, left, watchMaxPerScan, total-watchMaxPerScan)
	}
}
