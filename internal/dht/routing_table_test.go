package dht

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestPingSlowlyStopsWithoutReader locks the shutdown fix: when the run loop is
// gone (stop closed) and nobody reads pingRequest, pingSlowly must return
// instead of blocking on the send (which made DHT.Stop hang forever).
func TestPingSlowlyStopsWithoutReader(t *testing.T) {
	pingRequest := make(chan *remoteNode) // never read
	stop := make(chan bool)
	close(stop)

	done := make(chan struct{})
	go func() {
		pingSlowly(pingRequest, []*remoteNode{{}, {}}, time.Minute, stop)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pingSlowly did not return after stop")
	}
}

// TestRoutingTableMaxNodesFlood exercises the central admission point used by
// every discovery path: the first maxNodes entries stay, duplicates remain
// idempotent, and a flood of new addresses cannot grow the book or randomly
// evict an admitted node.
func TestRoutingTableMaxNodesFlood(t *testing.T) {
	const (
		maxNodes = 8
		flood    = 1024
	)
	var log DebugLogger = &nullLogger{}
	rt := newRoutingTable(&log, nil)
	rt.maxNodes = maxNodes

	accepted := make([]*remoteNode, 0, maxNodes)
	for i := 0; i < flood; i++ {
		n := newRemoteNode(net.UDPAddr{
			IP:   net.IPv4(10, byte(i>>16), byte(i>>8), byte(i)),
			Port: 6881,
		}, fmt.Sprintf("%020d", i), &log)
		err := rt.insert(n, "udp4")
		if i < maxNodes {
			if err != nil {
				t.Fatalf("insert(%d) before cap: %v", i, err)
			}
			accepted = append(accepted, n)
		} else if !errors.Is(err, errRoutingTableFull) {
			t.Fatalf("insert(%d) after cap = %v, want errRoutingTableFull", i, err)
		}
		want := i + 1
		if want > maxNodes {
			want = maxNodes
		}
		if got := rt.length(); got != want {
			t.Fatalf("after insert(%d): length = %d, want %d", i, got, want)
		}
	}
	if rt.length() != maxNodes {
		t.Fatalf("flood grew routing table to %d nodes, want %d", rt.length(), maxNodes)
	}
	for _, n := range accepted {
		if rt.addresses[n.address.String()] != n {
			t.Fatalf("admitted node %v was evicted by the flood", n.address)
		}
		if err := rt.insert(n, "udp4"); err != nil {
			t.Fatalf("duplicate insert(%v) at cap: %v", n.address, err)
		}
		got, err := rt.getOrCreateNode("", n.address.String(), "udp4")
		if err != nil || got != n {
			t.Fatalf("getOrCreateNode(%v) = (%p, %v), want admitted node %p", n.address, got, err, n)
		}
	}

	// A killed node releases its slot; the next new node is admitted without
	// touching any of the other deterministic admissions.
	rt.kill(accepted[0], newPeerStore(10, 10))
	replacement := newRemoteNode(net.UDPAddr{IP: net.IPv4(10, 1, 0, 1), Port: 6882}, fmt.Sprintf("%020d", flood), &log)
	if err := rt.insert(replacement, "udp4"); err != nil {
		t.Fatalf("insert after freeing a slot: %v", err)
	}
	if got := rt.length(); got != maxNodes {
		t.Fatalf("length after replacement = %d, want %d", got, maxNodes)
	}
	for _, n := range accepted[1:] {
		if rt.addresses[n.address.String()] != n {
			t.Fatalf("replacement evicted admitted node %v", n.address)
		}
	}
}

func TestRoutingTableMaxNodesDisabled(t *testing.T) {
	var log DebugLogger = &nullLogger{}
	for _, limit := range []int{0, -1} {
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			rt := newRoutingTable(&log, nil)
			rt.maxNodes = limit
			for i := 0; i < 16; i++ {
				n := newRemoteNode(net.UDPAddr{IP: net.IPv4(192, 0, 2, byte(i+1)), Port: 6881}, fmt.Sprintf("%020d", i), &log)
				if err := rt.insert(n, "udp4"); err != nil {
					t.Fatalf("insert(%d) with cap disabled: %v", i, err)
				}
			}
			if got := rt.length(); got != 16 {
				t.Fatalf("length = %d, want 16 with cap disabled", got)
			}
		})
	}
}

func TestMakeNodeAppliesRoutingTableLimits(t *testing.T) {
	cfg := NewConfig()
	cfg.MaxNodes = 7
	cfg.MaxNodesPerSubnet = 3
	d := makeNode(*cfg)
	defer d.clientThrottle.Stop()

	if got := d.routingTable.maxNodes; got != cfg.MaxNodes {
		t.Fatalf("routing table maxNodes = %d, want %d", got, cfg.MaxNodes)
	}
	if got := d.routingTable.maxPerSubnet; got != cfg.MaxNodesPerSubnet {
		t.Fatalf("routing table maxPerSubnet = %d, want %d", got, cfg.MaxNodesPerSubnet)
	}
}
