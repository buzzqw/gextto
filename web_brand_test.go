package gextto

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBrandingHasNoLegacyName guards the branding of the official server-rendered
// shell. The legacy tokens are assembled at runtime so the guard itself does
// not embed the previous name anywhere in the source tree.
func TestBrandingHasNoLegacyName(t *testing.T) {
	legacyTokens := [][]byte{
		[]byte("Rex" + "tto"),
		[]byte("rex" + "tto"),
		[]byte("REX" + "TTO"),
	}

	state := newTestAppState(t)
	// A fresh state opens the setup wizard; the branding is checked on the dashboard.
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	V2Page(recorder, httptest.NewRequest(http.MethodGet, "/", nil), state)
	body := recorder.Body.Bytes()
	if !bytes.Contains(body, []byte("Gextto")) {
		t.Fatal("index does not mention Gextto")
	}
	for _, legacy := range legacyTokens {
		if bytes.Contains(body, legacy) {
			t.Fatal("index still contains the previous product name")
		}
	}

	css, err := fs.ReadFile(v2StaticFSRoot(), "gextto-ui.css")
	if err != nil {
		t.Fatal("embedded server UI stylesheet missing")
	}
	for _, legacy := range legacyTokens {
		if bytes.Contains(css, legacy) {
			t.Fatal("embedded server UI still contains the previous product name")
		}
	}
	if !bytes.Contains(css, []byte("Gextto")) {
		t.Fatal("embedded server UI has no Gextto branding")
	}
}
