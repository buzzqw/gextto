package main

import (
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
