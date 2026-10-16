package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCrossSiteWriteIsRefused pins the cross-origin guard: a state-changing
// request a browser marks as cross-site must never reach the API (it would
// otherwise be able to reconfigure Gextto, hooks included, from the LAN where
// the login is bypassed by default).
func TestCrossSiteWriteIsRefused(t *testing.T) {
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	})
	handler := crossOriginGuard(next)

	req := httptest.NewRequest(http.MethodPost, "http://gextto.lan:5000/api/config/settings", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if reached {
		t.Fatal("the cross-site write reached the API")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body["ok"] != false {
		t.Fatalf("ok = %v, want false", body["ok"])
	}
}

// TestSameOriginWritePasses keeps the guard from breaking the interface itself:
// the browser's own same-origin writes (HTMX and fetch) must pass.
func TestSameOriginWritePasses(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := crossOriginGuard(next)

	req := httptest.NewRequest(http.MethodPost, "http://gextto.lan:5000/settings/save", nil)
	req.Header.Set("Origin", "http://gextto.lan:5000")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

// TestNonBrowserWritePasses keeps the guard transparent for non-browser clients
// (curl, the TUI, scripts): they send neither Origin nor Sec-Fetch-Site.
func TestNonBrowserWritePasses(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := crossOriginGuard(next)

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:5000/api/config/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

// TestSafeMethodIsNeverBlocked guards reads: GET is a safe method, so even a
// cross-site navigation (an <img>, a link, a calendar app) keeps working.
func TestSafeMethodIsNeverBlocked(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := crossOriginGuard(next)

	req := httptest.NewRequest(http.MethodGet, "http://gextto.lan:5000/api/health", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
