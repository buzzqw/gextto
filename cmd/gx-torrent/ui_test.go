package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clog "github.com/cenkalti/log"
)

func TestUIPageServes(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / -> %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET / content type = %q", ct)
	}
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	if !strings.Contains(body, "gx-torrent") || !strings.Contains(body, "Add") {
		t.Fatalf("GET / body unexpected: %s", body)
	}
}

func TestUILiveFragment(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/ui/live")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/live -> %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	if !strings.Contains(body, "IP filter") || strings.Contains(body, "<html") {
		t.Fatalf("live fragment unexpected: %s", body)
	}
}

func TestUIActions(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "src")
	torrent := makeTorrent(t, src, "payload.bin", 80_000)
	hash, _, err := d.add(addRequest{TorrentData: torrent, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool {
		info, ok := findInfo(d, hash)
		return ok && info.State == "seeding"
	})

	post := func(path string, values url.Values) int {
		resp, err := http.PostForm(server.URL+path, values)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := post("/ui/action", url.Values{"hash": {hash}, "op": {"pause"}}); code != http.StatusOK {
		t.Fatalf("pause -> %d", code)
	}
	waitFor(t, "paused", func() bool {
		info, ok := findInfo(d, hash)
		return ok && info.State == "paused"
	})

	if code := post("/ui/action", url.Values{"hash": {hash}, "op": {"resume"}}); code != http.StatusOK {
		t.Fatalf("resume -> %d", code)
	}
	waitFor(t, "resumed", func() bool {
		info, ok := findInfo(d, hash)
		return ok && info.State == "seeding"
	})

	// Removal keeps the payload.
	payload := filepath.Join(src, "payload.bin")
	if code := post("/ui/remove", url.Values{"hash": {hash}, "files": {"0"}}); code != http.StatusOK {
		t.Fatalf("remove -> %d", code)
	}
	if _, err := os.Stat(payload); err != nil {
		t.Fatalf("remove must keep the files: %v", err)
	}
	if _, ok := findInfo(d, hash); ok {
		t.Fatal("torrent must be gone after remove")
	}
}

func TestUIActionRejectsCrossOrigin(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/ui/action", strings.NewReader("op=pause-all"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST -> %d, want 403", resp.StatusCode)
	}
}

func TestUIAddRemoteTorrent(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "src")
	torrentBytes := makeTorrent(t, src, "payload.bin", 80_000)
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(torrentBytes)
	}))
	t.Cleanup(fileServer.Close)

	// Add a torrent from a remote URL (paste a URL).
	resp, err := http.PostForm(server.URL+"/ui/add", url.Values{
		"source":      {fileServer.URL + "/payload.torrent"},
		"destination": {src},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(d.list()) != 1 {
		t.Fatalf("remote add: %d torrents, want 1", len(d.list()))
	}
}

// makeTwoFileTorrent builds a .torrent with two files and one piece each, so a
// file can be skipped while another stays wanted.
func makeTwoFileTorrent(t *testing.T) []byte {
	t.Helper()
	a := make([]byte, 16384)
	b := make([]byte, 16384)
	_, _ = rand.Read(a)
	_, _ = rand.Read(b)
	sumA := sha1.Sum(a)
	sumB := sha1.Sum(b)
	pieces := append(sumA[:], sumB[:]...)
	return bencode(map[string]any{
		"info": map[string]any{
			"name": "pack",
			"files": []any{
				map[string]any{"length": len(a), "path": []any{"a.bin"}},
				map[string]any{"length": len(b), "path": []any{"b.bin"}},
			},
			"piece length": 16384,
			"pieces":       pieces,
		},
	})
}

func TestUIFilePrioritySkipsOneFile(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "src")
	hash, _, err := d.add(addRequest{TorrentData: makeTwoFileTorrent(t), Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "metadata", func() bool {
		info, ok := findInfo(d, hash)
		return ok && info.HasMetadata
	})

	resp, err := http.PostForm(server.URL+"/ui/file-priority", url.Values{
		"hash": {hash}, "index": {"0"}, "priority": {"0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("file-priority -> %d", resp.StatusCode)
	}

	d.mu.Lock()
	_, meta := d.findLocked(hash)
	var priorities []int
	if meta != nil {
		priorities = append(priorities, meta.FilePriorities...)
	}
	d.mu.Unlock()
	if len(priorities) != 2 || priorities[0] != 0 || priorities[1] <= 0 {
		t.Fatalf("file priorities not applied: %v", priorities)
	}
}

func TestUIDetailRendersGeneral(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "src")
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "payload.bin", 80_000), Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(server.URL + "/ui/detail?hash=" + hash + "&tab=general")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail -> %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"Piece size", "Available / total pieces", "Copy magnet", "Move data to"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("detail missing %q", want)
		}
	}
}

func TestUIPageRequiresToken(t *testing.T) {
	data := t.TempDir()
	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     NetworkOptions{PortBegin: 44400, PortEnd: 44500, Encryption: 1},
		Token:       "segreto",
		Tick:        50 * time.Millisecond,
		ProbeWindow: time.Minute,
	}
	d, err := newDaemon(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.close() })
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	if resp, _ := http.Get(server.URL + "/"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET / without token -> %d, want 401", resp.StatusCode)
	}
	if resp, _ := http.Get(server.URL + "/?token=sbagliato"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET / with wrong token -> %d, want 401", resp.StatusCode)
	}
	resp, err := http.Get(server.URL + "/?token=segreto")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /?token -> %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Set-Cookie"), "gx_token=segreto") {
		t.Fatalf("token page must remember the token: %q", resp.Header.Get("Set-Cookie"))
	}
	// The API keeps requiring the token: the page does not loosen it.
	if resp, _ := http.Get(server.URL + "/api/v1/stats"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/stats without token -> %d, want 401", resp.StatusCode)
	}
}

func TestUIGexttoLogTabReadsTheTail(t *testing.T) {
	d := newTestDaemon(t)
	logPath := filepath.Join(t.TempDir(), "gextto.log")
	var content strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&content, "2026-10-08 06:00:00  INFO line %d\n", i)
	}
	if err := os.WriteFile(logPath, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	// Without a configured log there is no tab and no endpoint.
	if resp, err := http.Get(server.URL + "/ui/gextto-log"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("no log configured -> %d, want 404", resp.StatusCode)
		}
	}

	d.opts.GexttoLog = logPath
	resp, err := http.Get(server.URL + "/ui/gextto-log?lines=200")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload struct {
		Path  string   `json:"path"`
		Lines []string `json:"lines"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Lines) != 200 || !strings.HasSuffix(payload.Lines[199], "line 3000") || !strings.HasSuffix(payload.Lines[0], "line 2801") {
		t.Fatalf("tail = %d lines, first %q, last %q", len(payload.Lines), payload.Lines[0], payload.Lines[len(payload.Lines)-1])
	}

	page, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(body), "Gextto log") {
		t.Fatal("the Gextto log tab is missing")
	}
}

func TestSwarmNoiseIsDemoted(t *testing.T) {
	cases := map[string]bool{
		"peer -> 1.2.3.4:6881|cannot complete outgoing handshake": true,
		"peer <- 1.2.3.4:6881|peer reset":                         true,
		"torrent abc|announce error: timeout":                     true,
		"torrent abc|file allocation error":                       false,
		"session|cannot open database":                            false,
	}
	for spec, want := range cases {
		name, message, _ := strings.Cut(spec, "|")
		if got := isSwarmNoise(&clog.Record{LoggerName: name, Message: message}); got != want {
			t.Errorf("%s: noise=%v, want %v", spec, got, want)
		}
	}
}
