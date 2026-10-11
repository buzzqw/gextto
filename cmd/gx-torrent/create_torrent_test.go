package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleCreateTorrent(t *testing.T) {
	d := newQuietDaemon(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"path":     dir,
		"name":     "mytorrent",
		"trackers": []string{"http://tracker.example/announce"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/create-torrent", strings.NewReader(string(body)))
	d.handleCreateTorrent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-bittorrent" {
		t.Errorf("Content-Type = %q", ct)
	}
	out := rec.Body.String()
	if !strings.HasPrefix(out, "d") || !strings.Contains(out, "mytorrent") {
		t.Errorf("body does not look like the created torrent: %q", out)
	}
}

func TestHandleCreateTorrentMissingPath(t *testing.T) {
	d := newQuietDaemon(t)
	body, _ := json.Marshal(map[string]any{"path": filepath.Join(t.TempDir(), "nope")})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/create-torrent", strings.NewReader(string(body)))
	d.handleCreateTorrent(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
