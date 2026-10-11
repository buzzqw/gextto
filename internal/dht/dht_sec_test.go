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

// resetReflected clears the package-level reflected-address state between tests.
func resetReflected() {
	reflectedMu.Lock()
	reflectedPorts = make(map[string]int)
	reflectedOrder = nil
	reflectedMu.Unlock()
	reflectedPort.Store(0)
	reflectedObs.Store(0)
	natMappingType.Store(0)
}

func bep42IPField(t *testing.T, ipStr string, port int) string {
	t.Helper()
	ip := net.ParseIP(ipStr)
	var raw []byte
	if v4 := ip.To4(); v4 != nil {
		raw = append(raw, v4...)
	} else {
		raw = append(raw, ip.To16()...)
	}
	raw = append(raw, byte(port>>8), byte(port))
	return string(raw)
}

// TestParseReflected covers the BEP 42 "ip" field decoding (v4 and v6).
func TestParseReflected(t *testing.T) {
	if _, _, ok := parseReflected(""); ok {
		t.Fatal("empty field must not parse")
	}
	if _, _, ok := parseReflected("short"); ok {
		t.Fatal("short field must not parse")
	}
	ip, port, ok := parseReflected(bep42IPField(t, "1.2.3.4", 6881))
	if !ok || !ip.Equal(net.ParseIP("1.2.3.4")) || port != 6881 {
		t.Fatalf("v4 field = %v %d %v", ip, port, ok)
	}
	ip, port, ok = parseReflected(bep42IPField(t, "2001:db8::1", 51413))
	if !ok || !ip.Equal(net.ParseIP("2001:db8::1")) || port != 51413 {
		t.Fatalf("v6 field = %v %d %v", ip, port, ok)
	}
}

// TestRecordReflectedNATMapping: the port is published from the first observer,
// the mapping stays unknown until two subnets report, then it is cone (same
// port) or symmetric (different ports).
func TestRecordReflectedNATMapping(t *testing.T) {
	resetReflected()
	a := bep42IPField(t, "203.0.113.7", 6881)
	b := bep42IPField(t, "203.0.113.7", 40000)

	recordReflected(a, net.ParseIP("198.51.100.1"))
	if got := reflectedPort.Load(); got != 6881 {
		t.Fatalf("reflected port = %d, want 6881", got)
	}
	if got := natMappingType.Load(); got != 0 {
		t.Fatalf("one observer must not classify the NAT: %d", got)
	}

	// A second observer in another subnet reports the same port: cone.
	recordReflected(a, net.ParseIP("203.0.113.9"))
	if got := natMappingType.Load(); got != 1 {
		t.Fatalf("agreeing observers = %d, want 1 (endpoint-independent)", got)
	}

	// A third observer reports a different port: symmetric.
	recordReflected(b, net.ParseIP("192.0.2.5"))
	if got := natMappingType.Load(); got != 2 {
		t.Fatalf("disagreeing observers = %d, want 2 (address/port-dependent)", got)
	}
	if got := reflectedPort.Load(); got != 6881 {
		t.Fatalf("most-frequent reflected port = %d, want 6881", got)
	}
	if got := reflectedObs.Load(); got != 3 {
		t.Fatalf("observations = %d, want 3", got)
	}
	resetReflected()
}
