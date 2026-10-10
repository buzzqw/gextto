package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUIRowLimit(t *testing.T) {
	managed := &Daemon{opts: Options{Mode: ModeManaged}}
	if got := managed.uiRowLimit(httptest.NewRequest(http.MethodGet, "/?rows=10", nil)); got != 0 {
		t.Fatalf("managed must render all rows, got %d", got)
	}

	d := standaloneTestDaemon(t, "", true)
	if got := d.uiRowLimit(httptest.NewRequest(http.MethodGet, "/", nil)); got != defaultUIRowLimit {
		t.Fatalf("default = %d, want %d", got, defaultUIRowLimit)
	}
	if got := d.uiRowLimit(httptest.NewRequest(http.MethodGet, "/?rows=500", nil)); got != 500 {
		t.Fatalf("rows=500 -> %d", got)
	}
	if got := d.uiRowLimit(httptest.NewRequest(http.MethodGet, "/?rows=0", nil)); got != defaultUIRowLimit {
		t.Fatalf("rows=0 must fall back to the default, got %d", got)
	}
	if got := d.uiRowLimit(httptest.NewRequest(http.MethodGet, "/?rows=bad", nil)); got != defaultUIRowLimit {
		t.Fatalf("rows=bad must fall back to the default, got %d", got)
	}
}
