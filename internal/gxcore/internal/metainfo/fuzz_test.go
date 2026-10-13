package metainfo

import (
	"bytes"
	"testing"
)

// FuzzNewV2Info feeds arbitrary bytes to the v2 info parser: it must never
// panic (no slice out of range, no division by zero), only return an error or a
// parsed info dictionary.
func FuzzNewV2Info(f *testing.F) {
	f.Add([]byte("d4:infod4:name1:x12:meta versioni2e11:piece lengthi16384eee"))
	f.Add([]byte("d12:meta versioni2e11:piece lengthi16384e9:file treede"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = NewV2Info(b)
	})
}

// FuzzMetaInfoNew feeds arbitrary bytes to the whole .torrent parser (v1 and
// v2 paths).
func FuzzMetaInfoNew(f *testing.F) {
	f.Add([]byte("d4:infod4:name1:x11:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaae12:meta versioni2eee"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = New(bytes.NewReader(b))
	})
}

// FuzzParseFileTree feeds arbitrary bytes to the BEP 52 "file tree" parser.
func FuzzParseFileTree(f *testing.F) {
	f.Add([]byte("d4:name1:x4:lengthi0ee"))
	f.Add([]byte("d0:d4:name1:x4:lengthi1eeee"))
	f.Add([]byte("de"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseFileTree(b)
	})
}

// FuzzParseInfo feeds arbitrary bytes to the info-dictionary parser, including
// the v2 and piece-layer paths.
func FuzzParseInfo(f *testing.F) {
	f.Add([]byte("d4:name1:x11:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaae"))
	f.Add([]byte("d4:name1:x4:lengthi0ee"))
	f.Add([]byte("de"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseInfo(b, true, true, nil)
		_, _ = DecodePieceLayers(b)
	})
}
