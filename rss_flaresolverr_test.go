package gextto

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
				"sessions": []string{"gextto-stale-id", "gextto-fresh-id", "gextto-orphan-id", "another-client-id"},
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
		server.URL + "\x00stale.example": {id: "gextto-stale-id", endpoint: server.URL, domain: "stale.example", lastUsed: time.Now().Add(-time.Hour)},
		server.URL + "\x00fresh.example": {id: "gextto-fresh-id", endpoint: server.URL, domain: "fresh.example", lastUsed: time.Now()},
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
	if destroyed["gextto-stale-id"] != 1 {
		t.Fatalf("stale-id destroyed %d times, want 1", destroyed["gextto-stale-id"])
	}
	if destroyed["gextto-fresh-id"] != 0 {
		t.Fatalf("fresh-id must be kept, destroyed %d times", destroyed["gextto-fresh-id"])
	}
	if destroyed["gextto-orphan-id"] != 1 {
		t.Fatalf("orphan-id destroyed %d times, want 1", destroyed["gextto-orphan-id"])
	}
	if destroyed["another-client-id"] != 0 {
		t.Fatalf("another client's session must not be destroyed")
	}

	fsSessionMu.Lock()
	defer fsSessionMu.Unlock()
	if _, ok := fsSessionByDomain[server.URL+"\x00fresh.example"]; !ok {
		t.Fatalf("fresh session must stay tracked")
	}
	if _, ok := fsSessionByDomain[server.URL+"\x00stale.example"]; ok {
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
	value = "http://127.0.0.1:8191/v1/"
	cfg.FlaresolverrURL = &value
	if got := flareSolverrEndpointFromConfig(&cfg); got != "http://127.0.0.1:8191/v1" {
		t.Fatalf("explicit API endpoint = %q, want one /v1", got)
	}
}

func TestConcurrentFlareSolverrRequestsCreateOneSessionPerTarget(t *testing.T) {
	resetCloudflareMemoryForTest(t)
	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Cmd     string `json:"cmd"`
			Session string `json:"session"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Cmd != "sessions.create" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		creates.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "session": payload.Session})
	}))
	defer server.Close()

	endpoint := flareSolverrEndpoint(server.URL)
	rawURL := "https://protected.example/listing"
	var ids [2]string
	var wait sync.WaitGroup
	for index := range ids {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			ids[index] = acquireFlareSolverrSession(context.Background(), server.Client(), endpoint, rawURL)
		}(index)
	}
	wait.Wait()

	if got := creates.Load(); got != 1 {
		t.Fatalf("sessions.create calls = %d, want 1", got)
	}
	if ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("concurrent calls returned different sessions: %#v", ids)
	}
	if !strings.HasPrefix(ids[0], fsSessionPrefix) {
		t.Fatalf("session id %q lacks Gextto ownership prefix", ids[0])
	}
}
