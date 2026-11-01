package main

// feed_pace_test.go covers the per-host pacing of the feed poller: the feeds of
// one site are spaced, and a site that answers 429 is left alone for a while
// instead of being asked once per feed (which is what turns one rate limit into
// a dozen failing feeds).

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFeedPacerSpacesOneHost(t *testing.T) {
	pacer := newFeedPacer(40*time.Millisecond, time.Minute)
	start := time.Now()
	pacer.wait(context.Background(), "example.com")
	pacer.done("example.com", nil)
	// The second request to the same host must wait for the gap.
	pacer.wait(context.Background(), "example.com")
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("the second request to the same host waited %v, want at least 40ms", elapsed)
	}
	// Another host is not held back by it.
	start = time.Now()
	pacer.wait(context.Background(), "other.example")
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("a different host waited %v", elapsed)
	}
}

func TestFeedPacerCoolsDownAfter429(t *testing.T) {
	pacer := newFeedPacer(time.Millisecond, 200*time.Millisecond)
	pacer.done("busy.example", errors.New("rss: busy.example returned HTTP 429"))
	if cooldown := pacer.wait(context.Background(), "busy.example"); cooldown <= 0 {
		t.Fatal("a host that answered 429 must be on cooldown")
	}
	// "too many requests" counts too, and a plain error does not.
	pacer.done("other.example", errors.New("rss: other.example returned HTTP 500"))
	if cooldown := pacer.wait(context.Background(), "other.example"); cooldown != 0 {
		t.Fatalf("a server error is not a rate limit (cooldown %v)", cooldown)
	}
}

func TestFeedPacerCoolDownEnds(t *testing.T) {
	pacer := newFeedPacer(time.Millisecond, 20*time.Millisecond)
	pacer.done("busy.example", errors.New("HTTP 429"))
	time.Sleep(30 * time.Millisecond)
	if cooldown := pacer.wait(context.Background(), "busy.example"); cooldown != 0 {
		t.Fatalf("the cooldown must end by itself, still %v", cooldown)
	}
}

// TestFeedHostKey: the pacer keys on the site, and a Torznab search or a DHT
// feed (which do not hit the URL) is never paced.
func TestFeedHostKey(t *testing.T) {
	cases := []struct {
		url, search, want string
	}{
		{"https://TorrentGalaxy.one/get-posts/user:X/", "", "torrentgalaxy.one"},
		{"https://extto.org/browse/?filter=u=x", "", "extto.org"},
		{"", "Serie S01", ""},
		{"dht://abc", "", ""},
	}
	for _, c := range cases {
		if got := feedHost(c.url, c.search); got != c.want {
			t.Errorf("feedHost(%q, %q) = %q, want %q", c.url, c.search, got, c.want)
		}
	}
}

// TestFeedPacerGrowsTheCooldownWhileTheSiteKeepsAnswering429 covers the
// escalation: one 429 costs the base cooldown, the next one twice as much, and
// so on, instead of probing a site that is blocking us every ten minutes for
// ever.
func TestFeedPacerGrowsTheCooldownWhileTheSiteKeepsAnswering429(t *testing.T) {
	pacer := newFeedPacer(time.Millisecond, 20*time.Millisecond)
	pacer.done("busy.example", errors.New("HTTP 429"))
	first := pacer.wait(context.Background(), "busy.example")
	if first <= 0 {
		t.Fatal("the first 429 must put the host on cooldown")
	}
	// After the first cooldown the site answers 429 again: the second one is
	// twice as long.
	pacer.done("busy.example", errors.New("HTTP 429"))
	second := pacer.wait(context.Background(), "busy.example")
	if second <= first {
		t.Fatalf("cooldown after the second 429 = %v, want more than the first (%v)", second, first)
	}
}

// TestFeedPacerStretchesTheGapAndRecovers covers the other half: the spacing
// between scheduled requests grows after a 429 and comes back after a run of
// good answers.
func TestFeedPacerStretchesTheGapAndRecovers(t *testing.T) {
	pacer := newFeedPacer(10*time.Millisecond, 5*time.Millisecond)
	pacer.done("busy.example", errors.New("HTTP 429"))
	pacer.mu.Lock()
	grown := pacer.host("busy.example").gap
	pacer.mu.Unlock()
	if grown <= 10*time.Millisecond {
		t.Fatalf("gap after a 429 = %v, want more than the base 10ms", grown)
	}
	for i := 0; i < feedHostRecoverAfter; i++ {
		pacer.done("busy.example", nil)
	}
	pacer.mu.Lock()
	state := pacer.host("busy.example")
	recoveredGap, strikes := state.gap, state.strikes
	pacer.mu.Unlock()
	if recoveredGap >= grown {
		t.Fatalf("gap after %d good answers = %v, want less than %v", feedHostRecoverAfter, recoveredGap, grown)
	}
	if strikes != 0 {
		t.Fatalf("strikes after recovery = %d, want 0", strikes)
	}
}
