package tracker

import (
	"bytes"
	"net"
	"testing"
)

func TestDecodePeersCompact6(t *testing.T) {
	ip := net.ParseIP("2001:db8::1").To16()
	if ip == nil {
		t.Fatal("bad test ip")
	}
	buf := make([]byte, 0, 18)
	buf = append(buf, ip...)
	buf = append(buf, 0x1a, 0xe1) // 6881

	addrs, err := DecodePeersCompact6(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(addrs) != 1 {
		t.Fatalf("want 1 peer, got %d", len(addrs))
	}
	if !addrs[0].IP.Equal(net.ParseIP("2001:db8::1")) {
		t.Errorf("ip = %s, want 2001:db8::1", addrs[0].IP)
	}
	if addrs[0].Port != 6881 {
		t.Errorf("port = %d, want 6881", addrs[0].Port)
	}
}

func TestDecodePeersCompact6InvalidLength(t *testing.T) {
	if _, err := DecodePeersCompact6(make([]byte, 6)); err == nil {
		t.Fatal("want error for a 6-byte (IPv4) list")
	}
	if _, err := DecodePeersCompact(make([]byte, 7)); err == nil {
		t.Fatal("want error for a 7-byte list in the IPv4 decoder")
	}
}

func TestDecodePeersCompactMixed(t *testing.T) {
	v4 := []byte{127, 0, 0, 1, 0x1a, 0xe1}
	addrs, err := DecodePeersCompact(v4)
	if err != nil || len(addrs) != 1 || !addrs[0].IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("v4 decode: %v %v", addrs, err)
	}
	if bytes.Equal(addrs[0].IP.To4(), nil) {
		t.Fatal("expected an IPv4 address")
	}
}
