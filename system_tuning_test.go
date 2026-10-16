package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/constants"
)

func tuningTestFacts() SystemFacts {
	const gib = uint64(1024 * 1024 * 1024)
	return SystemFacts{
		MemTotal:     16 * gib,
		MemAvailable: 12 * gib,
		NumCPU:       8,
		Download:     VolumeFacts{Path: "/downloads", Total: 100 * gib, Free: 50 * gib, Exists: true},
		Archive:      VolumeFacts{Path: "/library", Total: 100 * gib, Free: 50 * gib, Exists: true},
		SameDevice:   true,
	}
}

func tuningProposalKeys(proposals []TuningProposal) map[string]string {
	out := map[string]string{}
	for _, proposal := range proposals {
		out[proposal.Key] = proposal.Proposed
	}
	return out
}

// The engine manages the cache and the queue itself: the tuning must never
// propose to overwrite them (the daemon would ignore the value).
func TestProposeTuningSkipsEngineManagedKeys(t *testing.T) {
	cfg := DefaultConfig()
	managed := map[string]bool{
		"torrent_cache_size": true, "torrent_active_downloads": true,
		"torrent_active_seeds": true, "torrent_active_limit": true,
		"torrent_dynamic_queue": true,
	}
	for key := range managed {
		cfg.Settings[key] = "1"
	}
	for _, proposal := range ProposeTuning(tuningTestFacts(), &cfg, nil) {
		if managed[proposal.Key] {
			t.Fatalf("engine-managed key %q was proposed", proposal.Key)
		}
	}
}

func TestProposeTuningRespectsManualChoice(t *testing.T) {
	cfg := DefaultConfig()
	// Without a manual mark it is proposed; with one it disappears.
	if _, ok := tuningProposalKeys(ProposeTuning(tuningTestFacts(), &cfg, nil))["min_free_space_gb"]; !ok {
		t.Fatal("min_free_space_gb should be proposed for a fresh configuration")
	}
	keys := tuningProposalKeys(ProposeTuning(tuningTestFacts(), &cfg, map[string]bool{"min_free_space_gb": true}))
	if _, ok := keys["min_free_space_gb"]; ok {
		t.Fatal("a setting set by hand must never be proposed again")
	}
}

func TestProposeTuningSpaceGuardIsClampedToFreeSpace(t *testing.T) {
	cfg := DefaultConfig()
	// A big volume: at least 20 GB or 5% (5% of 100 GB = 5 GB < 20 GB).
	keys := tuningProposalKeys(ProposeTuning(tuningTestFacts(), &cfg, nil))
	if keys["min_free_space_gb"] != "20" {
		t.Fatalf("min_free_space_gb = %q, want 20", keys["min_free_space_gb"])
	}
	// A small, nearly full volume: never demand more than half of what is free.
	const gib = uint64(1024 * 1024 * 1024)
	small := tuningTestFacts()
	small.Download = VolumeFacts{Path: "/downloads", Total: 30 * gib, Free: 8 * gib, Exists: true}
	keys = tuningProposalKeys(ProposeTuning(small, &cfg, nil))
	if keys["min_free_space_gb"] != "4" {
		t.Fatalf("clamped min_free_space_gb = %q, want 4 (half of the 8 GB free)", keys["min_free_space_gb"])
	}
}

func TestProposeTuningDetectsNetworkFilesystem(t *testing.T) {
	cfg := DefaultConfig()
	facts := tuningTestFacts()
	facts.ArchiveNetwork = true
	keys := tuningProposalKeys(ProposeTuning(facts, &cfg, nil))
	if keys["torrent_preallocate"] != "no" {
		t.Fatalf("preallocate on a network filesystem = %q, want no", keys["torrent_preallocate"])
	}
	if keys["gxtorrent_request_timeout_secs"] != "90" {
		t.Fatalf("request timeout on a network filesystem = %q, want 90", keys["gxtorrent_request_timeout_secs"])
	}
}

func TestApplyTuningWritesOnlyConfirmedKeys(t *testing.T) {
	state := newTestAppState(t)
	dataDir := state.cfg.DataDir
	cfg := latestConfig(state)
	proposals := []TuningProposal{
		{Key: "min_free_space_gb", Proposed: "25", WhyTemplate: "test"},
		{Key: "torrent_preallocate", Proposed: "no", WhyTemplate: "test"},
	}
	applied, err := ApplyTuning(dataDir, cfg, proposals, map[string]bool{"min_free_space_gb": true})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	if got := latestConfig(state).Settings["min_free_space_gb"]; got != "25" {
		t.Fatalf("min_free_space_gb = %q, want 25", got)
	}
	if got := latestConfig(state).Settings["torrent_preallocate"]; got == "no" {
		t.Fatal("an unconfirmed key was written")
	}
	origins, err := SettingOrigins(dataDir)
	if err != nil {
		t.Fatalf("origins: %v", err)
	}
	if origin := origins["min_free_space_gb"].Origin; origin != OriginAuto {
		t.Fatalf("origin = %q, want auto", origin)
	}
	if _, ok := origins["torrent_preallocate"]; ok {
		t.Fatal("an unconfirmed key got a provenance row")
	}
}

// A key the tuning did not propose cannot be written even if the request asks
// for it: the form only chooses among the server's proposals.
func TestApplyTuningIgnoresUnproposedKeys(t *testing.T) {
	state := newTestAppState(t)
	proposals := []TuningProposal{{Key: "min_free_space_gb", Proposed: "25"}}
	applied, err := ApplyTuning(state.cfg.DataDir, latestConfig(state), proposals, map[string]bool{"torrent_cache_size": true})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0", applied)
	}
	if got := latestConfig(state).Settings["torrent_cache_size"]; got != "" {
		t.Fatalf("unproposed key was written: %q", got)
	}
}

func TestManualSaveMarksManualOrigin(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodPost, "/settings/save", url.Values{"key": {"min_free_space_gb"}, "value": {"7"}})
	if code != http.StatusOK || !strings.Contains(body, "salvata") {
		t.Fatalf("save -> %d: %s", code, body)
	}
	origins, err := SettingOrigins(state.cfg.DataDir)
	if err != nil {
		t.Fatalf("origins: %v", err)
	}
	if origin := origins["min_free_space_gb"].Origin; origin != OriginManual {
		t.Fatalf("origin after a manual save = %q, want manual", origin)
	}
	// A manual value is then never proposed again.
	if _, ok := tuningProposalKeys(ProposeTuning(tuningTestFacts(), latestConfig(state), ManualSettings(state.cfg.DataDir)))["min_free_space_gb"]; ok {
		t.Fatal("a manually saved setting is still being proposed")
	}
}

// An update restart is a new binary, not instability: the tuning must be able to
// tell them apart.
func TestRestartCauseNeverReadsUpdateAsInstability(t *testing.T) {
	update := runState{Runs: 3, Build: constants.Build + "-previous", StoppedAt: "2026-01-01T00:00:00Z"}
	if cause := ClassifyRestart(update); cause != RestartUpdate {
		t.Fatalf("a build change = %q, want update", cause)
	}
	if restartsMeaningInstability(ClassifyRestart(update)) {
		t.Fatal("an update restart must never be instability")
	}
	unclean := runState{Runs: 3, Build: constants.Build, StoppedAt: ""}
	if cause := ClassifyRestart(unclean); cause != RestartUnclean || !restartsMeaningInstability(cause) {
		t.Fatalf("an unclean stop = %q (instability %v), want unclean", cause, restartsMeaningInstability(cause))
	}
	manual := runState{Runs: 3, Build: constants.Build, StoppedAt: "2026-01-01T00:00:00Z"}
	if cause := ClassifyRestart(manual); cause != RestartManual || restartsMeaningInstability(cause) {
		t.Fatalf("a clean restart = %q (instability %v), want restart", cause, restartsMeaningInstability(cause))
	}
	if cause := ClassifyRestart(runState{}); cause != RestartFirstStart {
		t.Fatalf("first start = %q, want first-start", cause)
	}
}

// In Facilitata the card is one button: it applies the whole proposal (the user
// trusts the system), and every write is recorded as auto.
func TestTuningApplyBriefAppliesTheWholeProposal(t *testing.T) {
	state := newTestAppState(t)
	download := t.TempDir()
	archive := t.TempDir()
	if err := saveConfigSetting(state.cfg.DataDir, "torrent_dir", download); err != nil {
		t.Fatalf("download dir: %v", err)
	}
	if err := saveConfigSetting(state.cfg.DataDir, "archive_root", archive); err != nil {
		t.Fatalf("archive root: %v", err)
	}
	// The apply refuses to write in dry-run (correct): leave it for this test.
	if err := saveConfigSetting(state.cfg.DataDir, "dry_run", "false"); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	cfg := latestConfig(state)
	proposals := ProposeTuning(ProbeSystem(cfg), cfg, nil)
	if len(proposals) == 0 {
		t.Skip("this machine produces no proposal")
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodPost, "/settings/tuning/apply?mode=brief", nil)
	if code != http.StatusOK || !strings.Contains(body, "applicat") {
		t.Fatalf("brief apply -> %d: %s", code, body)
	}
	origins, err := SettingOrigins(state.cfg.DataDir)
	if err != nil {
		t.Fatalf("origins: %v", err)
	}
	for _, proposal := range proposals {
		if origin := origins[proposal.Key].Origin; origin != OriginAuto {
			t.Fatalf("%q was proposed but not applied as auto (origin %q)", proposal.Key, origin)
		}
	}
}

// In Esperto each value says where it came from; in Facilitata the badge is not
// shown (the internals are hidden and the limits are the user's).
func TestExpertShowsWhereAValueCameFrom(t *testing.T) {
	state := newTestAppState(t)
	if err := MarkSettingOrigin(state.cfg.DataDir, "min_free_space_gb", OriginAuto, "8 GB RAM"); err != nil {
		t.Fatalf("mark auto: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=settings&tab=paths", nil)
	if code != http.StatusOK || !strings.Contains(body, "gestito dal sistema") {
		t.Fatalf("expert page must show the auto origin -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/?view=settings&tab=paths&mode=brief", nil)
	if code != http.StatusOK || strings.Contains(body, "gestito dal sistema") {
		t.Fatalf("Facilitata must not show origin badges -> %d", code)
	}
}

func TestTuningPanelRendersInThePerformanceTab(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=settings&tab=performance", nil)
	if code != http.StatusOK || !strings.Contains(body, "v2-tuning-panel") {
		t.Fatalf("performance tab is missing the tuning panel -> %d", code)
	}
}
