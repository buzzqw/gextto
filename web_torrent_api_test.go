//go:build cgo

package gextto

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTorrentAPILifecycle drives the real HTTP torrent endpoints against a live
// leecher session that downloads from a local seeder: add via magnet, wait for
// it to appear, pause, resume and remove. It exercises the handler stack, the
// libtorrent client and the JSON contract together.
func TestTorrentAPILifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real torrent API test in short mode")
	}

	payload := make([]byte, 512*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("random payload: %v", err)
	}
	tracker := newLocalTracker()
	trackerServer := httptest.NewServer(tracker)
	defer trackerServer.Close()

	torrentBytes, infoHash := buildTorrentBytes(t, "api.bin", payload, 16384, trackerServer.URL+"/announce")
	torrentPath := filepath.Join(t.TempDir(), "api.torrent")
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}
	seedDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(seedDir, "api.bin"), payload, 0o644); err != nil {
		t.Fatalf("write seed data: %v", err)
	}

	seedCfg := transferTestConfig(t, t.TempDir(), freeTCPPort(t))
	seeder, err := NewLibtorrentClient(&seedCfg)
	if err != nil {
		t.Fatalf("seeder session: %v", err)
	}
	defer seeder.Shutdown(&seedCfg)
	if hash, err := seeder.AddTorrentFile(torrentPath, seedDir); err != nil || hash == nil {
		t.Fatalf("add seed torrent: %v", err)
	}
	seedDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(seedDeadline) {
		if current := findTorrent(seeder, infoHash); current != nil && current.State == "seeding" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	state := newTestAppState(t)
	realCfg := transferTestConfig(t, state.cfg.DataDir, freeTCPPort(t))
	leecher, err := NewLibtorrentClient(&realCfg)
	if err != nil {
		t.Fatalf("leecher session: %v", err)
	}
	defer leecher.Shutdown(&realCfg)
	state.cfg = &realCfg
	state.torrents = leecher
	if err := CompleteSetup(&realCfg); err != nil {
		t.Fatalf("complete setup: %v", err)
	}

	// latestConfig reloads the JSON file: make sure it is not dry-run.
	configJSON, _ := json.Marshal(map[string]any{
		"data_dir":            state.cfg.DataDir,
		"state_dir":           state.cfg.StateDir,
		"libtorrent_dir":      state.cfg.LibtorrentDir,
		"libtorrent_temp_dir": state.cfg.LibtorrentTempDir,
		"trash_path":          state.cfg.TrashPath,
		"dry_run":             false,
		"active":              false,
	})
	if err := os.WriteFile(state.config_path, configJSON, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	api := httptest.NewServer(Router(state))
	defer api.Close()

	magnet := "magnet:?xt=urn:btih:" + infoHash + "&dn=api.bin&tr=" + url.QueryEscape(trackerServer.URL+"/announce")
	addBody, _ := json.Marshal(map[string]any{"magnet": magnet})
	status, body := webPostJSON(t, api, "/api/torrents", string(addBody))
	if status >= 400 {
		t.Fatalf("add torrent -> %d: %s", status, body)
	}

	// The torrent must appear in the session listing.
	appeared := false
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		code, _, listing := webGet(t, api, "/api/torrents")
		if code == 200 && strings.Contains(string(listing), infoHash) {
			appeared = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !appeared {
		t.Fatal("torrent did not appear in /api/torrents")
	}

	// Pause, resume and remove through the API.
	if status, body := webPostJSON(t, api, "/api/torrents/"+infoHash+"/pause", "{}"); status >= 400 {
		t.Fatalf("pause -> %d: %s", status, body)
	}
	if status, body := webPostJSON(t, api, "/api/torrents/"+infoHash+"/resume", "{}"); status >= 400 {
		t.Fatalf("resume -> %d: %s", status, body)
	}
	if status, body := webPostJSON(t, api, "/api/torrents/"+infoHash+"/remove", `{"delete_files":false}`); status >= 400 {
		t.Fatalf("remove -> %d: %s", status, body)
	}

	removed := false
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		code, _, listing := webGet(t, api, "/api/torrents")
		if code == 200 && !strings.Contains(string(listing), infoHash) {
			removed = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !removed {
		t.Fatalf("torrent still present after removal: %s", fmt.Sprint(infoHash))
	}
}
