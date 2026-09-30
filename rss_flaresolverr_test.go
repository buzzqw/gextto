package gextto

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestSweepAndReconcileFlareSolverrSessions pins the leak fix: a tracked session
// whose reuse window expired is destroyed, a fresh one is kept, and a session
// left over from a previous Gextto run (untracked) is cleaned up too.
func TestSweepAndReconcileFlareSolverrSessions(t *testing.T) {
	var mu sync.Mutex
	destroyed := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		cmd, _ := payload["cmd"].(string)
		mu.Lock()
		defer mu.Unlock()
		switch cmd {
		case "sessions.list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":   "ok",
				"sessions": []string{"stale-id", "fresh-id", "orphan-id"},
			})
		case "sessions.destroy":
			id, _ := payload["session"].(string)
			destroyed[id]++
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		}
	}))
	defer server.Close()

	fsSessionMu.Lock()
	saved := fsSessionByDomain
	fsSessionByDomain = map[string]fsSessionEntry{
		"stale.example": {id: "stale-id", lastUsed: time.Now().Add(-time.Hour)},
		"fresh.example": {id: "fresh-id", lastUsed: time.Now()},
	}
	fsSessionMu.Unlock()
	t.Cleanup(func() {
		fsSessionMu.Lock()
		fsSessionByDomain = saved
		fsSessionMu.Unlock()
	})

	ctx := context.Background()
	if got := reconcileFlareSolverrSessions(ctx, server.Client(), server.URL); got != 1 {
		t.Fatalf("reconcile destroyed %d sessions, want 1 (the orphan)", got)
	}
	if got := sweepStaleFlareSolverrSessions(ctx, server.Client(), server.URL); got != 1 {
		t.Fatalf("sweep destroyed %d sessions, want 1 (the stale one)", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if destroyed["stale-id"] != 1 {
		t.Fatalf("stale-id destroyed %d times, want 1", destroyed["stale-id"])
	}
	if destroyed["fresh-id"] != 0 {
		t.Fatalf("fresh-id must be kept, destroyed %d times", destroyed["fresh-id"])
	}
	if destroyed["orphan-id"] != 1 {
		t.Fatalf("orphan-id destroyed %d times, want 1", destroyed["orphan-id"])
	}

	fsSessionMu.Lock()
	defer fsSessionMu.Unlock()
	if _, ok := fsSessionByDomain["fresh.example"]; !ok {
		t.Fatalf("fresh session must stay tracked")
	}
	if _, ok := fsSessionByDomain["stale.example"]; ok {
		t.Fatalf("stale session must be forgotten")
	}
}

// TestFlareSolverrEndpointFromConfig covers the endpoint helper used by the
// sweeper worker.
func TestFlareSolverrEndpointFromConfig(t *testing.T) {
	if got := flareSolverrEndpointFromConfig(nil); got != "" {
		t.Fatalf("nil config endpoint = %q, want empty", got)
	}
	cfg := DefaultConfig()
	if got := flareSolverrEndpointFromConfig(&cfg); got != "" {
		t.Fatalf("unset endpoint = %q, want empty", got)
	}
	value := "http://127.0.0.1:8191/"
	cfg.FlaresolverrURL = &value
	if got := flareSolverrEndpointFromConfig(&cfg); got != "http://127.0.0.1:8191/v1" {
		t.Fatalf("endpoint = %q, want trimmed /v1", got)
	}
}
