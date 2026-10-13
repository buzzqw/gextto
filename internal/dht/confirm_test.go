package dht

import (
	"net"
	"testing"
	"time"
)

// TestPeerConfirmCountsDistinctReporters: a peer counts once when a second
// distinct node reports it, and further reporters or repeats add nothing.
func TestPeerConfirmCountsDistinctReporters(t *testing.T) {
	d := newTestDHT()
	d.confirm = map[peerConfirmKey]*peerConfirmEntry{}
	ih := InfoHash("aaaaaaaaaaaaaaaaaaaa")

	before := peersConfirmedMulti.Value()
	d.notePeerReport(ih, "peer1", "nodeA")
	d.notePeerReport(ih, "peer1", "nodeA") // same reporter: no confirmation
	if got := peersConfirmedMulti.Value(); got != before {
		t.Fatalf("same reporter confirmed: %d → %d", before, got)
	}
	d.notePeerReport(ih, "peer1", "nodeB")
	if got := peersConfirmedMulti.Value(); got != before+1 {
		t.Fatalf("second reporter did not confirm: %d", got)
	}
	d.notePeerReport(ih, "peer1", "nodeC") // a third distinct reporter
	if got := peersConfirmedMulti.Value(); got != before+1 {
		t.Fatalf("third reporter double counted: %d", got)
	}
	// A different peer with a single reporter is not confirmed.
	d.notePeerReport(ih, "peer2", "nodeA")
	if got := peersConfirmedMulti.Value(); got != before+1 {
		t.Fatalf("single-report peer counted: %d", got)
	}
}

// TestPeerConfirmIsBounded: the tracker never grows past peerConfirmMax.
func TestPeerConfirmIsBounded(t *testing.T) {
	d := newTestDHT()
	d.confirm = map[peerConfirmKey]*peerConfirmEntry{}
	ih := InfoHash("bbbbbbbbbbbbbbbbbbbb")
	for i := 0; i < peerConfirmMax+50; i++ {
		d.notePeerReport(ih, string(rune(i%128))+string(rune('0'+i%10)), "node")
	}
	if len(d.confirm) > peerConfirmMax {
		t.Fatalf("tracker grew to %d, cap is %d", len(d.confirm), peerConfirmMax)
	}
}

// TestPeerConfirmExpiry: stale entries are dropped by the cleanup tick.
func TestPeerConfirmExpiry(t *testing.T) {
	d := newTestDHT()
	d.confirm = map[peerConfirmKey]*peerConfirmEntry{}
	ih := InfoHash("cccccccccccccccccccc")
	d.notePeerReport(ih, "p", "n")
	if len(d.confirm) != 1 {
		t.Fatalf("entry not stored: %d", len(d.confirm))
	}
	d.expirePeerConfirm(time.Now().Add(peerConfirmWindow + time.Second))
	if len(d.confirm) != 0 {
		t.Fatalf("entry not expired: %d", len(d.confirm))
	}
}

// TestReporterKeyIsTheSubnet: two nodes of the same /24 (or one host on two
// ports) are one path; a node of another /24 is a second one.
func TestReporterKeyIsTheSubnet(t *testing.T) {
	a := &remoteNode{address: net.UDPAddr{IP: net.ParseIP("203.0.113.5"), Port: 6881}}
	b := &remoteNode{address: net.UDPAddr{IP: net.ParseIP("203.0.113.77"), Port: 7000}}
	c := &remoteNode{address: net.UDPAddr{IP: net.ParseIP("198.51.100.5"), Port: 6881}}
	if reporterKey(a) != reporterKey(b) {
		t.Fatal("same /24 counted as two paths")
	}
	if reporterKey(a) == reporterKey(c) {
		t.Fatal("different subnets counted as one path")
	}
}
