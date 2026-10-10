package dht

import (
	"bytes"
	"net"
	"strings"
	"testing"

	bencode "github.com/jackpal/bencode-go"
	"github.com/nictuku/nettools"
)

// binaryContact builds the binary host:port part of a compact node/peer
// contact (6 bytes for IPv4, 18 bytes for IPv6) using the same nettools
// encoding the library uses on the wire.
func binaryContact(t *testing.T, hostPort string) string {
	t.Helper()
	c := nettools.DottedPortToBinary(hostPort)
	if c == "" {
		t.Fatalf("DottedPortToBinary(%q) returned empty", hostPort)
	}
	return c
}

// TestListenIPv6Brackets locks in that an IPv6 listen address is bracketed
// before use, otherwise net.ListenPacket fails with "too many colons".
func TestListenIPv6Brackets(t *testing.T) {
	if l, err := net.Listen("tcp6", "[::1]:0"); err != nil {
		t.Skip("no IPv6 loopback")
	} else {
		l.Close()
	}
	conn, err := listen("::1", 0, "udp6", &nullLogger{})
	if err != nil {
		t.Fatalf("listen on ::1 failed: %v", err)
	}
	conn.Close()
}

func TestParseNodesString(t *testing.T) {
	const (
		id4 = "AAAAAAAAAAAAAAAAAAAA" // 20 bytes
		id6 = "BBBBBBBBBBBBBBBBBBBB" // 20 bytes
	)
	v4contact := binaryContact(t, "1.2.3.4:6881")
	v6contact := binaryContact(t, "[2001:db8::1]:6881")
	if len(v4contact) != v4nodeContactLen-nodeIdLen {
		t.Fatalf("unexpected v4 contact length: %d", len(v4contact))
	}
	if len(v6contact) != v6nodeContactLen-nodeIdLen {
		t.Fatalf("unexpected v6 contact length: %d", len(v6contact))
	}

	v4nodes := id4 + v4contact
	v6nodes := id6 + v6contact
	if len(v4nodes) != v4nodeContactLen {
		t.Fatalf("unexpected v4 node length: %d", len(v4nodes))
	}
	if len(v6nodes) != v6nodeContactLen {
		t.Fatalf("unexpected v6 node length: %d", len(v6nodes))
	}

	tests := []struct {
		name    string
		nodes   string
		proto   string
		want    map[string]string
		wantNil bool
	}{
		{
			name:  "ipv4 26 bytes",
			nodes: v4nodes,
			proto: "udp4",
			want:  map[string]string{id4: "1.2.3.4:6881"},
		},
		{
			name:  "ipv6 38 bytes",
			nodes: v6nodes,
			proto: "udp6",
			want:  map[string]string{id6: "[2001:db8::1]:6881"},
		},
		{
			name:  "mixed string rejected as udp4",
			nodes: v4nodes + v6nodes,
			proto: "udp4",
			want:  map[string]string{},
		},
		{
			name:  "mixed string rejected as udp6",
			nodes: v4nodes + v6nodes,
			proto: "udp6",
			want:  map[string]string{},
		},
		{
			name:  "invalid length",
			nodes: id4 + v4contact[:len(v4contact)-1], // 25 bytes
			proto: "udp4",
			want:  map[string]string{},
		},
		{
			name:  "empty string is a valid, empty parse",
			nodes: "",
			proto: "udp4",
			want:  map[string]string{},
		},
		{
			name:    "family-agnostic proto is not parsable directly",
			nodes:   v4nodes,
			proto:   "udp",
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseNodesString(tc.nodes, tc.proto, &nullLogger{})
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %#v", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d parsed nodes, got %d (%#v)", len(tc.want), len(got), got)
			}
			for id, addr := range tc.want {
				if got[id] != addr {
					t.Fatalf("for id %q expected %q, got %q", id, addr, got[id])
				}
			}
		})
	}
}

// TestReadResponseDualStackFields checks that both BEP 32 contact fields are
// decoded from the wire (and that the struct tags keep their bencode keys).
func TestReadResponseDualStackFields(t *testing.T) {
	payload := map[string]interface{}{
		"t": "aa",
		"y": "r",
		"r": map[string]interface{}{
			"id":     strings.Repeat("I", 20),
			"token":  "tok",
			"nodes":  "v4blob",
			"nodes6": "v6blob",
		},
	}
	var b bytes.Buffer
	if err := bencode.Marshal(&b, payload); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := readResponse(packetType{b: b.Bytes()}, &nullLogger{})
	if err != nil {
		t.Fatalf("readResponse: %v", err)
	}
	if resp.T != "aa" || resp.Y != "r" {
		t.Fatalf("unexpected envelope: t=%q y=%q", resp.T, resp.Y)
	}
	if resp.R.Id != strings.Repeat("I", 20) || resp.R.Token != "tok" {
		t.Fatalf("unexpected id/token: %q / %q", resp.R.Id, resp.R.Token)
	}
	if resp.R.Nodes != "v4blob" || resp.R.Nodes6 != "v6blob" {
		t.Fatalf("nodes fields not decoded: nodes=%q nodes6=%q", resp.R.Nodes, resp.R.Nodes6)
	}
}

// newTestDHT builds a DHT with an empty routing table, enough for the
// pure functions under test that do not talk to the network.
func newTestDHT() *DHT {
	d := &DHT{DebugLogger: &nullLogger{}}
	d.routingTable = newRoutingTable(&d.DebugLogger)
	d.peerStore = newPeerStore(10, 10)
	return d
}

func (d *DHT) insertTestNode(t *testing.T, hostPort, id, proto string) {
	t.Helper()
	addr, err := net.ResolveUDPAddr(proto, hostPort)
	if err != nil {
		t.Fatalf("ResolveUDPAddr(%q, %q): %v", proto, hostPort, err)
	}
	node := newRemoteNode(*addr, id, &d.DebugLogger)
	if err := d.routingTable.insert(node, proto); err != nil {
		t.Fatalf("insert(%s): %v", hostPort, err)
	}
}

func TestNodesForInfoHashSplitsFamilies(t *testing.T) {
	const (
		id4 = "aaaaaaaaaaaaaaaaaaaa"
		id6 = "bbbbbbbbbbbbbbbbbbbb"
	)
	ih := InfoHash("xxxxxxxxxxxxxxxxxxxx")

	tests := []struct {
		name   string
		nodes  []struct{ hostPort, id, proto string }
		wantV4 int // bytes in n4
		wantV6 int // bytes in n6
		v4ID   string
		v6ID   string
	}{
		{
			name:   "no v6 node -> empty n6",
			nodes:  []struct{ hostPort, id, proto string }{{"1.2.3.4:6881", id4, "udp4"}},
			wantV4: v4nodeContactLen,
			wantV6: 0,
			v4ID:   id4,
		},
		{
			name:   "one v6 node -> appears in n6",
			nodes:  []struct{ hostPort, id, proto string }{{"[2001:db8::1]:6881", id6, "udp6"}},
			wantV4: 0,
			wantV6: v6nodeContactLen,
			v6ID:   id6,
		},
		{
			name: "mixed families are split",
			nodes: []struct{ hostPort, id, proto string }{
				{"1.2.3.4:6881", id4, "udp4"},
				{"[2001:db8::1]:6881", id6, "udp6"},
			},
			wantV4: v4nodeContactLen,
			wantV6: v6nodeContactLen,
			v4ID:   id4,
			v6ID:   id6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDHT()
			for _, n := range tc.nodes {
				d.insertTestNode(t, n.hostPort, n.id, n.proto)
			}
			n4, n6 := d.nodesForInfoHash(ih)
			if len(n4) != tc.wantV4 {
				t.Fatalf("len(n4)=%d, want %d (%q)", len(n4), tc.wantV4, n4)
			}
			if len(n6) != tc.wantV6 {
				t.Fatalf("len(n6)=%d, want %d (%q)", len(n6), tc.wantV6, n6)
			}
			if tc.v4ID != "" {
				if !strings.HasPrefix(n4, tc.v4ID) {
					t.Fatalf("n4=%q does not start with IPv4 id %q", n4, tc.v4ID)
				}
				if strings.Contains(n6, tc.v4ID) {
					t.Fatalf("IPv4 contact leaked into n6: %q", n6)
				}
			}
			if tc.v6ID != "" {
				if !strings.HasPrefix(n6, tc.v6ID) {
					t.Fatalf("n6=%q does not start with IPv6 id %q", n6, tc.v6ID)
				}
				if strings.Contains(n4, tc.v6ID) {
					t.Fatalf("IPv6 contact leaked into n4: %q", n4)
				}
			}
		})
	}
}

func TestNodeResponseFields(t *testing.T) {
	tests := []struct {
		name  string
		proto string
		want  []nodesField
	}{
		{"udp4 reads only nodes", "udp4", []nodesField{{"v4blob", "udp4"}}},
		{"udp6 reads only nodes6", "udp6", []nodesField{{"v6blob", "udp6"}}},
		{"udp reads both", "udp", []nodesField{{"v4blob", "udp4"}, {"v6blob", "udp6"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDHT()
			d.config.UDPProto = tc.proto
			got := d.nodeResponseFields("v4blob", "v6blob")
			if len(got) != len(tc.want) {
				t.Fatalf("got %d fields, want %d (%#v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("field %d: got %#v, want %#v", i, got[i], tc.want[i])
				}
			}
			if wantDual := tc.proto == "udp"; d.dualStack() != wantDual {
				t.Fatalf("dualStack()=%v, want %v", d.dualStack(), wantDual)
			}
		})
	}
}

func FuzzParseNodesString(f *testing.F) {
	f.Add("AAAAAAAAAAAAAAAAAAAA\x01\x02\x03\x04\x1a\xe1", "udp4")
	f.Add("BBBBBBBBBBBBBBBBBBBB\x20\x01\x0d\xb8\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x1a\xe1", "udp6")
	f.Fuzz(func(t *testing.T, nodes, proto string) {
		_ = parseNodesString(nodes, proto, &nullLogger{})
	})
}
