package logging

import (
	"io"
	"sync"
	"testing"
)

type captureHandler struct {
	mu      sync.Mutex
	records []*Record
}

func (h *captureHandler) SetFormatter(Formatter) {}
func (h *captureHandler) SetLevel(Level)         {}
func (h *captureHandler) Close() error           { return nil }

func (h *captureHandler) Handle(rec *Record) {
	h.mu.Lock()
	h.records = append(h.records, rec)
	h.mu.Unlock()
}

// TestEngineLevelDropsLowerSeverityBeforeTheHandler checks that a logger at the
// WARNING threshold never builds the DEBUG/INFO records the handler would drop,
// while ERROR and WARNING still reach it (so a counting wrapper keeps them).
func TestEngineLevelDropsLowerSeverityBeforeTheHandler(t *testing.T) {
	capture := &captureHandler{}
	SetEngineHandler(capture)
	SetEngineLevel(WARNING)
	defer SetEngineLevel(DEBUG)

	l := NewEngineLogger("test")
	l.Debugf("dropped %d", 1)
	l.Infof("dropped %d", 2)
	l.Warningf("kept %d", 3)
	l.Errorf("kept %d", 4)

	if len(capture.records) != 2 {
		t.Fatalf("records = %d, want 2 (warning+error)", len(capture.records))
	}
	if capture.records[0].Level != WARNING || capture.records[1].Level != ERROR {
		t.Fatalf("levels = %v, %v; want WARNING, ERROR", capture.records[0].Level, capture.records[1].Level)
	}
	if capture.records[0].Message != "kept 3" || capture.records[1].Message != "kept 4" {
		t.Fatalf("messages = %q, %q", capture.records[0].Message, capture.records[1].Message)
	}
}

// TestEngineLevelDebugForwardsEverything checks that at DEBUG (the daemon's
// -debug mode) no record is dropped early.
func TestEngineLevelDebugForwardsEverything(t *testing.T) {
	capture := &captureHandler{}
	SetEngineHandler(capture)
	SetEngineLevel(DEBUG)
	defer SetEngineLevel(DEBUG)

	l := NewEngineLogger("test")
	l.Debugf("kept %d", 1)
	l.Infof("kept %d", 2)
	l.Warningf("kept %d", 3)
	l.Errorf("kept %d", 4)

	if len(capture.records) != 4 {
		t.Fatalf("records = %d, want 4", len(capture.records))
	}
}

func BenchmarkEngineLoggerFiltered(b *testing.B) {
	SetEngineHandler(NewWriterHandler(io.Discard))
	SetEngineLevel(WARNING)
	defer SetEngineLevel(DEBUG)
	l := NewEngineLogger("bench")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Debugf("block %d from %s", i, "peer")
	}
}

func BenchmarkEngineLoggerForwarded(b *testing.B) {
	SetEngineHandler(NewWriterHandler(io.Discard))
	SetEngineLevel(DEBUG)
	defer SetEngineLevel(DEBUG)
	l := NewEngineLogger("bench")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Debugf("block %d from %s", i, "peer")
	}
}
