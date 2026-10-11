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
