package dht

import (
	"fmt"
	"net"
	"testing"
)

func TestSubnetKey(t *testing.T) {
	cases := []struct{ ip, want string }{
		{"1.2.3.4", "1.2.3.0"},
		{"1.2.3.250", "1.2.3.0"},
		{"1.2.4.1", "1.2.4.0"},
		{"2001:db8:1:2::1", "2001:db8:1::"},
	}
	for _, c := range cases {
		if got := subnetKey(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("subnetKey(%s) = %q, want %q", c.ip, got, c.want)
		}
	}
	if subnetKey(nil) != "" {
		t.Error("nil IP must have an empty key")
	}
}

// TestTableMetricsConcentration checks the Sybil-visibility counters: the
// largest number of tree nodes sharing a subnet and the insecure-ID count.
func TestTableMetricsConcentration(t *testing.T) {
	d := newTestDHT()
	for _, hp := range []string{"1.2.3.4:1", "1.2.3.5:2", "1.2.3.6:3"} {
		ip, _, _ := net.SplitHostPort(hp)
		d.insertTestNode(t, hp, string(generateSecureNodeID(net.ParseIP(ip))), "udp4")
	}
	d.insertTestNode(t, "9.9.9.9:4", string(generateSecureNodeID(net.ParseIP("9.9.9.9"))), "udp4")
	// An insecure ID on a distinct subnet.
	d.insertTestNode(t, "8.8.8.8:5", "aaaaaaaaaaaaaaaaaaaa", "udp4")

	maxShare, insecure := d.tableMetrics()
	if maxShare != 3 {
		t.Errorf("maxShare = %d, want 3", maxShare)
	}
	if insecure != 1 {
		t.Errorf("insecure = %d, want 1", insecure)
	}
}

// TestSubnetCapKeepsTreeBounded: with a cap of 2, a third node from the same
// /24 stays known (so it is not re-discovered in a loop) but gets no tree slot.
func TestSubnetCapKeepsTreeBounded(t *testing.T) {
	d := newTestDHT()
	d.routingTable.maxPerSubnet = 2
	for i := 0; i < 4; i++ {
		hp := fmt.Sprintf("1.2.3.%d:%d", 10+i, 7000+i)
		ip, _, _ := net.SplitHostPort(hp)
		d.insertTestNode(t, hp, string(generateSecureNodeID(net.ParseIP(ip))), "udp4")
	}
	if maxShare, _ := d.tableMetrics(); maxShare != 2 {
		t.Errorf("subnet share = %d, want 2 (cap)", maxShare)
	}
	if got := len(d.routingTable.addresses); got != 4 {
		t.Errorf("addresses = %d, want 4 (kept, no re-discovery loop)", got)
	}
	noTree := 0
	for _, n := range d.routingTable.addresses {
		if n.noTree {
			noTree++
		}
	}
	if noTree != 2 {
		t.Errorf("noTree nodes = %d, want 2", noTree)
	}
}

// recomputedSubnetCounts rebuilds the per-subnet counts from the address book
// using the slot invariant, independently of the incremental counter.
func recomputedSubnetCounts(rt *routingTable) map[string]int {
	out := map[string]int{}
	for _, n := range rt.addresses {
		if rt.hasSubnetSlot(n) {
			out[subnetKey(n.address.IP)]++
		}
	}
	return out
}

func assertSubnetCountsMatch(t *testing.T, rt *routingTable) {
	t.Helper()
	want := recomputedSubnetCounts(rt)
	for k, v := range want {
		if rt.subnetNodes[k] != v {
			t.Fatalf("subnet %s = %d, want %d (counter %v, want %v)", k, rt.subnetNodes[k], v, rt.subnetNodes, want)
		}
	}
	for k, v := range rt.subnetNodes {
		if want[k] != v {
			t.Fatalf("stale subnet %s = %d, want %d (counter %v, want %v)", k, v, want[k], rt.subnetNodes, want)
		}
	}
	for _, n := range rt.addresses {
		if n.countedSubnet != rt.hasSubnetSlot(n) {
			t.Fatalf("node %s countedSubnet=%v, slot=%v", n.address.String(), n.countedSubnet, rt.hasSubnetSlot(n))
		}
	}
}

// TestSubnetCounterTracksInsertUpdateKill keeps the O(1) per-subnet counter in
// sync with a brute-force recount across insert, update (an unknown node later
// identified, and a capped node re-evaluated) and kill.
func TestSubnetCounterTracksInsertUpdateKill(t *testing.T) {
	d := newTestDHT()
	d.routingTable.maxPerSubnet = 2
	for i := 0; i < 3; i++ { // two get a slot, the third is capped
		ip := fmt.Sprintf("1.2.3.%d", 10+i)
		d.insertTestNode(t, fmt.Sprintf("%s:%d", ip, 7000+i), string(generateSecureNodeID(net.ParseIP(ip))), "udp4")
	}
	d.insertTestNode(t, "9.9.9.9:1", string(generateSecureNodeID(net.ParseIP("9.9.9.9"))), "udp4")
	assertSubnetCountsMatch(t, d.routingTable)

	// A node learned by address only, then identified: it gains a slot and must
	// enter the counter (it never had one).
	d.insertTestNode(t, "7.7.7.7:5", "", "udp4")
	assertSubnetCountsMatch(t, d.routingTable)
	var unknown *remoteNode
	for _, n := range d.routingTable.addresses {
		if n.address.IP.String() == "7.7.7.7" {
			unknown = n
		}
	}
	if unknown == nil {
		t.Fatal("node 7.7.7.7 not found")
	}
	unknown.id = string(generateSecureNodeID(net.ParseIP("7.7.7.7")))
	if err := d.routingTable.update(unknown, "udp4"); err != nil {
		t.Fatalf("update: %v", err)
	}
	assertSubnetCountsMatch(t, d.routingTable)
	if d.routingTable.subnetNodes["7.7.7.0"] != 1 {
		t.Fatalf("7.7.7.0 count = %d, want 1", d.routingTable.subnetNodes["7.7.7.0"])
	}

	// Re-evaluating a capped node must not add it to the counter.
	for _, n := range d.routingTable.addresses {
		if n.noTree {
			if err := d.routingTable.update(n, "udp4"); err != nil {
				t.Fatalf("update capped: %v", err)
			}
		}
	}
	assertSubnetCountsMatch(t, d.routingTable)

	// Killing a node with a slot drops the subnet count.
	for _, n := range d.routingTable.addresses {
		if !n.noTree && subnetKey(n.address.IP) == "1.2.3.0" {
			d.routingTable.kill(n, d.peerStore)
			break
		}
	}
	assertSubnetCountsMatch(t, d.routingTable)
	if d.routingTable.subnetNodes["1.2.3.0"] != 1 {
		t.Fatalf("1.2.3.0 count = %d, want 1 after kill", d.routingTable.subnetNodes["1.2.3.0"])
	}
}
