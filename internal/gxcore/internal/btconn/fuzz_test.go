package btconn

import (
	"bytes"
	"testing"
)

// FuzzReadHandshake feeds arbitrary bytes to the peer-wire handshake readers
// (BEP 3 handshake plus the BEP 10 reserved-extension bytes): a hostile peer
// must make them return an error, never panic or slice out of range.
func FuzzReadHandshake(f *testing.F) {
	f.Add(make([]byte, 68))
	f.Add(append([]byte("\x13BitTorrent protocol"), make([]byte, 48)...))
	f.Add([]byte("\x13"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _, _ = readHandshake1(bytes.NewReader(b))
		_, _ = readHandshake2(bytes.NewReader(b))
	})
}
