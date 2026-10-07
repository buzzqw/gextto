package main

import (
	"net/http"
	"net/http/httptest"
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
	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "gx-torrent") || !strings.Contains(body, "sola consultazione") {
		t.Fatalf("GET / body unexpected: %s", body)
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
