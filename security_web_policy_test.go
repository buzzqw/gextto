package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebPolicyCrossOriginGuardAddsFrameHeaders(t *testing.T) {
	next := http.NewServeMux()
	next.HandleFunc("GET /page", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<main>page</main>"))
	})
	next.HandleFunc("GET /partial/rows", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<tr><td>row</td></tr>"))
	})
	next.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	handler := crossOriginGuard(next)

	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "HTML page", method: http.MethodGet, path: "/page", status: http.StatusOK},
		{name: "HTML fragment", method: http.MethodGet, path: "/partial/rows", status: http.StatusOK},
		{name: "API response", method: http.MethodGet, path: "/api/health", status: http.StatusOK},
		{name: "not found", method: http.MethodGet, path: "/missing", status: http.StatusNotFound},
		{name: "cross-origin refusal", method: http.MethodPost, path: "/api/config/settings", status: http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://gextto.lan:5000"+tc.path, nil)
			if tc.status == http.StatusForbidden {
				req.Header.Set("Origin", "https://evil.example")
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			assertWebPolicyFrameHeaders(t, rec)
		})
	}
}

func TestWebPolicyRunNowIsPostOnly(t *testing.T) {
	mux := Router(&AppState{})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/run_now", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/run_now status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodPost) {
		t.Fatalf("Allow = %q, want POST", allow)
	}

	foundPOST := false
	for _, pattern := range RegisteredRoutes() {
		if pattern == "GET /api/run_now" {
			t.Fatal("GET /api/run_now is still registered")
		}
		if pattern == "POST /api/run_now" {
			foundPOST = true
		}
	}
	if !foundPOST {
		t.Fatal("POST /api/run_now is not registered")
	}
}

func assertWebPolicyFrameHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
		t.Fatalf("Content-Security-Policy = %q, want frame-ancestors 'none'", got)
	}
}
