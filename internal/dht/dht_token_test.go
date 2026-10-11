package dht

import (
	"net"
	"testing"
)

// TestHostTokenBindsScope: a token is bound to both the source address and the
// scope (the info-hash it was issued for), so a token obtained for one swarm
// cannot be replayed on another or from another address.
func TestHostTokenBindsScope(t *testing.T) {
	d := newTestDHT()
	d.tokenSecrets = []string{"secret-current", "secret-previous"}
	addr := net.UDPAddr{IP: net.ParseIP("203.0.113.5"), Port: 6881}
	other := net.UDPAddr{IP: net.ParseIP("203.0.113.6"), Port: 6881}
	ihA := []byte("aaaaaaaaaaaaaaaaaaaa")
	ihB := []byte("bbbbbbbbbbbbbbbbbbbb")

	tok := d.hostToken(addr, ihA, d.tokenSecrets[0])
	if !d.checkToken(addr, ihA, tok) {
		t.Fatal("token rejected for the query it was issued for")
	}
	if d.checkToken(addr, ihB, tok) {
		t.Fatal("token accepted for a different info-hash (mismatched query)")
	}
	if d.checkToken(other, ihA, tok) {
		t.Fatal("token accepted for a different source address")
	}
	// The previous secret still validates a token issued before the rotation.
	prev := d.hostToken(addr, ihA, d.tokenSecrets[1])
	if !d.checkToken(addr, ihA, prev) {
		t.Fatal("token issued with the previous secret was rejected")
	}
}
