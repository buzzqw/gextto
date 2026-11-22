package dht

import (
	"net"
	"testing"
)

// BenchmarkProcessPacketBlocked measures the guard on ingress: a filtered
// packet is dropped with a map lookup and an atomic increment, before the
// bencode screen and the rate limiter. The no-filter case pays one nil check.
func BenchmarkProcessPacketBlocked(b *testing.B) {
	blocked := net.ParseIP("203.0.113.7")
	newNode := func(filter func(net.IP) bool) *DHT {
		cfg := NewConfig()
		cfg.Port = 0
		cfg.RateLimit = -1
		cfg.IPFilter = filter
		d, err := New(cfg)
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		return d
	}
	d := newNode(func(ip net.IP) bool { return ip != nil && ip.Equal(blocked) })
	p := packetType{b: []byte("x"), raddr: net.UDPAddr{IP: blocked, Port: 1}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d.processPacket(p)
	}
}

func BenchmarkProcessPacketNoFilter(b *testing.B) {
	cfg := NewConfig()
	cfg.Port = 0
	cfg.RateLimit = -1
	d, err := New(cfg)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	// Source 203.0.113.9 with a one-byte payload: the packet is not a
	// query, so it leaves right after the header check.
	p := packetType{b: []byte("x"), raddr: net.UDPAddr{IP: net.ParseIP("203.0.113.9"), Port: 1}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d.processPacket(p)
	}
}

// TestIPFilterCoversTheNode: a blocked address never enters the routing table,
// its packets are dropped before parsing and nothing is written back to it. The
// engine wires the session blocklist here, so the operator's list stops the DHT
// as it stops peers (libtorrent 2.1.1: "apply IP filter to DHT node").
func TestIPFilterCoversTheNode(t *testing.T) {
	blocked := net.ParseIP("203.0.113.7")
	allowed := net.ParseIP("198.51.100.9")

	cfg := NewConfig()
	cfg.Port = 0
	cfg.RateLimit = -1
	cfg.IPFilter = func(ip net.IP) bool { return ip != nil && ip.Equal(blocked) }

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.blocked(allowed) {
		t.Fatal("an allowed address must pass the filter")
	}
	if !d.blocked(blocked) {
		t.Fatal("a blocked address must be refused")
	}

	// Nothing enters the table: not from a node the network names, not from
	// a name to resolve.
	node := newRemoteNode(net.UDPAddr{IP: blocked, Port: 6881}, "", &d.DebugLogger)
	if err := d.routingTable.insert(node, "udp4"); err == nil {
		t.Fatal("a filtered address was inserted into the routing table")
	}
	if _, err := d.routingTable.getOrCreateNode("", net.JoinHostPort(blocked.String(), "6881"), "udp4"); err == nil {
		t.Fatal("a filtered address was resolved and inserted")
	}
	okNode := newRemoteNode(net.UDPAddr{IP: allowed, Port: 6881}, "", &d.DebugLogger)
	if err := d.routingTable.insert(okNode, "udp4"); err != nil {
		t.Fatalf("an allowed address was refused: %v", err)
	}

	// Ingress: the packet is dropped before the bencode screen runs (the
	// payload is not even valid bencode) and the counter moves.
	before := totalPacketsFiltered.Value()
	d.processPacket(packetType{b: []byte("not bencode"), raddr: net.UDPAddr{IP: blocked, Port: 1234}})
	if got := totalPacketsFiltered.Value(); got != before+1 {
		t.Fatalf("filtered packets = %d, want %d", got, before+1)
	}

	// A nil filter (the default) leaves everything as it was.
	plainCfg := NewConfig()
	plainCfg.Port = 0
	plain, err := New(plainCfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if plain.blocked(blocked) {
		t.Fatal("without a filter nothing is blocked")
	}
}
