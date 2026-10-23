package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
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
	if !strings.Contains(before, "provo un'alternativa dopo 30 minuti") {
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
	if !strings.Contains(after, "30 giorni") {
		t.Fatalf("stall summary %q does not mention the new 30-day window", after)
	}
}

func TestEd2kRememberIncomingRefreshesTheCache(t *testing.T) {
	ed2kState.mu.Lock()
	previous := ed2kState.incoming
	ed2kState.incoming = ""
	ed2kState.mu.Unlock()
	t.Cleanup(func() {
		ed2kState.mu.Lock()
		ed2kState.incoming = previous
		ed2kState.mu.Unlock()
	})
	ed2kRememberIncoming(" /srv/amule/new ")
	ed2kState.mu.Lock()
	got := ed2kState.incoming
	ed2kState.mu.Unlock()
	if got != "/srv/amule/new" {
		t.Fatalf("cached incoming = %q, want the trimmed new path", got)
	}
	ed2kRememberIncoming("")
	ed2kState.mu.Lock()
	got = ed2kState.incoming
	ed2kState.mu.Unlock()
	if got != "/srv/amule/new" {
		t.Fatalf("an empty read cleared the cache: %q", got)
	}
}

func TestSettingsEd2kPanelShowsAMuleFolders(t *testing.T) {
	var body strings.Builder
	view := v2SettingsEd2k{Enabled: true, Reachable: true, IncomingDir: "/srv/amule/incoming", TempDir: "/srv/amule/temp"}
	if err := v2Templates.ExecuteTemplate(&body, "v2_settings_ed2k", view); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Cartella download", "/srv/amule/incoming",
		"Cartella temporanea", "/srv/amule/temp",
		"Si cambiano da aMule/amuled",
		"Cartella in cui aMule salva i file completati",
	} {
		if !strings.Contains(body.String(), want) {
			t.Fatalf("eD2k settings panel is missing %q", want)
		}
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

// TestSourcesIndexLinksIndexerErrorsToHealth checks that a failed last cycle
// sends the indexer entry point to Health (where the errors are) instead of
// Integrations, and that a single failure is not called "1 errori".
func TestSourcesIndexLinksIndexerErrorsToHealth(t *testing.T) {
	state := newTestAppState(t)
	if err := state.db.SaveCycle(&models.CycleStats{
		Sources: []models.CycleSourceStat{{Kind: "indexer", Name: "Jackett", Fail: 1, LastError: "timeout"}},
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=settings&tab=sources", nil)
	if code != http.StatusOK {
		t.Fatalf("sources tab -> %d", code)
	}
	for _, want := range []string{
		"Errori indexer",
		`href="/?view=health"`,
		"Apri Salute per i dettagli",
		"ultimo ciclo: 1 errore",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sources entry point missing %q", want)
		}
	}
	if !strings.Contains(body, `href="/?view=health"`) || !strings.Contains(body, `>Errori indexer</a>`) {
		t.Fatal("with indexer errors the entry point must open Health, not Integrations")
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
