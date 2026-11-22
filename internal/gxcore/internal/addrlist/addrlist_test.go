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

// BenchmarkAddrListPush measures a PEX-sized push into a large pool. The tree
// degree is deliberately wide: degree 2 cloned a node at every level.
func BenchmarkAddrListPush(b *testing.B) {
	addrs := make([]*net.TCPAddr, 50)
	for i := range addrs {
		addrs[i] = &net.TCPAddr{IP: net.IPv4(10, 0, byte(i/250), byte(i)), Port: 6881 + i}
	}
	var clientIP net.IP
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		list := New(2000, nil, 6881, &clientIP)
		b.StartTimer()
		list.Push(addrs, peersource.PEX)
	}
}

// TestPreferLocalPopsLANFirst: with the preference on, an address on our own
// network comes out of the list before a remote one with a higher BEP 40
// priority; with it off, BEP 40 decides.
func TestPreferLocalPopsLANFirst(t *testing.T) {
	remote := &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 6881}
	local := &net.TCPAddr{IP: net.ParseIP("192.168.1.50"), Port: 6881}
	clientIP := net.ParseIP("203.0.113.1")

	preferred := New(10, nil, 6881, &clientIP, true)
	preferred.Push([]*net.TCPAddr{remote, local}, peersource.Tracker)
	if got, _ := preferred.Pop(); got == nil || got.IP.String() != local.IP.String() {
		t.Fatalf("popped %v, want the LAN peer first", got)
	}
	// The remote peer is still there: the preference moves one candidate to the
	// front, it does not drop the others.
	if got, _ := preferred.Pop(); got == nil || got.IP.String() != remote.IP.String() {
		t.Fatalf("second pop %v, want the remote peer", got)
	}

	plain := New(10, nil, 6881, &clientIP)
	plain.Push([]*net.TCPAddr{remote, local}, peersource.Tracker)
	// BEP 40 alone: both are offered, no panic, and the local one is not forced
	// to the front (the order only depends on the hash).
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		if got, _ := plain.Pop(); got != nil {
			seen[got.IP.String()] = true
		}
	}
	if !seen[local.IP.String()] || !seen[remote.IP.String()] {
		t.Fatalf("without the preference the list lost a peer: %v", seen)
	}
}
