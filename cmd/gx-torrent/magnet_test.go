package main

import (
	"net/url"
	"testing"
)

func TestMagnetInfoHash(t *testing.T) {
	hexHash := "0123456789abcdef0123456789abcdef01234567"
	if got, ok := magnetInfoHash("magnet:?xt=urn:btih:" + hexHash + "&dn=x"); !ok || got != hexHash {
		t.Fatalf("hex magnet: %q %v", got, ok)
	}
	if got, ok := magnetInfoHash("magnet:?xt=urn:btih:AERUKZ4JVPG66AJDIVTYTK6N54ASGRLH"); !ok || got != hexHash {
		t.Fatalf("base32 magnet: %q %v", got, ok)
	}
	if _, ok := magnetInfoHash("http://example.org/a.torrent"); ok {
		t.Fatal("not a magnet")
	}
}

func TestV2OnlyDetection(t *testing.T) {
	if !magnetIsV2Only("magnet:?xt=urn:btmh:1220abcd") {
		t.Fatal("btmh-only magnet must be v2-only")
	}
	if magnetIsV2Only("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&xt=urn:btmh:1220abcd") {
		t.Fatal("hybrid magnet is supported")
	}
	if !torrentIsV2Only([]byte("d4:infod9:file treed...e12:meta versioni2e4:name1:xee")) {
		t.Fatal("v2-only torrent not detected")
	}
	if torrentIsV2Only([]byte("d4:infod12:meta versioni2e6:pieces20:....................ee")) {
		t.Fatal("hybrid torrent is supported")
	}
}

func TestNormalizeMagnet(t *testing.T) {
	hexHash := "0123456789abcdef0123456789abcdef01234567"
	// btmh listed first: the engine reads only the first xt, so btih must move in
	// front and the btmh must be dropped.
	hybrid := "magnet:?xt=urn:btmh:1220aabb&xt=urn:btih:" + hexHash + "&dn=Title&tr=http%3A%2F%2Ft.example"
	got := normalizeMagnet(hybrid)
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("normalized magnet does not parse: %v", err)
	}
	xts := parsed.Query()["xt"]
	if len(xts) != 1 || xts[0] != "urn:btih:"+hexHash {
		t.Fatalf("xt must be exactly the v1 hash first, got %v", xts)
	}
	if parsed.Query().Get("dn") != "Title" || len(parsed.Query()["tr"]) != 1 {
		t.Fatalf("dn/tr must be preserved, got %q", got)
	}
	// A plain magnet is left usable (the v1 hash stays first).
	if h, ok := magnetInfoHash(normalizeMagnet("magnet:?xt=urn:btih:" + hexHash)); !ok || h != hexHash {
		t.Fatalf("plain magnet broken: %q %v", h, ok)
	}
}
