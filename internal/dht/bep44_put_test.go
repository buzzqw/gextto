package dht

import (
	"crypto/ed25519"
	"net"
	"strings"
	"testing"
)

// TestBEP44PutConformance drives replyPut with the argument combinations BEP 44
// allows and refuses. A mutable put needs k, sig and seq together: one carrying
// sig without k must not be stored, unsigned, as an immutable item.
func TestBEP44PutConformance(t *testing.T) {
	addr := net.UDPAddr{IP: net.IPv4(198, 51, 100, 20), Port: 6881}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	value := "Hello World!"
	encoded := bencodeValue(t, value)
	sig := SignMutable(priv, 1, encoded, nil)

	put := func(t *testing.T, extra map[string]interface{}) (*DHT, map[string]interface{}) {
		t.Helper()
		d, conn := newClientTestDHT(t)
		d.tokenSecrets = []string{"one", "two"}
		args := map[string]interface{}{
			"id":    strings.Repeat("d", 20),
			"token": d.hostToken(addr, nil, d.tokenSecrets[0]),
			"v":     value,
		}
		for key, v := range extra {
			args[key] = v
		}
		d.replyPut(addr, bencodeArgs(t, map[string]interface{}{"t": "p1", "y": "q", "q": "put", "a": args}))
		if len(conn.writes) != 1 {
			t.Fatalf("%d packets written, want 1", len(conn.writes))
		}
		top, err := decodeBencode(conn.writes[0])
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		return d, top.(map[string]interface{})
	}
	errorCode := func(t *testing.T, m map[string]interface{}) int64 {
		t.Helper()
		list, ok := m["e"].([]interface{})
		if !ok || len(list) == 0 {
			t.Fatalf("not an error reply: %v", m)
		}
		code, _ := list[0].(int64)
		return code
	}

	t.Run("immutable", func(t *testing.T) {
		d, m := put(t, nil)
		if m["y"] != "r" {
			t.Fatalf("immutable put refused: %v", m)
		}
		if d.items.get(ImmutableTarget(encoded)) == nil {
			t.Fatal("immutable item not stored")
		}
	})
	t.Run("mutable", func(t *testing.T) {
		d, m := put(t, map[string]interface{}{"k": string(pub), "sig": string(sig), "seq": int64(1)})
		if m["y"] != "r" {
			t.Fatalf("mutable put refused: %v", m)
		}
		if it := d.items.get(MutableTarget(pub, nil)); it == nil || !it.Mutable() {
			t.Fatal("mutable item not stored")
		}
	})
	for _, tc := range []struct {
		name  string
		extra map[string]interface{}
	}{
		{"sig without k", map[string]interface{}{"sig": string(sig), "seq": int64(1)}},
		{"sig alone", map[string]interface{}{"sig": string(sig)}},
		{"k without sig", map[string]interface{}{"k": string(pub), "seq": int64(1)}},
		{"k without seq", map[string]interface{}{"k": string(pub), "sig": string(sig)}},
		{"seq alone", map[string]interface{}{"seq": int64(1)}},
		{"cas alone", map[string]interface{}{"cas": int64(1)}},
		{"seq not an integer", map[string]interface{}{"k": string(pub), "sig": string(sig), "seq": "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, m := put(t, tc.extra)
			if code := errorCode(t, m); code != 203 {
				t.Errorf("error code = %d, want 203", code)
			}
			if d.items.get(ImmutableTarget(encoded)) != nil || d.items.get(MutableTarget(pub, nil)) != nil {
				t.Error("a refused put stored an item")
			}
		})
	}
	t.Run("bad signature", func(t *testing.T) {
		_, m := put(t, map[string]interface{}{"k": string(pub), "sig": string(sig), "seq": int64(2)})
		if code := errorCode(t, m); code != 206 {
			t.Errorf("error code = %d, want 206", code)
		}
	})
}
