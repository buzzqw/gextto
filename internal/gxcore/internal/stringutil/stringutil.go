package stringutil

import (
	"strings"
	"unicode"
)

// Asciify sanitizes s for contexts that only accept 7-bit printable ASCII
// (log lines, file names on limited filesystems): anything outside printable
// ASCII becomes '_'. It works byte-wise, so multi-byte runes collapse to one
// '_' per byte.
func Asciify(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c < 32 || c >= 127 {
			b[i] = '_'
		}
	}
	return string(b)
}

// Printable replaces every non-printable rune with the Unicode replacement
// character, keeping the string safe for terminals and the web UI.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return unicode.ReplacementChar
		}
		return r
	}, s)
}
