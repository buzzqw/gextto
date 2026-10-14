package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// TestUIAltSpeedToggle covers the status-bar alternative-limits button.
func TestUIAltSpeedToggle(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	post := func() int {
		resp, err := http.PostForm(srv.URL+"/ui/alt-speed", url.Values{})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if d.altSpeedActive(time.Now()) {
		t.Fatal("alternative limits should be off by default")
	}
	if code := post(); code < 200 || code >= 400 {
		t.Fatalf("alt-speed toggle: %d", code)
	}
	if !d.altSpeedActive(time.Now()) {
		t.Fatal("alternative limits were not enabled")
	}
	if code := post(); code < 200 || code >= 400 {
		t.Fatalf("alt-speed toggle: %d", code)
	}
	if d.altSpeedActive(time.Now()) {
		t.Fatal("alternative limits were not disabled")
	}
}
