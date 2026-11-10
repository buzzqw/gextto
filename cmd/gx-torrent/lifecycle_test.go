package main

import (
	"strings"
	"testing"
	"time"

	clog "github.com/buzzqw/gextto/internal/gxcore/logging"
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

// TestRepeatedRecordFilterWritesTheFirstAndCountsTheRest reproduces the flood
// seen in the field: one failed file allocation printed 874 lines in a second.
func TestRepeatedRecordFilterWritesTheFirstAndCountsTheRest(t *testing.T) {
	capture := &captureHandler{}
	filter := newRepeatedRecordFilter(capture)
	clock := time.Unix(1_700_000_000, 0)
	filter.now = func() time.Time { return clock }

	record := func() {
		filter.Handle(&clog.Record{
			LoggerName: "torrent dda5affef899f0dbb1d0e0da0f57d45c",
			Level:      clog.ERROR,
			Message:    "file allocation error: open /data/links/dda5/E01.mkv: operation not permitted",
		})
	}
	for i := 0; i < 874; i++ {
		clock = clock.Add(time.Millisecond)
		record()
	}
	if len(capture.records) != 1 {
		t.Fatalf("records = %d, want only the first copy", len(capture.records))
	}
	dropped := filter.drain()
	if len(dropped) != 1 {
		t.Fatalf("drained %d sentences, want 1", len(dropped))
	}
	for message, count := range dropped {
		if count != 873 || !strings.Contains(message, "operation not permitted") {
			t.Fatalf("collapsed %q %d times, want the 873 copies", message, count)
		}
	}
	if len(filter.drain()) != 0 {
		t.Fatal("draining twice must not repeat the count")
	}

	// Once the window has passed the sentence is written again, so a problem
	// that goes on is never invisible for longer than a minute.
	clock = clock.Add(repeatWindow + time.Second)
	record()
	if len(capture.records) != 2 {
		t.Fatalf("records = %d, want the message written again after the window", len(capture.records))
	}
}

// TestRepeatedRecordFilterKeepsLevelsAndDistinctMessages checks what the filter
// must never do: collapse the quiet levels, or fold two different failures
// (two torrents, one sentence each) into one.
func TestRepeatedRecordFilterKeepsLevelsAndDistinctMessages(t *testing.T) {
	capture := &captureHandler{}
	filter := newRepeatedRecordFilter(capture)
	filter.now = func() time.Time { return time.Unix(1_700_000_000, 0) }

	for i := 0; i < 5; i++ {
		filter.Handle(&clog.Record{LoggerName: "session", Level: clog.INFO, Message: "checking the data"})
	}
	if len(capture.records) != 5 {
		t.Fatalf("info records = %d, want all 5", len(capture.records))
	}

	capture.records = nil
	for _, name := range []string{"torrent a", "torrent b"} {
		filter.Handle(&clog.Record{LoggerName: name, Level: clog.WARNING, Message: "announce error: timeout"})
	}
	if len(capture.records) != 2 {
		t.Fatalf("distinct torrents = %d lines, want 2", len(capture.records))
	}

	// The same sentence at two levels is two facts.
	capture.records = nil
	filter.Handle(&clog.Record{LoggerName: "session", Level: clog.ERROR, Message: "cannot open database"})
	filter.Handle(&clog.Record{LoggerName: "session", Level: clog.WARNING, Message: "cannot open database"})
	if len(capture.records) != 2 {
		t.Fatalf("levels = %d lines, want 2", len(capture.records))
	}
}

// TestFormatRepeatedRecordsIsOneSentence pins the shape of the summary: a
// person reads it, so it quotes the sentence that repeated the most and only
// counts the others.
func TestFormatRepeatedRecordsIsOneSentence(t *testing.T) {
	if got := formatRepeatedRecords(nil, peerNoiseReportEvery); got != "" {
		t.Fatalf("empty summary = %q, want nothing", got)
	}
	line := formatRepeatedRecords(map[string]int64{
		"file allocation error: open /data/links/dda5/E01.mkv: operation not permitted": 873,
		"announce error: timeout": 4,
	}, peerNoiseReportEvery)
	if !strings.HasPrefix(line, "The engine repeated the same message 873 times in the last 30 minutes") {
		t.Fatalf("summary = %q", line)
	}
	if !strings.Contains(line, "operation not permitted") || !strings.Contains(line, "(and one more sentence)") {
		t.Fatalf("summary = %q", line)
	}
	if strings.Contains(line, "announce error") {
		t.Fatalf("summary quotes the runner-up: %q", line)
	}

	long := formatRepeatedRecords(map[string]int64{strings.Repeat("x", 5000): 2}, peerNoiseReportEvery)
	if len([]rune(long)) > 400 || !strings.HasSuffix(long, "…") {
		t.Fatalf("long message was not truncated: %d runes", len([]rune(long)))
	}
}

// TestEngineLogChainCountsSwarmNoiseOnce checks the order of the two filters:
// the peer errors the counters already summarise are demoted first, so they
// never show up twice, once as noise and once as a repetition.
func TestEngineLogChainCountsSwarmNoiseOnce(t *testing.T) {
	capture := &captureHandler{}
	repeated := newRepeatedRecordFilter(capture)
	chain := &swarmNoiseFilter{Handler: repeated}

	before := peerNoiseCounters.totals()["handshake"]
	for i := 0; i < 3; i++ {
		chain.Handle(&clog.Record{
			LoggerName: "peer -> 1.2.3.4:6881",
			Level:      clog.ERROR,
			Message:    "cannot complete outgoing handshake: i/o timeout",
		})
	}
	if got := peerNoiseCounters.totals()["handshake"]; got != before+3 {
		t.Fatalf("handshake counter = %d, want %d", got, before+3)
	}
	if dropped := repeated.drain(); len(dropped) != 0 {
		t.Fatalf("demoted peer noise was also counted as a repetition: %v", dropped)
	}

	// A real error of the daemon is still collapsed line by line.
	for i := 0; i < 3; i++ {
		chain.Handle(&clog.Record{
			LoggerName: "torrent dda5",
			Level:      clog.ERROR,
			Message:    "file allocation error: operation not permitted",
		})
	}
	if dropped := repeated.drain(); dropped["file allocation error: operation not permitted"] != 2 {
		t.Fatalf("collapsed copies = %v, want 2", dropped)
	}
}

// BenchmarkRepeatedRecordFilter measures what collapsing costs per record: the
// filter runs on the engine's warning and error path, which must stay cheap
// even when a loop hammers it.
func BenchmarkRepeatedRecordFilter(b *testing.B) {
	filter := newRepeatedRecordFilter(&captureHandler{})
	record := &clog.Record{
		LoggerName: "torrent dda5affef899f0dbb1d0e0da0f57d45c",
		Level:      clog.ERROR,
		Message:    "file allocation error: open /data/links/dda5/E01.mkv: operation not permitted",
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		filter.Handle(record)
	}
}
