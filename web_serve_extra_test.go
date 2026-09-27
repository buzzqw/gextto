package gextto

import (
	"net"
	"testing"
)

func TestListenConflicts(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen busy: %v", err)
	}
	defer busy.Close()

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen probe: %v", err)
	}
	freeAddress := probe.Addr().String()
	_ = probe.Close()

	cfg := DefaultConfig()
	cfg.Listen = busy.Addr().String()
	cfg.EngineListen = freeAddress
	conflicts := ListenConflicts(&cfg)
	if len(conflicts) != 1 || conflicts[0] != cfg.Listen {
		t.Fatalf("conflicts = %v, want [%s]", conflicts, cfg.Listen)
	}

	// No conflict at all: the warning must stay silent.
	cfg.Listen = freeAddress
	if conflicts := ListenConflicts(&cfg); len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %v", conflicts)
	}
}
