package source

import (
	"net"
	"testing"
	"time"
)

func TestAddDedupeAndAvailable(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	list := New(10)
	if !list.Add(Source{IP: net.IPv4(8, 8, 8, 8), TCPPort: 4662, Origin: "kad"}, now) {
		t.Fatal("valid source rejected")
	}
	if !list.Add(Source{IP: net.IPv4(8, 8, 8, 8), TCPPort: 4662, UDPPort: 4672, Origin: "server"}, now.Add(time.Minute)) {
		t.Fatal("refresh rejected")
	}
	if list.Len() != 1 {
		t.Fatalf("len = %d", list.Len())
	}
	available := list.Available(now.Add(time.Minute))
	if len(available) != 1 || available[0].UDPPort != 4672 || available[0].Origin != "server" {
		t.Fatalf("available = %+v", available)
	}
	if list.Add(Source{IP: net.IPv4(10, 0, 0, 1), TCPPort: 4662}, now) {
		t.Fatal("private IP must be rejected")
	}
}

func TestFailBackoffAndSuccess(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	list := New(10)
	ip := net.IPv4(8, 8, 4, 4)
	list.Add(Source{IP: ip, TCPPort: 4662}, now)
	list.Fail(ip, 4662, now)
	if got := list.Available(now.Add(29 * time.Second)); len(got) != 0 {
		t.Fatalf("backoff too short: %+v", got)
	}
	if got := list.Available(now.Add(31 * time.Second)); len(got) != 1 {
		t.Fatalf("backoff should expire: %+v", got)
	}
	list.Fail(ip, 4662, now)
	list.Fail(ip, 4662, now)
	if got := list.Available(now.Add(90 * time.Second)); len(got) != 0 {
		t.Fatalf("third failure needs more backoff: %+v", got)
	}
	list.Success(ip, 4662, now.Add(2*time.Minute))
	if got := list.Available(now.Add(2 * time.Minute)); len(got) != 1 {
		t.Fatalf("success must clear backoff: %+v", got)
	}
}

func TestEvictionAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	list := New(2)
	list.Add(Source{IP: net.IPv4(8, 8, 8, 8), TCPPort: 1}, now)
	list.Add(Source{IP: net.IPv4(8, 8, 4, 4), TCPPort: 2}, now.Add(time.Minute))
	list.Add(Source{IP: net.IPv4(1, 1, 1, 1), TCPPort: 3}, now.Add(2*time.Minute))
	if list.Len() != 2 {
		t.Fatalf("cap not enforced: %d", list.Len())
	}
	if got := list.Available(now.Add(2 * time.Minute)); len(got) != 2 || got[0].TCPPort != 3 {
		t.Fatalf("oldest should be evicted: %+v", got)
	}
	if removed := list.Expire(time.Hour, now.Add(2*time.Hour)); removed != 2 {
		t.Fatalf("expired = %d", removed)
	}
}
