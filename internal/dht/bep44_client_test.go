package dht

import (
	"crypto/ed25519"
	"net"
	"strings"
	"testing"
)

// fakeConn records the packets written to it, so a test can drive the run-loop
// handlers without a socket.
type fakeConn struct {
	writes [][]byte
	addrs  []net.UDPAddr // destination of each write
}

func (f *fakeConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, nil }
func (f *fakeConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	f.writes = append(f.writes, append([]byte(nil), p...))
	if u, ok := addr.(*net.UDPAddr); ok {
		f.addrs = append(f.addrs, *u)
	} else {
		f.addrs = append(f.addrs, net.UDPAddr{})
	}
	return len(p), nil
}
func (f *fakeConn) LocalAddr() net.Addr { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }
func (f *fakeConn) Close() error        { return nil }

func newClientTestDHT(t *testing.T) (*DHT, *fakeConn) {
	t.Helper()
	d := &DHT{
		nodeId: strings.Repeat("a", 20),
		items:  newItemStore(),
		bep44s: map[uint64]*bep44Lookup{},
	}
	d.DebugLogger = &nullLogger{}
	d.conn = &fakeConn{}
	d.routingTable = newRoutingTable(&d.DebugLogger)
	d.config.UDPProto = "udp4"
	return d, d.conn.(*fakeConn)
}

// reply builds a bencoded DHT response packet with the given "r" dictionary.
func reply(t *testing.T, r map[string]interface{}) packetType {
	t.Helper()
	if _, ok := r["id"]; !ok {
		r["id"] = strings.Repeat("b", 20)
	}
	b, err := encodeBencodeValue(replyMessage{T: "1", Y: "r", R: r})
	if err != nil {
		t.Fatal(err)
	}
	return packetType{b: b, raddr: net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 9}}
}

func TestBEP44HandleGetReplyImmutable(t *testing.T) {
	d, _ := newClientTestDHT(t)
	encoded, _ := encodeBencodeValue("Hello World!")
	lk := &bep44Lookup{
		id: 1, req: bep44LookupRequest{target: ImmutableTarget(encoded)},
		queried: map[string]bool{}, tokens: map[string]bep44Token{},
	}
	d.bep44s[1] = lk

	node := newRemoteNode(net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 9}, "n", &d.DebugLogger)
	d.handleBep44Reply(node, reply(t, map[string]interface{}{"token": "tok", "v": "Hello World!"}), &queryType{Type: "get", bep44: 1})

	if lk.best == nil || lk.best.Value != "Hello World!" {
		t.Fatalf("best = %+v", lk.best)
	}
	if lk.tokens[node.address.String()].token != "tok" {
		t.Fatalf("token not recorded: %v", lk.tokens)
	}

	// A value that does not hash to the target is rejected.
	lk2 := &bep44Lookup{id: 2, req: bep44LookupRequest{target: ImmutableTarget([]byte("other"))}, queried: map[string]bool{}, tokens: map[string]bep44Token{}}
	d.bep44s[2] = lk2
	d.handleBep44Reply(node, reply(t, map[string]interface{}{"v": "Hello World!"}), &queryType{Type: "get", bep44: 2})
	if lk2.best != nil {
		t.Fatalf("a mismatching immutable value was accepted: %+v", lk2.best)
	}
}

func TestBEP44HandleGetReplyMutable(t *testing.T) {
	d, _ := newClientTestDHT(t)
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	salt := []byte("it")
	value := map[string]interface{}{"ih": strings.Repeat("z", 20)}
	encoded, _ := encodeBencodeValue(value)
	sig := SignMutable(priv, 1, encoded, salt)

	lk := &bep44Lookup{
		id: 1, req: bep44LookupRequest{target: MutableTarget(pub, salt), mutable: true, pub: pub, salt: salt},
		queried: map[string]bool{}, tokens: map[string]bep44Token{},
	}
	d.bep44s[1] = lk
	node := newRemoteNode(net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 9}, "n", &d.DebugLogger)

	d.handleBep44Reply(node, reply(t, map[string]interface{}{
		"v": value, "k": string(pub), "sig": string(sig), "seq": int64(1),
	}), &queryType{Type: "get", bep44: 1})
	if lk.best == nil || lk.best.Seq != 1 {
		t.Fatalf("mutable value not accepted: %+v", lk.best)
	}

	// A wrong signature (or a wrong key) is rejected.
	lk2 := &bep44Lookup{
		id: 2, req: bep44LookupRequest{target: MutableTarget(pub, salt), mutable: true, pub: pub, salt: salt},
		queried: map[string]bool{}, tokens: map[string]bep44Token{},
	}
	d.bep44s[2] = lk2
	d.handleBep44Reply(node, reply(t, map[string]interface{}{
		"v": value, "k": string(pub), "sig": string(make([]byte, ed25519.SignatureSize)), "seq": int64(1),
	}), &queryType{Type: "get", bep44: 2})
	if lk2.best != nil {
		t.Fatalf("a forged mutable value was accepted: %+v", lk2.best)
	}
}

// TestBEP44HandleGetReplyPublishes checks the publish flow: the token of each
// get reply is collected during the traversal and, when the lookup ends, the
// put goes to the closest nodes only (BEP 44: the K closest to the target).
func TestBEP44HandleGetReplyPublishes(t *testing.T) {
	d, conn := newClientTestDHT(t)
	_, priv, _ := ed25519.GenerateKey(nil)
	salt := []byte("s")
	pub := priv.Public().(ed25519.PublicKey)
	target := MutableTarget(pub, salt)
	encoded, _ := encodeBencodeValue(map[string]interface{}{"ih": strings.Repeat("q", 20)})
	resC := make(chan bep44Result, 1)
	lk := &bep44Lookup{
		id: 1,
		req: bep44LookupRequest{
			target: target, mutable: true, pub: pub, salt: salt, result: resC,
			publish: &StoredItem{Target: target, Value: map[string]interface{}{"ih": strings.Repeat("q", 20)}, Seq: 1, PublicKey: pub, Signature: SignMutable(priv, 1, encoded, salt)},
		},
		queried: map[string]bool{}, tokens: map[string]bep44Token{},
	}
	d.bep44s[1] = lk

	// Twelve responders: their ids differ from the target only in the last
	// byte, by i+1, so responder i is the i-th closest.
	before := len(conn.writes)
	for i := 0; i < 12; i++ {
		id := []byte(string(target[:]))
		id[19] ^= byte(i + 1)
		node := newRemoteNode(net.UDPAddr{IP: net.IPv4(127, 0, 0, byte(10+i)), Port: 9}, string(id), &d.DebugLogger)
		d.handleBep44Reply(node, reply(t, map[string]interface{}{"id": string(id), "token": "tok"}), &queryType{Type: "get", bep44: 1})
	}
	if len(conn.writes) != before {
		t.Fatalf("no put before the traversal ends, got %d write(s)", len(conn.writes)-before)
	}
	d.finishBep44(1)
	if res := <-resC; res.err != nil {
		t.Fatalf("publish result: %v", res.err)
	}
	if lk.putSent != bep44PutNodes || len(conn.writes)-before != bep44PutNodes {
		t.Fatalf("puts = %d (writes %d), want %d", lk.putSent, len(conn.writes)-before, bep44PutNodes)
	}
	// The puts went to the eight closest responders (127.0.0.10 … .17).
	for _, w := range conn.addrs[before:] {
		if last := w.IP.To4()[3]; last < 10 || last > 17 {
			t.Fatalf("put sent to a far node %v", w)
		}
	}
}

func TestBEP44Finish(t *testing.T) {
	d, _ := newClientTestDHT(t)

	// Not found: an error.
	resC := make(chan bep44Result, 1)
	d.bep44s[1] = &bep44Lookup{id: 1, req: bep44LookupRequest{result: resC}}
	d.finishBep44(1)
	if res := <-resC; res.err == nil {
		t.Fatal("finish of an empty get must report not found")
	}

	// Found: the best item is returned and the lookup is dropped.
	resC = make(chan bep44Result, 1)
	item := &StoredItem{Value: "v"}
	d.bep44s[2] = &bep44Lookup{id: 2, req: bep44LookupRequest{result: resC}, best: item}
	d.finishBep44(2)
	if res := <-resC; res.item != item {
		t.Fatalf("finish returned %+v", res.item)
	}
	if _, ok := d.bep44s[2]; ok {
		t.Fatal("the lookup was not dropped")
	}
}
