package torznab

import (
	"bytes"
	"testing"
)

// FuzzParse feeds arbitrary bytes to the Torznab XML and Prowlarr JSON parsers.
// Both read remote, untrusted bodies: they must return an error or a result
// list, never panic.
func FuzzParse(f *testing.F) {
	f.Add([]byte(`<?xml version="1.0"?><rss><channel><item><title>t</title></item></channel></rss>`))
	f.Add([]byte(`{"Results":[{"Title":"t","Size":1}]}`))
	f.Add([]byte(`&`))
	f.Add([]byte(`<rss>`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = Parse(bytes.NewReader(b), "fuzz")
		_, _ = ParseProwlarr(bytes.NewReader(b), "fuzz")
	})
}
