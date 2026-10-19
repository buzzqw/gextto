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

func TestClientWireInvariants(t *testing.T) {
	hash := filledHash(1)
	hello := Hello{UserHash: hash, Port: 4662, Name: "gx-ed2k", Version: 0x3C, KadPort: 4672, UDPPort: 4672}
	payload := EncodeHelloRequest(hello)
	if payload[0] != 16 {
		t.Fatalf("hello hash-size byte = %d", payload[0])
	}
	if len(payload) < 1+16+4+2+4+6 {
		t.Fatalf("hello payload too short: %d", len(payload))
	}
	if len(payload) < 6 {
		t.Fatal("hello has no server suffix")
	}
	frame := Frame(OpHello, payload)
	if frame[0] != EdonkeyProtocol || frame[5] != OpHello {
		t.Fatalf("hello header = %x", frame[:6])
	}
	if got := int(frame[1]) | int(frame[2])<<8 | int(frame[3])<<16 | int(frame[4])<<24; got != len(payload)+1 {
		t.Fatalf("hello length field = %d, want %d", got, len(payload)+1)
	}
	extended := Frame(OpQueueRanking, []byte{0, 0})
	if extended[0] != EmuleProtocol || extended[5] != OpQueueRanking {
		t.Fatalf("extended header = %x", extended[:6])
	}
	answer := EncodeFileNameAnswer(FileNameAnswer{Hash: hash, Name: "movie.mkv"})
	if got := int(answer[16]) | int(answer[17])<<8; got != len("movie.mkv") {
		t.Fatalf("file-name length = %d", got)
	}
	if len(EncodeFileRequestExtended(FileRequest{Hash: hash})) != 20 {
		t.Fatal("extended file request must carry two 16-bit words")
	}
}
