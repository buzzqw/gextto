package main

import (
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

func TestDHTFeedTarget(t *testing.T) {
	key := strings.Repeat("ab", 32) // 32 bytes = ed25519 public key
	pub, salt, ok := dhtFeedTarget("dht://" + key)
	if !ok || len(pub) != 32 || salt != nil {
		t.Fatalf("dht://<key>: ok=%v len=%d salt=%v", ok, len(pub), salt)
	}
	if !isDHTFeed("dht://" + key) {
		t.Fatal("isDHTFeed must accept a dht:// key")
	}

	_, salt, ok = dhtFeedTarget("dht://" + key + "?salt=0011ff")
	if !ok || string(salt) != "\x00\x11\xff" {
		t.Fatalf("salt: ok=%v salt=%x", ok, salt)
	}

	// A bare "dht:<key>" form is tolerated too.
	if _, _, ok := dhtFeedTarget("dht:" + key); !ok {
		t.Fatal("dht:<key> must be accepted")
	}

	for name, raw := range map[string]string{
		"empty":         "",
		"not dht":       "http://tracker/announce",
		"short key":     "dht://abcd",
		"odd length":    "dht://abc",
		"not hex":       "dht://" + strings.Repeat("zz", 32),
		"salt too long": "dht://" + key + "?salt=" + strings.Repeat("00", 65),
		"bad salt hex":  "dht://" + key + "?salt=zz",
	} {
		if _, _, ok := dhtFeedTarget(raw); ok {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestDHTFeedItemsToRSS(t *testing.T) {
	var a [20]byte
	for i := range a {
		a[i] = byte(i + 1)
	}
	b := a
	b[0] = 0xff

	aHex := "0102030405060708090a0b0c0d0e0f1011121314"
	bHex := "ff" + aHex[2:]

	items := dhtFeedItemsToRSS([]torrent.DHTFeedItem{{InfoHash: a, Name: "release"}, {InfoHash: b}})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Title != "release" || items[0].Magnet != "magnet:?xt=urn:btih:"+aHex {
		t.Fatalf("item 0 = %+v", items[0])
	}
	// A nameless entry falls back to its info hash as the title.
	if items[1].Title != bHex || items[1].Magnet != "magnet:?xt=urn:btih:"+bHex {
		t.Fatalf("item 1 = %+v", items[1])
	}
	if items[0].GUID == items[1].GUID {
		t.Fatal("items must have distinct GUIDs")
	}
}
