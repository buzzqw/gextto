package gextto

// cycle_simulation_test.go runs real search cycles end to end (feed scrape ->
// cycle persisted -> Salute rows -> warning decision) so M1 and M2 are exercised
// together, not only through their unit functions.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// newSimCycleConfig is a feed-only cycle: no indexers, no web engines and no
// monitored titles, with dry-run on so nothing is downloaded.
func newSimCycleConfig(state *AppState, feedURL string) *Config {
	cfg := *state.cfg
	cfg.FeedURLs = []string{feedURL}
	cfg.Indexers = nil
	cfg.WebsearchEngines = nil
	cfg.Series = nil
	cfg.Movies = nil
	cfg.DryRun = true
	return &cfg
}

func runSimCycle(t *testing.T, state *AppState, cfg *Config) *models.CycleStats {
	t.Helper()
	stats, err := RunCycle(context.Background(), cfg, state.engine, state.db, state.archive, state.comics, state.notifier, state.activeEngine())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if stats == nil {
		t.Fatal("RunCycle returned nil stats")
	}
	return stats
}

// TestCycleSimulationPersistsSourcesAndDuration checks that a real cycle stores
// its duration and per-source outcome, that the Salute rows read them back, and
// that a healthy cycle raises no warning.
func TestCycleSimulationPersistsSourcesAndDuration(t *testing.T) {
	state := newCycleState(t)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<rss><channel><item><title>Sim.Show.S01E01.1080p</title><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link></item></channel></rss>`)
	}))
	defer feed.Close()

	stats := runSimCycle(t, state, newSimCycleConfig(state, feed.URL))

	cycles, err := state.db.RecentCycleStats(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 1 {
		t.Fatalf("want 1 stored cycle, got %d", len(cycles))
	}
	stored := cycles[0]
	if stored.DurationSeconds != stats.DurationSeconds {
		t.Fatalf("stored duration %d, stats %d", stored.DurationSeconds, stats.DurationSeconds)
	}
	if len(stored.Sources) == 0 {
		t.Fatal("no per-source outcome stored with the cycle")
	}
	foundFeed := false
	for _, source := range stored.Sources {
		if source.Kind == "feed" && source.OK > 0 {
			foundFeed = true
		}
	}
	if !foundFeed {
		t.Fatalf("working feed not recorded: %+v", stored.Sources)
	}

	// The Salute "Ricerche" rows read the persisted cycle back.
	rows := uiCycleRowsFrom(cycles, time.Now())
	if len(rows) != 1 {
		t.Fatalf("want 1 Salute row, got %d", len(rows))
	}
	if rows[0].Scraped != stats.Scraped || rows[0].Downloads != stats.DownloadsStarted {
		t.Fatalf("Salute row does not match the cycle: %+v vs %+v", rows[0], stats)
	}
	if rows[0].Duration == "" || rows[0].FailedCount != 0 {
		t.Fatalf("unexpected Salute row: %+v", rows[0])
	}

	// M2: a recent clean cycle must not warn.
	if reason := cycleWarningReason(cycles, 6*time.Hour, time.Now()); reason != "" {
		t.Fatalf("healthy cycle warned: %q", reason)
	}
}

// TestCycleSimulationWarnsOnRepeatedSourceFailures runs three real cycles
// against a feed that always fails. The provider backoff is cleared between
// cycles so the source is retried each time, as it is on a real refresh interval
// much longer than the backoff: the persisted history must then make the cycle
// monitor warn.
func TestCycleSimulationWarnsOnRepeatedSourceFailures(t *testing.T) {
	state := newCycleState(t)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer feed.Close()
	cfg := newSimCycleConfig(state, feed.URL)

	for i := 0; i < cycleSourceFailCount; i++ {
		if _, err := state.db.db.Exec("DELETE FROM provider_status"); err != nil {
			t.Fatal(err)
		}
		runSimCycle(t, state, cfg)
	}

	cycles, err := state.db.RecentCycleStats(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) < cycleSourceFailCount {
		t.Fatalf("want at least %d stored cycles, got %d", cycleSourceFailCount, len(cycles))
	}
	for _, cycle := range cycles {
		if len(cycle.Sources) == 0 {
			t.Fatalf("cycle stored without source outcome: %+v", cycle)
		}
	}

	reason := cycleWarningReason(cycles, 6*time.Hour, time.Now())
	if !strings.Contains(reason, "ha fallito") {
		t.Fatalf("repeated source failures not warned: %q", reason)
	}
}
