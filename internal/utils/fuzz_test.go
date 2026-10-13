package utils

import "testing"

// FuzzTorrentBytes feeds arbitrary bytes to the hand-written bencode span
// scanner used to extract the info hash and the name: malformed or truncated
// input must not slice out of range.
func FuzzTorrentBytes(f *testing.F) {
	f.Add([]byte("d4:infod4:name1:x6:pieces0:ee"))
	f.Add([]byte("d4:infod"))
	f.Add([]byte("d4:info4:name"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = TorrentInfoHash(b)
		_, _ = TorrentName(b)
	})
}

// FuzzMagnetParsing feeds arbitrary strings to the magnet hash helpers.
func FuzzMagnetParsing(f *testing.F) {
	f.Add("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567")
	f.Add("magnet:?xt=urn:btih:zzzz")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = MagnetHash(s)
		_, _ = ParsedMagnetHash(s)
		_, _ = ParseDateAny(s)
		_ = ParseSizeMB(s)
	})
}
