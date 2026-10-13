package rss

import (
	"bytes"
	"testing"
)

// FuzzParse feeds arbitrary bytes to the RSS/Atom feed parser. Feeds come from
// the network and may be malformed, hostile or huge; the parser must not panic.
func FuzzParse(f *testing.F) {
	f.Add([]byte(`<?xml version="1.0"?><rss><channel><item><title>t</title><link>http://x</link></item></channel></rss>`))
	f.Add([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	f.Add([]byte(`<`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = Parse(bytes.NewReader(b), "fuzz")
	})
}
