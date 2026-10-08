package main

import "testing"

func TestParseByteRange(t *testing.T) {
	cases := []struct {
		header  string
		start   int64
		length  int64
		partial bool
		wantErr bool
	}{
		{"", 0, 1000, false, false},
		{"bytes=0-99", 0, 100, true, false},
		{"bytes=100-", 100, 900, true, false},
		{"bytes=-100", 900, 100, true, false},
		{"bytes=990-2000", 990, 10, true, false}, // clamped to the size
		{"bytes=1000-", 0, 0, false, true},       // start past the end
		{"bytes=50-10", 0, 0, false, true},       // end before start
		{"bytes=1-2,4-5", 0, 0, false, true},     // multiple ranges
		{"items=0-1", 0, 0, false, true},         // wrong unit
		{"bytes=abc", 0, 0, false, true},
	}
	for _, tc := range cases {
		start, length, partial, err := parseByteRange(tc.header, 1000)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: expected an error", tc.header)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.header, err)
		}
		if start != tc.start || length != tc.length || partial != tc.partial {
			t.Fatalf("%q = %d/%d partial=%v, want %d/%d partial=%v",
				tc.header, start, length, partial, tc.start, tc.length, tc.partial)
		}
	}
}
