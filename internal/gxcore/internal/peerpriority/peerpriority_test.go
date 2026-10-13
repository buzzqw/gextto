package peerpriority

import (
	"net"
	"testing"
)

// TestCalculateCommutesAndSeparatesIPv6 checks BEP 40 priority is symmetric in
// its arguments and that two different IPv6 peers do not collapse to the same
// priority (they would otherwise replace each other in the address list).
func TestCalculateCommutesAndSeparatesIPv6(t *testing.T) {
	a := &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 1}
	b := &net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 2}
	c := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 3}

	if Calculate(a, b) != Calculate(b, a) {
		t.Fatal("Calculate must be commutative")
	}
	if Calculate(a, c) == Calculate(b, c) {
		t.Fatal("distinct IPv6 peers must not share a priority")
	}
}

// port16 clamps an out-of-range port instead of wrapping it, so a mis-parsed
// value never fabricates a different 16-bit port on the wire.
func TestPort16Clamps(t *testing.T) {
	cases := map[int]uint16{-1: 0, 0: 0, 80: 80, 65535: 65535, 65536: 65535, 1 << 20: 65535}
	for in, want := range cases {
		if got := port16(in); got != want {
			t.Fatalf("port16(%d) = %d, want %d", in, got, want)
		}
	}
}
