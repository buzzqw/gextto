package peer

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/secident"
)

// TestLiveAMuleSecureIdent checks the secure identification handshake against
// a real aMule on 127.0.0.1:4772. It is opt-in because it needs the aMule lab.
func TestLiveAMuleSecureIdent(t *testing.T) {
	if os.Getenv("ED2K_LIVE_AMULE") == "" {
		t.Skip("set ED2K_LIVE_AMULE=1 with aMule listening on 127.0.0.1:4772")
	}
	key, err := secident.LoadOrCreate(filepath.Join(t.TempDir(), "cryptkey.pem"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", "127.0.0.1:4772")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := NewConn(conn)
	_ = client.SetDeadline(time.Now().Add(20 * time.Second))

	user, err := ed2k.RandomID()
	if err != nil {
		t.Fatal(err)
	}
	answer, err := client.Handshake(context.Background(), Hello{
		UserHash: user, Port: 5000, Name: "gx-ed2k", SecIdent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer.MiscOption&0x000F0000 == 0 {
		t.Fatalf("aMule did not advertise secure identification: %#x", answer.MiscOption)
	}
	if err := client.SendExtended(OpEmuleInfo, EncodeMuleInfo()); err != nil {
		t.Fatal(err)
	}
	ident := NewSecIdent(key)
	ident.SetPeerAddresses(net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.1"))
	start, err := ident.Start()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendExtended(start.Opcode, start.Payload); err != nil {
		t.Fatal(err)
	}

	for !ident.Verified() {
		opcode, payload, err := client.Receive()
		if err != nil {
			t.Fatalf("waiting for aMule identification: %v", err)
		}
		switch opcode {
		case OpSecIdentState, OpPublicKey:
			var packets []Packet
			if opcode == OpSecIdentState {
				packets, err = ident.HandleState(payload)
			} else {
				packets, err = ident.HandlePublicKey(payload)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, packet := range packets {
				if err := client.SendExtended(packet.Opcode, packet.Payload); err != nil {
					t.Fatal(err)
				}
			}
		case OpSignature:
			if _, err := ident.HandleSignature(payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Log("aMule passed secure identification")
}
