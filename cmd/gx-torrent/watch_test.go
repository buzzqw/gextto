package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
