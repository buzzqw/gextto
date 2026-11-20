package peerprotocol

import (
	"testing"

	"github.com/zeebo/bencode"
)

// TestExtensionHandshakeListenPort: the de-facto "p" field travels in the BEP
// 10 handshake, so peers learn the port we accept connections on instead of
// the ephemeral one an outgoing connection shows them.
func TestExtensionHandshakeListenPort(t *testing.T) {
	msg := NewExtensionHandshake(0, "v", nil, 10, true, false, 6881)
	raw, err := bencode.EncodeBytes(msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded ExtensionMessage
	if err := decoded.UnmarshalBinary(append([]byte{ExtensionIDHandshake}, raw...)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, ok := decoded.Payload.(ExtensionHandshakeMessage)
	if !ok {
		t.Fatalf("payload is %T, want an extension handshake", decoded.Payload)
	}
	if got.ListenPort != 6881 {
		t.Fatalf("listen port = %d, want 6881", got.ListenPort)
	}
}

// TestExtensionHandshakeOmitsUnknownPort: with no listen port (a torrent that
// has not bound one yet) the field is absent, not zero.
func TestExtensionHandshakeOmitsUnknownPort(t *testing.T) {
	msg := NewExtensionHandshake(0, "v", nil, 10, false, false, 0)
	raw, err := bencode.EncodeBytes(msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var fields map[string]any
	if err := bencode.DecodeBytes(raw, &fields); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := fields["p"]; ok {
		t.Fatal("handshake carries a listen port when it has none")
	}
}
