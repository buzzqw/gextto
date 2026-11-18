// Package emuleqttest is a fake emule-qt core for tests: the REST routes
// Gextto uses, an API key, paged lists and searches that finish at once.
package emuleqttest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"

	"github.com/buzzqw/gextto/internal/emuleqt"
)

// Core is the fake emulecored.
type Core struct {
	Key string

	mu        sync.Mutex
	server    *httptest.Server
	downloads []emuleqt.Download
	shared    []emuleqt.SharedFile
	results   []emuleqt.SearchResult
	dirs      emuleqt.Directories
	searches  []string
	calls     []string
	added     []string
	snapshot  emuleqt.Snapshot
	limits    [2]int64
	prefs     map[string]any
	patched   []map[string]any
}

// Start runs a core that wants key.
func Start(key string) *Core {
	core := &Core{Key: key, prefs: map[string]any{}}
	core.snapshot.Version = "0.7.37"
	core.snapshot.Ed2k.Connected = true
	core.snapshot.Kad.Running, core.snapshot.Kad.Connected = true, true
	core.server = httptest.NewServer(http.HandlerFunc(core.serve))
	return core
}

// URL is the core's base address.
func (c *Core) URL() string { return c.server.URL }

// Close stops the core.
func (c *Core) Close() { c.server.Close() }

// SetDownloads, SetShared, SetResults and SetDirectories fill the core.
func (c *Core) SetDownloads(downloads []emuleqt.Download) {
	c.mu.Lock()
	c.downloads = downloads
	c.mu.Unlock()
}

func (c *Core) SetShared(files []emuleqt.SharedFile) {
	c.mu.Lock()
	c.shared = files
	c.mu.Unlock()
}

func (c *Core) SetResults(results []emuleqt.SearchResult) {
	c.mu.Lock()
	c.results = results
	c.mu.Unlock()
}

func (c *Core) SetDirectories(dirs emuleqt.Directories) {
	c.mu.Lock()
	c.dirs = dirs
	c.mu.Unlock()
}

// Searches are the expressions searched; Added the links queued; Calls
// every "METHOD /path".
func (c *Core) Searches() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.searches...)
}
func (c *Core) Added() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.added...)
}
func (c *Core) Calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

// Limits are the last speed limits set, KiB/s.
func (c *Core) Limits() (int64, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limits[0], c.limits[1]
}

// SetPreferences makes the core report these schema keys and values: the
// settings a given emule-qt version knows. A key that is not here is refused
// by PATCH, as the real core does.
func (c *Core) SetPreferences(values map[string]any) {
	c.mu.Lock()
	c.prefs = values
	c.mu.Unlock()
}

// Patched are the bodies PATCHed to /preferences.
func (c *Core) Patched() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]any(nil), c.patched...)
}

func page[T any](w http.ResponseWriter, r *http.Request, items []T) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	offset = min(max(offset, 0), len(items))
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items[offset:], "total": len(items), "offset": offset, "limit": 1000})
}

func (c *Core) serve(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, r.Method+" "+r.URL.Path)
	if r.URL.Path != "/api/v1/health" && r.Header.Get("X-Api-Key") != c.Key {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":401,"message":"Missing or wrong API key"}}`)
		return
	}
	var body map[string]any
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	path := r.URL.Path
	switch {
	case path == "/api/v1/health":
		reply(map[string]any{"status": "ok"})
	case path == "/api/v1/snapshot":
		reply(c.snapshot)
	case path == "/api/v1/connection":
		reply(map[string]any{"isConnected": c.snapshot.Ed2k.Connected, "isLowID": false, "clientID": 123456789,
			"currentServer": map[string]any{"name": "Server A", "address": "1.2.3.4", "port": 4661}})
	case path == "/api/v1/stats":
		reply(map[string]any{"sessionReceivedBytes": 1000, "sessionSentBytes": 500})
	case path == "/api/v1/preferences" && r.Method == http.MethodGet:
		values := map[string]any{"incomingDir": c.dirs.Incoming, "maxDownload": c.limits[0], "maxUpload": c.limits[1]}
		for key, value := range c.prefs {
			values[key] = value
		}
		reply(values)
	case path == "/api/v1/preferences" && r.Method == http.MethodPatch:
		known := map[string]bool{"incomingDir": true, "maxDownload": true, "maxUpload": true}
		for key := range c.prefs {
			known[key] = true
		}
		for key := range body {
			if !known[key] {
				w.WriteHeader(http.StatusBadRequest)
				reply(map[string]any{"error": map[string]any{"code": 400, "message": "Unknown preference: " + key}})
				return
			}
		}
		c.patched = append(c.patched, body)
		if down, ok := body["maxDownload"].(float64); ok {
			c.limits[0] = int64(down)
		}
		if up, ok := body["maxUpload"].(float64); ok {
			c.limits[1] = int64(up)
		}
		for key, value := range body {
			c.prefs[key] = value
		}
		reply(body)
	case path == "/api/v1/shared/directories":
		reply(c.dirs)
	case path == "/api/v1/shared":
		page(w, r, c.shared)
	case path == "/api/v1/downloads" && r.Method == http.MethodGet:
		page(w, r, c.downloads)
	case path == "/api/v1/downloads" && r.Method == http.MethodPost:
		link, _ := body["link"].(string)
		c.added = append(c.added, link)
		reply(map[string]any{"added": 1})
	case strings.HasPrefix(path, "/api/v1/downloads/") && strings.HasSuffix(path, "/cancel"):
		if body["confirm"] != true {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		hash := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/downloads/"), "/cancel")
		kept := c.downloads[:0]
		for _, download := range c.downloads {
			if !strings.EqualFold(download.Hash, hash) {
				kept = append(kept, download)
			}
		}
		c.downloads = kept
		reply(map[string]any{})
	case path == "/api/v1/search" && r.Method == http.MethodPost:
		expression, _ := body["expression"].(string)
		c.searches = append(c.searches, expression)
		reply(map[string]any{"searchID": len(c.searches)})
	case strings.HasPrefix(path, "/api/v1/search/") && strings.HasSuffix(path, "/results"):
		reply(map[string]any{"state": "finished", "items": c.results})
	case strings.HasPrefix(path, "/api/v1/search/") && r.Method == http.MethodDelete:
		reply(map[string]any{})
	case path == "/api/v1/uploads", path == "/api/v1/servers":
		page(w, r, []any{})
	case path == "/api/v1/logs":
		reply(map[string]any{"items": []map[string]any{{"id": 1, "timestamp": "2026-11-17 10:00:00", "severity": "info", "message": "Kad connected"}}, "lastId": 1})
	default:
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			reply(map[string]any{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":404,"message":"no route"}}`)
	}
}
