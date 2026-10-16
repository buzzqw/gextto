package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The three reference examples are scored with the live weights: moving a
// weight must move the example, so the opaque score numbers stay explainable.
func TestScoreExamplesTrackWeights(t *testing.T) {
	state := newTestAppState(t)
	base := scoreExamples(latestConfig(state))[2].Value // 2160p Dolby Vision
	if err := saveConfigSetting(state.cfg.DataDir, "score_bonus_dv", "400"); err != nil {
		t.Fatalf("save Dolby Vision weight: %v", err)
	}
	raised := scoreExamples(latestConfig(state))[2].Value
	if raised != base+100 {
		t.Fatalf("Dolby Vision weight +100 changed the example by %d (base %d, raised %d)", raised-base, base, raised)
	}
}

func TestStallPolicySummaryTracksValues(t *testing.T) {
	state := newTestAppState(t)
	before := stallPolicySummary(state, latestConfig(state))
	if !strings.Contains(before, "360") {
		t.Fatalf("stall summary %q does not mention the alternative window", before)
	}
	// 43200 minutes = 30 days.
	if err := saveConfigSetting(state.cfg.DataDir, "torrent_stall_giveup_min", "43200"); err != nil {
		t.Fatalf("save give-up window: %v", err)
	}
	after := stallPolicySummary(state, latestConfig(state))
	if after == before {
		t.Fatalf("stall summary did not change after the give-up window changed: %q", after)
	}
	if !strings.Contains(after, "30") {
		t.Fatalf("stall summary %q does not mention the new 30-day window", after)
	}
}

func TestSettingsSearchFindsProwlarr(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/settings/search?q=prowlarr", nil)
	if code != http.StatusOK || !strings.Contains(body, "Indexer e manager") {
		t.Fatalf("searching Prowlarr -> %d: %s", code, body)
	}
}

func TestSettingsPageShowsTheNewInsights(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=settings&tab=seeding", nil)
	if code != http.StatusOK || !strings.Contains(body, "settings-summary") {
		t.Fatalf("seeding tab is missing the stall timeline -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/?view=settings&tab=sources", nil)
	if code != http.StatusOK || !strings.Contains(body, "v2-sources-index") || !strings.Contains(body, "Indexer e manager") {
		t.Fatalf("sources tab is missing the entry point -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodGet, "/?view=settings&tab=scores", nil)
	if code != http.StatusOK || !strings.Contains(body, "v2-score-examples") {
		t.Fatalf("scores tab is missing the score examples -> %d", code)
	}
}
