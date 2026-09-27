package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
