package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestProvidersStatusDeduplicatesProbedProvider locks in that a saved backoff
// row is not shown next to the live probe row for the same provider: a stale
// Prowlarr failure plus its live service row used to list "Prowlarr" twice.
func TestProvidersStatusDeduplicatesProbedProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"appName":"Prowlarr"}`))
	}))
	defer server.Close()

	state := newTestAppState(t)
	if err := state.db.ProviderFailure("indexer", "Prowlarr", "context deadline exceeded"); err != nil {
		t.Fatalf("seed provider failure: %v", err)
	}
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
			Provider string `json:"provider"`
			Kind     string `json:"kind"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	prowlarr := 0
	for _, item := range payload.Items {
		if strings.EqualFold(item.Provider, "Prowlarr") {
			prowlarr++
			if item.Kind != "servizio" {
				t.Fatalf("stale backoff row shown instead of the live one: %#v", item)
			}
		}
	}
	if prowlarr != 1 {
		t.Fatalf("Prowlarr rows = %d, want 1: %#v", prowlarr, payload.Items)
	}
}
