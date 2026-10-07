package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	buf := make([]byte, 16384)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "gx-torrent") || !strings.Contains(body, "Aggiungi") {
		t.Fatalf("GET / body unexpected: %s", body)
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
