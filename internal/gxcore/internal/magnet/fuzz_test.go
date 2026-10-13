package magnet

import "testing"

// FuzzNew feeds arbitrary strings to the magnet parser and, when it succeeds,
// renders the parsed link back: neither parse nor formatting may panic on
// hostile input (huge "so" ranges, malformed hex/base32, bogus URLs).
func FuzzNew(f *testing.F) {
	f.Add("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567")
	f.Add("magnet:?xt=urn:btmh:12201111111111111111111111111111111111111111111111111111111111")
	f.Add("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&so=0,1,3-5&tr=udp%3A%2F%2Ftracker%3A80")
	f.Add("magnet:?xs=urn:btpk:1111111111111111111111111111111111111111")
	f.Add("not a magnet")
	f.Fuzz(func(t *testing.T, s string) {
		m, err := New(s)
		if err != nil {
			return
		}
		_ = m.String()
	})
}
