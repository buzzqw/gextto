package dht

import (
	"crypto/ed25519"
	"net"
	"strings"
	"testing"
)

// fakeConn records the packets written to it, so a test can drive the run-loop
// handlers without a socket.
type fakeConn struct{ writes [][]byte }

func (f *fakeConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, nil }
func (f *fakeConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	f.writes = append(f.writes, append([]byte(nil), p...))
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
		queried: map[string]bool{}, tokens: map[string]string{},
	}
	d.bep44s[1] = lk

	node := newRemoteNode(net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 9}, "n", &d.DebugLogger)
	d.handleBep44Reply(node, reply(t, map[string]interface{}{"token": "tok", "v": "Hello World!"}), &queryType{Type: "get", bep44: 1})

	if lk.best == nil || lk.best.Value != "Hello World!" {
		t.Fatalf("best = %+v", lk.best)
	}
	if lk.tokens[node.address.String()] != "tok" {
		t.Fatalf("token not recorded: %v", lk.tokens)
	}

	// A value that does not hash to the target is rejected.
	lk2 := &bep44Lookup{id: 2, req: bep44LookupRequest{target: ImmutableTarget([]byte("other"))}, queried: map[string]bool{}, tokens: map[string]string{}}
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
		queried: map[string]bool{}, tokens: map[string]string{},
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
		queried: map[string]bool{}, tokens: map[string]string{},
	}
	d.bep44s[2] = lk2
	d.handleBep44Reply(node, reply(t, map[string]interface{}{
		"v": value, "k": string(pub), "sig": string(make([]byte, ed25519.SignatureSize)), "seq": int64(1),
	}), &queryType{Type: "get", bep44: 2})
	if lk2.best != nil {
		t.Fatalf("a forged mutable value was accepted: %+v", lk2.best)
	}
}

// TestBEP44HandleGetReplyPublishes checks the publish flow: the token from the
// get reply triggers a put to the same node.
func TestBEP44HandleGetReplyPublishes(t *testing.T) {
	d, conn := newClientTestDHT(t)
	auth, priv, _ := ed25519.GenerateKey(nil)
	_ = auth
	salt := []byte("s")
	encoded, _ := encodeBencodeValue(map[string]interface{}{"ih": strings.Repeat("q", 20)})
	lk := &bep44Lookup{
		id: 1,
		req: bep44LookupRequest{
			target: MutableTarget(priv.Public().(ed25519.PublicKey), salt), mutable: true,
			pub: priv.Public().(ed25519.PublicKey), salt: salt,
			publish: &StoredItem{Target: MutableTarget(priv.Public().(ed25519.PublicKey), salt), Value: map[string]interface{}{"ih": strings.Repeat("q", 20)}, Seq: 1, PublicKey: priv.Public().(ed25519.PublicKey), Signature: SignMutable(priv, 1, encoded, salt)},
		},
		queried: map[string]bool{}, tokens: map[string]string{},
	}
	d.bep44s[1] = lk
	node := newRemoteNode(net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 9}, "n", &d.DebugLogger)

	before := len(conn.writes)
	d.handleBep44Reply(node, reply(t, map[string]interface{}{"token": "tok"}), &queryType{Type: "get", bep44: 1})
	if len(conn.writes) != before+1 {
		t.Fatalf("expected one put write, got %d", len(conn.writes)-before)
	}
	if lk.putSent != 1 {
		t.Fatalf("putSent = %d, want 1", lk.putSent)
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
