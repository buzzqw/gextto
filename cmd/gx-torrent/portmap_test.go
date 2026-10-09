package main

import (
	"net"
	"testing"
	"time"
)

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
