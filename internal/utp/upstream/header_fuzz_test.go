package upstream

import (
	"testing"
)

// FuzzHeaderUnmarshal feeds arbitrary bytes to the uTP packet header parser
// (fixed 20B header plus the extension chain, including selective ACKs): a
// hostile peer must make it return an error, never panic or slice out of
// range.
func FuzzHeaderUnmarshal(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x10})
	f.Add(make([]byte, 19))
	f.Add(make([]byte, 20))
	// SYN with a selective-ACK extension.
	f.Add([]byte{0x10, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x02, 0x00, 0x02, 0xff, 0x00})
	f.Fuzz(func(t *testing.T, b []byte) {
		var h header
		_, _ = h.Unmarshal(b)
	})
}
