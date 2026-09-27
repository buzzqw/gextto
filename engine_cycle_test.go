package gextto

import (
	"context"
	"testing"
)

// newCycleState returns the shared fixture used by the cycle tests. It is the
// same hermetic AppState as the web API tests: temporary databases, no feeds,
// no indexers, no series/movies/comics and DryRun enabled.
func newCycleState(t *testing.T) *AppState {
	t.Helper()
	return newTestAppState(t)
}

// TestCycleFullNoSourcesIsNoOp runs a full cycle with an empty configuration.
// With no feeds, indexers, series, movies or monitored comics every network
// phase is a no-op, so the cycle must complete cleanly and report zero work.
func TestCycleFullNoSourcesIsNoOp(t *testing.T) {
	state := newCycleState(t)

	stats, err := RunCycle(
		context.Background(),
		state.cfg,
		state.engine,
		state.db,
		state.archive,
		state.comics,
		state.notifier,
		state.torrents,
	)
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if stats == nil {
		t.Fatal("RunCycle returned nil stats")
	}
	if stats.Scraped != 0 {
		t.Fatalf("Scraped = %d, want 0", stats.Scraped)
	}
	if stats.DownloadsStarted != 0 {
		t.Fatalf("DownloadsStarted = %d, want 0", stats.DownloadsStarted)
	}
}

// TestCycleDomainNoSourcesIsNoOp runs one cycle per domain with an empty
// configuration. "comics" reaches `RunComicsCycle`, which returns immediately
// because no comic is monitored (no HTTP client call); "series"/"movies" only
// scrape the empty config.
func TestCycleDomainNoSourcesIsNoOp(t *testing.T) {
	for _, domain := range []string{"series", "movies", "comics"} {
		t.Run(domain, func(t *testing.T) {
			state := newCycleState(t)
			current := domain

			stats, err := RunCycleDomain(
				context.Background(),
				state.cfg,
				state.engine,
				state.db,
				state.archive,
				state.comics,
				state.notifier,
				state.torrents,
				&current,
			)
			if err != nil {
				t.Fatalf("RunCycleDomain(%s): %v", domain, err)
			}
			if stats == nil {
				t.Fatalf("RunCycleDomain(%s) returned nil stats", domain)
			}
			if stats.Scraped != 0 {
				t.Fatalf("RunCycleDomain(%s): Scraped = %d, want 0", domain, stats.Scraped)
			}
			if stats.DownloadsStarted != 0 {
				t.Fatalf("RunCycleDomain(%s): DownloadsStarted = %d, want 0", domain, stats.DownloadsStarted)
			}
		})
	}
}
