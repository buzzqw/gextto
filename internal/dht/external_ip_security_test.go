package dht

import (
	"net"
	"strings"
	"testing"
)

func TestNoteExternalIPRequiresIndependentConfirmation(t *testing.T) {
	resetReflected()
	t.Cleanup(resetReflected)
	d := makeNode(*NewConfig())
	originalID := d.nodeId
	raw := bep42IPField(t, "203.0.113.7", 6881)
	first := net.ParseIP("198.51.100.1")
	second := net.ParseIP("203.0.113.9")

	d.noteExternalIP(raw, first)
	if d.publicIP != nil || d.externalIPCandidate == nil {
		t.Fatal("one unconfirmed report was adopted")
	}
	if d.nodeId != originalID {
		t.Fatal("the node ID changed before confirmation")
	}
	d.noteExternalIP(raw, first)
	if d.publicIP != nil {
		t.Fatal("the same observer counted as an independent confirmation")
	}
	// A differing report cannot replace the locked candidate.
	d.noteExternalIP(bep42IPField(t, "192.0.2.44", 6881), second)
	if d.publicIP != nil || !d.externalIPCandidate.Equal(net.ParseIP("203.0.113.7")) {
		t.Fatal("a differing report replaced or confirmed the candidate")
	}

	d.noteExternalIP(raw, second)
	if !d.publicIP.Equal(net.ParseIP("203.0.113.7")) {
		t.Fatalf("adopted external IP = %v", d.publicIP)
	}
	var id [20]byte
	copy(id[:], d.nodeId)
	if !NodeIDSecure(id, d.publicIP) {
		t.Fatal("the adopted node ID is not secure for the confirmed IP")
	}
	adoptedID, adoptedIP := d.nodeId, append(net.IP(nil), d.publicIP...)
	d.noteExternalIP(bep42IPField(t, "192.0.2.45", 6881), net.ParseIP("198.51.100.2"))
	if d.nodeId != adoptedID || !d.publicIP.Equal(adoptedIP) {
		t.Fatal("external IP adoption was not one-time")
	}
}

func TestNoteExternalIPRejectsNonPublicAddresses(t *testing.T) {
	for _, rawIP := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "100.64.1.1", "::1", "fe80::1", "224.0.0.1"} {
		d := makeNode(*NewConfig())
		raw := bep42IPField(t, rawIP, 6881)
		d.noteExternalIP(raw, net.ParseIP("198.51.100.1"))
		d.noteExternalIP(raw, net.ParseIP("203.0.113.9"))
		if d.publicIP != nil || d.externalIPCandidate != nil {
			t.Errorf("non-public address %s was considered for adoption", rawIP)
		}
	}
}

func TestNoteExternalIPHonorsConfiguredAddress(t *testing.T) {
	cfg := NewConfig()
	cfg.PublicIP = net.ParseIP("8.8.8.8")
	d := makeNode(*cfg)
	d.publicIP = append(net.IP(nil), cfg.PublicIP...)
	originalID := d.nodeId
	raw := bep42IPField(t, "203.0.113.7", 6881)
	d.noteExternalIP(raw, net.ParseIP("198.51.100.1"))
	d.noteExternalIP(raw, net.ParseIP("203.0.113.9"))
	if !d.publicIP.Equal(cfg.PublicIP) || d.nodeId != originalID {
		t.Fatal("a reported address replaced the operator-configured external IP")
	}
}

func TestUnmatchedResponseCannotInfluenceExternalIP(t *testing.T) {
	resetReflected()
	t.Cleanup(resetReflected)
	d := makeNode(*NewConfig())
	d.nodeId = strings.Repeat("a", 20)
	d.routingTable.nodeId = d.nodeId
	d.conn = &fakeConn{}
	addr := net.UDPAddr{IP: net.ParseIP("198.51.100.1"), Port: 6881}
	node := newRemoteNode(addr, strings.Repeat("b", 20), &d.DebugLogger)
	if err := d.routingTable.insert(node, "udp4"); err != nil {
		t.Fatal(err)
	}
	raw := bep42IPField(t, "203.0.113.7", 6881)
	r := responseType{T: "not-pending", Y: "r", R: getPeersResponse{Id: node.id, IP: raw}}
	d.handleResponsePacket(packetType{raddr: addr}, r)
	if d.publicIP != nil || d.externalIPCandidate != nil {
		t.Fatal("an unmatched response influenced external-IP adoption")
	}

	node.pendingQueries["pending"] = &queryType{Type: "ping"}
	r.T = "pending"
	d.handleResponsePacket(packetType{raddr: addr}, r)
	if d.externalIPCandidate == nil || !d.externalIPCandidate.Equal(net.ParseIP("203.0.113.7")) {
		t.Fatal("a matched pending response did not start confirmation")
	}
	if d.publicIP != nil {
		t.Fatal("one matched response was adopted without confirmation")
	}
}
