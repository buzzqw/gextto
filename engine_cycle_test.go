package gextto

import (
	"context"
	"testing"
	"time"
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
		state.activeEngine(),
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

// TestCycleCancelledContextAborts verifies that a cycle with an already
// cancelled context (daemon shutting down) returns cleanly without doing work,
// so the torrent session can be destroyed without a late engine call.
func TestCycleCancelledContextAborts(t *testing.T) {
	state := newCycleState(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stats, err := RunCycle(
		ctx,
		state.cfg,
		state.engine,
		state.db,
		state.archive,
		state.comics,
		state.notifier,
		state.activeEngine(),
	)
	if err != nil {
		t.Fatalf("RunCycle with cancelled context: %v", err)
	}
	if stats == nil {
		t.Fatal("RunCycle returned nil stats")
	}
	if stats.DownloadsStarted != 0 || stats.Scraped != 0 {
		t.Fatalf("cancelled cycle did work: scraped=%d started=%d", stats.Scraped, stats.DownloadsStarted)
	}
}

// TestBackgroundContextCancelledOnStop verifies that BackgroundContext is
// cancelled when the daemon shutdown is signalled, which is what makes the
// cycle abort before the session is destroyed.
func TestBackgroundContextCancelledOnStop(t *testing.T) {
	state := newTestAppState(t)
	ctx, cancel := state.BackgroundContext()
	defer cancel()

	stopBackgroundWorkers(state)

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("BackgroundContext was not cancelled on shutdown")
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
				state.activeEngine(),
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
