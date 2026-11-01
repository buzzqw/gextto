package gextto

// host_pace_test.go covers the per-host cooldown of the feed fetch path: a site
// that answers 429 is left alone (growing cooldown) instead of being asked once
// per feed of the list, and the feeds of that site are not counted as broken.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// reset_host_pace forgets what the process learnt about the sites. The pace is
// process-wide on purpose, so a test that simulates a 429 must not leave it to
// the next test.
func reset_host_pace() {
	hostLastRequestMu.Lock()
	hostLastRequest = map[string]*hostPaceState{}
	hostLastRequestMu.Unlock()
}

func host_strikes(rawURL string) int {
	hostLastRequestMu.Lock()
	defer hostLastRequestMu.Unlock()
	state := hostLastRequest[host_pace_key(rawURL)]
	if state == nil {
		return 0
	}
	return state.strikes
}

func TestHostCooldownGrowsWhileTheSiteKeepsAnswering429(t *testing.T) {
	reset_host_pace()
	defer reset_host_pace()

	url := "https://busy.example/get-posts/user:x/"
	penalize_host(url, feedRateLimitPenalty)
	first, cooling := host_cooling(url)
	if !cooling || first <= 0 {
		t.Fatalf("the first 429 must put the site on cooldown, got %v (%v)", first, cooling)
	}

	// The site answers 429 again after the first cooldown: the second one is
	// twice as long instead of the same ten minutes for ever.
	penalize_host(url, feedRateLimitPenalty)
	second, cooling := host_cooling(url)
	if !cooling {
		t.Fatal("the site must still be cooling down")
	}
	if second <= first {
		t.Fatalf("second cooldown = %v, want more than the first (%v)", second, first)
	}
}

func TestHostCooldownRecoversAfterGoodAnswers(t *testing.T) {
	reset_host_pace()
	defer reset_host_pace()

	url := "https://busy.example/feed"
	penalize_host(url, feedRateLimitPenalty)
	if got := host_strikes(url); got != 1 {
		t.Fatalf("strikes after one 429 = %d, want 1", got)
	}
	for i := 0; i < feedHostRecoverAfter; i++ {
		host_succeeded(url)
	}
	if got := host_strikes(url); got != 0 {
		t.Fatalf("strikes after %d good answers = %d, want 0", feedHostRecoverAfter, got)
	}
}

// TestFetchBodyLeavesAHostInCooldownAlone locks the point of the cooldown: the
// other feeds of a site that answered 429 are not sent at all.
func TestFetchBodyLeavesAHostInCooldownAlone(t *testing.T) {
	reset_host_pace()
	defer reset_host_pace()

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	penalize_host(server.URL, time.Minute)
	attempt := fetch_body_direct(context.Background(), server.Client(), server.URL, time.Second)
	if attempt.kind != fetchAttemptCooling {
		t.Fatalf("attempt kind = %v, want fetchAttemptCooling (value %q)", attempt.kind, attempt.value)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("the server was contacted %d time(s) during the cooldown, want 0", got)
	}

	// FetchFeed turns the same condition into a sentinel error the feed loop
	// can recognise and skip, without recording a failure of the feed.
	if _, err := FetchFeed(context.Background(), server.URL, nil, 1, 0, 0.8); !errors.Is(err, errHostCooling) {
		t.Fatalf("FetchFeed error = %v, want errHostCooling", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("FetchFeed contacted the server %d time(s) during the cooldown, want 0", got)
	}
}
