package gextto

import (
	"context"
	"testing"
	"time"
)

func TestCrossnetSearchStopsOnCancelledRootContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// All collaborators are nil: a search which ignores its root context would
	// dereference them instead of returning at the cancellation checkpoint.
	crossnetSearchFor(ctx, nil, nil, nil, nil, "hash", "shutdown")
}

func TestCrossnetTickDoesNotLaunchAfterRootCancellation(t *testing.T) {
	resetCrossnet()
	t.Cleanup(resetCrossnet)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := &AppState{bgStop: make(chan struct{}), bgContext: ctx, bgCancel: cancel}
	crossnetTickContext(ctx, state, nil, nil, nil, nil, time.Now())
	crossnet.mu.Lock()
	searching := crossnet.searching
	crossnet.mu.Unlock()
	if searching {
		t.Fatal("a cancelled crossnet root launched a search")
	}
}
