package torrent

import (
	"net"
	"testing"
)

func TestParseDHTPeersFamilies(t *testing.T) {
	v4 := []byte{127, 0, 0, 1, 0x1a, 0xe1} // 127.0.0.1:6881
	v6ip := net.ParseIP("2001:db8::1").To16()
	v6 := append(append([]byte{}, v6ip...), 0x1a, 0xe2) // [2001:db8::1]:6882

	peers := []string{
		string(v4),
		string(v6),
		"abc",                // too short
		string([]byte{1, 2}), // too short
	}
	addrs := parseDHTPeers(peers)
	if len(addrs) != 2 {
		t.Fatalf("got %d addresses, want 2", len(addrs))
	}
	if !addrs[0].IP.Equal(net.IPv4(127, 0, 0, 1)) || addrs[0].Port != 6881 {
		t.Errorf("ipv4 = %s:%d", addrs[0].IP, addrs[0].Port)
	}
	if !addrs[1].IP.Equal(net.ParseIP("2001:db8::1")) || addrs[1].Port != 6882 {
		t.Errorf("ipv6 = %s:%d", addrs[1].IP, addrs[1].Port)
	}
}
