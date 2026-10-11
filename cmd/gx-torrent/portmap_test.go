package main

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestNATHint(t *testing.T) {
	if got := natHint(portCheck{Port: 6881, ReflectedPort: 6881}); !strings.Contains(got, "conserva") {
		t.Errorf("port-preserving hint = %q, want conserva", got)
	}
	if got := natHint(portCheck{Port: 6881, ReflectedPort: 40000}); !strings.Contains(got, "riscrive") {
		t.Errorf("port-rewriting hint = %q, want riscrive", got)
	}
	if got := natHint(portCheck{Port: 6881, ReflectedPort: 6881, NATMapping: "cone"}); !strings.Contains(got, "endpoint-independent") {
		t.Errorf("cone hint = %q", got)
	}
	if got := natHint(portCheck{Port: 6881, ReflectedPort: 40000, NATMapping: "symmetric"}); !strings.Contains(got, "address/port-dependent") {
		t.Errorf("symmetric hint = %q", got)
	}
}

func TestPortCheckLocalListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if !checkLocalListener(port) {
		t.Fatal("an open listener was not detected")
	}
	if checkLocalListener(0) {
		t.Fatal("port 0 must not be considered open")
	}
	_ = listener.Close()
	if checkLocalListener(port) {
		t.Fatal("a closed listener was reported open")
	}
}

func TestPortCheckNilMapper(t *testing.T) {
	var mapper *portMapper
	got := mapper.check()
	if got.Open || got.Listening || got.Mapped {
		t.Fatalf("nil mapper = %+v, want zero", got)
	}
}

func TestPortMapperCheckRecentMapping(t *testing.T) {
	mapper := newPortMapper(6881, false, true)
	mapper.mu.Lock()
	mapper.method = "natpmp"
	mapper.externalIP = "1.2.3.4"
	mapper.mappedAt = time.Now()
	mapper.mu.Unlock()
	got := mapper.check()
	if !got.Mapped {
		t.Fatalf("a recent NAT-PMP mapping was not detected: %+v", got)
	}
	if got.Method != "natpmp" || got.ExternalIP != "1.2.3.4" {
		t.Fatalf("status not copied: %+v", got)
	}
}

func TestPortMapperLifecycle(t *testing.T) {
	// Case 1: Close without start must return immediately without blocking.
	m1 := newPortMapper(6881, true, true)
	m1.close()

	// Case 2: Start with disabled mapping or port 0 must close cleanly.
	m2 := newPortMapper(0, false, false)
	m2.start()
	m2.start() // idempotent
	m2.close()
}
