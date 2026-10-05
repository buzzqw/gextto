package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProvidersStatusIncludesHealthyProwlarr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/status" {
			t.Errorf("probe path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("apikey") != "test-key" {
			t.Errorf("probe API key = %q", r.URL.Query().Get("apikey"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"appName":"Prowlarr"}`))
	}))
	defer server.Close()

	state := newTestAppState(t)
	cfg := *state.cfg
	cfg.Indexers = []IndexerConfig{{
		Name:    "Prowlarr",
		URL:     server.URL,
		APIKey:  "test-key",
		Enabled: true,
		Manager: ManagerProwlarr,
	}}
	state.config_cache = &ConfigCache{
		cfg:        &cfg,
		generation: ConfigGeneration(),
		valid:      true,
	}

	response := httptest.NewRecorder()
	ProvidersStatusView(response, httptest.NewRequest(http.MethodGet, "/api/providers/status", nil), state)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []struct {
			Provider        string `json:"provider"`
			Kind            string `json:"kind"`
			UserMessage     string `json:"user_message"`
			SuggestedAction string `json:"suggested_action"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("provider rows = %#v", payload.Items)
	}
	row := payload.Items[0]
	if row.Provider != "Prowlarr" || row.Kind != "servizio" {
		t.Fatalf("unexpected row: %#v", row)
	}
	if row.UserMessage != "Nessun problema rilevato." || row.SuggestedAction != "Nessuna azione necessaria." {
		t.Fatalf("unexpected healthy status: %#v", row)
	}
}
