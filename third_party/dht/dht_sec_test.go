package dht

import (
	"encoding/hex"
	"net"
	"testing"
)

// TestBEP42Vectors checks the algorithm against the test vectors in BEP 42
// (https://www.bittorrent.org/beps/bep_0042.html).
func TestBEP42Vectors(t *testing.T) {
	cases := []struct {
		ip string
		id string
	}{
		{"124.31.75.21", "5fbfbff10c5d6a4ec8a88e4c6ab4c28b95eee401"},
		{"21.75.31.124", "5a3ce9c14e7a08645677bbd1cfe7d8f956d53256"},
		{"65.23.51.170", "a5d43220bc8f112a3d426c84764f8c2a1150e616"},
		{"84.124.73.14", "1b0321dd1bb1fe518101ceef99462b947a01ff41"},
		{"43.213.53.83", "e56f6cbf5b7c4be0237986d5243b87aa6d51305a"},
	}
	for _, c := range cases {
		raw, err := hex.DecodeString(c.id)
		if err != nil || len(raw) != 20 {
			t.Fatalf("bad vector %q", c.id)
		}
		var id [20]byte
		copy(id[:], raw)
		if !NodeIDSecure(id, net.ParseIP(c.ip)) {
			t.Errorf("NodeIDSecure(%s, %s) = false, want true", c.id, c.ip)
		}
	}
}

// TestSecureNodeIDRoundTrip checks a generated ID validates for its IP.
func TestSecureNodeIDRoundTrip(t *testing.T) {
	for _, ipStr := range []string{"124.31.75.21", "2001:4860:4860::8888"} {
		ip := net.ParseIP(ipStr)
		raw := generateSecureNodeID(ip)
		var id [20]byte
		copy(id[:], raw)
		if !NodeIDSecure(id, ip) {
			t.Errorf("generated ID is not secure for %s", ipStr)
		}
	}
}

// TestNodeIDSecureLocalNetwork: local addresses are exempt from verification.
func TestNodeIDSecureLocalNetwork(t *testing.T) {
	var id [20]byte
	for _, ipStr := range []string{"10.1.2.3", "192.168.0.5", "172.16.0.1", "127.0.0.1", "169.254.1.1"} {
		if !NodeIDSecure(id, net.ParseIP(ipStr)) {
			t.Errorf("%s should be treated as a local network address", ipStr)
		}
	}
}
