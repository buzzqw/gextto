package gextto

import (
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func testCycle(at time.Time, durationSeconds, errors int, sources ...models.CycleSourceStat) models.CycleHistoryEntry {
	return models.CycleHistoryEntry{
		At: at.UTC().Format("2006-01-02 15:04:05"),
		CycleStats: models.CycleStats{
			DurationSeconds: durationSeconds,
			Errors:          errors,
			Sources:         sources,
		},
	}
}

func TestCycleWarningReasonHealthy(t *testing.T) {
	now := time.Now()
	cycles := []models.CycleHistoryEntry{
		testCycle(now.Add(-time.Minute), 60, 0, models.CycleSourceStat{Kind: "feed", Name: "TGx", OK: 1}),
		testCycle(now.Add(-6*time.Hour), 60, 0, models.CycleSourceStat{Kind: "feed", Name: "TGx", OK: 1}),
	}
	if got := cycleWarningReason(cycles, 6*time.Hour, now); got != "" {
		t.Fatalf("healthy history warned: %q", got)
	}
}

func TestCycleWarningReasonToleratesOneBadCycle(t *testing.T) {
	now := time.Now()
	cycles := []models.CycleHistoryEntry{
		testCycle(now, 60, 1, models.CycleSourceStat{Kind: "feed", Name: "TGx", Fail: 1}),
	}
	if got := cycleWarningReason(cycles, 6*time.Hour, now); got != "" {
		t.Fatalf("a single bad cycle warned: %q", got)
	}
}

func TestCycleWarningReasonSourceStreak(t *testing.T) {
	now := time.Now()
	var cycles []models.CycleHistoryEntry
	for i := 0; i < cycleSourceFailCount; i++ {
		cycles = append(cycles, testCycle(now.Add(-time.Duration(i)*time.Hour), 60, 1,
			models.CycleSourceStat{Kind: "feed", Name: "TGx", Fail: 1, LastError: "timeout"}))
	}
	got := cycleWarningReason(cycles, 6*time.Hour, now)
	if !strings.Contains(got, "TGx") || !strings.Contains(got, "3 delle ultime 3") {
		t.Fatalf("repeated source failures not reported: %q", got)
	}
}

// TestCycleWarningReasonSourceFailsAcrossWindow checks the warning still fires
// when the provider backoff skips the source on some cycles: failures are
// counted across the window, not consecutively.
func TestCycleWarningReasonSourceFailsAcrossWindow(t *testing.T) {
	now := time.Now()
	cycles := []models.CycleHistoryEntry{
		testCycle(now.Add(-1*time.Hour), 60, 1, models.CycleSourceStat{Kind: "feed", Name: "TGx", Fail: 1}),
		testCycle(now.Add(-2*time.Hour), 60, 0), // skipped (backoff): source absent
		testCycle(now.Add(-3*time.Hour), 60, 1, models.CycleSourceStat{Kind: "feed", Name: "TGx", Fail: 1}),
		testCycle(now.Add(-4*time.Hour), 60, 0), // skipped (backoff)
		testCycle(now.Add(-5*time.Hour), 60, 1, models.CycleSourceStat{Kind: "feed", Name: "TGx", Fail: 1}),
	}
	got := cycleWarningReason(cycles, 6*time.Hour, now)
	if !strings.Contains(got, "TGx") || !strings.Contains(got, "3 delle ultime 3") {
		t.Fatalf("windowed source failures not reported: %q", got)
	}
}

func TestCycleWarningReasonSlowCycle(t *testing.T) {
	now := time.Now()
	cycles := []models.CycleHistoryEntry{
		testCycle(now, int((cycleSlowWarning + time.Hour).Seconds()), 0),
	}
	got := cycleWarningReason(cycles, 6*time.Hour, now)
	if !strings.Contains(got, "impiegato") {
		t.Fatalf("slow cycle not reported: %q", got)
	}
}

func TestCycleWarningReasonStaleSearch(t *testing.T) {
	now := time.Now()
	cycles := []models.CycleHistoryEntry{
		testCycle(now.Add(-5*time.Hour), 60, 2),
	}
	// Interval one hour -> threshold max(2h, 3h) = 3h; the last clean cycle is
	// more than five hours old.
	got := cycleWarningReason(cycles, time.Hour, now)
	if !strings.Contains(got, "senza errori") {
		t.Fatalf("stale search not reported: %q", got)
	}
}

func TestCycleWarningLogStateTransitions(t *testing.T) {
	var state cycleWarningLogState
	now := time.Now()
	if got := state.observe("", now); got != "" {
		t.Fatalf("clean -> %q", got)
	}
	if got := state.observe("boom", now); got != "warning" {
		t.Fatalf("first problem -> %q", got)
	}
	if got := state.observe("boom", now.Add(time.Minute)); got != "" {
		t.Fatalf("same problem repeated -> %q", got)
	}
	if got := state.observe("boom", now.Add(cycleMonitorHeartbeat+time.Minute)); got != "warning" {
		t.Fatalf("heartbeat -> %q", got)
	}
	if got := state.observe("", now.Add(cycleMonitorHeartbeat+2*time.Minute)); got != "recovered" {
		t.Fatalf("recovery -> %q", got)
	}
}
