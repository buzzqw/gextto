package gextto

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBrandingHasNoLegacyName guards the rebranding: neither the served shell
// nor the embedded interface bundle may carry the previous product name. The
// legacy tokens are assembled at runtime so the guard itself does not embed the
// previous name anywhere in the source tree.
func TestBrandingHasNoLegacyName(t *testing.T) {
	legacyTokens := [][]byte{
		[]byte("Rex" + "tto"),
		[]byte("rex" + "tto"),
		[]byte("REX" + "TTO"),
	}

	recorder := httptest.NewRecorder()
	Index(recorder, httptest.NewRequest(http.MethodGet, "/", nil), &AppState{})
	body := recorder.Body.Bytes()
	if !bytes.Contains(body, []byte("Gextto")) {
		t.Fatal("index does not mention Gextto")
	}
	for _, legacy := range legacyTokens {
		if bytes.Contains(body, legacy) {
			t.Fatal("index still contains the previous product name")
		}
	}

	wasm, ok := uiAsset("pkg/ui.wasm")
	if !ok {
		t.Fatal("embedded wasm missing")
	}
	for _, legacy := range legacyTokens {
		if bytes.Contains(wasm, legacy) {
			t.Fatal("embedded UI still contains the previous product name")
		}
	}
	if !bytes.Contains(wasm, []byte("Gextto")) {
		t.Fatal("embedded UI has no Gextto branding")
	}
}
