package bencodecheck

import (
	"strings"
	"testing"
)

// TestValidate exercises the guard: it accepts the decoder's grammar, and
// refuses declared lengths beyond the input and over-deep nesting.
func TestValidate(t *testing.T) {
	ok := []string{
		"de",
		"i42e",
		"4:spam",
		"0:",
		"l4:spami42ee",
		"d3:bar4:spam3:fooi42ee",
		"d3:fooi1eee", // trailing "e" ignored, like the decoder
		"d3:zzz" + strings.Repeat("l", 64) + strings.Repeat("e", 64) + "e", // nested but shallow
	}
	for _, in := range ok {
		if err := Validate([]byte(in)); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", in, err)
		}
	}
	bad := []string{
		"",            // empty
		"5:abc",       // length beyond input
		"2000000000:", // huge length
		"12000000000:",
		"d4:info2000000000:",
		"x",       // not bencode
		"i42",     // unterminated integer
		"l4:spam", // unterminated list
	}
	for _, in := range bad {
		if err := Validate([]byte(in)); err == nil {
			t.Errorf("Validate(%q) = nil, want error", in)
		}
	}
	// Nesting deeper than the limit is refused rather than recursed into.
	deep := "d3:zzz" + strings.Repeat("l", MaxDepth+2) + strings.Repeat("e", MaxDepth+2) + "e"
	if err := Validate([]byte(deep)); err == nil {
		t.Error("expected an error for nesting beyond MaxDepth")
	}
}
