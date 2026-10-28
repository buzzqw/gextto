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

// TestProvidersStatusShowsOnlyConfiguredAndEnabledSources locks in that a saved
// backoff row does not outlive its integration: removing an indexer or switching
// it off used to leave it in *Health → Provider status* forever.
func TestProvidersStatusShowsOnlyConfiguredAndEnabledSources(t *testing.T) {
	activeFeed := "https://example.test/rss"
	staleFeed := "https://removed.test/rss"
	state := newTestAppState(t)
	for _, seed := range []struct{ kind, provider string }{
		{"indexer", "mircrew indexerr"},
		{"indexer", "prowlarr"},
		{"feed", feedSourceName(activeFeed)},
		{"feed", feedSourceName(staleFeed)},
	} {
		if err := state.db.ProviderFailure(seed.kind, seed.provider, "connection refused"); err != nil {
			t.Fatalf("seed %s failure: %v", seed.provider, err)
		}
	}
	cfg := *state.cfg
	cfg.FeedURLs = []string{activeFeed}
	cfg.Indexers = []IndexerConfig{
		{Name: "indexarr", URL: "http://127.0.0.1:8788", Enabled: true},
		{Name: "prowlarr", URL: "http://127.0.0.1:9696", Enabled: false, Manager: ManagerProwlarr},
	}
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
	shown := map[string]bool{}
	for _, item := range payload.Items {
		shown[strings.ToLower(item.Provider)] = true
	}
	for _, want := range []string{"indexarr", strings.ToLower(feedSourceName(activeFeed))} {
		if !shown[want] {
			t.Fatalf("active source %q is missing: %#v", want, payload.Items)
		}
	}
	for _, gone := range []string{"mircrew indexerr", "prowlarr", strings.ToLower(feedSourceName(staleFeed))} {
		if shown[gone] {
			t.Fatalf("removed or disabled source %q is still listed: %#v", gone, payload.Items)
		}
	}

	// The stale rows are not only hidden: they are forgotten, so re-adding the
	// same source later starts from a clean backoff.
	saved, err := state.db.ProviderStatuses()
	if err != nil {
		t.Fatalf("read saved rows: %v", err)
	}
	if len(saved) != 1 || saved[0].Provider != feedSourceName(activeFeed) {
		t.Fatalf("saved provider rows = %#v, want only the active feed", saved)
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
