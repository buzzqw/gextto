package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRenderUIUnknownTemplateReturns500 checks that a template render failure
// is not swallowed: the client gets 500 and the error is logged. Before, the
// handler returned 200 with an empty body.
func TestRenderUIUnknownTemplateReturns500(t *testing.T) {
	d := &Daemon{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	d.renderUI(rec, req, "does-not-exist", struct{}{})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "page render failed" {
		t.Fatalf("body = %q, want %q", body, "page render failed")
	}
}

// TestUIPageTranslation checks the whole page is translated end to end for a
// non-English language: the <html lang>, the visible text and the client
// dictionary the JavaScript reads.
func TestUIPageTranslation(t *testing.T) {
	d := newTestDaemon(t)
	server := httptest.NewServer(d.routes())
	defer server.Close()

	resp, err := http.Get(server.URL + "/?lang=de")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)

	for _, want := range []string{
		`lang="de"`,
		"Hinzufügen",           // Add
		"Freier Speicherplatz", // Free space
		"Port testen",          // Test ports
		"window.__uiI18n",      // client dictionary injected
		"Magnet kopiert",       // a client-dictionary value (German)
	} {
		if !strings.Contains(page, want) {
			t.Errorf("German page is missing %q", want)
		}
	}
	for _, bad := range []string{`lang="en"`, "Free space", ">Add<", "Test ports"} {
		if strings.Contains(page, bad) {
			t.Errorf("German page still shows English %q", bad)
		}
	}
}
