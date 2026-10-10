package peerprotocol

import (
	"bytes"
	"net"
	"testing"
)

func TestHolepunchRoundTrip(t *testing.T) {
	cases := []HolepunchMessage{
		{Type: HolepunchRendezvous, Addr: net.IPv4(1, 2, 3, 4), Port: 6881},
		{Type: HolepunchConnect, Addr: net.ParseIP("2001:db8::1"), Port: 51413},
		{Type: HolepunchError, Addr: net.IPv4(10, 0, 0, 1), Port: 1, ErrCode: HolepunchNoSelf},
		{Type: HolepunchError, Addr: net.ParseIP("fe80::1"), Port: 65535, ErrCode: HolepunchNoSupport},
	}
	for _, want := range cases {
		encoded, err := want.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal %+v: %v", want, err)
		}
		var got HolepunchMessage
		if err := got.UnmarshalBinary(encoded); err != nil {
			t.Fatalf("unmarshal %+v: %v", want, err)
		}
		if got.Type != want.Type || got.Port != want.Port || got.ErrCode != want.ErrCode || !got.Addr.Equal(want.Addr) {
			t.Fatalf("round trip: got %+v want %+v", got, want)
		}
	}
}

func TestHolepunchMarshalInvalidAddress(t *testing.T) {
	if _, err := (HolepunchMessage{Type: HolepunchConnect}).MarshalBinary(); err == nil {
		t.Fatal("expected an error for a nil address")
	}
}

func TestHolepunchUnmarshalErrors(t *testing.T) {
	valid := []byte{byte(HolepunchRendezvous), 0x00, 1, 2, 3, 4, 0x1a, 0xe1, 0, 0, 0, 0}
	for name, data := range map[string][]byte{
		"empty":         nil,
		"too short":     valid[:11],
		"trailing data": append(append([]byte(nil), valid...), 0x00),
		"bad addr type": {byte(HolepunchConnect), 0xFF, 1, 2, 3, 4, 0, 1, 0, 0, 0, 0},
		"short ipv6":    {byte(HolepunchConnect), 0x01, 1, 2, 3, 4, 0, 1, 0, 0, 0, 0},
	} {
		var got HolepunchMessage
		if err := got.UnmarshalBinary(data); err == nil {
			t.Fatalf("%s: expected an error, got %+v", name, got)
		}
	}
	// The valid buffer must still parse.
	var got HolepunchMessage
	if err := got.UnmarshalBinary(valid); err != nil {
		t.Fatalf("valid buffer: %v", err)
	}
	if got.Type != HolepunchRendezvous || got.Port != 6881 {
		t.Fatalf("valid buffer parsed as %+v", got)
	}
}

// The BEP 55 payload is raw binary inside a BEP 10 extended message: the write
// and the read paths must not bencode it.
func TestHolepunchExtensionMessageRoundTrip(t *testing.T) {
	want := HolepunchMessage{Type: HolepunchConnect, Addr: net.IPv4(8, 8, 8, 8), Port: 1234}
	var buf bytes.Buffer
	if _, err := (ExtensionMessage{ExtendedMessageID: ExtensionIDHolepunch, Payload: want}).WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 13 {
		t.Fatalf("wire size = %d, want 13 (1 id + 12 payload)", buf.Len())
	}
	if buf.Bytes()[0] != ExtensionIDHolepunch {
		t.Fatalf("first byte = %d, want the extension id", buf.Bytes()[0])
	}
	var got ExtensionMessage
	if err := got.UnmarshalBinary(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	hp, ok := got.Payload.(HolepunchMessage)
	if !ok {
		t.Fatalf("payload type = %T", got.Payload)
	}
	if hp.Type != want.Type || hp.Port != want.Port || !hp.Addr.Equal(want.Addr) {
		t.Fatalf("round trip: got %+v want %+v", hp, want)
	}
}

func TestExtensionHandshakeAdvertisesHolepunch(t *testing.T) {
	on := NewExtensionHandshake(0, "v", nil, 0, true)
	if _, ok := on.M[ExtensionKeyHolepunch]; !ok {
		t.Fatal("ut_holepunch not advertised when enabled")
	}
	off := NewExtensionHandshake(0, "v", nil, 0, false)
	if _, ok := off.M[ExtensionKeyHolepunch]; ok {
		t.Fatal("ut_holepunch advertised although disabled")
	}
}

// Any payload that parses must also re-encode and parse identically: a
// truncated or malformed message must never panic or be silently accepted.
func FuzzHolepunchUnmarshal(f *testing.F) {
	seed, err := (HolepunchMessage{Type: HolepunchConnect, Addr: net.IPv4(1, 2, 3, 4), Port: 6881}).MarshalBinary()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte{0, 0, 1, 2, 3, 4, 0, 1, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		var m HolepunchMessage
		if err := m.UnmarshalBinary(data); err != nil {
			return
		}
		encoded, err := m.MarshalBinary()
		if err != nil {
			t.Fatalf("decoded %+v but marshal failed: %v", m, err)
		}
		var again HolepunchMessage
		if err := again.UnmarshalBinary(encoded); err != nil {
			t.Fatalf("re-encoded %x did not parse: %v", encoded, err)
		}
		if again.Type != m.Type || again.Port != m.Port || again.ErrCode != m.ErrCode || !again.Addr.Equal(m.Addr) {
			t.Fatalf("round trip mismatch: %+v vs %+v", m, again)
		}
	})
}
