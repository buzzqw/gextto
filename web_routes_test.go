package gextto

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAPIDocumentationMatchesRouter prevents the integration route table from
// silently drifting when a handler is added or removed. Server-rendered UI
// routes are registered with v2Handle and are not part of RegisteredRoutes(), so
// they never appear in docs/API.md.
func TestAPIDocumentationMatchesRouter(t *testing.T) {
	if _, err := os.Stat("docs/API.md"); err != nil {
		t.Skip("API documentation is unavailable outside the source tree")
	}
	Router(newTestAppState(t))
	want := map[string]struct{}{}
	contents, err := os.ReadFile("docs/API.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		parts := strings.Split(line, "`")
		if len(parts) != 3 || !strings.HasPrefix(parts[0], "| ") || !strings.HasSuffix(parts[2], " |") {
			continue
		}
		method := strings.TrimSpace(strings.TrimPrefix(parts[0], "|"))
		method = strings.TrimSpace(strings.TrimSuffix(method, "|"))
		if method == "GET" || method == "POST" || method == "DELETE" {
			want[method+" "+parts[1]] = struct{}{}
		}
	}
	got := map[string]struct{}{}
	for _, route := range RegisteredRoutes() {
		if route == "GET /{$}" {
			got["GET /"] = struct{}{}
			continue
		}
		got[route] = struct{}{}
	}
	for route := range want {
		if _, ok := got[route]; !ok {
			t.Errorf("documented route is not registered: %s", route)
		}
	}
	for route := range got {
		if _, ok := want[route]; !ok {
			t.Errorf("registered route is not documented: %s", route)
		}
	}
}

// TestEveryGetRouteRespondsWithoutServerError installs the real router and calls
// every registered GET endpoint with a placeholder parameter. The daemon must
// answer with a client error (or a valid response) but never a 500: a panic in
// any handler or an unexpected nil dereference would surface here.
func TestEveryGetRouteRespondsWithoutServerError(t *testing.T) {
	state := newTestAppState(t)
	mux := Router(state)
	routes := RegisteredRoutes()
	if len(routes) < 200 {
		t.Fatalf("only %d routes registered", len(routes))
	}

	// Streaming endpoints never return; they are exercised separately.
	skip := map[string]bool{
		"/api/logs/stream":          true,
		"/api/notifications/stream": true,
	}

	checked := 0
	for _, pattern := range routes {
		if !strings.HasPrefix(pattern, "GET ") {
			continue
		}
		path := strings.TrimPrefix(pattern, "GET ")
		if skip[path] {
			continue
		}
		path = strings.ReplaceAll(path, "{name}", "Show")
		path = strings.ReplaceAll(path, "{hash}", strings.Repeat("a", 40))
		path = strings.ReplaceAll(path, "{lang}", "it")
		path = strings.ReplaceAll(path, "{id}", "1")
		path = strings.ReplaceAll(path, "{series}", "Show")
		path = strings.ReplaceAll(path, "{season}", "1")
		path = strings.ReplaceAll(path, "{episode}", "1")
		path = strings.ReplaceAll(path, "{key}", "test")
		path = strings.ReplaceAll(path, "{$}", "")

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)

		done := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panicked: %v", pattern, r)
				}
				close(done)
			}()
			mux.ServeHTTP(recorder, request)
		}()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatalf("%s timed out", pattern)
		}
		if recorder.Code >= 500 {
			t.Errorf("%s -> status %d (%s)", pattern, recorder.Code, strings.TrimSpace(recorder.Body.String()[:min(160, recorder.Body.Len())]))
		}
		checked++
	}
	if checked < 80 {
		t.Fatalf("only %d GET routes exercised", checked)
	}
	t.Logf("exercised %d GET routes", checked)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
