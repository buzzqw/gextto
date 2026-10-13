package transport

import (
	"net/netip"
	"testing"
)

// TestNewConnIDAvoidsCollisions: newConnID must never return an id already in
// use, nor one adjacent to an existing connection's id (the remote would use it
// as its send_id). So every chosen id differs by at least 2 from the others.
func TestNewConnIDAvoidsCollisions(t *testing.T) {
	s := &Socket{conns: map[connKey]*Conn{}}
	addr := netip.MustParseAddrPort("198.51.100.9:51413")
	var chosen []uint16
	for i := 0; i < 500; i++ {
		id := s.newConnID(addr)
		for _, p := range chosen {
			if id == p || id == p+1 || id == p-1 {
				t.Fatalf("id %d collides with an existing neighbour %d", id, p)
			}
		}
		if !s.registerConn(id, addr, &Conn{}) {
			t.Fatalf("registerConn refused the id %d it just produced", id)
		}
		chosen = append(chosen, id)
	}
}
