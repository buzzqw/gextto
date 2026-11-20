package announcer

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestAnnounceHostKey: the gate is per host and port, whatever the scheme.
func TestAnnounceHostKey(t *testing.T) {
	cases := map[string]string{
		"http://tracker.example:8080/announce": "tracker.example:8080",
		"udp://Tracker.Example:80/announce":    "tracker.example:80",
		"wss://tracker.example/announce":       "tracker.example",
		"https://[2001:db8::1]:6969/announce":  "[2001:db8::1]:6969",
		"not a url":                            "not a url",
	}
	for raw, want := range cases {
		if got := AnnounceHost(raw); got != want {
			t.Errorf("AnnounceHost(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestPacerSpacesTheSameHost: with one token, consecutive waits to one host
// are spaced by the rate.
func TestPacerSpacesTheSameHost(t *testing.T) {
	p := NewPacer(50, 1) // one token, then one every 20 ms
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := p.Wait(context.Background(), "tracker.example:80"); err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 30*time.Millisecond {
		t.Fatalf("three waits took %v, want the two 20 ms gaps", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("three waits took %v, far too slow", elapsed)
	}
}

// TestPacerHostsDoNotBlockEachOther: a token spent on one host leaves another
// host's burst untouched.
func TestPacerHostsDoNotBlockEachOther(t *testing.T) {
	p := NewPacer(1, 1)
	ctx := context.Background()
	if err := p.Wait(ctx, "a.example:80"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := p.Wait(ctx, "b.example:80"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("a second host waited %v", elapsed)
	}
}

// TestPacerWaitIsCancelled: closing the announcer must not leave a goroutine
// waiting for a token.
func TestPacerWaitIsCancelled(t *testing.T) {
	p := NewPacer(1, 1)
	if err := p.Wait(context.Background(), "slow.example:80"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := p.Wait(ctx, "slow.example:80")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("cancel took %v", elapsed)
	}
}

// TestNilPacer: a session without pacing (or a test) never waits.
func TestNilPacer(t *testing.T) {
	var p *Pacer
	if err := p.Wait(context.Background(), "anything"); err != nil {
		t.Fatalf("nil pacer = %v", err)
	}
	if NewPacer(0, 8) != nil || NewPacer(4, 0) != nil {
		t.Fatal("a non-positive pacer must be nil")
	}
}
