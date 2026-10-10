package main

// contract_test.go pins the /api/v1 wire format that Gextto's adapter
// (gxtorrent_engine.go) consumes. The key lists below are the consumer's
// expectations: if a future refactor renames or drops one of them, this test
// fails. Additive fields are allowed (the adapter ignores unknown keys), so the
// daemon may grow; removals and renames may not. It is the frozen contract of
// F0 (see docs/gx-torrent-evoluto.md).

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func jsonObject(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, raw)
	}
	return out
}

func requireKeys(t *testing.T, what string, got map[string]json.RawMessage, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := got[key]; !ok {
			t.Errorf("%s: missing contract key %q", what, key)
		}
	}
}

func getJSONObject(t *testing.T, server *httptest.Server, path string) map[string]json.RawMessage {
	t.Helper()
	resp, err := http.Get(server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", path, resp.StatusCode, body)
	}
	return jsonObject(t, body)
}

// gxTorrentItemKeys are the fields gxtorrent_engine.go's gxTorrentItem reads
// from each element of GET /api/v1/torrents.
var gxTorrentItemKeys = []string{
	"hash", "name", "state", "save_path", "progress", "total_size",
	"total_done", "download_rate", "upload_rate", "downloaded", "uploaded",
	"seeding_seconds", "active_seconds", "queue_position", "num_peers",
	"num_seeds", "num_complete", "num_incomplete", "seed_ratio", "seed_days",
	"download_limit_kib", "upload_limit_kib", "has_metadata", "auto_managed",
	"parked", "error", "completed_at", "current_tracker", "torrent_version",
	"super_seeding",
}

func TestContractTorrentItemShape(t *testing.T) {
	// The omitempty fields are populated so every contract key is emitted; a
	// real torrent always reports them once it is known.
	item := torrentInfo{CurrentTracker: "udp://tracker.example:6969", Error: "boom", CompletedAt: 1}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	requireKeys(t, "torrents[]", jsonObject(t, raw), gxTorrentItemKeys...)
}

func TestContractHealthShape(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	defer server.Close()

	requireKeys(t, "health", getJSONObject(t, server, "/api/v1/health"),
		"ok", "version", "torrents", "pid", "mode", "fingerprint", "data_dir")
}

func TestContractStatsShape(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	defer server.Close()

	// Keys read by the Gextto Health page and by GET /api/v1/stats consumers.
	requireKeys(t, "stats", getJSONObject(t, server, "/api/v1/stats"),
		"version", "uptime_seconds", "torrents", "downloading", "seeding",
		"queued", "stalled", "paused", "slow", "moving", "download_rate",
		"upload_rate", "peers", "peer_port", "listen_address", "port_mapping",
		"ip_filter_rules", "encryption", "proxy", "dht", "utp", "cache_read_mb",
		"cache_write_mb", "cache_auto", "preallocate", "lsd", "disk_free_bytes",
		"disk_total_bytes", "session", "active_downloads", "active_seeds",
		"active_limit", "restart_pending", "ratio")
}

func TestContractConfigShape(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	defer server.Close()

	body := getJSONObject(t, server, "/api/v1/config")
	requireKeys(t, "config", body, "config", "effective", "restart_pending")
	inner := jsonObject(t, body["config"])
	requireKeys(t, "config.config", inner,
		"active_downloads", "active_seeds", "active_limit", "dont_count_slow",
		"slow_rate", "slow_after_secs", "slow_rotate_secs", "dynamic_queue",
		"dynamic_min", "dynamic_max", "speed_limit_download",
		"speed_limit_upload", "max_peer_dial", "max_peer_accept", "cache_mb",
		"cache_ttl_secs", "preallocate", "sequential", "auto")
}

func TestContractListIsArrayAndPortCheckIsObject(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/torrents")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET torrents: status %d: %s", resp.StatusCode, body)
	}
	var list []json.RawMessage
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("torrents must be a JSON array: %v\n%s", err, body)
	}

	getJSONObject(t, server, "/api/v1/portcheck")
}
