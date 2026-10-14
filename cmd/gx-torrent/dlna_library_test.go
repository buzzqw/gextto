package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dlnaLibFixture builds a library folder:
//
//	lib/Film/Movie (2020)/movie.mkv + movie.srt
//	lib/Serie/Show/S01/ep1.mp4
//	lib/Serie/ZZZ-Cestino/old.mkv   (excluded)
//	lib/@eaDir/thumb.mkv, lib/.hidden/x.mkv (NAS system / hidden)
//	lib/notes.txt                   (not media)
//	lib/escape -> outside dir       (symlink out of the root)
//
// and returns the server and the library root.
func dlnaLibFixture(t *testing.T) (*dlnaServer, string) {
	t.Helper()
	base := t.TempDir()
	lib := filepath.Join(base, "lib")
	write := func(rel, data string) {
		p := filepath.Join(lib, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Film/Movie (2020)/movie.mkv", "0123456789abcdef")
	write("Film/Movie (2020)/movie.srt", "1\n00:00:01,000 --> 00:00:02,000\nhi\n")
	write("Serie/Show/S01/ep1.mp4", "mp4data")
	write("Serie/ZZZ-Cestino/old.mkv", "old")
	write("@eaDir/thumb.mkv", "x")
	write(".hidden/x.mkv", "x")
	write("notes.txt", "x")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.mkv"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(lib, "escape")); err != nil {
		t.Fatal(err)
	}
	srv := dlnaTestServer()
	srv.libRoots = dlnaLibRoots([]string{lib})
	srv.exclude = []string{filepath.Join(lib, "Serie", "ZZZ-Cestino")}
	return srv, lib
}

func browseIDs(t *testing.T, srv *dlnaServer, id string) (string, int) {
	t.Helper()
	didl, n, total, fault := srv.browseObject(srv.library(), id, "BrowseDirectChildren", 0, 0, "http://h:8200")
	if fault != nil {
		t.Fatalf("browse %q: fault %v", id, fault)
	}
	if n != total {
		t.Fatalf("browse %q: %d of %d", id, n, total)
	}
	return didl, n
}

func TestDLNALibraryTree(t *testing.T) {
	srv, _ := dlnaLibFixture(t)

	// Root: the library folder, then the torrents container.
	didl, n := browseIDs(t, srv, "0")
	if n != 2 || !strings.Contains(didl, `id="lib:0" parentID="0"`) || !strings.Contains(didl, `<dc:title>lib</dc:title>`) ||
		!strings.Contains(didl, `id="torrents" parentID="0"`) || strings.Index(didl, "lib:0") > strings.Index(didl, "torrents") {
		t.Fatalf("root wrong: %s", didl)
	}
	// The torrents moved under "torrents".
	if didl, n = browseIDs(t, srv, "torrents"); n != 2 || !strings.Contains(didl, `parentID="torrents"`) {
		t.Fatalf("torrents container wrong: %s", didl)
	}
	// The library root: only the real folders (no hidden/@eaDir, no txt, no
	// escaping symlink).
	didl, n = browseIDs(t, srv, "lib:0")
	if n != 2 || !strings.Contains(didl, `id="lib:0/Film"`) || !strings.Contains(didl, `id="lib:0/Serie"`) {
		t.Fatalf("library root wrong (%d): %s", n, didl)
	}
	for _, bad := range []string{"eaDir", "hidden", "notes", "escape"} {
		if strings.Contains(didl, bad) {
			t.Fatalf("library root lists %q: %s", bad, didl)
		}
	}
	// The excluded trash is not listed.
	if didl, _ = browseIDs(t, srv, "lib:0/Serie"); strings.Contains(didl, "ZZZ-Cestino") || !strings.Contains(didl, `id="lib:0/Serie/Show"`) {
		t.Fatalf("the trash must not be listed: %s", didl)
	}
	// A movie folder: the item with its subtitle and a Range-able URL.
	didl, n = browseIDs(t, srv, "lib:0/Film/Movie (2020)")
	for _, want := range []string{
		`id="lib:0/Film/Movie (2020)/movie.mkv" parentID="lib:0/Film/Movie (2020)"`,
		`>http://h:8200/lib/0/Film/Movie%20%282020%29/movie.mkv</res>`,
		`<sec:CaptionInfoEx sec:type="srt">http://h:8200/lib/0/Film/Movie%20%282020%29/movie.srt</sec:CaptionInfoEx>`,
		`size="16"`,
	} {
		if !strings.Contains(didl, want) {
			t.Fatalf("movie folder misses %q:\n%s", want, didl)
		}
	}
	if n != 1 {
		t.Fatalf("movie folder items = %d, want 1 (the subtitle is attached)", n)
	}
	// Metadata of a folder and of a file.
	if didl, n, _, f := srv.browseObject(srv.library(), "lib:0/Serie/Show", "BrowseMetadata", 0, 0, "http://h"); f != nil || n != 1 ||
		!strings.Contains(didl, `id="lib:0/Serie/Show" parentID="lib:0/Serie"`) {
		t.Fatalf("folder metadata wrong: %v %s", f, didl)
	}
	if didl, n, _, f := srv.browseObject(srv.library(), "lib:0/Serie/Show/S01/ep1.mp4", "BrowseMetadata", 0, 0, "http://h"); f != nil || n != 1 ||
		!strings.Contains(didl, "<dc:title>ep1</dc:title>") {
		t.Fatalf("file metadata wrong: %v %s", f, didl)
	}
	// Ids that must not resolve.
	for _, id := range []string{"lib:0/../outside", "lib:0/escape", "lib:0/escape/secret.mkv", "lib:1", "lib:01", "lib:0/notes.txt",
		"lib:0/Serie/ZZZ-Cestino", "lib:0/Serie/ZZZ-Cestino/old.mkv", "lib:0/@eaDir", "lib:0/.hidden/x.mkv", "lib:0//Film", "lib:x"} {
		if _, _, _, f := srv.browseObject(srv.library(), id, "BrowseMetadata", 0, 0, "http://h"); f != dlnaNoSuchObject {
			t.Errorf("%q must be no such object, got %v", id, f)
		}
	}
}

func TestDLNALibraryMedia(t *testing.T) {
	srv, _ := dlnaLibFixture(t)
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/lib/0/Film/Movie%20%282020%29/movie.mkv", nil)
	req.Header.Set("Range", "bytes=4-7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "4567" ||
		resp.Header.Get("Content-Type") != "video/x-matroska" || resp.Header.Get("contentFeatures.dlna.org") == "" {
		t.Fatalf("range read -> %d %q %v", resp.StatusCode, body, resp.Header)
	}
	resp, err = http.Get(ts.URL + "/lib/0/Film/Movie%20%282020%29/movie.srt")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("transferMode.dlna.org") != "Interactive" {
		t.Fatalf("subtitle -> %d %v", resp.StatusCode, resp.Header)
	}
	for _, p := range []string{
		"/lib/0/escape/secret.mkv",                 // symlink out of the root
		"/lib/0/..%2Foutside/secret.mkv",           // encoded traversal
		"/lib/0/Serie/ZZZ-Cestino/old.mkv",         // excluded
		"/lib/0/notes.txt",                         // not media
		"/lib/0/@eaDir/thumb.mkv",                  // NAS system folder
		"/lib/0/.hidden/x.mkv",                     // hidden
		"/lib/1/Film/Movie%20%282020%29/movie.mkv", // no such root
		"/lib/0/Film",                              // a folder
	} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 404 || strings.Contains(string(b), "secret") {
			t.Errorf("%s -> %d %q, want 404", p, resp.StatusCode, b)
		}
	}
}

func TestDLNALibraryMissingRootIsEmpty(t *testing.T) {
	srv := dlnaTestServer()
	srv.libRoots = dlnaLibRoots([]string{filepath.Join(t.TempDir(), "not-mounted")})
	if _, n := browseIDs(t, srv, "lib:0"); n != 0 {
		t.Fatalf("a missing library folder must be an empty container, got %d", n)
	}
}

func TestDLNASplitList(t *testing.T) {
	got := dlnaSplitList(" /a ;\n/b c;; \n")
	if len(got) != 2 || got[0] != "/a" || got[1] != "/b c" {
		t.Fatalf("dlnaSplitList = %q", got)
	}
}
