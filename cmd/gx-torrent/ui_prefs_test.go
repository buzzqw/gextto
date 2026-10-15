package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUIPrefsRoundTrip covers the server copy of the display preferences: the
// page saves columns/filters/theme and another browser reads them back;
// invalid bodies are refused and nothing is stored.
func TestUIPrefsRoundTrip(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	get := func() map[string]any {
		t.Helper()
		resp, err := http.Get(server.URL + "/ui/uiprefs")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /ui/uiprefs -> %d", resp.StatusCode)
		}
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if prefs := get(); len(prefs) != 0 {
		t.Fatalf("prefs = %v, want empty", prefs)
	}

	post := func(body string) int {
		t.Helper()
		resp, err := http.Post(server.URL+"/ui/uiprefs", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(`{"cols":{"size":{"on":false}},"filter":{"status":"seeding"},"theme":"dark"}`); code != http.StatusOK {
		t.Fatalf("POST /ui/uiprefs -> %d", code)
	}
	prefs := get()
	if _, ok := prefs["cols"]; !ok {
		t.Errorf("cols not stored: %v", prefs)
	}
	if filter, ok := prefs["filter"].(map[string]any); !ok || filter["status"] != "seeding" {
		t.Errorf("filter = %v", prefs["filter"])
	}
	if prefs["theme"] != "dark" {
		t.Errorf("theme = %v", prefs["theme"])
	}
	for _, bad := range []string{`not json`, `{"theme":"neon"}`, `{"cols":[]}`} {
		if code := post(bad); code != http.StatusBadRequest {
			t.Errorf("POST %q -> %d, want 400", bad, code)
		}
	}
}

// TestUIPrefsManagedOffline checks managed mode (no settings store): reads are
// empty and writes are refused, so the page silently stays on localStorage.
func TestUIPrefsManagedOffline(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/ui/uiprefs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/uiprefs -> %d", resp.StatusCode)
	}
	resp2, err := http.Post(server.URL+"/ui/uiprefs", "application/json", strings.NewReader(`{"theme":"dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("POST /ui/uiprefs -> %d, want 404", resp2.StatusCode)
	}
}
