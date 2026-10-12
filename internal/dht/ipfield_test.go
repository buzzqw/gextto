package dht

import (
	"net"
	"strings"
	"testing"
)

// replyTop decodes the top-level dictionary of the i-th packet written to conn.
// BEP 42 puts the "ip" field there, next to "t", "y" and "r".
func replyTop(t *testing.T, conn *fakeConn, i int) map[string]interface{} {
	t.Helper()
	if i >= len(conn.writes) {
		t.Fatalf("only %d packet(s) written", len(conn.writes))
	}
	top, err := decodeBencode(conn.writes[i])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	m, ok := top.(map[string]interface{})
	if !ok {
		t.Fatalf("not a dictionary")
	}
	if r, ok := m["r"].(map[string]interface{}); !ok {
		t.Fatalf("no r dictionary")
	} else if _, inside := r["ip"]; inside {
		t.Fatalf(`"ip" must be a top-level key, not inside "r"`)
	}
	return m
}

// TestRepliesIncludeIPField checks the BEP 42 sending side: every reply carries
// the compact "ip" of the requester as seen by us.
func TestRepliesIncludeIPField(t *testing.T) {
	newDHT := func() (*DHT, *fakeConn) {
		d := &DHT{nodeId: strings.Repeat("a", 20), items: newItemStore()}
		d.DebugLogger = &nullLogger{}
		d.conn = &fakeConn{}
		d.routingTable = newRoutingTable(&d.DebugLogger)
		d.tokenSecrets = []string{"one", "two"}
		return d, d.conn.(*fakeConn)
	}
	addr := net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 51413}
	want := encodeCompactPeerAddr(addr.String())
	sentBefore := reflectedSent.Load()

	// ping
	d, conn := newDHT()
	d.replyPing(addr, responseType{T: "t1"})
	if got := replyTop(t, conn, 0)["ip"]; got != want {
		t.Errorf("ping reply ip = %v, want %q", got, want)
	}

	// find_node
	d, conn = newDHT()
	d.replyFindNode(addr, responseType{T: "t2", A: answerType{Target: strings.Repeat("b", 20)}})
	if got := replyTop(t, conn, 0)["ip"]; got != want {
		t.Errorf("find_node reply ip = %v, want %q", got, want)
	}

	// announce_peer (needs a peer store)
	d, conn = newDHT()
	d.peerStore = newPeerStore(16, 16)
	d.replyAnnouncePeer(addr, nil, responseType{T: "t3", A: answerType{InfoHash: InfoHash(strings.Repeat("c", 20))}})
	if got := replyTop(t, conn, 0)["ip"]; got != want {
		t.Errorf("announce_peer reply ip = %v, want %q", got, want)
	}

	// BEP 44 get reply
	d, conn = newDHT()
	query := bencodeArgs(t, map[string]interface{}{
		"t": "t4", "y": "q", "q": "get",
		"a": map[string]interface{}{"id": strings.Repeat("d", 20), "target": strings.Repeat("e", 20)},
	})
	d.replyGet(addr, query)
	if got := replyTop(t, conn, 0)["ip"]; got != want {
		t.Errorf("bep44 get reply ip = %v, want %q", got, want)
	}

	// The send side is observable: each reply counted a "reflected_sent".
	if got := reflectedSent.Load() - sentBefore; got < 4 {
		t.Errorf("reflected_sent grew by %d, want >= 4", got)
	}
}

// bencodeArgs bencodes a query dictionary for replyGet/replyPut.
func bencodeArgs(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := encodeBencodeValue(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestAnnouncePeerImpliedPort checks the BEP 5 "implied_port" rule: when it is 1,
// the source port is used instead of the announced one.
func TestAnnouncePeerImpliedPort(t *testing.T) {
	newDHT := func() *DHT {
		d := &DHT{nodeId: strings.Repeat("a", 20), items: newItemStore()}
		d.DebugLogger = &nullLogger{}
		d.conn = &fakeConn{}
		d.routingTable = newRoutingTable(&d.DebugLogger)
		d.tokenSecrets = []string{"one", "two"}
		d.peerStore = newPeerStore(16, 16)
		d.PeersRequestResults = make(chan map[InfoHash][]string, 1)
		return d
	}
	addr := net.UDPAddr{IP: net.IPv4(198, 51, 100, 9), Port: 40000}
	ih := InfoHash(strings.Repeat("f", 20))

	// implied_port = 1: the source port (40000) wins over the announced 1234.
	d := newDHT()
	token := d.hostToken(addr, []byte(ih), d.tokenSecrets[0])
	node := newRemoteNode(addr, strings.Repeat("b", 20), &d.DebugLogger)
	d.replyAnnouncePeer(addr, node, responseType{T: "t1", A: answerType{InfoHash: ih, Port: 1234, Token: token, ImpliedPort: 1}})
	src := net.TCPAddr{IP: addr.IP, Port: addr.Port}
	want := encodeCompactPeerAddr(src.String())
	if got := d.peerStore.peerContacts(ih, false); len(got) != 1 || got[0] != want {
		t.Fatalf("implied_port: peerContacts = %v, want [%q]", got, want)
	}

	// implied_port = 0: the announced port is used.
	d = newDHT()
	token = d.hostToken(addr, []byte(ih), d.tokenSecrets[0])
	node = newRemoteNode(addr, strings.Repeat("b", 20), &d.DebugLogger)
	d.replyAnnouncePeer(addr, node, responseType{T: "t2", A: answerType{InfoHash: ih, Port: 1234, Token: token}})
	announced := net.TCPAddr{IP: addr.IP, Port: 1234}
	want = encodeCompactPeerAddr(announced.String())
	if got := d.peerStore.peerContacts(ih, false); len(got) != 1 || got[0] != want {
		t.Fatalf("explicit port: peerContacts = %v, want [%q]", got, want)
	}
}

// TestAnnouncePeerRejectsInvalidPort: an announce with port 0 (and no
// implied_port) or above 65535 must not become a contact served to others.
func TestAnnouncePeerRejectsInvalidPort(t *testing.T) {
	addr := net.UDPAddr{IP: net.IPv4(198, 51, 100, 9), Port: 40000}
	ih := InfoHash(strings.Repeat("f", 20))
	for _, port := range []int{0, -1, 70000} {
		d := &DHT{nodeId: strings.Repeat("a", 20), items: newItemStore()}
		d.DebugLogger = &nullLogger{}
		conn := &fakeConn{}
		d.conn = conn
		d.routingTable = newRoutingTable(&d.DebugLogger)
		d.tokenSecrets = []string{"one", "two"}
		d.peerStore = newPeerStore(16, 16)
		d.PeersRequestResults = make(chan map[InfoHash][]string, 1)
		token := d.hostToken(addr, []byte(ih), d.tokenSecrets[0])
		node := newRemoteNode(addr, strings.Repeat("b", 20), &d.DebugLogger)
		d.replyAnnouncePeer(addr, node, responseType{T: "t", A: answerType{InfoHash: ih, Port: port, Token: token}})
		if got := d.peerStore.peerContacts(ih, false); len(got) != 0 {
			t.Errorf("port %d stored as %v, want nothing", port, got)
		}
		if len(conn.writes) != 1 {
			t.Errorf("port %d: the query must still get a reply", port)
		}
	}
}
