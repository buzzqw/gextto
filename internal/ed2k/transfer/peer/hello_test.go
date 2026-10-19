package peer

import (
	"testing"

	"github.com/buzzqw/gextto/internal/ed2k"
)

func TestHelloRoundTrip(t *testing.T) {
	hash, _ := ed2k.ParseID("0123456789abcdef0123456789abcdef")
	want := Hello{
		UserHash: hash, ClientID: 1234, Port: 4662, Name: "gx-ed2k test",
		Version: 0x3C, UDPPort: 4672, KadPort: 4672, MiscOption: 1,
	}
	got, err := DecodeHello(EncodeHello(want))
	if err != nil {
		t.Fatal(err)
	}
	if got.UserHash != want.UserHash || got.ClientID != want.ClientID || got.Port != want.Port ||
		got.Name != want.Name || got.Version != want.Version || got.UDPPort != want.UDPPort ||
		got.KadPort != want.KadPort || got.MiscOption != want.MiscOption {
		t.Fatalf("hello = %+v", got)
	}
}

func TestFrameUnframe(t *testing.T) {
	payload := EncodeHello(Hello{Name: "x"})
	packet := Frame(OpHello, payload)
	opcode, body, err := Unframe(packet)
	if err != nil || opcode != OpHello || len(body) != len(payload) {
		t.Fatalf("unframe = %d/%d/%v", opcode, len(body), err)
	}
	if _, _, err := Unframe(packet[:5]); err == nil {
		t.Fatal("short packet must fail")
	}
	if _, _, err := Unframe([]byte{0xAA, OpHello, 0, 0, 0, 0}); err == nil {
		t.Fatal("wrong protocol must fail")
	}
	truncated := append([]byte{}, packet...)
	truncated[2] = 255
	if _, _, err := Unframe(truncated); err == nil {
		t.Fatal("oversized packet must fail")
	}
}

func TestDecodeHelloRejectsShort(t *testing.T) {
	if _, err := DecodeHello([]byte{1, 2, 3}); err == nil {
		t.Fatal("short hello must fail")
	}
}
