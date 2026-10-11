package peerprotocol

import (
	"bytes"
	"testing"
)

func TestDontHaveExtensionRoundTrip(t *testing.T) {
	want := DontHaveMessage{Index: 0x01020304}
	var buf bytes.Buffer
	if _, err := (ExtensionMessage{ExtendedMessageID: ExtensionIDDontHave, Payload: want}).WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	// BEP 54 wire: subop (1) + 4-byte index = 5 bytes before the framing id.
	if buf.Len() != 5 {
		t.Fatalf("wire size = %d, want 5", buf.Len())
	}
	if buf.Bytes()[0] != ExtensionIDDontHave {
		t.Fatalf("first byte = %d, want the extension id", buf.Bytes()[0])
	}
	var got ExtensionMessage
	if err := got.UnmarshalBinary(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	dh, ok := got.Payload.(DontHaveMessage)
	if !ok {
		t.Fatalf("payload type = %T", got.Payload)
	}
	if dh.Index != want.Index {
		t.Fatalf("index = %d, want %d", dh.Index, want.Index)
	}
}

func TestExtensionHandshakeAdvertisesDontHave(t *testing.T) {
	h := NewExtensionHandshake(0, "v", nil, 0, false, false)
	if _, ok := h.M[ExtensionKeyDontHave]; !ok {
		t.Fatal("lt_donthave not advertised in the extension handshake")
	}
}

func TestDontHaveRejectsShortPayload(t *testing.T) {
	var m ExtensionMessage
	if err := m.UnmarshalBinary([]byte{ExtensionIDDontHave, 0x01, 0x02}); err == nil {
		t.Fatal("expected an error for a short dont-have payload")
	}
}
