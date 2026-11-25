package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestV2SourceTrustTabShowsFacts(t *testing.T) {
	state := newTestAppState(t)
	if err := state.db.RecordSourceTrustCycle([]models.CycleSourceStat{{Kind: "indexer", Name: "jackett:Tracker One", OK: 2, Fail: 1, Results: 4}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, page := v2Request(t, server, http.MethodGet, "/?view=health&tab=trust&days=7", nil)
	if code != http.StatusOK || !strings.Contains(page, "Affidabilità e HnR") || !strings.Contains(page, "jackett:Tracker One") {
		t.Fatalf("trust page -> %d: %s", code, page)
	}
}

func TestSourceTrustAPIValidatesDays(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	if code, _ := v2Request(t, server, http.MethodGet, "/api/source-trust?days=0", nil); code != http.StatusBadRequest {
		t.Fatalf("days=0 -> %d", code)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/api/source-trust?days=7", nil); code != http.StatusOK || !strings.Contains(body, `"sources"`) {
		t.Fatalf("days=7 -> %d: %s", code, body)
	}
}
