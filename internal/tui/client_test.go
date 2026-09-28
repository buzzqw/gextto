package tui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordedRequest struct {
	method string
	path   string
	body   string
	token  string
}

func newTestClient(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body string)) (*Client, *[]recordedRequest) {
	t.Helper()
	recorded := &[]recordedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		*recorded = append(*recorded, recordedRequest{
			method: r.Method,
			path:   r.URL.RequestURI(),
			body:   string(data),
			token:  r.Header.Get("X-Gextto-Token"),
		})
		handler(w, r, string(data))
	}))
	t.Cleanup(server.Close)
	return NewClient(server.URL, "secret-token"), recorded
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func TestClientReadsEndpoints(t *testing.T) {
	client, recorded := newTestClient(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		switch {
		case r.URL.Path == "/api/status":
			writeJSON(t, w, map[string]any{
				"name": "gextto", "version": "1.0.42", "active": true, "dry_run": false,
				"next_cycle_at": "2030-01-01T00:00:00Z",
				"torrent_stats": map[string]any{"count": 3, "downloading": 1, "seeding": 2},
				"last_cycle":    map[string]any{"scraped": 10, "errors": 1},
				"seen":          map[string]any{"movies": 5, "series": 7, "groups": 12},
			})
		case r.URL.Path == "/api/torrents":
			writeJSON(t, w, []map[string]any{{
				"hash": "abc", "name": "Example", "state": "downloading", "progress": 12.5,
				"total_size": 2048, "total_done": 1024, "download_rate": 100, "upload_rate": 50,
				"num_peers": 2, "num_seeds": 1, "seed_ratio": 1.5,
			}})
		case r.URL.Path == "/api/torrents/abc":
			writeJSON(t, w, map[string]any{
				"ok": true, "magnet": "magnet:?xt=urn:btih:abc", "no_rename": true,
				"torrent": map[string]any{"hash": "abc", "name": "Example", "state": "paused"},
			})
		case r.URL.Path == "/api/torrents/abc/trackers":
			writeJSON(t, w, map[string]any{"trackers": []map[string]any{{"tier": 1, "url": "udp://tracker"}}})
		case r.URL.Path == "/api/logs":
			writeJSON(t, w, map[string]any{"items": []string{"line one", "line two"}})
		case r.URL.Path == "/api/health":
			writeJSON(t, w, map[string]any{"status": "ok", "process_id": 42, "resident_bytes": 1024,
				"paths": []map[string]any{{"label": "dati", "path": "/data", "exists": true, "writable": true}}})
		case r.URL.Path == "/api/torrent-events":
			writeJSON(t, w, []map[string]any{{"kind": "completed", "hash": "abc", "name": "Example"}})
		case r.URL.Path == "/api/i18n/active":
			writeJSON(t, w, map[string]any{"ok": true, "lang": "en"})
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Version != "1.0.42" || status.TorrentStats.Count != 3 || status.Seen.Groups != 12 {
		t.Fatalf("unexpected status: %+v", status)
	}
	torrents, err := client.Torrents(ctx)
	if err != nil || len(torrents) != 1 {
		t.Fatalf("Torrents: %v %+v", err, torrents)
	}
	if torrents[0].Progress != 12.5 || torrents[0].SeedRatio == nil || *torrents[0].SeedRatio != 1.5 {
		t.Fatalf("unexpected torrent: %+v", torrents[0])
	}
	detail, err := client.TorrentDetail(ctx, "abc")
	if err != nil || !detail.NoRename || detail.Magnet == "" {
		t.Fatalf("TorrentDetail: %v %+v", err, detail)
	}
	trackers, err := client.TorrentCollection(ctx, "abc", "trackers")
	if err != nil || len(trackers) != 1 || trackers[0]["url"] != "udp://tracker" {
		t.Fatalf("TorrentCollection: %v %+v", err, trackers)
	}
	logs, err := client.Logs(ctx, 200)
	if err != nil || len(logs) != 2 {
		t.Fatalf("Logs: %v %+v", err, logs)
	}
	health, err := client.Health(ctx)
	if err != nil || health.ProcessID != 42 || len(health.Paths) != 1 {
		t.Fatalf("Health: %v %+v", err, health)
	}
	events, err := client.Events(ctx)
	if err != nil || len(events) != 1 || events[0].Kind != "completed" {
		t.Fatalf("Events: %v %+v", err, events)
	}
	lang, err := client.Language(ctx)
	if err != nil || lang != "en" {
		t.Fatalf("Language: %v %q", err, lang)
	}
	// Every read carried the token.
	for _, request := range *recorded {
		if request.token != "secret-token" {
			t.Fatalf("missing token on %s %s", request.method, request.path)
		}
	}
}

func TestClientReadsCatalogViews(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		switch r.URL.Path {
		case "/api/archive":
			writeJSON(t, w, map[string]any{"items": []map[string]any{{"title": "Archived", "magnet": "magnet:x"}}, "total": 1, "pages": 1})
		case "/api/gaps":
			writeJSON(t, w, map[string]any{"items": []map[string]any{{"series": "Example", "season": 1, "episode": 2}}})
		case "/api/blocklist":
			writeJSON(t, w, map[string]any{"items": []map[string]any{{"hash": "abc", "title": "Blocked"}}})
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	archive, total, pages, err := client.Archive(ctx, "Example")
	if err != nil || len(archive) != 1 || total != 1 || pages != 1 {
		t.Fatalf("Archive: %v %+v %d %d", err, archive, total, pages)
	}
	gaps, err := client.Gaps(ctx)
	if err != nil || len(gaps) != 1 || gaps[0].Episode != 2 {
		t.Fatalf("Gaps: %v %+v", err, gaps)
	}
	blocklist, err := client.Blocklist(ctx)
	if err != nil || len(blocklist) != 1 || blocklist[0].Hash != "abc" {
		t.Fatalf("Blocklist: %v %+v", err, blocklist)
	}
}

func TestClientWritesEndpoints(t *testing.T) {
	client, recorded := newTestClient(t, func(w http.ResponseWriter, r *http.Request, body string) {
		if r.Method == http.MethodPost {
			writeJSON(t, w, map[string]any{"ok": true, "removed": 2, "skipped": 1, "files": 3, "results": []any{}})
			return
		}
		writeJSON(t, w, map[string]any{"ok": true})
	})
	ctx := context.Background()

	if _, err := client.RunCycle(ctx, "movies"); err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if err := client.AddMagnet(ctx, "magnet:?xt=urn:btih:x"); err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	if _, err := client.Search(ctx, "query"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if err := client.AddRelease(ctx, map[string]any{"title": "x"}); err != nil {
		t.Fatalf("AddRelease: %v", err)
	}
	if err := client.Pause(ctx, "abc"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := client.Resume(ctx, "abc"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := client.Restart(ctx, "abc"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if err := client.Recheck(ctx, "abc"); err != nil {
		t.Fatalf("Recheck: %v", err)
	}
	if err := client.Reannounce(ctx, "abc"); err != nil {
		t.Fatalf("Reannounce: %v", err)
	}
	if err := client.Remove(ctx, "abc", true, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := client.Pin(ctx, "abc"); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if err := client.Unpin(ctx); err != nil {
		t.Fatalf("Unpin: %v", err)
	}
	if err := client.SetNoRename(ctx, "abc", true); err != nil {
		t.Fatalf("SetNoRename: %v", err)
	}
	if _, err := client.RemoveCompleted(ctx, false); err != nil {
		t.Fatalf("RemoveCompleted: %v", err)
	}
	if err := client.SetSpeedLimits(ctx, 100, 50); err != nil {
		t.Fatalf("SetSpeedLimits: %v", err)
	}
	if _, err := client.CleanTrash(ctx); err != nil {
		t.Fatalf("CleanTrash: %v", err)
	}

	want := map[string]string{
		"POST /api/run_now?domain=movies":     "",
		"POST /api/send-magnet":               `"magnet":"magnet:?xt=urn:btih:x"`,
		"POST /api/search":                    `"query":"query"`,
		"POST /api/search/add":                `"release"`,
		"POST /api/torrents/abc/pause":        "",
		"POST /api/torrents/abc/resume":       "",
		"POST /api/torrents/abc/restart":      "",
		"POST /api/torrents/abc/recheck":      "",
		"POST /api/torrents/abc/reannounce":   "",
		"POST /api/torrents/abc/remove":       `"delete_files":true`,
		"POST /api/torrents/pin":              `"hash":"abc"`,
		"POST /api/torrents/unpin":            "",
		"POST /api/torrents/abc/no_rename":    `"value":true`,
		"POST /api/torrents/remove_completed": `"delete_files":false`,
		"POST /api/set-speed-limits":          `"download_kib":100`,
		"POST /api/maintenance/clean-trash":   `"force":true`,
	}
	seen := map[string]bool{}
	for _, request := range *recorded {
		key := request.method + " " + request.path
		seen[key] = true
		if needle, ok := want[key]; ok && needle != "" && !strings.Contains(request.body, needle) {
			t.Errorf("%s body %q missing %q", key, request.body, needle)
		}
	}
	for key := range want {
		if !seen[key] {
			t.Errorf("endpoint not called: %s", key)
		}
	}
}

func TestClientUploadTorrentFile(t *testing.T) {
	client, recorded := newTestClient(t, func(w http.ResponseWriter, r *http.Request, body string) {
		if r.Header.Get("Content-Type") != "application/x-bittorrent" {
			t.Errorf("upload content-type = %q", r.Header.Get("Content-Type"))
		}
		if body != "d8:announce..." {
			t.Errorf("upload body = %q", body)
		}
		writeJSON(t, w, map[string]any{"ok": true})
	})
	path := filepath.Join(t.TempDir(), "file.torrent")
	if err := os.WriteFile(path, []byte("d8:announce..."), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := client.AddTorrentFile(context.Background(), path); err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}
	if len(*recorded) != 1 || (*recorded)[0].path != "/api/upload-torrent" {
		t.Fatalf("unexpected requests: %+v", *recorded)
	}
}

func TestClientAPIError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error":"boom"}`))
	})
	_, err := client.Status(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 400 || !strings.Contains(apiErr.Detail, "boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}
