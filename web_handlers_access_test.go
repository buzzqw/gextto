package gextto

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGenerateAccessKeyStoresAndReturns pins the access-key generator: it saves
// a fresh random secret and returns it once so the UI can show it for copying.
func TestGenerateAccessKeyStoresAndReturns(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	post := func(key string) (int, map[string]any) {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{"key": key})
		response, err := http.Post(server.URL+"/api/access/generate-key", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("post %s: %v", key, err)
		}
		defer response.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(response.Body).Decode(&body)
		return response.StatusCode, body
	}

	code, body := post("auth_api_key")
	if code != http.StatusOK {
		t.Fatalf("auth_api_key status = %d (%v)", code, body)
	}
	value, _ := body["value"].(string)
	if len(value) < 40 {
		t.Fatalf("generated API key is too short: %q", value)
	}
	if stored := latestConfig(state).Settings["auth_api_key"]; stored != value {
		t.Fatalf("stored key = %q, want %q", stored, value)
	}

	// A second call rotates the value: the old secret stops working.
	if _, again := post("auth_api_key"); again["value"] == value {
		t.Fatalf("generation did not rotate the key")
	}

	// The gx-torrent token is a daemon launch argument: it asks for a restart.
	code, body = post("gxtorrent_token")
	if code != http.StatusOK || body["restart_required"] != true {
		t.Fatalf("gxtorrent_token: status %d, body %v", code, body)
	}
	if _, ok := body["value"].(string); !ok {
		t.Fatalf("gxtorrent_token: missing value: %v", body)
	}

	// Anything that is not an access secret is refused.
	if code, _ := post("torrent_backend"); code != http.StatusBadRequest {
		t.Fatalf("non-secret setting status = %d, want 400", code)
	}
}

// TestAccessTabOffersKeyGenerationAndDocs checks the Access tab exposes the
// generate/copy controls and the link to the key documentation.
func TestAccessTabOffersKeyGenerationAndDocs(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/settings/body?tab=access", nil)
	if code != http.StatusOK {
		t.Fatalf("access tab -> %d", code)
	}
	for _, marker := range []string{
		`data-v2-generate="auth_api_key"`,
		`data-v2-generate="gxtorrent_token"`,
		`data-v2-copy-field="auth_api_key"`,
		`data-v2-copy-field="gxtorrent_token"`,
		`id="gen-status-auth_api_key"`,
		"docs/api-keys",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("access tab lacks %q", marker)
		}
	}
}
