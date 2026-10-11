package dht

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	bencode "github.com/jackpal/bencode-go"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

func bencodeValue(t *testing.T, v interface{}) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := bencode.Marshal(&buf, v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const (
	vecPub = "77ff84905a91936367c01360803104f92432fcd904a43511876df5cdf3e7e548"
)

// TestBEP44Immutable checks the immutable target against the BEP 44 test 3.
func TestBEP44Immutable(t *testing.T) {
	encoded := bencodeValue(t, "Hello World!")
	if string(encoded) != "12:Hello World!" {
		t.Fatalf("encoded = %q", encoded)
	}
	target := ImmutableTarget(encoded)
	if hex.EncodeToString(target[:]) != "e5f96f6f38320f0f33959cb4d3d656452117aadb" {
		t.Fatalf("target = %x", target)
	}
}

// TestBEP44MutableVectors checks the signing buffer, the target and the
// official signature-verification vectors of BEP 44 (test 1, no salt; test 2,
// salt "foobar"). The official vectors give the private key in libtorrent's
// expanded form, so they are validated by verification rather than re-signing.
func TestBEP44MutableVectors(t *testing.T) {
	pub := mustHex(t, vecPub)
	encoded := bencodeValue(t, "Hello World!")

	// Test 1: no salt.
	if got := string(signBuffer(1, encoded, nil)); got != "3:seqi1e1:v12:Hello World!" {
		t.Fatalf("sign buffer = %q", got)
	}
	if mt := MutableTarget(pub, nil); hex.EncodeToString(mt[:]) != "4a533d47ec9c7d95b1ad75f576cffc641853b750" {
		t.Fatalf("target (no salt) = %x", mt)
	}
	sig1 := mustHex(t, "305ac8aeb6c9c151fa120f120ea2cfb923564e11552d06a5d856091e5e853cff"+
		"1260d3f39e4999684aa92eb73ffd136e6f4f3ecbfda0ce53a1608ecd7ae21f01")
	if !VerifyMutable(pub, encoded, nil, sig1, 1) {
		t.Fatal("VerifyMutable rejected the BEP 44 test-1 signature")
	}
	if VerifyMutable(pub, encoded, nil, sig1, 2) {
		t.Fatal("VerifyMutable accepted the signature at the wrong sequence")
	}

	// Test 2: salt "foobar".
	salt := []byte("foobar")
	if got := string(signBuffer(1, encoded, salt)); got != "4:salt6:foobar3:seqi1e1:v12:Hello World!" {
		t.Fatalf("sign buffer with salt = %q", got)
	}
	if mt := MutableTarget(pub, salt); hex.EncodeToString(mt[:]) != "411eba73b6f087ca51a3795d9c8c938d365e32c1" {
		t.Fatalf("target (salt) = %x", mt)
	}
	sig2 := mustHex(t, "6834284b6b24c3204eb2fea824d82f88883a3d95e8b4a21b8c0ded553d17d17d"+
		"df9a8a7104b1258f30bed3787e6cb896fca78c58f8e03b5f18f14951a87d9a08")
	if !VerifyMutable(pub, encoded, salt, sig2, 1) {
		t.Fatal("VerifyMutable rejected the BEP 44 test-2 signature")
	}
	if VerifyMutable(pub, encoded, nil, sig2, 1) {
		t.Fatal("VerifyMutable accepted the salted signature without the salt")
	}
}

// TestBEP44LocalStore checks the local put/get round-trip.
func TestBEP44LocalStore(t *testing.T) {
	d := &DHT{items: newItemStore()}

	target, err := d.PutImmutable("Hello World!")
	if err != nil {
		t.Fatal(err)
	}
	it := d.Get(target)
	if it == nil || it.Mutable() || it.Value != "Hello World!" {
		t.Fatalf("immutable item = %+v", it)
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	salt := []byte("foobar")
	mtarget, err := d.PutMutable(priv, 1, "Hello World!", salt)
	if err != nil {
		t.Fatal(err)
	}
	if want := MutableTarget(pub, salt); mtarget != want {
		t.Fatalf("mutable target = %x, want %x", mtarget, want)
	}
	mit := d.Get(mtarget)
	if mit == nil || !mit.Mutable() || mit.Seq != 1 {
		t.Fatalf("mutable item = %+v", mit)
	}
	// The stored signature verifies against the public key and value.
	encoded := bencodeValue(t, "Hello World!")
	if !VerifyMutable(mit.PublicKey, encoded, salt, mit.Signature, mit.Seq) {
		t.Fatal("stored mutable signature does not verify")
	}
}
