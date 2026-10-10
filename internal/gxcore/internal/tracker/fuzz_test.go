package tracker

import "testing"

// FuzzDecodePeersCompact feeds arbitrary bytes to the compact peer decoders
// (IPv4 and IPv6): they must never panic.
func FuzzDecodePeersCompact(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 6))
	f.Add(make([]byte, 18))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = DecodePeersCompact(b)
		_, _ = DecodePeersCompact6(b)
	})
}
