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
