package dht

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// hangingResolver returns a resolver that waits until release is called, like
// one that does not answer, and the release function.
func hangingResolver(t *testing.T) (ipLookup, func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return func(ctx context.Context, host string) ([]net.IPAddr, error) {
		select {
		case <-gate:
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, release
}

// TestDHTLoopNeverWaitsOnDNS reproduces the gx-torrent crash: with routers
// given by name and a resolver that hangs, the loop must keep serving and
// AddNode must never block its caller (a torrent's run loop), even with the
// queue full.
func TestDHTLoopNeverWaitsOnDNS(t *testing.T) {
	lookup, release := hangingResolver(t)
	cfg := NewConfig()
	cfg.SaveRoutingTable = false
	cfg.Port = 0
	cfg.Address = "127.0.0.1"
	cfg.DHTRouters = "router.invalid:6881,other.invalid:6881"
	node, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	node.lookupIP = lookup
	if err := node.Start(); err != nil {
		t.Fatal(err)
	}
	defer node.Stop()

	// The table is empty, so a peers request falls back to the routers: the
	// old code resolved them here, on the loop.
	node.PeersRequest(string(make([]byte, 20)), true)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			node.AddNode("10.0.0.1:" + strconv.Itoa(1000+i))
			node.AddNode("peer.invalid:6881")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AddNode blocked its caller while the resolver hangs")
	}

	port := make(chan int, 1)
	go func() { port <- node.Port() }()
	select {
	case <-port:
	case <-time.After(5 * time.Second):
		t.Fatal("the DHT loop stopped serving while a router name was being resolved")
	}
	release()
}

// TestRoutersResolvedOffTheLoop checks the router list: literal addresses need
// no resolution, names come back resolved through routersArrived, and a failed
// resolution is retried later rather than at every call.
func TestRoutersResolvedOffTheLoop(t *testing.T) {
	if got, ok := literalRouters("1.2.3.4:6881, [::1]:6881"); !ok || len(got) != 2 {
		t.Fatalf("literal routers = %v, %v", got, ok)
	}
	if _, ok := literalRouters("router.example:6881,1.2.3.4:6881"); ok {
		t.Fatal("a name among the routers needs a resolution")
	}

	lookup := func(ctx context.Context, host string) ([]net.IPAddr, error) {
		if host == "dead.invalid" {
			return nil, &net.DNSError{Err: "no such host", Name: host}
		}
		return []net.IPAddr{{IP: net.ParseIP("2001:db8::1")}, {IP: net.ParseIP("192.0.2.7")}}, nil
	}
	got := resolveRouters(lookup, "router.example:6881,dead.invalid:6881,198.51.100.1:6881", "udp4")
	if len(got) != 2 || got[0] != "192.0.2.7:6881" || got[1] != "198.51.100.1:6881" {
		t.Fatalf("resolveRouters = %v", got)
	}

	d := newTestDHT()
	d.stop = make(chan bool)
	d.routersArrived(nil)
	if d.routersRetryAt.IsZero() || len(d.routerAddrs) != 0 {
		t.Fatal("a failed resolution must schedule a retry and keep no router")
	}
}
