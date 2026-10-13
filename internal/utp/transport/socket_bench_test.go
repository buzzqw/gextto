package transport

import (
	"net/netip"
	"testing"
)

// BenchmarkNewConnID guards against reintroducing a per-dial allocation: the
// reference built a 64K-entry table (512 KiB) on every call. It must stay
// allocation-free.
func BenchmarkNewConnID(b *testing.B) {
	s := &Socket{conns: map[connKey]*Conn{}}
	addr := netip.MustParseAddrPort("198.51.100.9:51413")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = s.newConnID(addr)
	}
}
