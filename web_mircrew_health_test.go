package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGh5MirCrewServiceStatus verifies the synthetic "Stato provider" row for
// the mircrew-indexer service: it probes the configured Torznab indexer and
// reports reachability without touching the provider backoff table.
func TestGh5MirCrewServiceStatus(t *testing.T) {
	gh5_mirCrewProbe.Lock()
	gh5_mirCrewProbe.entry = nil
	gh5_mirCrewProbe.Unlock()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "caps" {
			http.Error(w, "expected caps", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server title="mircrew-indexer"/></caps>`))
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Indexers = []IndexerConfig{{Name: "mircrew-indexer", URL: server.URL + "/api", APIKey: "k", Enabled: true}}

	status := gh5_mirCrewServiceStatus(&cfg)
	if status == nil {
		t.Fatal("expected a synthetic provider row for the configured mircrew indexer")
	}
	if status.Provider != "mircrew-indexer" || status.Kind != "servizio" {
		t.Fatalf("unexpected row: %#v", status)
	}
	if !strings.Contains(status.UserMessage, "attivo") {
		t.Fatalf("expected reachable message, got %q", status.UserMessage)
	}

	// A config without the indexer yields no row.
	plain := DefaultConfig()
	if gh5_mirCrewServiceStatus(&plain) != nil {
		t.Fatal("expected no row when the indexer is not configured")
	}
}
