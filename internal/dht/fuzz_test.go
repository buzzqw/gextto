package dht

import "testing"

// FuzzDecodeBencode feeds arbitrary bytes to the hand-written BEP 44 bencode
// decoder: hostile nesting and malformed lengths must return an error, never
// panic or exhaust the stack.
func FuzzDecodeBencode(f *testing.F) {
	f.Add([]byte("d1:ai1e1:t2:aa1:ad2:id20:aaaaaaaaaaaaaaaaaaaae"))
	f.Add([]byte("d"))
	f.Add([]byte("li1ei2ee"))
	f.Add([]byte("3:abc"))
	f.Add([]byte("d1:ad1:t2:aaee"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = decodeBencode(b)
		_, _ = bep44RequestDecode(b)
		_ = validBencode(b)
	})
}

// FuzzReadResponse feeds arbitrary bytes to the bencode-go response decoder.
// The wrapper recovers from bencode-go panics, so the whole node must never go
// down on a hostile UDP datagram; this fixes that guard in place.
func FuzzReadResponse(f *testing.F) {
	f.Add([]byte("d1:t2:aa1:y1:re"))
	f.Add([]byte("d1:y1:r1:r1:id20:aaaaaaaaaaaaaaaaaaaae"))
	f.Add([]byte("de"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = readResponse(packetType{b: b}, &nullLogger{})
	})
}
