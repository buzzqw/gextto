package addrlist

import (
	"net"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerpriority"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
)

// TestPushKeepsPeersWithTheSamePriority: with our own IP unknown the BEP 40 mask
// hides bits of the last byte, so these four neighbours fall on two priorities.
// All four must stay dialable.
func TestPushKeepsPeersWithTheSamePriority(t *testing.T) {
	var clientIP net.IP
	list := New(100, nil, 6881, &clientIP)
	var addrs []*net.TCPAddr
	for _, last := range []byte{20, 21, 22, 23} {
		addrs = append(addrs, &net.TCPAddr{IP: net.IPv4(172, 30, 0, last), Port: 6881})
	}
	self := &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 6881}
	if peerpriority.Calculate(addrs[0], self) != peerpriority.Calculate(addrs[2], self) {
		t.Fatal("the test needs two addresses with the same priority")
	}
	list.Push(addrs, peersource.Tracker)
	if got := list.Len(); got != 4 {
		t.Fatalf("%d candidates after pushing 4 addresses", got)
	}
	seen := map[string]bool{}
	for {
		addr, _ := list.Pop()
		if addr == nil {
			break
		}
		seen[addr.String()] = true
	}
	if len(seen) != 4 {
		t.Fatalf("popped %v, want all 4 addresses", seen)
	}
}

// TestPushSameIPDifferentPorts: two clients behind one IP are two peers.
func TestPushSameIPDifferentPorts(t *testing.T) {
	var clientIP net.IP
	list := New(100, nil, 6881, &clientIP)
	ip := net.IPv4(203, 0, 113, 7)
	list.Push([]*net.TCPAddr{{IP: ip, Port: 1000}, {IP: ip, Port: 2000}}, peersource.Tracker)
	if got := list.Len(); got != 2 {
		t.Fatalf("%d candidates, want 2", got)
	}
}

// TestPushTwiceRefreshes: the same address pushed again replaces its entry,
// across sources and across the IPv4 and IPv6 forms of the same IP.
func TestPushTwiceRefreshes(t *testing.T) {
	var clientIP net.IP
	list := New(100, nil, 6881, &clientIP)
	addr := &net.TCPAddr{IP: net.IPv4(198, 51, 100, 9), Port: 51413}
	list.Push([]*net.TCPAddr{addr}, peersource.Tracker)
	list.Push([]*net.TCPAddr{{IP: addr.IP.To4(), Port: addr.Port}}, peersource.DHT)
	if got := list.Len(); got != 1 {
		t.Fatalf("%d candidates, want 1", got)
	}
	if got := list.LenSource(peersource.Tracker); got != 0 {
		t.Fatalf("tracker count %d after the DHT replaced the entry", got)
	}
	if got := list.LenSource(peersource.DHT); got != 1 {
		t.Fatalf("DHT count %d, want 1", got)
	}
	if popped, _ := list.Pop(); popped == nil || popped.Port != addr.Port {
		t.Fatalf("popped %v", popped)
	}
	if popped, _ := list.Pop(); popped != nil {
		t.Fatalf("a second entry was left: %v", popped)
	}
}
