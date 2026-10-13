package peerprotocol

import "testing"

// FuzzExtensionMessageUnmarshal feeds arbitrary bytes to the BEP 10 extension
// message decoder. The metadata payload is sliced with BytesParsed(), which a
// hostile peer controls: a bad bencode length must not slice out of range.
func FuzzExtensionMessageUnmarshal(f *testing.F) {
	f.Add([]byte{0})
	f.Add([]byte{1})
	f.Add([]byte{1, 'd', '1', ':', 'm', 'd', 'e', 'e'})
	f.Add([]byte{2})
	f.Add([]byte{3})
	f.Add([]byte{4})
	f.Fuzz(func(t *testing.T, b []byte) {
		var m ExtensionMessage
		_ = m.UnmarshalBinary(b)
	})
}
