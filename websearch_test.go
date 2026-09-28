package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEngineCooldownBlocksAfterAFailure(t *testing.T) {
	if engine_in_cooldown("test-cooldown-engine") {
		t.Fatal("engine should not start in cooldown")
	}
	set_engine_cooldown("test-cooldown-engine", "HTTP 429 Too Many Requests")
	if !engine_in_cooldown("test-cooldown-engine") {
		t.Fatal("engine should be in cooldown after a 429")
	}
	// Un motore diverso non è toccato.
	if engine_in_cooldown("test-cooldown-other") {
		t.Fatal("unrelated engine must not be in cooldown")
	}
}

func TestBuildsSanitizedMagnetWithTrackers(t *testing.T) {
	magnet := build_magnet("0123456789012345678901234567890123456789", "Example Show")
	if magnet == "" {
		t.Fatal("build_magnet returned no magnet")
	}
	if !strings.Contains(magnet, "xt=urn:btih:0123456789012345678901234567890123456789") {
		t.Fatalf("magnet = %q", magnet)
	}
	if !strings.Contains(magnet, "&dn=Example%20Show") && !strings.Contains(magnet, "&dn=Example+Show") {
		t.Fatalf("magnet missing display name: %q", magnet)
	}
	if !strings.Contains(magnet, "&tr=") {
		t.Fatalf("magnet missing trackers: %q", magnet)
	}
}

func TestExtractsAndSanitizesHTMLMagnet(t *testing.T) {
	body := `<a href="magnet:?xt=urn:btih:0123456789012345678901234567890123456789&dn=Example%20Show&x=bad space">download</a>`
	magnet := first_magnet(body)
	if !strings.HasPrefix(magnet, "magnet:?xt=urn:btih:") {
		t.Fatalf("magnet = %q", magnet)
	}
	if strings.Contains(magnet, "bad space") {
		t.Fatalf("magnet should be sanitized: %q", magnet)
	}
}

func resetCloudflareMemoryForTest(t *testing.T) {
	t.Helper()
	cfSessionsMu.Lock()
	cfSessions = map[string]cfSession{}
	cfSessionsMu.Unlock()
	ConfigureCloudflareState(t.TempDir())
}

func TestWebHTMLReusesFlareSolverrSession(t *testing.T) {
	resetCloudflareMemoryForTest(t)
	var targetCalls atomic.Int32
	var flareCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		if r.Header.Get("Cookie") == "cf_clearance=ok" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>solved target page</body></html>"))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><title>Just a moment...</title></html>"))
	}))
	defer target.Close()
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flareCalls.Add(1)
		if r.URL.Path != "/v1" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"solution": map[string]any{
				"userAgent": "TestBrowser/1.0",
				"cookies":   []map[string]string{{"name": "cf_clearance", "value": "ok"}},
				"response":  "<html><body>solved by browser</body></html>",
			},
		})
	}))
	defer flare.Close()

	body, err := fetch_html(context.Background(), target.URL, &flare.URL)
	if err != nil || !strings.Contains(body, "solved by browser") {
		t.Fatalf("first fetch = %q, %v", body, err)
	}
	body, err = fetch_html(context.Background(), target.URL, &flare.URL)
	if err != nil || !strings.Contains(body, "solved target page") {
		t.Fatalf("second fetch = %q, %v", body, err)
	}
	if got := flareCalls.Load(); got != 1 {
		t.Fatalf("FlareSolverr calls = %d, want 1", got)
	}
	if got := targetCalls.Load(); got != 2 {
		t.Fatalf("target calls = %d, want 2", got)
	}
}

func TestWebHTMLDoesNotUseFlareSolverrForNonCloudflareStatuses(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			resetCloudflareMemoryForTest(t)
			var flareCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer target.Close()
			flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				flareCalls.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer flare.Close()

			_, err := fetch_html(context.Background(), target.URL, &flare.URL)
			if err == nil || !strings.Contains(err.Error(), http.StatusText(status)) && !strings.Contains(err.Error(), "HTTP ") {
				t.Fatalf("fetch error = %v", err)
			}
			if got := flareCalls.Load(); got != 0 {
				t.Fatalf("FlareSolverr calls for HTTP %d = %d", status, got)
			}
		})
	}
}
