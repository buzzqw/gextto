package amule

import "testing"

func TestSearchListForeign(t *testing.T) {
	results := func(names ...string) []SearchResult {
		out := make([]SearchResult, 0, len(names))
		for _, name := range names {
			out = append(out, SearchResult{Name: name})
		}
		return out
	}
	cases := []struct {
		query string
		names []SearchResult
		want  bool
	}{
		{"the pitt", results("The.Pitt.S01E01.mkv"), false},
		{"the pitt", results("Unrelated.S01E01.mkv"), true},
		// One matching file is enough.
		{"the pitt", results("Unrelated.S01E01.mkv", "the_pitt_s01e02.avi"), false},
		// Accents and separators are folded on both sides.
		{"città", results("Citta.Di.Notte.avi"), false},
		// A query without words of three letters is never judged.
		{"ab 12", results("Unrelated.mkv"), false},
		{"the pitt", nil, false},
	}
	for _, tc := range cases {
		if got := searchListForeign(tc.query, tc.names); got != tc.want {
			t.Fatalf("searchListForeign(%q, %v) = %v, want %v", tc.query, tc.names, got, tc.want)
		}
	}
}
