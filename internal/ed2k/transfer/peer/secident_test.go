//go:debug rsa1024min=0

package peer

import (
	"encoding/binary"
	"net"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/ed2k/secident"
)

func testIdentity(t *testing.T, name string) *secident.Key {
	t.Helper()
	key, err := secident.LoadOrCreate(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func publicKeyOf(t *testing.T, packets []Packet) []byte {
	t.Helper()
	for _, packet := range packets {
		if packet.Opcode == OpPublicKey {
			return packet.Payload
		}
	}
	t.Fatalf("no public key in %#v", packets)
	return nil
}

func signatureOf(t *testing.T, packets []Packet) []byte {
	t.Helper()
	for _, packet := range packets {
		if packet.Opcode == OpSignature {
			return packet.Payload
		}
	}
	t.Fatalf("no signature in %#v", packets)
	return nil
}

// TestSecIdentHandshake runs the real packet flow: both sides start, each
// answers with its public key, and the signatures follow once both keys are
// known.
func TestSecIdentHandshake(t *testing.T) {
	a := NewSecIdent(testIdentity(t, "a.pem"))
	b := NewSecIdent(testIdentity(t, "b.pem"))

	aStart, err := a.Start()
	if err != nil {
		t.Fatal(err)
	}
	bStart, err := b.Start()
	if err != nil {
		t.Fatal(err)
	}
	if aStart.Opcode != OpSecIdentState || len(aStart.Payload) != 5 {
		t.Fatalf("unexpected start packet %#v", aStart)
	}

	// Each side asks for the other's key: no signature yet, because neither
	// has the peer key.
	aState, err := a.HandleState(bStart.Payload)
	if err != nil {
		t.Fatal(err)
	}
	bState, err := b.HandleState(aStart.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(aState) != 1 || aState[0].Opcode != OpPublicKey {
		t.Fatalf("expected only a public key, got %#v", aState)
	}
	if len(bState) != 1 || bState[0].Opcode != OpPublicKey {
		t.Fatalf("expected only a public key, got %#v", bState)
	}

	// Keys arrive: each side can now sign the other's key and challenge.
	aReplies, err := a.HandlePublicKey(publicKeyOf(t, bState))
	if err != nil {
		t.Fatal(err)
	}
	bReplies, err := b.HandlePublicKey(publicKeyOf(t, aState))
	if err != nil {
		t.Fatal(err)
	}
	if len(aReplies) != 1 || aReplies[0].Opcode != OpSignature {
		t.Fatalf("expected a signature, got %#v", aReplies)
	}
	if len(bReplies) != 1 || bReplies[0].Opcode != OpSignature {
		t.Fatalf("expected a signature, got %#v", bReplies)
	}

	verified, err := a.HandleSignature(signatureOf(t, bReplies))
	if err != nil || !verified {
		t.Fatalf("A did not verify B: verified=%v err=%v", verified, err)
	}
	verified, err = b.HandleSignature(signatureOf(t, aReplies))
	if err != nil || !verified {
		t.Fatalf("B did not verify A: verified=%v err=%v", verified, err)
	}
	if !a.Verified() || !b.Verified() {
		t.Fatal("both sides must end verified")
	}
}

func TestSecIdentKeyBeforeState(t *testing.T) {
	a := NewSecIdent(testIdentity(t, "a.pem"))
	b := NewSecIdent(testIdentity(t, "b.pem"))

	aStart, err := a.Start()
	if err != nil {
		t.Fatal(err)
	}
	bStart, err := b.Start()
	if err != nil {
		t.Fatal(err)
	}
	bState, err := b.HandleState(aStart.Payload)
	if err != nil {
		t.Fatal(err)
	}
	// A gets B's key before B's state: it can answer with key and signature
	// in one go.
	aReplies, err := a.HandlePublicKey(publicKeyOf(t, bState))
	if err != nil {
		t.Fatal(err)
	}
	if len(aReplies) != 0 {
		t.Fatalf("no signature before the peer challenge, got %#v", aReplies)
	}
	aReplies, err = a.HandleState(bStart.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(aReplies) != 2 || aReplies[0].Opcode != OpPublicKey || aReplies[1].Opcode != OpSignature {
		t.Fatalf("expected key and signature, got %#v", aReplies)
	}
	if _, err := b.HandlePublicKey(publicKeyOf(t, aReplies)); err != nil {
		t.Fatal(err)
	}
	if verified, err := b.HandleSignature(signatureOf(t, aReplies)); err != nil || !verified {
		t.Fatalf("B did not verify A: verified=%v err=%v", verified, err)
	}
}

func TestSecIdentRejectsTamperedSignature(t *testing.T) {
	a := NewSecIdent(testIdentity(t, "a.pem"))
	b := NewSecIdent(testIdentity(t, "b.pem"))
	aStart, _ := a.Start()
	bStart, _ := b.Start()
	aState, err := a.HandleState(bStart.Payload)
	if err != nil {
		t.Fatal(err)
	}
	bState, err := b.HandleState(aStart.Payload)
	if err != nil {
		t.Fatal(err)
	}
	aReplies, err := a.HandlePublicKey(publicKeyOf(t, bState))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.HandlePublicKey(publicKeyOf(t, aState)); err != nil {
		t.Fatal(err)
	}

	signature := append([]byte(nil), signatureOf(t, aReplies)...)
	signature[len(signature)-1] ^= 0xFF
	verified, err := b.HandleSignature(signature)
	if err != nil {
		t.Fatal(err)
	}
	if verified || b.Verified() {
		t.Fatal("tampered signature must not verify")
	}
}

func TestSecIdentUnavailable(t *testing.T) {
	a := NewSecIdent(testIdentity(t, "a.pem"))
	packets, err := a.HandleState([]byte{IdentUnavailable, 0, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 0 || !a.Unavailable() {
		t.Fatalf("unavailable state must not reply: %#v", packets)
	}
}

func TestHelloAdvertisesSecIdent(t *testing.T) {
	raw := EncodeHello(Hello{UserHash: [16]byte{1}, SecIdent: true})
	hello, err := DecodeHello(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := hello.MiscOption & 0x000F0000; got != 0x00010000 {
		t.Fatalf("secure identification field = %#x, want version 1", got)
	}
	raw = EncodeHello(Hello{UserHash: [16]byte{1}})
	hello, err = DecodeHello(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := hello.MiscOption & 0x000F0000; got != 0 {
		t.Fatalf("secure identification advertised without a key: %#x", got)
	}
}

func TestSecIdentVerifiesVersion2Signature(t *testing.T) {
	a := NewSecIdent(testIdentity(t, "a.pem"))
	b := NewSecIdent(testIdentity(t, "b.pem"))
	a.SetPeerAddresses(net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.1"))
	b.SetPeerAddresses(net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.1"))

	aStart, _ := a.Start()
	bStart, _ := b.Start()
	aState, err := a.HandleState(bStart.Payload)
	if err != nil {
		t.Fatal(err)
	}
	bState, err := b.HandleState(aStart.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.HandlePublicKey(publicKeyOf(t, bState)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.HandlePublicKey(publicKeyOf(t, aState)); err != nil {
		t.Fatal(err)
	}

	// b signs a's key, a's challenge and the IP kind 10 binds: the address
	// a is seen from, which is 127.0.0.1 in this test.
	data := append([]byte(nil), a.key.PublicKey()...)
	var challenge [4]byte
	binary.LittleEndian.PutUint32(challenge[:], a.challengeFor)
	data = append(data, challenge[:]...)
	data = append(data, 127, 0, 0, 1)
	data = append(data, 10)
	signature, err := b.key.Sign(data)
	if err != nil {
		t.Fatal(err)
	}
	payload := append([]byte{byte(len(signature))}, signature...)
	payload = append(payload, 10)
	verified, err := a.HandleSignature(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("version 2 signature did not verify")
	}
}
