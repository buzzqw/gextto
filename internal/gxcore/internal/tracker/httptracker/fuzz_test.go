package httptracker

import (
	"testing"

	"github.com/zeebo/bencode"
)

// FuzzParsePeersDictionary feeds arbitrary bytes to the dictionary form of the
// tracker "peers" field (BEP 3). A tracker is untrusted input: a malformed
// dictionary must return an error, never panic.
func FuzzParsePeersDictionary(f *testing.F) {
	f.Add([]byte("ld2:ip9:127.0.0.14:porti6881eee"))
	f.Add([]byte("le"))
	f.Add([]byte("l"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parsePeersDictionary(bencode.RawMessage(b))
	})
}
