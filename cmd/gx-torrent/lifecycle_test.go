package main

import (
	"testing"

	clog "github.com/cenkalti/log"
)

// captureHandler records what the filter forwarded, with the final level.
type captureHandler struct {
	records []*clog.Record
}

func (h *captureHandler) SetFormatter(clog.Formatter) {}
func (h *captureHandler) SetLevel(clog.Level)         {}
func (h *captureHandler) Close() error                { return nil }
func (h *captureHandler) Handle(rec *clog.Record)     { h.records = append(h.records, rec) }

func TestSwarmNoiseFilterDemotesAndCounts(t *testing.T) {
	capture := &captureHandler{}
	filter := &swarmNoiseFilter{Handler: capture}

	before := peerNoiseCounters.totals()["handshake"]
	filter.Handle(&clog.Record{
		LoggerName: "peer -> 1.2.3.4:6881",
		Level:      clog.ERROR,
		Message:    "cannot complete outgoing handshake: context deadline exceeded",
	})
	if len(capture.records) != 1 {
		t.Fatalf("records = %d, want 1", len(capture.records))
	}
	if capture.records[0].Level != clog.DEBUG {
		t.Fatalf("peer noise level = %v, want DEBUG (demoted)", capture.records[0].Level)
	}
	if got := peerNoiseCounters.totals()["handshake"]; got != before+1 {
		t.Fatalf("handshake counter = %d, want %d", got, before+1)
	}

	// A real daemon record must pass through unchanged and uncounted.
	capture.records = nil
	total := len(peerNoiseCounters.totals())
	filter.Handle(&clog.Record{
		LoggerName: "session",
		Level:      clog.ERROR,
		Message:    "cannot reopen the session",
	})
	if len(capture.records) != 1 || capture.records[0].Level != clog.ERROR {
		t.Fatalf("daemon error was altered: %+v", capture.records)
	}
	if len(peerNoiseCounters.totals()) != total {
		t.Fatal("daemon error was counted as swarm noise")
	}
}

func TestSwarmNoiseCategory(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{"cannot complete outgoing handshake: i/o timeout", "handshake"},
		{"peer reset", "reset"},
		{"timed out waiting for ack", "ack_timeout"},
		{"peerreader.go:98 i/o timeout", "io_timeout"},
		{"cannot write message [piece]: closed", "write"},
		{"announce error: *url.Error: ... ip is blocked", "tracker"},
		{"something else entirely", "other"},
	}
	for _, test := range cases {
		if got := swarmNoiseCategory(&clog.Record{Message: test.message}); got != test.want {
			t.Errorf("category(%q) = %q, want %q", test.message, got, test.want)
		}
	}
}

func TestFormatPeerNoise(t *testing.T) {
	if got := formatPeerNoise(nil); got != "" {
		t.Fatalf("empty deltas = %q, want empty", got)
	}
	got := formatPeerNoise(map[string]int64{"handshake": 3, "reset": 1})
	if got != "handshake=3 reset=1" {
		t.Fatalf("formatPeerNoise = %q", got)
	}
}
